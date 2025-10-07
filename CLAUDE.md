# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is the MCP Workshop repository for learning Model Context Protocol (MCP) development with Go. The project demonstrates building MCP servers and clients across 5 progressive modules, from basic implementations to advanced features like OAuth, observability, and proxy servers.

## Architecture

The codebase follows a modular structure with shared packages and separate example modules:

### Core Architecture

- **pkg/core/**: Context management and OAuth storage interfaces
  - `core.go`: Context helpers for request IDs (via `uuid`) and auth token extraction from HTTP headers or environment
  - `store.go`: Store interface for OAuth authorization codes and client management
- **pkg/operation/**: Tool registration system categorizing tools as read/write operations
  - `operation.go`: `RegisterCommonTool()` and `RegisterAuthTool()` functions
  - `echo/`, `caculator/`, `token/`: Individual tool implementations
- **pkg/logger/**: Structured logging with slog
- **pkg/store/**: Store implementations for OAuth data with factory pattern
  - `memory.go`: In-memory store implementation with thread-safe operations
  - `redis.go`: Redis-backed persistent store using rueidis client
  - `factory.go`: Factory pattern for creating store instances (memory or redis)
- **Module structure**: Each numbered directory (01-05) contains a complete working example

### Key Components

- **MCPServer wrapper**: Wraps `github.com/mark3labs/mcp-go/server.MCPServer` with Gin HTTP integration
  - `ServeHTTP()`: Returns `StreamableHTTPServer` with 30s heartbeat and context injection
  - `ServeStdio()`: Stdio transport with context injection via `server.ServeStdio()`
- **Tool registration**: Tools categorized as read/write operations via `operation.Tool` struct
  - `RegisterRead()` / `RegisterWrite()` methods append to internal slices
  - `Tools()` returns combined slice for batch registration with `s.AddTools()`
- **Transport support**: Both stdio and HTTP with unified auth via Go context
  - HTTP: Extracts `Authorization` header via `core.AuthFromRequest()`
  - Stdio: Extracts `API_KEY` env var via `core.AuthFromEnv()`
- **Context propagation**: `RequestIDKey` and `AuthKey` custom types for type-safe context values
  - `core.WithRequestID()`: Generates UUID request ID
  - `core.TokenFromContext()`: Retrieves auth token from context
  - `core.LoggerFromCtx()`: Returns logger with request_id field

## Development Commands

### Building All Binaries

The Makefile automates building all server binaries:

```bash
# Build all binaries to bin/ directory
make

# Clean all built binaries
make clean
```

### Running Individual Modules

Each module can be run independently:

```bash
# Basic MCP server (stdio mode)
go run 01-basic-mcp/server.go

# HTTP mode with custom address
go run 01-basic-mcp/server.go -transport http -addr :8080

# Token passthrough example
go run 02-basic-token-passthrough/server.go -transport http

# OAuth MCP server with memory store (default)
go run 03-oauth-mcp/oauth-server/server.go -client_id=<id> -client_secret=<secret> -addr :8095

# OAuth MCP server with Redis store
go run 03-oauth-mcp/oauth-server/server.go -client_id=<id> -client_secret=<secret> -addr :8095 -store redis -redis-addr localhost:6379

# OAuth client example
go run 03-oauth-mcp/oauth-client/main.go

# Observability server
go run 04-observability/server.go -transport http
```

### Common Server Flags

Most servers support these flags:

- `-transport` or `-t`: Transport type (`stdio` or `http`)
- `-addr`: Address to listen on (varies by module: `:8080` for basic, `:8095` for OAuth)
- `-log-level`: Log level (DEBUG, INFO, WARN, ERROR) - defaults to DEBUG in dev, INFO in production

OAuth server additional flags:

- `-client_id`: OAuth 2.0 Client ID (required)
- `-client_secret`: OAuth 2.0 Client Secret (required)
- `-provider`: OAuth provider (github, gitea, gitlab)
- `-gitea-host`: Gitea host URL (default: `https://gitea.com`)
- `-gitlab-host`: GitLab host URL (default: `https://gitlab.com`)
- `-store`: Store type (`memory` or `redis`) - defaults to `memory`
- `-redis-addr`: Redis address (default: `localhost:6379`) - only used when `-store=redis`
- `-redis-password`: Redis password - only used when `-store=redis`
- `-redis-db`: Redis database number (default: 0) - only used when `-store=redis`

### Standard Go Commands

```bash
# Install dependencies
go mod tidy

# Run tests
go test ./...

# Format code
go fmt ./...

# Vet code
go vet ./...
```

## MCP Configuration

The repository includes `mcp.json` in the root for MCP client integration:

```json
{
  "mcpServers": {
    "default-stdio-server": {
      "type": "stdio",
      "command": "mcp-server",
      "args": ["-t", "stdio"]
    },
    "default-http-server": {
      "type": "http",
      "url": "http://localhost:8080/mcp",
      "headers": {
        "Authorization": "xxxxxx"
      }
    }
  }
}
```

## Module Progression

1. **01-basic-mcp**: Foundation - stdio/HTTP transports, tool registration, Gin router setup
2. **02-basic-token-passthrough**: Context injection for auth tokens from HTTP headers or environment
3. **03-oauth-mcp**: OAuth 2.0 authorization server with PKCE, token endpoints, and client/auth code storage
   - `oauth-server/`: Full OAuth provider with `/authorize`, `/token`, `/resource_metadata` endpoints
   - `oauth-client/`: Example OAuth client demonstrating the authorization flow
4. **04-observability**: OpenTelemetry distributed tracing and structured logging with request IDs
5. **05-mcp-proxy**: Proxy server aggregating multiple MCP servers with SSE streaming (config in `config.json`)

## Key Dependencies

- `github.com/mark3labs/mcp-go`: Core MCP protocol implementation
  - **Note**: Replaced via `go.mod` with `github.com/appleboy/mcp-go` fork
- `github.com/gin-gonic/gin`: HTTP router and middleware
- `github.com/google/uuid`: Request ID generation
- `golang.org/x/oauth2`: OAuth 2.0 client and PKCE challenge handling
- `go.opentelemetry.io/otel`: OpenTelemetry tracing SDK
- `github.com/appleboy/graceful`: Graceful HTTP server shutdown

## Working with Tools

Tools are registered through the `pkg/operation` package in two categories:

**Common Tools** (via `RegisterCommonTool()`):

- `echo_message`: Echoes back the provided message (read operation)
- `add_numbers`: Adds two numbers and returns the sum (write operation)

**Auth Tools** (via `RegisterAuthTool()`):

- `make_authenticated_request`: Makes HTTP request using auth token from context (read operation)
- `show_auth_token`: Displays the current auth token from context (read operation)

Tool registration pattern:

```go
tool := &operation.Tool{}
tool.RegisterRead(server.ServerTool{Tool: myTool, Handler: myHandler})
tool.RegisterWrite(server.ServerTool{Tool: myTool, Handler: myHandler})
s.AddTools(tool.Tools()...)
```

## Important Implementation Details

- **Graceful shutdown**: All HTTP servers implement signal handling for `SIGINT` and `SIGTERM` with 10-second timeout
- **Build tags**: Servers use `//go:build !windows` to exclude Windows builds
- **HTTP timeouts**: Servers configure 10s read/write timeouts and 60s idle timeout
- **MCP endpoint**: All HTTP servers expose MCP protocol at `/mcp` path with POST, GET, DELETE methods
- **OAuth flow**: Module 03 implements full authorization code flow with PKCE, requiring external OAuth provider credentials
