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

func handleEchoMessageTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	message, ok := req.GetArguments()["message"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid message argument")
	}
	return mcp.NewToolResultText(fmt.Sprintf("Echo: %s", message)), nil
}

// Example of a simple MCP server using Gin that handles a tool to echo messages.

type MCPServer struct {
	server *server.MCPServer
}

func NewMCPServer() *MCPServer {
	mcpServer := server.NewMCPServer(
		"dynamic-path-example",
		"1.0.0",
		server.WithResourceCapabilities(true, true),
		server.WithPromptCapabilities(true),
		server.WithToolCapabilities(true),
	)

	mcpServer.AddTool(mcp.NewTool("echo_message",
		mcp.WithDescription("Echoes a message back"),
		mcp.WithString("message",
			mcp.Description("Message to echo"),
			mcp.Required(),
		),
	), handleEchoMessageTool)

	return &MCPServer{
		server: mcpServer,
	}
}

func (s *MCPServer) ServeHTTP() *server.StreamableHTTPServer {
	return server.NewStreamableHTTPServer(s.server)
}

func main() {
	var addr string
	flag.StringVar(&addr, "addr", ":8080", "address to listen on")
	flag.Parse()

	mcpServer := NewMCPServer()

	router := gin.Default()
	router.POST("/mcp", gin.WrapH(mcpServer.ServeHTTP()))
	router.GET("/mcp", gin.WrapH(mcpServer.ServeHTTP()))
	router.DELETE("/mcp", gin.WrapH(mcpServer.ServeHTTP()))

	log.Printf("Dynamic HTTP server listening on %s", addr)
	if err := http.ListenAndServe(addr, router); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
