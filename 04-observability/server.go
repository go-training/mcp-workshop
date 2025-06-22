// Package main demonstrates an MCP server that passes authentication tokens
// through context, supporting both HTTP and stdio transports.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/appleboy/graceful"
	"github.com/go-training/mcp-workshop/pkg/logger"
	"github.com/go-training/mcp-workshop/pkg/operation"

	"github.com/gin-gonic/gin"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// MCPServer wraps the underlying MCP server instance.
type MCPServer struct {
	server *server.MCPServer
}

/*
NewMCPServer creates and configures a new instance of MCPServer.

It registers available tools (such as make_authenticated_request and show_auth_token)
with the underlying MCP server, configures logging, recovery, and tool handler middleware
for observability and error handling purposes.

Returns:
  - Pointer to a fully initialized MCPServer, ready to serve via HTTP or stdio transports.
*/
func NewMCPServer() *MCPServer {
	mcpServer := server.NewMCPServer(
		"mcp-server-observability",
		"1.0.0",
		server.WithToolCapabilities(true),
		server.WithLogging(),
		server.WithRecovery(),
		server.WithToolHandlerMiddleware(MCPToolHandlerMiddleware()),
	)

	// Register Tool
	operation.RegisterTool(mcpServer)

	return &MCPServer{
		server: mcpServer,
	}
}

/*
ServeHTTP constructs and returns a streamable HTTP server utilizing the underlying MCP server.

The returned HTTP server is configured to inject authentication tokens from HTTP requests
into the context, and it is suitable for long-running, persistent client-server communication
according to the MCP protocol.

Returns:
  - A pointer to a StreamableHTTPServer instance that can be used with an HTTP handler (e.g., Gin).
*/
func (s *MCPServer) ServeHTTP() *server.StreamableHTTPServer {
	return server.NewStreamableHTTPServer(s.server,
		server.WithHeartbeatInterval(30*time.Second),
	)
}

/*
ServeStdio starts the MCP server using standard input/output (stdio) as its transport.

The authentication token is extracted from the environment and injected into the
request context for each call, allowing headless or locally-scripted interactions.

Returns:
  - An error if the server fails to start or encounters any issue, nil on clean exit.
*/
func (s *MCPServer) ServeStdio() error {
	return server.ServeStdio(s.server)
}

/*
main is the application entry point.

It initializes logging, parses command-line flags for transport type and address,
then starts the MCP server accordingly—either over stdio or HTTP, using Gin as the HTTP router.

Exits with a non-zero status on failure.
*/
func main() {
	logger.New()
	var transport string
	var addr string
	flag.StringVar(&addr, "addr", ":8080", "address to listen on")
	// Support both short (-t) and long (--transport) flags for convenience.
	flag.StringVar(&transport, "t", "stdio", "Transport type (stdio or http) (alias for --transport)")
	flag.StringVar(&transport, "transport", "stdio", "Transport type (stdio or http)")
	flag.Parse()

	mcpServer := NewMCPServer()

	switch transport {
	case "stdio":
		if err := mcpServer.ServeStdio(); err != nil {
			slog.Error("Server error", "error", err)
			os.Exit(1)
		}
	case "http":
		m := graceful.NewManager()
		// If transport is http, continue to set up the HTTP server
		// This will be handled below with Gin
		// Create a Gin router
		router := gin.New()
		router.Use(gin.Recovery()) // Use Gin's recovery middleware

		// Handler to ensure context propagation from Gin to MCP server handler
		withGinContext := func(h http.Handler) gin.HandlerFunc {
			return func(c *gin.Context) {
				// Pass Gin's context to the underlying handler
				req := c.Request.WithContext(c.Request.Context())
				h.ServeHTTP(c.Writer, req)
			}
		}

		// Register POST, GET, DELETE methods for the /mcp path, all handled by MCPServer
		for _, method := range []string{http.MethodPost, http.MethodGet, http.MethodDelete} {
			router.Handle(method, "/mcp", withGinContext(mcpServer.ServeHTTP()))
		}

		// Output server startup message
		srv := &http.Server{
			Addr:         addr,
			Handler:      router,
			ReadTimeout:  10 * time.Second, // 10 seconds
			WriteTimeout: 10 * time.Second, // 10 seconds
			IdleTimeout:  60 * time.Second, // 60 seconds
		}
		m.AddRunningJob(func(ctx context.Context) error {
			// Output server startup message
			slog.Info("Dynamic HTTP server listening", "addr", addr)
			return srv.ListenAndServe()
		})
		m.AddShutdownJob(func() error {
			slog.Info("Shutting down HTTP server gracefully")
			// Create a context with a timeout for the shutdown process
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			return srv.Shutdown(ctx)
		})
		<-m.Done()
	default:
		slog.Error("Invalid transport type", "transport", transport)
		os.Exit(1)
	}
}

