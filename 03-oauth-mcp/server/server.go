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
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-training/mcp-workshop/pkg/logger"
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
		"example-oauth-server",
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
		server.WithHeartbeatInterval(30*time.Second),
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

func main() {
	logger.New()
	var addr string
	var clientID string
	var clientSecret string
	flag.StringVar(&clientID, "client_id", "", "OAuth 2.0 Client ID")
	flag.StringVar(&clientSecret, "client_secret", "", "OAuth 2.0 Client Secret")
	flag.StringVar(&addr, "addr", ":8095", "address to listen on")
	flag.Parse()

	if clientID == "" || clientSecret == "" {
		slog.Error("Client ID and Client Secret must be provided")
		os.Exit(1)
	}

	// Initialize provider (GitHub for now)
	provider := &GitHubProvider{}

	mcpServer := NewMCPServer()
	router := gin.Default()

	// Middleware to check Authorization header
	authMiddleware := func(c *gin.Context) {
		if c.GetHeader("Authorization") == "" {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.Next()
	}

	// CORS middleware for handling preflight and actual requests
	corsMiddleware := func(allowedHeaders ...string) gin.HandlerFunc {
		headers := "Mcp-Protocol-Version, Authorization, Content-Type"
		if len(allowedHeaders) > 0 {
			headers = ""
			for i, h := range allowedHeaders {
				if i > 0 {
					headers += ", "
				}
				headers += h
			}
		}
		return func(c *gin.Context) {
			c.Header("Access-Control-Allow-Origin", "*")
			c.Header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			c.Header("Access-Control-Allow-Headers", headers)
			c.Header("Access-Control-Max-Age", "86400")
			if c.Request.Method == "OPTIONS" {
				c.AbortWithStatus(http.StatusNoContent)
				return
			}
			c.Next()
		}
	}

	router.Use(corsMiddleware())

	// Register POST, GET, DELETE methods for the /mcp path, all handled by MCPServer
	router.POST("/mcp", authMiddleware, gin.WrapH(mcpServer.ServeHTTP()))
	router.GET("/mcp", authMiddleware, gin.WrapH(mcpServer.ServeHTTP()))
	router.DELETE("/mcp", authMiddleware, gin.WrapH(mcpServer.ServeHTTP()))

	router.GET("/.well-known/oauth-protected-resource", corsMiddleware(), func(c *gin.Context) {
		metadata := &transport.OAuthProtectedResource{
			AuthorizationServers: []string{"http://localhost" + addr + "/.well-known/oauth-authorization-server"},
			Resource:             "Example OAuth Protected Resource",
			ResourceName:         "Example OAuth Protected Resource",
		}
		c.JSON(http.StatusOK, metadata)
	})

	router.GET("/.well-known/oauth-authorization-server", corsMiddleware(), func(c *gin.Context) {
		metadata := transport.AuthServerMetadata{
			Issuer:                            "http://localhost" + addr,
			AuthorizationEndpoint:             "http://localhost" + addr + "/authorize",
			TokenEndpoint:                     "http://localhost" + addr + "/token",
			RegistrationEndpoint:              "http://localhost" + addr + "/register",
			ScopesSupported:                   []string{"openid", "profile", "email"},
			ResponseTypesSupported:            []string{"code"},
			GrantTypesSupported:               []string{"authorization_code", "client_credentials", "refresh_token"},
			TokenEndpointAuthMethodsSupported: []string{"client_secret_basic", "client_secret_post"},
			CodeChallengeMethodsSupported:     []string{"S256"}, // for inspector
		}
		c.JSON(http.StatusOK, metadata)
	})

	router.GET("/authorize", corsMiddleware("Authorization", "Content-Type"), func(c *gin.Context) {
		clientIDParam := c.Query("client_id")
		if clientIDParam == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "client_id is required"})
			return
		}
		state := c.Query("state")
		if state == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "state is required"})
			return
		}
		// optional: scopes, redirect_uri
		redirectURI := c.Query("redirect_uri")
		scopes := c.Query("scope")
		if scopes == "" {
			scopes = "user" // default GitHub
		}
		authURL, err := provider.GetAuthorizeURL(clientIDParam, state, redirectURI, scopes)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.Redirect(http.StatusFound, authURL)
	})

	router.POST("/token", corsMiddleware("Authorization", "Content-Type"), func(c *gin.Context) {
		grantType := c.PostForm("grant_type")
		code := c.PostForm("code")
		clientIDParam := c.PostForm("client_id")
		redirectURI := c.PostForm("redirect_uri")
		slog.Info("Token request received", "grant_type", grantType, "client_id", clientIDParam, "redirect_uri", redirectURI)
		if grantType != "authorization_code" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported grant_type"})
			return
		}
		if code == "" || clientIDParam == "" || redirectURI == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "code, client_id, and redirect_uri are required"})
			return
		}

		token, err := provider.ExchangeToken(clientIDParam, clientSecret, code, redirectURI)
		if err != nil {
			slog.Error("Token exchange failed", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if token == nil {
			slog.Error("Token exchange returned nil token without error")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "empty token response"})
			return
		}

		accessToken := token.AccessToken

		userInfo, userErr := provider.FetchUserInfo(accessToken)
		if userErr != nil {
			slog.Error("Failed to fetch user info", "error", userErr)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch user info", "details": userErr.Error()})
			return
		}

		slog.Info("Token response with user info",
			"email", userInfo["email"],
			"login", userInfo["login"],
		)

		c.JSON(http.StatusOK, token)
	})

	// Add /register endpoint: echoes back the JSON body
	router.POST("/register", corsMiddleware("Authorization", "Content-Type"), func(c *gin.Context) {
		var body map[string]interface{}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		body["client_id"] = clientID
		body["client_secret"] = clientSecret
		c.JSON(http.StatusOK, body)
	})

	// Output server startup message
	slog.Info("MCP HTTP server listening", "addr", addr)
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
}

// fetchGitHubUser fetches the authenticated user's profile from GitHub.
func fetchGitHubUser(accessToken string) (map[string]interface{}, error) {
	req, err := http.NewRequest("GET", "https://api.github.com/user", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to fetch user info: %s", string(body))
	}
	var user map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, err
	}
	return user, nil
}
