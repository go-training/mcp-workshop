// Package main demonstrates an MCP server that passes authentication tokens
// through context, supporting both HTTP and stdio transports.
package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/go-training/mcp-workshop/pkg/operation"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// authKey is a custom context key type for storing the auth token in context.
type authKey struct{}

// requestIDKey is a custom context key type for storing the request ID in context.
type requestIDKey struct{}

// withRequestID returns a new context with a generated request ID set.
func withRequestID(ctx context.Context) context.Context {
	b := make([]byte, 8)
	_, err := rand.Read(b)
	if err != nil {
		for i := range b {
			b[i] = byte(i * 31)
		}
	}
	reqID := fmt.Sprintf("%x", b)
	return context.WithValue(ctx, requestIDKey{}, reqID)
}

// loggerFromCtx returns a slog.Logger with request_id field if present in context.
func loggerFromCtx(ctx context.Context) *slog.Logger {
	reqID, _ := ctx.Value(requestIDKey{}).(string)
	if reqID != "" {
		return slog.Default().With("request_id", reqID)
	}
	return slog.Default()
}

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
	// Check HTTP status code, return error if not 2xx
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("http request failed: status %d %s", resp.StatusCode, resp.Status)
	}
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
	logger := loggerFromCtx(ctx)
	logger.Info("Handling make_authenticated_request tool")
	message, ok := request.GetArguments()["message"].(string)
	if !ok {
		logger.Error("Missing message argument")
		return nil, fmt.Errorf("missing message")
	}
	token, err := tokenFromContext(ctx)
	if err != nil {
		logger.Error("Missing token", "error", err)
		return nil, fmt.Errorf("missing token: %v", err)
	}
	// Make the HTTP request with the token, regardless of its source.
	resp, err := makeRequest(ctx, message, token)
	if err != nil {
		logger.Error("HTTP request failed", "error", err)
		return nil, err
	}
	logger.Info("HTTP request succeeded")
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
	// Mask the token: show only the first 4 and last 4 characters, hide the middle with asterisks for security
	masked := token
	if len(token) > 8 {
		masked = token[:4] + "****" + token[len(token)-4:]
	} else if len(token) > 0 {
		masked = "****"
	}
	return mcp.NewToolResultText(masked), nil
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
		server.WithHTTPContextFunc(func(ctx context.Context, r *http.Request) context.Context {
			ctx = authFromRequest(ctx, r)
			return withRequestID(ctx)
		}),
	)
}

// ServeStdio starts the MCP server using stdio transport, injecting the
// auth token from the environment into the context.
func (s *MCPServer) ServeStdio() error {
	return server.ServeStdio(s.server, server.WithStdioContextFunc(func(ctx context.Context) context.Context {
		ctx = authFromEnv(ctx)
		return withRequestID(ctx)
	}))
}

// main is the entry point of the program. It parses command-line flags and
// starts the MCP server using the selected transport (stdio or http).
func initLogger() {
	// Use text format and DEBUG level for development, JSON and INFO for production
	var handler slog.Handler
	handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	if os.Getenv("ENV") == "production" {
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		})
	}
	slog.SetDefault(slog.New(handler))
}

func main() {
	initLogger()
	var t string
	var addr string
	flag.StringVar(&addr, "addr", ":8080", "address to listen on")
	flag.StringVar(&t, "t", "sse", "Transport type (sse or http)")
	flag.StringVar(
		&t,
		"transport",
		"sse",
		"Transport type (sse or http)",
	)
	flag.Parse()

	mcpServer := NewMCPServer()

	switch t {
	case "sse":
		// If transport is sse, start the MCP server using SSE transport
		sseServer := server.NewSSEServer(mcpServer.server)
		slog.Info("MCP SSE server listening", "addr", addr)
		if err := sseServer.Start(addr); err != nil {
			slog.Error("Server error", "error", err)
			os.Exit(1)
		}
	case "http":
		// If transport is http, continue to set up the HTTP server
		// This will be handled below with Gin
		// Create a Gin router
		router := gin.Default()

		// Middleware to check Authorization header
		authMiddleware := func(c *gin.Context) {
			if c.GetHeader("Authorization") == "" {
				c.AbortWithStatus(http.StatusUnauthorized)
				return
			}
			c.Next()
		}

		// Register POST, GET, DELETE methods for the /mcp path, all handled by MCPServer
		router.POST("/mcp", authMiddleware, gin.WrapH(mcpServer.ServeHTTP()))
		router.GET("/mcp", authMiddleware, gin.WrapH(mcpServer.ServeHTTP()))
		router.DELETE("/mcp", authMiddleware, gin.WrapH(mcpServer.ServeHTTP()))

		router.OPTIONS("/.well-known/oauth-authorization-server", func(c *gin.Context) {
			// Handle CORS preflight request
			c.Header("Access-Control-Allow-Origin", "*")
			c.Header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			c.Header("Access-Control-Allow-Headers", "Mcp-Protocol-Version, Authorization, Content-Type")
			c.Header("Access-Control-Max-Age", "86400") // Cache preflight response for 24 hours
			c.Status(http.StatusNoContent)              // Respond with 204 No Content
		})
		router.GET("/.well-known/oauth-authorization-server", func(c *gin.Context) {
			// Set CORS headers for actual GET request
			c.Header("Access-Control-Allow-Origin", "*")
			c.Header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			c.Header("Access-Control-Allow-Headers", "Mcp-Protocol-Version, Authorization, Content-Type")
			c.Header("Access-Control-Max-Age", "86400")
			metadata := transport.AuthServerMetadata{
				Issuer:                            "http://localhost:8080",
				AuthorizationEndpoint:             "http://localhost:8080/authorize",
				TokenEndpoint:                     "http://localhost:8080/token",
				RegistrationEndpoint:              "http://localhost:8080/register",
				ScopesSupported:                   []string{"openid", "profile", "email"},
				ResponseTypesSupported:            []string{"code", "token"},
				GrantTypesSupported:               []string{"authorization_code", "client_credentials", "refresh_token"},
				TokenEndpointAuthMethodsSupported: []string{"client_secret_basic", "client_secret_post"},
			}
			c.JSON(http.StatusOK, metadata)
		})

		router.OPTIONS("/register", func(c *gin.Context) {
			// Handle CORS preflight request
			c.Header("Access-Control-Allow-Origin", "*")
			c.Header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type")
			c.Header("Access-Control-Max-Age", "86400") // Cache preflight response for 24 hours
			c.Status(http.StatusNoContent)              // Respond with 204 No Content
		})

		// Add /register endpoint: echoes back the JSON body
		router.POST("/register", func(c *gin.Context) {
			// Set CORS headers for actual GET request
			c.Header("Access-Control-Allow-Origin", "*")
			c.Header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type")
			c.Header("Access-Control-Max-Age", "86400")
			var body map[string]interface{}
			if err := c.ShouldBindJSON(&body); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			body["client_id"] = "test-client-id"         // Add a dummy client_id for demonstration
			body["client_secret"] = "test-client-secret" // Add a dummy client_secret for demonstration
			c.JSON(http.StatusOK, body)
		})

		// Output server startup message
		slog.Info("MCP HTTP server listening", "addr", addr)
		// Start the HTTP server, listening on the specified address
		if err := http.ListenAndServe(addr, router); err != nil {
			slog.Error("Server error", "error", err)
			os.Exit(1)
		}
	default:
		slog.Error("Invalid transport type", "transport", t)
		os.Exit(1)
	}
}
