// Package main implements an MCP resource server for the Authorization Code
// + PKCE flow. It validates incoming Bearer tokens locally via JWKS using
// github.com/go-signet/sdk-go/jwksauth and delegates all OAuth issuance to
// an external authorization server (e.g. Signet).
//
// This binary intentionally exposes no /authorize, /token, or /register
// endpoints — those live on the upstream authorization server. The only
// OAuth-flavoured surface here is RFC 9728 Protected Resource Metadata,
// which points clients at that upstream AS.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/go-training/mcp-workshop/pkg/logger"

	"github.com/go-signet/sdk-go/jwksauth"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

// accessTokenType is the value the JWT `type` claim must carry for Signet
// access tokens. Refresh JWTs presented as Bearer would otherwise pass
// signature, iss, aud, and exp checks unchanged — the SDK does not enforce
// this distinction, and its parsed Claims struct does not surface `type`.
const accessTokenType = "access"

type jwksVerifier struct {
	verifier         *jwksauth.Verifier
	expectedAudience string
}

type rawTokenType struct {
	Type string `json:"type"`
}

func (j *jwksVerifier) Verify(
	ctx context.Context,
	token string,
	_ *http.Request,
) (*auth.TokenInfo, error) {
	info, err := j.verifier.Verify(ctx, token)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", auth.ErrInvalidToken, err)
	}

	var typ rawTokenType
	if err := info.IDToken.Claims(&typ); err != nil {
		return nil, fmt.Errorf("%w: decode type claim: %w",
			auth.ErrInvalidToken, err)
	}
	if typ.Type != accessTokenType {
		slog.WarnContext(ctx, "non-access token rejected",
			"got_type", typ.Type, "subject", info.Subject)
		return nil, fmt.Errorf("%w: token type %q is not %q",
			auth.ErrInvalidToken, typ.Type, accessTokenType)
	}

	if err := checkAudience(ctx, j.expectedAudience, info.Audience); err != nil {
		return nil, err
	}

	slog.DebugContext(ctx, "jwt verified",
		"iss", info.Issuer,
		"sub", info.Subject,
		"aud", info.Audience,
		"exp", info.Expiry,
		"client_id", info.Claims.ClientID,
		"uid", info.Claims.UID,
		"domain", info.Claims.Domain,
		"scopes", info.Scopes,
		"extras", info.Claims.Extras,
	)

	out := &auth.TokenInfo{
		Scopes:     info.Scopes,
		Expiration: info.Expiry,
		UserID:     info.Subject,
		Extra: map[string]any{
			"aud": info.Audience,
			"iss": info.Issuer,
		},
	}
	if info.Claims.ClientID != "" {
		out.Extra["client_id"] = info.Claims.ClientID
	}
	if info.Claims.UID != "" {
		out.Extra["uid"] = info.Claims.UID
	}
	if info.Claims.Domain != "" {
		out.Extra["domain"] = info.Claims.Domain
	}
	if info.Claims.Project != "" {
		out.Extra["project"] = info.Claims.Project
	}
	if info.Claims.ServiceAccount != "" {
		out.Extra["service_account"] = info.Claims.ServiceAccount
	}
	maps.Copy(out.Extra, info.Claims.Extras)
	return out, nil
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

func checkAudience(ctx context.Context, expected string, got []string) error {
	if expected == "" {
		return nil
	}
	if slices.Contains(got, expected) {
		slog.InfoContext(ctx, "audience verified",
			"expected_aud", expected, "got_aud", got)
		return nil
	}
	slog.WarnContext(ctx, "audience mismatch",
		"expected_aud", expected, "got_aud", got)
	return fmt.Errorf("%w: aud claim %v does not match %q",
		auth.ErrInvalidToken, got, expected)
}

func main() {
	var (
		addr           string
		resourceURL    string
		authServerURL  string
		requiredScopes string
		claimPrefix    string
		discoveryTO    time.Duration
		verifyTO       time.Duration
		logLevel       string
	)
	flag.StringVar(&addr, "addr", ":8095", "address to listen on")
	flag.StringVar(&resourceURL, "resource", "",
		"public URL of this MCP resource (defaults to http://localhost<addr>/mcp); "+
			"also the audience this verifier requires in the JWT's aud claim")
	flag.StringVar(&authServerURL, "auth-server", "http://localhost:8080",
		"issuer URL of the external OAuth 2.0 authorization server (e.g. Signet)")
	flag.StringVar(&requiredScopes, "required-scopes", "",
		"space-separated scopes an access token must contain to reach /mcp "+
			"(empty = no scope check)")
	flag.StringVar(&claimPrefix, "private-claim-prefix", "extra",
		"Signet JWT_PRIVATE_CLAIM_PREFIX — must match the issuer's setting")
	flag.DurationVar(&discoveryTO, "discovery-timeout", 15*time.Second,
		"timeout for the OIDC discovery call at startup")
	flag.DurationVar(&verifyTO, "verify-timeout", 5*time.Second,
		"per-request timeout for JWT verification (bounds JWKS fetch on cache miss)")
	flag.StringVar(&logLevel, "log-level", "INFO", "log level: DEBUG, INFO, WARN, ERROR")
	flag.Parse()

	logger.NewWithLevel(logLevel)

	if resourceURL == "" {
		resourceURL = "http://localhost" + addr + "/mcp"
	}

	scopes := strings.Fields(requiredScopes)

	discoveryCtx, cancelDiscovery := context.WithTimeout(context.Background(), discoveryTO)
	verifier, err := jwksauth.NewVerifier(discoveryCtx, authServerURL, resourceURL,
		jwksauth.WithPrivateClaimPrefix(claimPrefix),
		jwksauth.WithDiscoveryTimeout(discoveryTO),
		jwksauth.WithVerifyTimeout(verifyTO),
	)
	cancelDiscovery()
	if err != nil {
		slog.Error("OIDC discovery failed — is the authorization server running?",
			"auth_server", authServerURL, "err", err)
		os.Exit(1)
	}

	adapter := &jwksVerifier{verifier: verifier, expectedAudience: resourceURL}

	resourceMetadataPath := "/.well-known/oauth-protected-resource"
	resourceMetadataURL, err := buildResourceMetadataURL(resourceURL, resourceMetadataPath)
	if err != nil {
		slog.Error("invalid -resource URL", "resource", resourceURL, "err", err)
		os.Exit(1)
	}

	authMiddleware := auth.RequireBearerToken(adapter.Verify, &auth.RequireBearerTokenOptions{
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

	slog.Info("dcr JWKS MCP server starting",
		"addr", addr,
		"resource", resourceURL,
		"auth_server", authServerURL,
		"issuer", verifier.Issuer(),
		"required_scopes", scopes,
		"private_claim_prefix", claimPrefix,
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
