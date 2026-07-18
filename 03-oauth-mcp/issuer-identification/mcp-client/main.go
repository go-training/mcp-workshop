//go:build !windows

// Package main is the MCP OAuth client for the RFC 9207 mix-up demonstration.
// It discovers an authorization server via RFC 8414 metadata, runs a
// hand-rolled Authorization Code + PKCE flow against it, and — when started
// with -defense — validates the RFC 9207 `iss` authorization-response
// parameter before it ever sends the code to a token endpoint.
//
// Why hand-rolled: go-sdk v1.6.1's auth.AuthorizationResult exposes only Code
// and State — there is no Iss field and the SDK does not validate RFC 9207 for
// you. RFC 9207 support is a client responsibility today, so this sample reads
// the `iss` query parameter itself and calls validateIssuerResponse. That gap
// is the lesson.
//
// Run it two ways:
//
//	-auth-server <AuthGate>  : honest happy path; iss matches; reaches mcp-server
//	-auth-server <evil-as>   : mix-up; without -defense the code is stolen,
//	                           with -defense the iss mismatch aborts the flow.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/go-training/mcp-workshop/pkg/logger"

	"github.com/go-authgate/sdk-go/authflow"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

func main() {
	if err := run(); err != nil {
		slog.Error("client failed", "err", err)
		os.Exit(1)
	}
}

type config struct {
	authServer   string
	mcpURL       string
	resource     string
	clientID     string
	clientSecret string
	scopes       []string
	callbackPort int
	defense      bool
	connect      bool
}

func parseFlags() *config {
	var (
		authServer   string
		mcpURL       string
		resource     string
		clientID     string
		clientSecret string
		scopes       string
		callbackPort int
		defense      bool
		connect      bool
		logLevel     string
	)
	flag.StringVar(&authServer, "auth-server", "http://localhost:8080",
		"authorization server issuer URL to trust and discover "+
			"(AuthGate for the honest path, evil-as for the mix-up)")
	flag.StringVar(&mcpURL, "mcp-url", "http://localhost:8095/mcp",
		"MCP streamable HTTP endpoint reached on the happy path")
	flag.StringVar(&resource, "resource", "",
		"RFC 8707 resource indicator (default: -mcp-url)")
	flag.StringVar(&clientID, "client_id", "",
		"OAuth client_id registered with the honest AS (required)")
	flag.StringVar(&clientSecret, "client_secret", "",
		"OAuth client_secret (omit for public clients — PKCE is always used)")
	flag.StringVar(&scopes, "scopes", "openid profile email",
		"space-separated scopes to request")
	flag.IntVar(&callbackPort, "callback-port", 8085,
		"local TCP port for the OAuth callback server")
	flag.BoolVar(&defense, "defense", false,
		"enable RFC 9207 iss validation (the defense against the mix-up attack)")
	flag.BoolVar(&connect, "connect", true,
		"after obtaining a token, connect to -mcp-url and call who_am_i")
	flag.StringVar(&logLevel, "log-level", "INFO", "log level: DEBUG, INFO, WARN, ERROR")
	flag.Parse()

	logger.NewWithLevel(logLevel)

	if clientID == "" {
		slog.Error("client_id is required")
		os.Exit(2)
	}
	if resource == "" {
		resource = mcpURL
	}

	return &config{
		authServer:   strings.TrimRight(authServer, "/"),
		mcpURL:       mcpURL,
		resource:     resource,
		clientID:     clientID,
		clientSecret: clientSecret,
		scopes:       strings.Fields(scopes),
		callbackPort: callbackPort,
		defense:      defense,
		connect:      connect,
	}
}

// validateIssuerResponse implements the RFC 9207 client check that go-sdk
// v1.6.1 does not provide. expectedIssuer is the `issuer` from the AS metadata
// the client discovered; iss is the value returned on the authorization
// response; issParameterSupported is the AS's advertised
// authorization_response_iss_parameter_supported flag. The comparison is
// byte-for-byte per RFC 9207 §2.4.
func validateIssuerResponse(iss, expectedIssuer string, issParameterSupported bool) error {
	if issParameterSupported {
		if iss == "" {
			return fmt.Errorf(
				"issuer identification required but authorization response carried no iss "+
					"(expected %q)", expectedIssuer)
		}
		if iss != expectedIssuer {
			return fmt.Errorf(
				"issuer mismatch: got %q want %q — aborting", iss, expectedIssuer)
		}
		return nil
	}
	// The AS does not advertise RFC 9207 support. A conforming AS then must not
	// send iss; if one appears, the response is not trustworthy.
	if iss != "" {
		return fmt.Errorf(
			"authorization response carried iss %q but the AS does not advertise "+
				"issuer identification support — aborting", iss)
	}
	return nil
}

