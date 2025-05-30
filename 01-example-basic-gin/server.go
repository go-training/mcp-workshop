package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// handleEchoMessageTool is the handler function for the MCP tool "echo_message".
// Parameters:
//   - ctx: context.Context, the request context, used for cancellation, timeout, etc.
//   - req: mcp.CallToolRequest, the MCP tool call request object containing tool arguments.
//
// Returns:
//   - *mcp.CallToolResult: the result of the tool execution (here, a text message)
//   - error: returns an error if the argument is invalid or processing fails
func handleEchoMessageTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Retrieve the "message" argument from arguments and check its type
	message, ok := req.GetArguments()["message"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid message argument") // Argument type error
	}
	// Return the formatted message
	return mcp.NewToolResultText(fmt.Sprintf("Echo: %s", message)), nil
}

// MCPServer struct encapsulates the MCP server instance.
type MCPServer struct {
	server *server.MCPServer // The internal MCPServer instance
}

// NewMCPServer creates and initializes an MCPServer instance.
// Registers the echo_message tool and sets up basic server information and middleware.
func NewMCPServer() *MCPServer {
	// Create MCPServer, set name, version, and middleware (tool capabilities, logging, recovery)
	mcpServer := server.NewMCPServer(
		"dynamic-path-example",            // Server name
		"1.0.0",                           // Version
		server.WithToolCapabilities(true), // Enable tool capabilities
		server.WithLogging(),              // Enable logging
		server.WithRecovery(),             // Enable error recovery
	)

	// Register the echo_message tool and specify the handler function
	mcpServer.AddTool(mcp.NewTool("echo_message",
		mcp.WithDescription("Echoes a message back"), // Tool description
		mcp.WithString("message", // Tool argument name
			mcp.Description("Message to echo"), // Argument description
			mcp.Required(),                     // Argument is required
		),
	), handleEchoMessageTool)

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
	// Parse the command-line flag -addr, default is :8080
	flag.StringVar(&addr, "addr", ":8080", "address to listen on")
	flag.Parse()

	// Create an MCPServer instance
	mcpServer := NewMCPServer()

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
}
