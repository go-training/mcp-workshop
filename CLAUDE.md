# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is the MCP Workshop repository for learning Model Context Protocol (MCP) development with Go. The project demonstrates building MCP servers and clients across 5 progressive modules, from basic implementations to advanced features like OAuth, observability, and proxy servers.

## Architecture

The codebase follows a modular structure with shared packages and separate example modules:

### Core Architecture

- **pkg/core/**: Context management utilities for request IDs and authentication tokens
- **pkg/operation/**: Tool registration system with read/write operations for MCP tools
- **pkg/logger/**: Structured logging utilities
- **Module structure**: Each numbered directory (01-05) contains a complete working example

### Key Components

- **MCPServer**: Wrapper around mark3labs/mcp-go server with Gin HTTP integration
- **Tool System**: Categorizes operations as read/write with centralized registration
- **Transport Support**: Both stdio and HTTP transports with unified authentication
- **Context Propagation**: Request IDs and auth tokens passed through Go context

## Development Commands

### Running Individual Modules

Each module can be run independently:

```bash
# Basic MCP server (stdio mode)
go run 01-basic-mcp/server.go

# HTTP mode with custom address
go run 01-basic-mcp/server.go -transport http -addr :8080

# Token passthrough example
go run 02-basic-token-passthrough/server.go -t http

# OAuth MCP server
go run 03-oauth-mcp/server/server.go -t http

# Observability server
go run 04-observability/server.go -t http
```

### Server Options

All servers support these flags:

- `-transport` or `-t`: Transport type (`stdio` or `http`)
- `-addr`: Address to listen on (default `:8080` for HTTP mode)

### Building the MCP Server Binary

The project includes a pre-built `mcp-server` binary. To rebuild:

```bash
# Build for current platform
go build -o mcp-server [module]/server.go
```

### Standard Go Commands

```bash
# Install dependencies
go mod tidy

# Run tests (if any exist)
go test ./...

# Format code
go fmt ./...

# Vet code
go vet ./...

# Build all modules
go build ./...
```

## MCP Configuration

The repository includes `mcp.json` for VS Code MCP integration:

- **stdio server**: Uses the `mcp-server` binary with stdio transport
- **HTTP server**: Connects to localhost:8080 with Authorization header

## Module Progression

1. **01-basic-mcp**: Foundation - stdio/HTTP transports, tool registration
2. **02-basic-token-passthrough**: Authentication context propagation
3. **03-oauth-mcp**: OAuth 2.0 flow with protected resources
4. **04-observability**: OpenTelemetry tracing and structured logging
5. **05-mcp-proxy**: Multi-server aggregation with SSE streaming

## Key Dependencies

- `github.com/mark3labs/mcp-go`: Core MCP protocol implementation (forked version)
- `github.com/gin-gonic/gin`: HTTP router and middleware
- `github.com/google/uuid`: Request ID generation
- `go.opentelemetry.io/otel`: Observability and tracing
- `github.com/appleboy/graceful`: Graceful shutdown handling

## Working with Tools

Tools are registered through the operation package:

- `RegisterCommonTool()`: Echo and calculator tools (no auth required)
- `RegisterAuthTool()`: Authenticated request and token display tools

Tools are categorized as read or write operations and registered in batches for better organization.
