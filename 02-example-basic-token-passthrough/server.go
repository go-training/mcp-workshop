// Package main demonstrates an MCP server that passes authentication tokens
// through context, supporting both HTTP and stdio transports.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/go-training/mcp-workshop/pkg/operation"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// authKey is a custom context key type for storing the auth token in context.
type authKey struct{}

// withAuthKey returns a new context with the provided auth token set.
func withAuthKey(ctx context.Context, auth string) context.Context {
	return context.WithValue(ctx, authKey{}, auth)
}

// authFromRequest extracts the Authorization header from the HTTP request
// and stores it in the context. Used for HTTP transport.
func authFromRequest(ctx context.Context, r *http.Request) context.Context {
	return withAuthKey(ctx, r.Header.Get("Authorization"))
}

// authFromEnv extracts the API_KEY environment variable and stores it in the context.
// Used for stdio transport.
func authFromEnv(ctx context.Context) context.Context {
	return withAuthKey(ctx, os.Getenv("API_KEY"))
}

// tokenFromContext retrieves the auth token from the context.
// Returns the token string if present, or an error if missing.
func tokenFromContext(ctx context.Context) (string, error) {
	auth, ok := ctx.Value(authKey{}).(string)
	if !ok {
		return "", fmt.Errorf("missing auth")
	}
	return auth, nil
}

// response represents the structure of the response from httpbin.org/anything.
type response struct {
	Args    map[string]any    `json:"args"`
	Headers map[string]string `json:"headers"`
}

// makeRequest sends a GET request to https://httpbin.org/anything, including
// the provided auth token in the Authorization header and the message as a query parameter.
// Returns the parsed response or an error.
func makeRequest(ctx context.Context, message, token string) (*response, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://httpbin.org/anything", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", token)
	query := req.URL.Query()
	query.Add("message", message)
	req.URL.RawQuery = query.Encode()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var r *response
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, err
	}
	return r, nil
}

// handleMakeAuthenticatedRequestTool is an MCP tool handler that makes an
// authenticated HTTP request using the token from context and the provided message argument.
// Returns the response as a formatted string.
func handleMakeAuthenticatedRequestTool(
	ctx context.Context,
	request mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	message, ok := request.GetArguments()["message"].(string)
	if !ok {
		return nil, fmt.Errorf("missing message")
	}
	token, err := tokenFromContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("missing token: %v", err)
	}
	// Make the HTTP request with the token, regardless of its source.
	resp, err := makeRequest(ctx, message, token)
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultText(fmt.Sprintf("%+v", resp)), nil
}

// handleShowAuthTokenTool is an MCP tool handler that returns the current
// auth token from context as a string.
func handleShowAuthTokenTool(
	ctx context.Context,
	_ mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	token, err := tokenFromContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("missing token: %v", err)
	}
	return mcp.NewToolResultText(fmt.Sprintf("%+v", token)), nil
}

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
	operation.RegisterTool(mcpServer)

	mcpServer.AddTool(
		mcp.NewTool("make_authenticated_request",
			mcp.WithDescription("Make an authenticated HTTP request to httpbin.org/anything"),
			mcp.WithString("message",
				mcp.Description("The message to include in the request"),
				mcp.Required(),
			),
		),
		handleMakeAuthenticatedRequestTool,
	)

	mcpServer.AddTool(
		mcp.NewTool("show_auth_token",
			mcp.WithDescription("Show the current authentication token"),
		),
		handleShowAuthTokenTool,
	)

	return &MCPServer{
		server: mcpServer,
	}
}

// ServeHTTP returns a streamable HTTP server that injects the auth token
// from HTTP requests into the context.
func (s *MCPServer) ServeHTTP() *server.StreamableHTTPServer {
	return server.NewStreamableHTTPServer(s.server,
		server.WithHTTPContextFunc(authFromRequest),
	)
}

// ServeStdio starts the MCP server using stdio transport, injecting the
// auth token from the environment into the context.
func (s *MCPServer) ServeStdio() error {
	return server.ServeStdio(s.server, server.WithStdioContextFunc(authFromEnv))
}

// main is the entry point of the program. It parses command-line flags and
// starts the MCP server using the selected transport (stdio or http).
func main() {
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
			log.Fatalf("Server error: %v", err)
		}
	case "sse":
		// If transport is sse, start the MCP server using SSE transport
		sseServer := server.NewSSEServer(mcpServer.server)
		log.Printf("MCP SSE server listening on %s", addr)
		if err := sseServer.Start(addr); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	case "http":
		httpServer := mcpServer.ServeHTTP()
		log.Printf("HTTP server listening on %s", addr)
		if err := httpServer.Start(addr); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	default:
		log.Fatalf(
			"Invalid transport type: %s. Must be 'stdio' or 'http'",
			transport,
		)
	}
}
