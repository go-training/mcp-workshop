// Package main demonstrates an MCP server that passes authentication tokens
// through context, supporting both HTTP and stdio transports.
package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/go-training/mcp-workshop/pkg/core"
	"github.com/go-training/mcp-workshop/pkg/logger"
	"github.com/go-training/mcp-workshop/pkg/operation"

	"github.com/gin-gonic/gin"
	"github.com/mark3labs/mcp-go/server"
)

// tokenFromContext retrieves the auth token from the context.
// Returns the token string if present, or an error if missing.
// MCPServer wraps the underlying MCP server instance.
type MCPServer struct {
	server *server.MCPServer
}

// NewMCPServer creates and configures a new MCPServer instance.
// Registers the make_authenticated_request and show_auth_token tools.
func NewMCPServer() *MCPServer {
	mcpServer := server.NewMCPServer(
		"example-server",
		"1.0.0",
		server.WithToolCapabilities(true),
		server.WithLogging(),
		server.WithRecovery(),
	)

	// Register Tool
	operation.RegisterAuthTool(mcpServer)

	return &MCPServer{
		server: mcpServer,
	}
}

// ServeHTTP returns a streamable HTTP server that injects the auth token
// from HTTP requests into the context.
func (s *MCPServer) ServeHTTP() *server.StreamableHTTPServer {
	return server.NewStreamableHTTPServer(s.server,
		server.WithHeartbeatInterval(30*time.Second),
		server.WithHTTPContextFunc(func(
			ctx context.Context,
			r *http.Request,
		) context.Context {
			ctx = core.AuthFromRequest(ctx, r)
			return core.WithRequestID(ctx)
		}),
	)
}

// ServeStdio starts the MCP server using stdio transport, injecting the
// auth token from the environment into the context.
func (s *MCPServer) ServeStdio() error {
	return server.ServeStdio(s.server, server.WithStdioContextFunc(func(ctx context.Context) context.Context {
		ctx = core.AuthFromEnv(ctx)
		return core.WithRequestID(ctx)
	}))
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
		for _, method := range []string{http.MethodPost, http.MethodGet, http.MethodDelete} {
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
