package main

import (
	"flag"
	"log"
	"net/http"

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
		"mcp-with-gin-path-example",       // Server name
		"1.0.0",                           // Version
		server.WithToolCapabilities(true), // Enable tool capabilities
		server.WithLogging(),              // Enable logging
		server.WithRecovery(),             // Enable error recovery
	)

	// Register Tool
	registerTool(mcpServer)

	return &MCPServer{
		server: mcpServer,
	}
}

// ServeHTTP produces a streamable HTTP server, wrapping the MCPServer as an HTTP handler.
// Returns: *server.StreamableHTTPServer, which can be used for HTTP routing.
func (s *MCPServer) ServeHTTP() *server.StreamableHTTPServer {
	return server.NewStreamableHTTPServer(s.server)
}

// main function, the program entry point, responsible for parsing flags and starting the HTTP server.
func main() {
	var addr string
	var transport string

	// Parse the command-line flag -addr, default is :8080
	flag.StringVar(&addr, "addr", ":8080", "address to listen on")
	flag.StringVar(&transport, "transport", "stdio", "transport type (stdio, sse or http)")
	flag.Parse()

	// Create an MCPServer instance
	mcpServer := NewMCPServer()

	switch transport {
	case "stdio":
		// If transport is stdio, start the MCP server using stdio transport
		if err := server.ServeStdio(mcpServer.server); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	case "sse":
		// If transport is sse, start the MCP server using SSE transport
		sseServer := server.NewSSEServer(mcpServer.server)
		log.Printf("Gitea MCP SSE server listening on :%s", addr)
		if err := sseServer.Start(addr); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	case "http":
		// If transport is http, continue to set up the HTTP server
		// This will be handled below with Gin
		// Create a Gin router
		router := gin.Default()
		// Register POST, GET, DELETE methods for the /mcp path, all handled by MCPServer
		router.POST("/mcp", gin.WrapH(mcpServer.ServeHTTP()))
		router.GET("/mcp", gin.WrapH(mcpServer.ServeHTTP()))
		router.DELETE("/mcp", gin.WrapH(mcpServer.ServeHTTP()))

		// Output server startup message
		log.Printf("Dynamic HTTP server listening on %s", addr)
		// Start the HTTP server, listening on the specified address
		if err := http.ListenAndServe(addr, router); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	default:
		log.Fatalf("Invalid transport type: %s. Must be 'stdio', 'sse' or 'http'", transport)
	}
}
