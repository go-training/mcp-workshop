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

// NewMCPServer creates and configures a new MCPServer instance.
// Registers the make_authenticated_request and show_auth_token tools.
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

// ServeHTTP returns a streamable HTTP server that injects the auth token
// from HTTP requests into the context.
func (s *MCPServer) ServeHTTP() *server.StreamableHTTPServer {
	return server.NewStreamableHTTPServer(s.server)
}

// ServeStdio starts the MCP server using stdio transport, injecting the
// auth token from the environment into the context.
func (s *MCPServer) ServeStdio() error {
	return server.ServeStdio(s.server)
}

func main() {
	logger.New()
	var transport string
	var addr string
	flag.StringVar(&addr, "addr", ":8080", "address to listen on")
	flag.StringVar(&transport, "t", "stdio", "Transport type (stdio or http)")
	flag.StringVar(
		&transport,
		"transport",
		"stdio",
		"Transport type (stdio or http)",
	)
	flag.Parse()

	mcpServer := NewMCPServer()

	switch transport {
	case "stdio":
		if err := mcpServer.ServeStdio(); err != nil {
			slog.Error("Server error", "error", err)
			os.Exit(1)
		}
	case "http":
		// If transport is http, continue to set up the HTTP server
		// This will be handled below with Gin
		// Create a Gin router
		router := gin.Default()
		// Register POST, GET, DELETE methods for the /mcp path, all handled by MCPServer
		for _, method := range []string{"POST", "GET", "DELETE"} {
			router.Handle(method, "/mcp", gin.WrapH(mcpServer.ServeHTTP()))
		}

		// Output server startup message
		slog.Info("Dynamic HTTP server listening", "addr", addr)
		// Start the HTTP server, listening on the specified address
		srv := &http.Server{
			Addr:         addr,
			Handler:      router,
			ReadTimeout:  10 * time.Second, // 10 seconds
			WriteTimeout: 10 * time.Second, // 10 seconds
			IdleTimeout:  60 * time.Second, // 60 seconds
		}
		// Start the HTTP server, listening on the specified address
		if err := srv.ListenAndServe(); err != nil {
			slog.Error("Server error", "err", err)
			os.Exit(1)
		}
	default:
		slog.Error("Invalid transport type", "transport", transport)
		os.Exit(1)
	}
}

/*
AddRequestAttributes sets attributes on the current trace span, and if no active span,
logs the attributes via slog for observability fallback. Also logs trace/span id for correlation.
*/
func AddRequestAttributes(ctx context.Context, attrs ...attribute.KeyValue) {
	span := trace.SpanFromContext(ctx)
	if span == nil || !span.IsRecording() {
		// Fallback: log attributes via slog
		logAttrs := make([]slog.Attr, 0, len(attrs)+3)
		for _, attr := range attrs {
			logAttrs = append(logAttrs, slog.Any(string(attr.Key), attr.Value.AsInterface()))
		}
		logAttrs = append(logAttrs, slog.Bool("observability.fallback", true))
		// Try to extract trace/span id if available
		if span != nil {
			sc := span.SpanContext()
			if sc.HasTraceID() {
				logAttrs = append(logAttrs, slog.String("trace_id", sc.TraceID().String()))
			}
			if sc.HasSpanID() {
				logAttrs = append(logAttrs, slog.String("span_id", sc.SpanID().String()))
			}
		}
		slog.LogAttrs(ctx, slog.LevelInfo, "AddRequestAttributes fallback", logAttrs...)
		return
	}
	// Normal: set attributes on the span
	span.SetAttributes(attrs...)
}

// MCPToolHandlerMiddleware is a middleware for MCP tool handlers that adds MCP-related observability attributes.
// It is expected to run on an MCP server that has already been wrapped with observability.Middleware(...).
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
			status := "ok"
			var errMsg string
			if err != nil {
				status = "error"
				errMsg = err.Error()
			} else if res != nil && res.IsError {
				status = "error"
				if len(res.Content) > 0 {
					txt, ok := res.Content[0].(mcp.TextContent)
					if ok {
						errMsg = txt.Text
					} else {
						errMsg = fmt.Sprintf("unknown error with content type %T", res.Content[0])
					}
				} else {
					errMsg = "unknown error with no content"
				}
			}
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
