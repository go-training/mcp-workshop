// Package main implements an MCP resource server for the Authorization Code
// + PKCE flow whose Bearer-token validation goes through the RFC 7662 token
// introspection endpoint on the upstream authorization server.
//
// This is the sibling of ../oauth-server/ (which validates locally via JWKS).
// Use this variant when the deployment policy requires that every token check
// hit the AS — e.g. so revocations propagate immediately — at the cost of
// one extra HTTP round-trip per request.
//
// Why not use github.com/go-signet/sdk-go/middleware.BearerAuth directly?
// The SDK's IntrospectionResult does not surface the `aud` claim, so a
// middleware.BearerAuth pipeline cannot enforce the RFC 8707 resource binding
// that this module requires (the plan calls this out explicitly). We use the
// SDK's discovery package to resolve the introspection endpoint and then
// POST to it with our own response struct that includes aud, finally
// adapting to modelcontextprotocol/go-sdk's auth.TokenInfo.
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
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/go-training/mcp-workshop/pkg/logger"

	"github.com/go-signet/sdk-go/discovery"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

type introspector struct {
	endpoint               string
	clientID               string
	clientSecret           string
	expectedAudience       string
	requireResourceBinding bool
	httpClient             *http.Client
}

// audClaim accepts the RFC 7662 §2.2 `aud` shape — either a single string or
// an array of strings — and normalises it to a slice for comparison.
type audClaim []string

func (a *audClaim) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*a = audClaim{s}
		return nil
	}
	var arr []string
	if err := json.Unmarshal(data, &arr); err != nil {
		return fmt.Errorf("aud claim must be string or []string: %w", err)
	}
	*a = arr
	return nil
}

