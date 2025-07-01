# Basic MCP Server Example

This example demonstrates how to implement a basic [Model Context Protocol (MCP)](https://github.com/mark3labs/mcp-go) server in Go, supporting multiple transport modes (**stdio** and **HTTP**) and using the [Gin](https://github.com/gin-gonic/gin) web framework for HTTP routing.

## Overview

The [`server.go`](server.go) file provides a minimal yet extensible MCP server that:

- Registers a common tool (see `operation.RegisterCommonTool`)
- Supports two transport types: **stdio** and **HTTP**
- Uses Gin for HTTP routing and middleware
- Demonstrates best practices for server setup, logging, and error handling

## Directory Structure

```bash
01-basic-mcp/
├── server.go
└── README.md   ← (this file)
```

## Key Components

### MCPServer Struct

Encapsulates the MCP server instance and provides a method to expose it as an HTTP handler.

```go
type MCPServer struct {
    server *server.MCPServer
}
```

### Server Initialization

The `NewMCPServer` function creates and configures the MCP server, enabling tool capabilities, logging, and recovery middleware. It also registers a common tool for demonstration.

```go
func NewMCPServer() *MCPServer
```

### Transport Modes

The server can be started in one of two modes, controlled by the `-transport` flag:

- **stdio**: Communicates via standard input/output (for CLI or local integration)
- **http**: Runs as a standard HTTP server using Gin

### Command-Line Flags

| Flag         | Default  | Description                          |
| ------------ | -------- | ------------------------------------ |
| `-addr`      | `:8080`  | Address to listen on (for HTTP mode) |
| `-transport` | `stdio`  | Transport type: `stdio` or `http`    |
| `-t`         | _(none)_ | Alias for `-transport`               |

## Usage

### Prerequisites

- Go 1.18 or later
- Required dependencies (see [`go.mod`](../go.mod))

### Running the Server

1. **Clone the repository** and navigate to the `01-basic-mcp` directory.

2. **Build and run** the server:

```sh
go run server.go -transport http -addr :8080
```

#### Example: Start with stdio transport

```sh
go run server.go
```

#### Example: Start with HTTP transport

```sh
go run server.go -transport http -addr :8080
```

### HTTP Endpoints (when using `-transport http`)

- **POST /mcp**
- **GET /mcp**
- **DELETE /mcp**

All handled by the MCP server via Gin.

## Code Structure

- **Server Initialization**: Sets up the MCP server with middleware and registers tools.
- **Transport Selection**: Chooses the transport based on the `-transport` flag.
- **HTTP Routing**: Uses Gin to route `/mcp` requests to the MCP server handler.

## Example: Minimal Main Function

```go
func main() {
    // Parse flags
    // Create MCPServer
    // Switch on transport type:
    //   - stdio: server.ServeStdio
    //   - http:  Gin router with /mcp endpoint
}
```

## Extending the Example

- **Register additional tools** by calling their registration functions with the MCP server instance.
- **Add custom middleware** to the Gin router for authentication, logging, etc.
- **Integrate with other transports** as needed.

## References

- [mark3labs/mcp-go](https://github.com/mark3labs/mcp-go) — MCP Go SDK
- [gin-gonic/gin](https://github.com/gin-gonic/gin) — Gin web framework

## License

This example is provided under the [MIT License](../LICENSE).