func run() error {
	cfg := parseFlags()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// RFC 8414 discovery of the AS the client was told to trust.
	metaURL := cfg.authServer + "/.well-known/oauth-authorization-server"
	meta, err := oauthex.GetAuthServerMeta(ctx, metaURL, cfg.authServer, http.DefaultClient)
	if err != nil {
		return fmt.Errorf("discover authorization server %s: %w", cfg.authServer, err)
	}
	if meta == nil || meta.AuthorizationEndpoint == "" || meta.TokenEndpoint == "" {
		return fmt.Errorf("AS metadata missing authorize/token endpoint: %+v", meta)
	}

	// go-sdk's AuthServerMeta does not surface RFC 9207's
	// authorization_response_iss_parameter_supported flag, so read it ourselves.
	issSupported := fetchIssParameterSupported(ctx, metaURL)

	slog.Info("discovered authorization server",
		"issuer", meta.Issuer,
		"authorize", meta.AuthorizationEndpoint,
		"token", meta.TokenEndpoint,
		"iss_parameter_supported", issSupported,
		"defense_enabled", cfg.defense,
	)
	if !cfg.defense {
		slog.Warn("RFC 9207 iss validation is OFF — the client will trust any AS that " +
			"returns a code, which is exactly what the mix-up attack exploits")
	}

	token, err := runAuthCodeFlow(ctx, cfg, meta, issSupported)
	if err != nil {
		return fmt.Errorf("auth code flow: %w", err)
	}
	slog.Info("obtained access token", "token_type", token.TokenType,
		"expires_in_s", token.ExpiresIn)

	if !cfg.connect {
		slog.Info("connect disabled — stopping after token exchange")
		return nil
	}
	// Use a fresh deadline for the MCP call: the parent ctx's 5-minute budget
	// has been ticking during the interactive browser login, so reusing it here
	// could spuriously fail the tool call with "context deadline exceeded" even
	// though we already hold a valid token.
	mcpCtx, mcpCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer mcpCancel()
	if err := callMCP(mcpCtx, cfg, token); err != nil {
		return fmt.Errorf("call MCP: %w", err)
	}
	slog.Info("client run complete")
	return nil
}

// fetchIssParameterSupported reads the RFC 9207
// authorization_response_iss_parameter_supported flag directly from the AS
// metadata document. It defaults to false on any error so a missing flag is
// treated as "not supported".
func fetchIssParameterSupported(ctx context.Context, metadataURL string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metadataURL, nil)
	if err != nil {
		return false
	}
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Warn("could not fetch AS metadata for iss flag", "err", err)
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		slog.Warn("AS metadata request for iss flag returned non-200 — treating as unsupported",
			"status", resp.StatusCode)
		return false
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return false
	}
	var doc struct {
		IssParameterSupported bool `json:"authorization_response_iss_parameter_supported"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		slog.Warn("could not parse AS metadata for iss flag", "err", err)
		return false
	}
	return doc.IssParameterSupported
}

// authResult carries the parameters returned on the authorization response
// callback, including the RFC 9207 iss the SDK's AuthorizationResult omits.
type authResult struct {
	code string
	iss  string
}

func runAuthCodeFlow(
	ctx context.Context,
	cfg *config,
	meta *oauthex.AuthServerMeta,
	issSupported bool,
) (*tokenResponse, error) {
	pkce, err := authflow.NewPKCE()
	if err != nil {
		return nil, fmt.Errorf("generate PKCE: %w", err)
	}
	state, err := randomState()
	if err != nil {
		return nil, err
	}

	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", cfg.callbackPort))
	if err != nil {
		return nil, fmt.Errorf("listen on callback port %d: %w", cfg.callbackPort, err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", port)

	resultCh := make(chan authResult, 1)
	errCh := make(chan error, 2)
	var once sync.Once

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		handled := false
		once.Do(func() {
			handled = true
			q := r.URL.Query()
			if q.Get("state") != state {
				errCh <- errors.New("callback state mismatch")
				writeCallbackHTML(w, "Authentication failed: state mismatch.")
				return
			}
			if e := q.Get("error"); e != "" {
				desc := q.Get("error_description")
				errCh <- fmt.Errorf("authorization error: %s (%s)", e, desc)
				writeCallbackHTML(w, "Authentication failed: "+e)
				return
			}
			code := q.Get("code")
			if code == "" {
				errCh <- errors.New("callback missing code")
				writeCallbackHTML(w, "Authentication failed: no code.")
				return
			}
			resultCh <- authResult{code: code, iss: q.Get("iss")}
			writeCallbackHTML(w, "Authentication successful. You can close this window.")
		})
		if !handled {
			writeCallbackHTML(w, "Already processed. You can close this window.")
		}
	})

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if serveErr := srv.Serve(listener); serveErr != nil &&
			!errors.Is(serveErr, http.ErrServerClosed) {
			select {
			case errCh <- fmt.Errorf("callback server: %w", serveErr):
			default:
			}
		}
	}()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			_ = srv.Close()
		}
	}()

	authURL := buildAuthorizeURL(
		meta.AuthorizationEndpoint,
		cfg,
		redirectURI,
		state,
		pkce.Challenge,
	)
	slog.Info("opening browser for authorization", "url", authURL)
	if err := openBrowser(authURL); err != nil {
		slog.Warn("could not open browser automatically — open this URL manually",
			"url", authURL, "err", err)
	}

	var res authResult
	select {
	case res = <-resultCh:
	case err := <-errCh:
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	slog.Info("authorization response received at callback",
		"expected_issuer", meta.Issuer,
		"received_iss", res.iss,
		"iss_parameter_supported", issSupported,
	)

	// RFC 9207 checkpoint. This is the whole point of the sample: with -defense
	// on we validate the iss before the code leaves the client. With it off we
	// skip straight to the token endpoint the client "thinks" it is talking to
	// — which, in the mix-up, is the attacker's.
	if cfg.defense {
		if err := validateIssuerResponse(res.iss, meta.Issuer, issSupported); err != nil {
			return nil, fmt.Errorf("RFC 9207 issuer validation failed: %w", err)
		}
		slog.Info("iss OK — issuer matches the discovered authorization server",
			"iss", res.iss)
	} else {
		slog.Warn("skipping RFC 9207 validation (defense off) — posting code to the "+
			"discovered token endpoint regardless of who really issued it",
			"token_endpoint", meta.TokenEndpoint)
	}

	return exchangeCode(ctx, cfg, meta.TokenEndpoint, res.code, redirectURI, pkce.Verifier)
}

func buildAuthorizeURL(
	endpoint string,
	cfg *config,
	redirectURI, state, codeChallenge string,
) string {
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {cfg.clientID},
		"redirect_uri":          {redirectURI},
		"scope":                 {strings.Join(cfg.scopes, " ")},
		"state":                 {state},
		"code_challenge":        {codeChallenge},
		"code_challenge_method": {"S256"},
		"resource":              {cfg.resource},
	}
	sep := "?"
	if strings.Contains(endpoint, "?") {
		sep = "&"
	}
	return endpoint + sep + q.Encode()
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope,omitempty"`
	IDToken      string `json:"id_token,omitempty"`
}

type oauthErrorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description,omitempty"`
}

func exchangeCode(
	ctx context.Context,
	cfg *config,
	tokenEndpoint, code, redirectURI, codeVerifier string,
) (*tokenResponse, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {cfg.clientID},
		"code_verifier": {codeVerifier},
		"resource":      {cfg.resource},
	}
	if cfg.clientSecret != "" {
		form.Set("client_secret", cfg.clientSecret)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint,
		strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	httpClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read token response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		var oerr oauthErrorResponse
		if jerr := json.Unmarshal(body, &oerr); jerr == nil && oerr.Error != "" {
			return nil, fmt.Errorf("token endpoint %d: %s (%s)",
				resp.StatusCode, oerr.Error, oerr.ErrorDescription)
		}
		return nil, fmt.Errorf("token endpoint %d: %s", resp.StatusCode, body)
	}

	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}
	if tr.AccessToken == "" {
		return nil, fmt.Errorf("token response had no access_token: %s", body)
	}
	return &tr, nil
}

// bearerRoundTripper attaches a fixed Bearer token to every request.
type bearerRoundTripper struct {
	base  http.RoundTripper
	token string
}

func (b *bearerRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	req := r.Clone(r.Context())
	req.Header.Set("Authorization", "Bearer "+b.token)
	return b.base.RoundTrip(req)
}

func callMCP(ctx context.Context, cfg *config, token *tokenResponse) error {
	httpClient := &http.Client{
		Transport: &bearerRoundTripper{base: http.DefaultTransport, token: token.AccessToken},
		Timeout:   30 * time.Second,
	}

	transport := &mcp.StreamableClientTransport{
		Endpoint:   cfg.mcpURL,
		HTTPClient: httpClient,
	}
	client := mcp.NewClient(&mcp.Implementation{
		Name:    "issuer-identification-client",
		Version: "1.0.0",
	}, nil)

	slog.Info("connecting to MCP server", "url", cfg.mcpURL)
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer session.Close()

	slog.Info("connected",
		"server_name", session.InitializeResult().ServerInfo.Name,
		"server_version", session.InitializeResult().ServerInfo.Version,
	)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "who_am_i"})
	if err != nil {
		return fmt.Errorf("call who_am_i: %w", err)
	}
	printToolResult("who_am_i", res)
	// A transport-level success can still carry a tool-level error result; surface
	// it so the process exits non-zero instead of reporting a clean run.
	if res.IsError {
		return errors.New("who_am_i returned an error result")
	}
	return nil
}

func printToolResult(name string, r *mcp.CallToolResult) {
	if r.IsError {
		slog.Error("tool reported error", "tool", name)
	}
	for _, c := range r.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			slog.Info("tool text content", "tool", name, "text", tc.Text)
		}
	}
	if r.StructuredContent != nil {
		b, _ := json.MarshalIndent(r.StructuredContent, "", "  ")
		slog.Info("tool structured content", "tool", name, "json", string(b))
	}
}

func randomState() (string, error) {
	pkce, err := authflow.NewPKCE()
	if err != nil {
		return "", fmt.Errorf("generate state: %w", err)
	}
	return pkce.Verifier, nil
}

func openBrowser(rawURL string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", rawURL).Start()
	case "linux":
		return exec.Command("xdg-open", rawURL).Start()
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

func writeCallbackHTML(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, "<html><body><h1>%s</h1><script>window.close();</script></body></html>",
		html.EscapeString(msg))
}