type introspectionResponse struct {
	Active   bool     `json:"active"`
	Scope    string   `json:"scope,omitempty"`
	ClientID string   `json:"client_id,omitempty"`
	Username string   `json:"username,omitempty"`
	Sub      string   `json:"sub,omitempty"`
	Iss      string   `json:"iss,omitempty"`
	TokenTyp string   `json:"token_type,omitempty"`
	Exp      int64    `json:"exp,omitempty"`
	Aud      audClaim `json:"aud,omitempty"`
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
	slog.DebugContext(ctx, "introspection response",
		"active", body.Active,
		"client_id", body.ClientID,
		"sub", body.Sub,
		"iss", body.Iss,
		"scope", body.Scope,
		"aud", body.Aud,
		"exp", body.Exp,
	)
	if !body.Active {
		return nil, fmt.Errorf("%w: token is not active", auth.ErrInvalidToken)
	}

	if err := i.checkAudience(ctx, body.Aud); err != nil {
		return nil, err
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
	if body.Iss != "" {
		info.Extra["iss"] = body.Iss
	}
	if len(body.Aud) > 0 {
		info.Extra["aud"] = []string(body.Aud)
	}
	switch {
	case body.Sub != "":
		info.UserID = body.Sub
		info.Extra["sub"] = body.Sub
	case body.Username != "":
		info.UserID = body.Username
	}
	if body.Username != "" {
		info.Extra["username"] = body.Username
	}
	return info, nil
}

// buildResourceMetadataURL anchors the RFC 9728 metadata URL to the public
// resource URL so a deployment with `-resource https://mcp.example.com/mcp`
// does not advertise an unreachable `http://localhost...` discovery hint.
func buildResourceMetadataURL(resourceURL, metadataPath string) (string, error) {
	u, err := url.Parse(resourceURL)
	if err != nil {
		return "", err
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("resource URL %q missing scheme or host", resourceURL)
	}
	return u.Scheme + "://" + u.Host + metadataPath, nil
}

// checkAudience enforces the RFC 8707 resource binding contract. Signet's
// introspection endpoint surfaces the JWT `aud` claim verbatim, so we can
// reject tokens minted for a different MCP resource even when the user
// happened to consent on the same authorization server.
func (i *introspector) checkAudience(ctx context.Context, aud audClaim) error {
	if i.expectedAudience == "" {
		return nil
	}
	if len(aud) == 0 {
		if i.requireResourceBinding {
			slog.WarnContext(ctx, "audience missing or unbound",
				"expected_aud", i.expectedAudience)
			return fmt.Errorf(
				"%w: token has no aud claim but resource binding is required",
				auth.ErrInvalidToken,
			)
		}
		slog.WarnContext(ctx, "token accepted without aud claim",
			"expected_aud", i.expectedAudience,
			"hint", "set -require-resource-binding=true to reject")
		return nil
	}
	if slices.Contains([]string(aud), i.expectedAudience) {
		slog.InfoContext(ctx, "audience verified",
			"expected_aud", i.expectedAudience,
			"got_aud", aud)
		return nil
	}
	slog.WarnContext(ctx, "audience mismatch",
		"expected_aud", i.expectedAudience,
		"got_aud", aud)
	return fmt.Errorf("%w: aud claim %v does not match %q",
		auth.ErrInvalidToken, aud, i.expectedAudience)
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
		requireResourceBinding bool
		discoveryTO            time.Duration
		introspectTO           time.Duration
		logLevel               string
	)
	flag.StringVar(&addr, "addr", ":8095", "address to listen on")
	flag.StringVar(&resourceURL, "resource", "",
		"public URL of this MCP resource (defaults to http://localhost<addr>/mcp)")
	flag.StringVar(&authServerURL, "auth-server", "http://localhost:8080",
		"issuer URL of the external OAuth 2.0 authorization server (e.g. Signet)")
	flag.StringVar(&introspectionURL, "introspection-url", "",
		"RFC 7662 introspection endpoint (default: OIDC discovery from -auth-server)")
	flag.StringVar(&introspectClientID, "introspect-client-id", "",
		"client_id this resource server uses to call the introspection endpoint (required)")
	flag.StringVar(&introspectClientSecret, "introspect-client-secret", "",
		"client_secret this resource server uses to call the introspection endpoint (required)")
	flag.StringVar(&requiredScopes, "required-scopes", "",
		"space-separated scopes an access token must contain to reach /mcp "+
			"(empty = no scope check)")
	flag.BoolVar(&requireResourceBinding, "require-resource-binding", true,
		"reject tokens whose introspection response has no aud claim")
	flag.DurationVar(&discoveryTO, "discovery-timeout", 15*time.Second,
		"timeout for the OIDC discovery call at startup")
	flag.DurationVar(&introspectTO, "introspect-timeout", 5*time.Second,
		"per-request timeout for the introspection HTTP call")
	flag.StringVar(&logLevel, "log-level", "INFO", "log level: DEBUG, INFO, WARN, ERROR")
	flag.Parse()

	logger.NewWithLevel(logLevel)

	if introspectClientID == "" || introspectClientSecret == "" {
		slog.Error("introspect-client-id and introspect-client-secret are required")
		os.Exit(1)
	}
	if resourceURL == "" {
		resourceURL = "http://localhost" + addr + "/mcp"
	}

	if introspectionURL == "" {
		discoveryCtx, cancelDiscovery := context.WithTimeout(context.Background(), discoveryTO)
		discoClient, err := discovery.NewClient(authServerURL)
		if err != nil {
			slog.Error("discovery client init failed", "err", err)
			os.Exit(1)
		}
		meta, err := discoClient.Fetch(discoveryCtx)
		cancelDiscovery()
		if err != nil {
			slog.Error("OIDC discovery failed — is the authorization server running?",
				"auth_server", authServerURL, "err", err)
			os.Exit(1)
		}
		introspectionURL = meta.Endpoints().IntrospectionURL
		if introspectionURL == "" {
			slog.Error("authorization server did not advertise an introspection endpoint",
				"auth_server", authServerURL)
			os.Exit(1)
		}
		slog.Info("discovered introspection endpoint",
			"auth_server", authServerURL, "introspection_url", introspectionURL)
	}

	scopes := strings.Fields(requiredScopes)

	ins := &introspector{
		endpoint:               introspectionURL,
		clientID:               introspectClientID,
		clientSecret:           introspectClientSecret,
		expectedAudience:       resourceURL,
		requireResourceBinding: requireResourceBinding,
		httpClient:             &http.Client{Timeout: introspectTO},
	}

	resourceMetadataPath := "/.well-known/oauth-protected-resource"
	resourceMetadataURL, err := buildResourceMetadataURL(resourceURL, resourceMetadataPath)
	if err != nil {
		slog.Error("invalid -resource URL", "resource", resourceURL, "err", err)
		os.Exit(1)
	}

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

	slog.Info("dcr introspect MCP server starting",
		"addr", addr,
		"resource", resourceURL,
		"auth_server", authServerURL,
		"introspection_url", introspectionURL,
		"required_scopes", scopes,
		"require_resource_binding", requireResourceBinding,
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
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("server forced to shutdown", "err", err)
		return
	}
	slog.Info("server shutdown gracefully")
}
