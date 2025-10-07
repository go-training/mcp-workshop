// Package main demonstrates an MCP server that passes authentication tokens
// through context, supporting both HTTP and stdio transports.
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-training/mcp-workshop/pkg/core"
	"github.com/go-training/mcp-workshop/pkg/logger"
	"github.com/go-training/mcp-workshop/pkg/operation"
	"github.com/go-training/mcp-workshop/pkg/store"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/server"
	"golang.org/x/oauth2"
)

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
		server.WithHTTPContextFunc(func(ctx context.Context, r *http.Request) context.Context {
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
	var addr string
	var externalClientID string
	var externalClientSecret string
	var providerName string
	var giteaHost string
	var gitlabHost string
	var logLevel string
	var storeType string
	var redisAddr string
	var redisPassword string
	var redisDB int
	flag.StringVar(&externalClientID, "client_id", "", "OAuth 2.0 Client ID")
	flag.StringVar(&externalClientSecret, "client_secret", "", "OAuth 2.0 Client Secret")
	flag.StringVar(&addr, "addr", ":8095", "address to listen on")
	flag.StringVar(&providerName, "provider", "github", "OAuth provider: github, gitea, or gitlab")
	flag.StringVar(&giteaHost, "gitea-host", "https://gitea.com", "Gitea host")
	flag.StringVar(&gitlabHost, "gitlab-host", "https://gitlab.com", "GitLab host")
	flag.StringVar(&logLevel, "log-level", "", "Log level (DEBUG, INFO, WARN, ERROR). Defaults to DEBUG in development, INFO in production")
	flag.StringVar(&storeType, "store", "memory", "Store type: memory or redis")
	flag.StringVar(&redisAddr, "redis-addr", "localhost:6379", "Redis address (only used when store=redis)")
	flag.StringVar(&redisPassword, "redis-password", "", "Redis password (only used when store=redis)")
	flag.IntVar(&redisDB, "redis-db", 0, "Redis database (only used when store=redis)")
	flag.Parse()

	// Initialize logger with the specified log level
	logger.NewWithLevel(logLevel)

	if externalClientID == "" || externalClientSecret == "" {
		slog.Error("Client ID and Client Secret must be provided")
		os.Exit(1)
	}

	// Initialize provider based on the flag
	var provider OAuthProvider
	switch providerName {
	case "github":
		provider = NewGitHubProvider()
		slog.Info("Using GitHub OAuth provider")
	case "gitea":
		provider = NewGiteaProvider(giteaHost)
		slog.Info("Using Gitea OAuth provider", "host", giteaHost)
	case "gitlab":
		provider = NewGitLabProvider(gitlabHost)
		slog.Info("Using GitLab OAuth provider", "host", gitlabHost)
	default:
		slog.Error("Invalid provider specified. Use 'github' or 'gitea'.")
		os.Exit(1)
	}

	// Initialize store using factory pattern
	storeConfig := store.Config{
		Type: store.ParseStoreType(storeType),
		Redis: store.RedisOptions{
			Addr:     redisAddr,
			Password: redisPassword,
			DB:       redisDB,
		},
	}

	oauthStore, err := store.NewStore(storeConfig)
	if err != nil {
		slog.Error("Failed to create store", "type", storeType, "error", err)
		os.Exit(1)
	}

	// Log the store type being used
	switch storeConfig.Type {
	case store.StoreTypeMemory:
		slog.Info("Using in-memory store")
	case store.StoreTypeRedis:
		slog.Info("Using Redis store", "addr", redisAddr, "db", redisDB)
		// Ensure Redis connection is closed on shutdown
		if redisStore, ok := oauthStore.(*store.RedisStore); ok {
			defer redisStore.Close()
		}
	}

	mcpServer := NewMCPServer()
	router := gin.Default()
	router.Use(corsMiddleware())

	// Register POST, GET, DELETE methods for the /mcp path, all handled by MCPServer
	router.POST("/mcp", authMiddleware, gin.WrapH(mcpServer.ServeHTTP()))
	router.GET("/mcp", authMiddleware, gin.WrapH(mcpServer.ServeHTTP()))
	router.DELETE("/mcp", authMiddleware, gin.WrapH(mcpServer.ServeHTTP()))

	router.GET("/.well-known/oauth-protected-resource",
		corsMiddleware(), func(c *gin.Context) {
			metadata := &transport.OAuthProtectedResource{
				AuthorizationServers: []string{"http://localhost" + addr},
				Resource:             "Example OAuth Protected Resource",
				ResourceName:         "Example OAuth Protected Resource",
			}
			c.JSON(http.StatusOK, metadata)
		})

	router.GET("/.well-known/oauth-authorization-server",
		corsMiddleware(), func(c *gin.Context) {
			// Set supported scopes based on provider
			var scopesSupported []string
			switch providerName {
			case "gitlab":
				scopesSupported = []string{"read_user"}
			case "github", "gitea":
				scopesSupported = []string{"openid", "profile", "email"}
			default:
				scopesSupported = []string{"openid", "profile", "email"}
			}

			metadata := transport.AuthServerMetadata{
				Issuer:                            "http://localhost" + addr,
				AuthorizationEndpoint:             "http://localhost" + addr + "/authorize",
				TokenEndpoint:                     "http://localhost" + addr + "/token",
				RegistrationEndpoint:              "http://localhost" + addr + "/register",
				ScopesSupported:                   scopesSupported,
				ResponseTypesSupported:            []string{"code"},
				GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
				TokenEndpointAuthMethodsSupported: []string{"none", "client_secret_basic", "client_secret_post"},
				CodeChallengeMethodsSupported:     []string{"plain", "S256"}, // for inspector
			}
			c.JSON(http.StatusOK, metadata)
		})

	router.GET("/authorize", corsMiddleware("Authorization", "Content-Type"), func(c *gin.Context) {
		clientID := c.Query("client_id")
		redirectURI := c.Query("redirect_uri")
		responseType := c.Query("response_type")
		scope := c.Query("scope")
		state := c.Query("state")
		codeChallenge := c.Query("code_challenge")
		codeChallengeMethod := c.Query("code_challenge_method")

		// Validate required parameters
		if clientID == "" || redirectURI == "" || responseType == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "client_id, redirect_uri, and response_type are required"})
			return
		}

		// Validate response type
		if responseType != "code" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported response_type"})
			return
		}

		// Get client
		client, err := oauthStore.GetClient(c.Request.Context(), clientID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid client_id"})
			return
		}

		// Validate redirect URI
		if !isValidRedirectURI(redirectURI, client.RedirectURIs) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid redirect_uri"})
			return
		}

		// Validate code challenge if provided
		if codeChallenge != "" {
			if codeChallengeMethod != "plain" && codeChallengeMethod != "S256" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid code_challenge_method"})
				return
			}
		}

		slog.Debug("Authorization request received",
			"client_id", clientID,
			"redirect_uri", redirectURI,
			"response_type", responseType,
			"scope", scope,
			"state", state,
			"code_challenge", codeChallenge,
			"code_challenge_method", codeChallengeMethod,
		)

		// Save authorization code
		authCode := &core.AuthorizationCode{
			ClientID:            clientID,
			Code:                generateAuthorizationCode(),
			RedirectURI:         redirectURI,
			Scope:               strings.Split(scope, " "),
			CodeChallenge:       codeChallenge,
			CodeChallengeMethod: codeChallengeMethod,
			CreatedAt:           time.Now().Unix(),
			ExpiresAt:           time.Now().Add(10 * time.Minute).Unix(),
		}
		if err := oauthStore.SaveAuthorizationCode(c.Request.Context(), authCode); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		authURL, err := provider.GetAuthorizeURL(
			externalClientID, state, redirectURI, scope, codeChallenge, codeChallengeMethod)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.Redirect(http.StatusFound, authURL)
	})

	router.POST("/token",
		corsMiddleware("Authorization", "Content-Type"), func(c *gin.Context) {
			grantType := c.PostForm("grant_type")
			code := c.PostForm("code")
			clientID := c.PostForm("client_id")
			clientSecret := c.PostForm("client_secret")
			redirectURI := c.PostForm("redirect_uri")
			codeVerifier := c.PostForm("code_verifier")

			// Log all received parameters except client_secret
			slog.Debug("Token request received",
				"grant_type", grantType,
				"code", code,
				"client_id", clientID,
				"client_secret", clientSecret,
				"redirect_uri", redirectURI,
				"code_verifier", codeVerifier,
			)

			if code == "" || clientID == "" || redirectURI == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "code, client_id, and redirect_uri are required"})
				return
			}

			// Validate client credentials
			client, err := oauthStore.GetClient(c.Request.Context(), clientID)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid client_id"})
				return
			}

			// Validate client secret
			if client.Secret != clientSecret {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid client_secret"})
				return
			}

			// Get authorization code
			authCode, err := oauthStore.GetAuthorizationCode(c.Request.Context(), clientID)
			if err != nil || authCode == nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid code"})
				return
			}

			// Validate redirect URI
			if authCode.RedirectURI != redirectURI {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid redirect_uri"})
				return
			}

			// Validate code verifier if code challenge was provided
			if authCode.CodeChallenge != "" {
				if codeVerifier == "" {
					c.JSON(http.StatusBadRequest, gin.H{"error": "code_verifier is required"})
					return
				}

				// Compute challenge from verifier
				computedChallenge, err := computeCodeChallenge(codeVerifier, authCode.CodeChallengeMethod)
				if err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"error": "failed to compute code challenge", "details": err.Error()})
					return
				}

				// Verify the challenge matches
				if computedChallenge != authCode.CodeChallenge {
					c.JSON(http.StatusBadRequest, gin.H{"error": "invalid code_verifier"})
					return
				}
			}

			token, err := provider.ExchangeToken(externalClientID, externalClientSecret, code, redirectURI, codeVerifier)
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

			// Log all user info fields
			slog.Info("Token exchange successful",
				"user_email", userInfo.Email,
				"user_name", userInfo.Name,
				"user_login", userInfo.Login,
				"user_avatar_url", userInfo.AvatarURL,
			)

			// Delete authorization code
			if err := oauthStore.DeleteAuthorizationCode(c.Request.Context(), clientID); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete authorization code", "details": err.Error()})
				return
			}

			c.JSON(http.StatusOK, token)
		})

	// Add /register endpoint: echoes back the JSON body
	router.POST("/register",
		corsMiddleware("Authorization", "Content-Type"), func(c *gin.Context) {
			var req ClientRegistrationMetadata
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}

			if len(req.RedirectURIs) == 0 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "redirect_uris is required"})
				return
			}

			// Set default values if not provided
			if len(req.GrantTypes) == 0 {
				req.GrantTypes = []string{"authorization_code", "refresh_token"}
			}
			if len(req.ResponseTypes) == 0 {
				req.ResponseTypes = []string{"code"}
			}
			if req.Scope == "" {
				req.Scope = "openid profile email"
			}

			// Generate client credentials
			clientID := uuid.New().String()
			clientSecret := generateClientSecret()

			// Create client
			client := &core.Client{
				ID:                    clientID,
				Secret:                clientSecret,
				RedirectURIs:          req.RedirectURIs,
				GrantTypes:            req.GrantTypes,
				ResponseTypes:         req.ResponseTypes,
				Scope:                 req.Scope,
				IssuedAt:              time.Now().Unix(),
				ClientSecretExpiresAt: time.Now().Add(1 * time.Minute).Unix(),
			}

			err := oauthStore.CreateClient(context.Background(), client)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create client", "details": err.Error()})
				return
			}

			// Create response using RegisterResponse struct
			response := &ClientRegistrationResponse{
				ClientID:                client.ID,
				ClientSecret:            client.Secret,
				ClientIDIssuedAt:        time.Unix(client.IssuedAt, 0),
				ClientSecretExpiresAt:   time.Unix(client.ClientSecretExpiresAt, 0),
				RedirectURIs:            req.RedirectURIs,
				GrantTypes:              req.GrantTypes,
				TokenEndpointAuthMethod: req.TokenEndpointAuthMethod,
			}

			slog.Debug("Client registered", "response", response)

			c.JSON(http.StatusCreated, response)
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
	// Graceful shutdown logic
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Server error", "err", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt)
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
}

func generateClientSecret() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func generateAuthorizationCode() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func isValidRedirectURI(redirectURI string, allowedURIs []string) bool {
	u, err := url.Parse(redirectURI)
	if err != nil {
		return false
	}

	for _, allowed := range allowedURIs {
		allowedURL, err := url.Parse(allowed)
		if err != nil {
			continue
		}

		if u.Scheme == allowedURL.Scheme &&
			u.Host == allowedURL.Host &&
			strings.HasPrefix(u.Path, allowedURL.Path) {
			return true
		}
	}

	return false
}

func computeCodeChallenge(codeVerifier string, method string) (string, error) {
	switch method {
	case "plain":
		return codeVerifier, nil
	case "S256":
		return oauth2.S256ChallengeFromVerifier(codeVerifier), nil
	default:
		return "", errors.New("unsupported code challenge method")
	}
}
