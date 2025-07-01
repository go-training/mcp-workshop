# MCP Server with Observability Example

This example demonstrates an MCP (Model Context Protocol) server implementation in Go, focusing on **observability** and **authentication token propagation**. The server supports both HTTP and stdio transports, and integrates structured logging and OpenTelemetry for enhanced traceability.

## Features

- **MCP Protocol Support**: Implements an MCP server with tool registration and context propagation.
- **Observability**: Integrates OpenTelemetry and structured logging (`slog`) for tracing, metrics, and error reporting.
- **Authentication Token Propagation**: Passes authentication tokens through context, supporting both HTTP headers and environment variables.
- **Middleware Architecture**: Uses middleware to inject observability attributes and error details into traces or logs.
- **Flexible Transport**: Can run as a long-running HTTP server (with Gin) or as a stdio-based process for headless/scripted use.

---

## File Structure

```bash
04-observability/
├── server.go    # Main server implementation with observability features
```

---

## Getting Started

### Prerequisites

- Go 1.24+ (recommended)
- MCP Go SDK (`github.com/mark3labs/mcp-go`)
- Gin, OpenTelemetry, and related dependencies (see `go.mod`)

### Running the Server

You can run the server in two modes: **stdio** (default) or **http**.

#### 1. Stdio Mode (Default)

This mode is suitable for headless or local script integration.

```bash
go run server.go
# or explicitly specify stdio
go run server.go --transport stdio
```

- The authentication token is read from the environment and injected into each request context.

#### 2. HTTP Mode

This mode starts a persistent HTTP server using Gin.

```bash
go run server.go --transport http --addr :8080
```

- The server listens on the specified address (default `:8080`).
- Authentication tokens are extracted from HTTP requests and injected into the context.
- Supports POST, GET, and DELETE on the `/mcp` endpoint.

##### Example HTTP Request

```bash
curl -X POST http://localhost:8080/mcp -H "Authorization: Bearer <token>" -d '{"tool": "make_authenticated_request", ...}'
```

---

## Architecture Overview

### MCPServer

The `MCPServer` struct wraps the underlying MCP server instance and provides methods to serve via HTTP or stdio.

- **Tool Registration**: Registers tools such as `make_authenticated_request` and `show_auth_token` via the `operation` package.
- **Middleware**: Adds logging, recovery, and a custom tool handler middleware for observability.

### Middleware for Observability

The custom middleware (`MCPToolHandlerMiddleware`) records:

- Tool name and parameters
- Execution status (success/error)
- Error messages (if any)
- Wall-clock duration (ms)
- All attributes are injected into the current OpenTelemetry trace span, or logged if tracing is not active.

#### Key Functions

- `AddRequestAttributes(ctx, attrs...)`: Adds OpenTelemetry attributes to the current span, or logs them if no span is active.
- `composeLogAttrs(span, attrs...)`: Converts attributes and span context into structured log fields.
- `extractStatusAndError(res, err)`: Extracts status and error messages for observability.

### HTTP Server with Gin

- Uses Gin for HTTP routing and recovery.
- Integrates `slog` for structured logging.
- Ensures context propagation from Gin to the MCP handler.

### Graceful Shutdown

- Uses `github.com/appleboy/graceful` for managing server lifecycle and graceful shutdowns.

---

## Observability & Logging

- **Tracing**: All tool invocations are traced with OpenTelemetry attributes.
- **Fallback Logging**: If no trace span is active, attributes are logged with `slog` for consistent observability.
- **Structured Logs**: All logs include trace and span IDs (or "none" if unavailable), tool names, parameters, status, errors, and durations.

---

## Example: Adding Observability to a Tool Handler

```go
func MCPToolHandlerMiddleware() server.ToolHandlerMiddleware {
  return func(next server.ToolHandlerFunc) server.ToolHandlerFunc {
    return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
      start := time.Now()
      AddRequestAttributes(
        ctx,
        attribute.String("mcp.tool", req.Params.Name),
        attribute.String("mcp.params", fmt.Sprintf("%+v", req.Params)),
      )
      res, err := next(ctx, req)
      durationMs := float64(time.Since(start).Microseconds()) / 1000.0
      status, errMsg := extractStatusAndError(res, err)
      attrs := []attribute.KeyValue{
        attribute.String("mcp.status", status),
        attribute.Float64("mcp.duration_ms", durationMs),
      }
      if errMsg != "" {
        attrs = append(attrs, attribute.String("mcp.error", errMsg))
      }
      AddRequestAttributes(ctx, attrs...)
      return res, err
    }
  }
}
```

---

## Command-Line Flags

| Flag                | Description                           | Default |
| ------------------- | ------------------------------------- | ------- |
| `--transport`, `-t` | Transport type: `stdio` or `http`     | `stdio` |
| `--addr`            | Address to listen on (HTTP mode only) | `:8080` |

---

## References

- [MCP Go SDK](https://github.com/mark3labs/mcp-go)
- [OpenTelemetry for Go](https://opentelemetry.io/docs/instrumentation/go/)
- [Gin Web Framework](https://gin-gonic.com/)
- [Go slog Logging](https://pkg.go.dev/log/slog)

---

## License

This example is part of the [go-training/mcp-workshop](https://github.com/go-training/mcp-workshop) and is licensed under the MIT License.
