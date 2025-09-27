//go:build !windows
// +build !windows

package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-training/mcp-workshop/pkg/logger"
	"github.com/go-training/mcp-workshop/pkg/operation"

	"github.com/gin-gonic/gin"
	"github.com/mark3labs/mcp-go/server"
)

// MCPServer struct encapsulates the MCP server instance.
type MCPServer struct {
	server *server.MCPServer // The internal MCPServer instance
}

// NewMCPServer creates and initializes an MCPServer instance.
// Registers the echo_message tool and sets up basic server information and middleware.
func NewMCPServer() *MCPServer {
	// Create MCPServer, set name, version, and middleware (tool capabilities, logging, recovery)
	mcpServer := server.NewMCPServer(
		"mcp-with-gin-example",            // Server name
		"1.0.0",                           // Version
		server.WithToolCapabilities(true), // Enable tool capabilities
		server.WithLogging(),              // Enable logging
		server.WithRecovery(),             // Enable error recovery
	)

	// Register Tool
	operation.RegisterCommonTool(mcpServer)

	return &MCPServer{
		server: mcpServer,
	}
}

// ServeHTTP produces a streamable HTTP server, wrapping the MCPServer as an HTTP handler.
// Returns: *server.StreamableHTTPServer, which can be used for HTTP routing.
func (s *MCPServer) ServeHTTP() *server.StreamableHTTPServer {
	return server.NewStreamableHTTPServer(s.server,
		server.WithHeartbeatInterval(30*time.Second),
	)
}

// main function, the program entry point, responsible for parsing flags and starting the HTTP server.
func main() {
	logger.New()
	var addr string
	var transport string
	var transportAlias string

	// Parse the command-line flag -addr, default is :8080
	flag.StringVar(&addr, "addr", ":8080", "address to listen on")
	flag.StringVar(&transport, "transport", "stdio", "transport type (stdio or http)")
	flag.StringVar(&transportAlias, "t", "", "alias for -transport")
	flag.Parse()
	if transportAlias != "" {
		transport = transportAlias
	}

	// Create an MCPServer instance
	mcpServer := NewMCPServer()

	switch transport {
	case "stdio":
		// If transport is stdio, start the MCP server using stdio transport
		if err := server.ServeStdio(mcpServer.server); err != nil {
			slog.Error("Server error", "err", err)
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
		// Graceful shutdown logic
		go func() {
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				slog.Error("Server error", "err", err)
			}
		}()

		// Signal handling for shutdown
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, os.Interrupt)
		// For Unix systems, also handle SIGTERM
		signal.Notify(quit, syscall.SIGTERM)

		<-quit
		slog.Info("Shutdown signal received, shutting down server...")

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			slog.Error("Server forced to shutdown", "err", err)
			os.Exit(1)
		}

		slog.Info("Server shutdown gracefully")
	default:
		slog.Error("Invalid transport type", "transport", transport)
		os.Exit(1)
	}
}