/*
AddRequestAttributes sets OpenTelemetry attributes on the current trace span for enhanced observability.

If there is no active trace span in the provided context or the span is not recording,
the attributes (along with trace IDs or an explicit "none" fallback) are logged using slog instead.
This ensures that observability metadata is always reported, even outside traced execution.

Parameters:
  - ctx: The context tied to the trace span or logger.
  - attrs: One or more OpenTelemetry attribute.KeyValue pairs to record or log.
*/
func AddRequestAttributes(ctx context.Context, attrs ...attribute.KeyValue) {
	span := trace.SpanFromContext(ctx)
	if span == nil || !span.IsRecording() {
		slog.LogAttrs(ctx, slog.LevelInfo, "observability.fallback",
			composeLogAttrs(span, attrs...)...,
		)
		return
	}
	span.SetAttributes(attrs...)
}

/*
composeLogAttrs converts OpenTelemetry attributes and optional span context
to a slice of slog.Attr for structured logging.

Each provided attribute is included, along with a fallback flag and the
trace and span IDs (or "none" if unavailable).

Parameters:
  - span: The trace.Span whose context (trace/span IDs) will be recorded (optional).
  - attrs: List of OpenTelemetry attributes to convert.

Returns:
  - A slice of slog.Attr containing the structured log information.
*/
func composeLogAttrs(span trace.Span, attrs ...attribute.KeyValue) []slog.Attr {
	logAttrs := make([]slog.Attr, 0, len(attrs)+4)
	for _, attr := range attrs {
		logAttrs = append(logAttrs, slog.Any(string(attr.Key), attr.Value.AsInterface()))
	}
	logAttrs = append(logAttrs, slog.Bool("observability.fallback", true))
	if span != nil {
		sc := span.SpanContext()
		if sc.HasTraceID() {
			logAttrs = append(logAttrs, slog.String("trace_id", sc.TraceID().String()))
		}
		if sc.HasSpanID() {
			logAttrs = append(logAttrs, slog.String("span_id", sc.SpanID().String()))
		}
	} else {
		logAttrs = append(logAttrs, slog.String("trace_id", "none"))
		logAttrs = append(logAttrs, slog.String("span_id", "none"))
	}
	return logAttrs
}

/*
extractStatusAndError extracts status and error message from the result and error.
*/
func extractStatusAndError(res *mcp.CallToolResult, err error) (string, string) {
	if err != nil {
		return "error", err.Error()
	}
	if res != nil && res.IsError {
		if len(res.Content) > 0 {
			if txt, ok := res.Content[0].(mcp.TextContent); ok {
				return "error", txt.Text
			}
			return "error", fmt.Sprintf("unknown error with content type %T", res.Content[0])
		}
		return "error", "unknown error with no content"
	}
	return "ok", ""
}

/*
MCPToolHandlerMiddleware returns a middleware function for MCP tool handlers
that injects MCP-related observability attributes into the current trace or log entry.

This middleware records the tool name, request parameters, execution status
(success or error), error messages, and the wall-clock duration in milliseconds for each call.
It is intended to be stacked after any global observability middleware for consistent metrics.

Returns:
  - A ToolHandlerMiddleware compatible with the MCP server, for enhanced traceability and monitoring.
*/
func MCPToolHandlerMiddleware() server.ToolHandlerMiddleware {
	return func(next server.ToolHandlerFunc) server.ToolHandlerFunc {
		return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			start := time.Now()
			// Record tool name and all parameters for observability
			AddRequestAttributes(
				ctx,
				attribute.String("mcp.tool", req.Params.Name),
				attribute.String("mcp.params", fmt.Sprintf("%+v", req.Params)),
			)

			res, err := next(ctx, req)
			durationMs := float64(time.Since(start).Microseconds()) / 1000.0

			// Record execution status and duration for observability
			status, errMsg := extractStatusAndError(res, err)
			attrs := []attribute.KeyValue{
				attribute.String("mcp.status", status),
				attribute.Float64("mcp.duration_ms", durationMs),
			}
			if errMsg != "" {
				attrs = append(attrs, attribute.String("mcp.error", errMsg))
			}
			// Add status, duration, and error attributes to the trace or log
			AddRequestAttributes(ctx, attrs...)

			return res, err
		}
	}
}
