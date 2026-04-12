// Package main implements an MCP resource server that validates incoming
// Bearer tokens by calling an external OAuth 2.0 authorization server's
// RFC 7662 introspection endpoint. Built against
// github.com/modelcontextprotocol/go-sdk v1.5.0.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-training/mcp-workshop/pkg/logger"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

type introspector struct {
	endpoint     string
	clientID     string
	clientSecret string
	httpClient   *http.Client
}

type introspectionResponse struct {
	Active   bool   `json:"active"`
	Scope    string `json:"scope,omitempty"`
	ClientID string `json:"client_id,omitempty"`
	Username string `json:"username,omitempty"`
	Sub      string `json:"sub,omitempty"`
	TokenTyp string `json:"token_type,omitempty"`
	Exp      int64  `json:"exp,omitempty"`
}

func (i *introspector) Verify(
	ctx context.Context,
	token string,
	_ *http.Request,
) (*auth.TokenInfo, error) {
	form := url.Values{}
	form.Set("token", token)
	form.Set("token_type_hint", "access_token")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, i.endpoint,
		strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.SetBasicAuth(i.clientID, i.clientSecret)

	resp, err := i.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("introspection request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(
			"%w: introspection returned HTTP %d",
			auth.ErrInvalidToken,
			resp.StatusCode,
		)
	}

	var body introspectionResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode introspection response: %w", err)
	}
	if !body.Active {
		return nil, fmt.Errorf("%w: token is not active", auth.ErrInvalidToken)
	}

	info := &auth.TokenInfo{
		Scopes: strings.Fields(body.Scope),
		Extra:  map[string]any{},
	}
	if body.Exp > 0 {
		info.Expiration = time.Unix(body.Exp, 0)
	}
	if body.ClientID != "" {
		info.Extra["client_id"] = body.ClientID
	}
	if body.Username != "" {
		info.UserID = body.Username
		info.Extra["username"] = body.Username
	} else if body.Sub != "" {
		info.UserID = body.Sub
		info.Extra["sub"] = body.Sub
	}
	return info, nil
}

type EchoInput struct {
	Message string `json:"message" jsonschema:"the message to echo back"`
}

type EchoOutput struct {
	Message  string   `json:"message"   jsonschema:"echoed message"`
	ClientID string   `json:"client_id" jsonschema:"the authenticated client id"`
	Scopes   []string `json:"scopes"    jsonschema:"scopes granted to the access token"`
}

func echoHandler(
	_ context.Context,
	req *mcp.CallToolRequest,
	input EchoInput,
) (*mcp.CallToolResult, EchoOutput, error) {
	info := req.Extra.TokenInfo
	clientID, _ := info.Extra["client_id"].(string)
	return nil, EchoOutput{
		Message:  input.Message,
		ClientID: clientID,
		Scopes:   info.Scopes,
	}, nil
}

type AddInput struct {
	A float64 `json:"a" jsonschema:"first addend"`
	B float64 `json:"b" jsonschema:"second addend"`
}

type AddOutput struct {
	Sum float64 `json:"sum" jsonschema:"the sum of a and b"`
}

func addHandler(
	_ context.Context,
	_ *mcp.CallToolRequest,
	input AddInput,
) (*mcp.CallToolResult, AddOutput, error) {
	return nil, AddOutput{Sum: input.A + input.B}, nil
}

func newMCPServer() *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "client-credentials-mcp-server",
		Version: "1.0.0",
	}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "echo_message",
		Description: "Echoes the provided message back along with the authenticated client id and token scopes.",
	}, echoHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "add_numbers",
		Description: "Returns the sum of two numbers.",
	}, addHandler)

	return server
}

func main() {
	var (
		addr                   string
		resourceURL            string
		authServerURL          string
		introspectionURL       string
		introspectClientID     string
		introspectClientSecret string
		requiredScopes         string
		logLevel               string
	)
	flag.StringVar(&addr, "addr", ":8096", "address to listen on")
	flag.StringVar(&resourceURL, "resource", "",
		"public URL of this MCP resource (defaults to http://localhost<addr>/mcp)")
	flag.StringVar(&authServerURL, "auth-server", "http://localhost:8080",
		"issuer URL of the external OAuth 2.0 authorization server (e.g. AuthGate)")
	flag.StringVar(&introspectionURL, "introspection-url", "",
		"RFC 7662 introspection endpoint (defaults to <auth-server>/oauth/introspect)")
	flag.StringVar(&introspectClientID, "introspect-client-id", "",
		"client_id this resource server uses to call the introspection endpoint (required)")
	flag.StringVar(&introspectClientSecret, "introspect-client-secret", "",
		"client_secret this resource server uses to call the introspection endpoint (required)")
	flag.StringVar(&requiredScopes, "required-scopes", "mcp:read",
		"space-separated scopes an access token must contain to reach /mcp")
	flag.StringVar(&logLevel, "log-level", "INFO", "log level: DEBUG, INFO, WARN, ERROR")
	flag.Parse()

	logger.NewWithLevel(logLevel)

	if introspectClientID == "" || introspectClientSecret == "" {
		slog.Error("introspect-client-id and introspect-client-secret are required")
		os.Exit(1)
	}
	if introspectionURL == "" {
		introspectionURL = strings.TrimRight(authServerURL, "/") + "/oauth/introspect"
	}
	if resourceURL == "" {
		resourceURL = "http://localhost" + addr + "/mcp"
	}

	scopes := strings.Fields(requiredScopes)

	ins := &introspector{
		endpoint:     introspectionURL,
		clientID:     introspectClientID,
		clientSecret: introspectClientSecret,
		httpClient:   &http.Client{Timeout: 5 * time.Second},
	}

	resourceMetadataPath := "/.well-known/oauth-protected-resource"
	resourceMetadataURL := "http://localhost" + addr + resourceMetadataPath

	authMiddleware := auth.RequireBearerToken(ins.Verify, &auth.RequireBearerTokenOptions{
		Scopes:              scopes,
		ResourceMetadataURL: resourceMetadataURL,
	})

	mcpServer := newMCPServer()
	mcpHandler := mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server {
		return mcpServer
	}, nil)

	mux := http.NewServeMux()
	mux.Handle("/mcp", authMiddleware(mcpHandler))
	mux.Handle(
		resourceMetadataPath,
		auth.ProtectedResourceMetadataHandler(&oauthex.ProtectedResourceMetadata{
			Resource:               resourceURL,
			AuthorizationServers:   []string{authServerURL},
			BearerMethodsSupported: []string{"header"},
			ScopesSupported:        scopes,
		}),
	)

	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	slog.Info("client-credentials MCP server starting",
		"addr", addr,
		"resource", resourceURL,
		"auth_server", authServerURL,
		"introspection_url", introspectionURL,
		"required_scopes", scopes,
	)

	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	slog.Info("shutdown signal received, shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("server forced to shutdown", "err", err)
		return
	}
	slog.Info("server shutdown gracefully")
}
