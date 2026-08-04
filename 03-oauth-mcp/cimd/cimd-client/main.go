//go:build !windows

// Package main is the MCP OAuth client for the CIMD (Client ID Metadata
// Document) sample. Instead of pre-registering with the authorization server
// or using Dynamic Client Registration, the client IS its registration: it
// hosts a small JSON document over HTTPS whose URL doubles as the OAuth
// client_id. Signet fetches that document during the authorization request,
// materializes a shadow client from it, and runs a normal Authorization Code
// + S256 PKCE flow.
//
// The binary therefore plays two roles at once:
//
//  1. client metadata origin — an HTTPS listener serving the document at
//     -cimd-url (mkcert provides a locally trusted certificate; Signet
//     requires the https scheme even for loopback testing, and needs
//     CIMD_ALLOW_PRIVATE_NETWORKS=true to fetch a loopback origin at all)
//  2. MCP OAuth client — RFC 8414 discovery, the authorization-code flow
//     with the CIMD URL as client_id, RFC 9207 iss validation, and finally a
//     Bearer-authenticated who_am_i call against cimd-server.
package main

import (
	"bytes"
	"context"
	"crypto/tls"
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

	"github.com/go-signet/sdk-go/authflow"
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
	cimdAddr     string
	cimdURL      string
	certFile     string
	keyFile      string
	clientName   string
	scopes       []string
	callbackPort int
	connect      bool
}

func parseFlags() *config {
	var (
		authServer   string
		mcpURL       string
		resource     string
		cimdAddr     string
		cimdURL      string
		certFile     string
		keyFile      string
		clientName   string
		scopes       string
		callbackPort int
		connect      bool
		logLevel     string
	)
	flag.StringVar(&authServer, "auth-server", "http://localhost:8080",
		"authorization server issuer URL (Signet, with CIMD_ENABLED=true)")
	flag.StringVar(&mcpURL, "mcp-url", "http://localhost:8095/mcp",
		"MCP streamable HTTP endpoint to call after obtaining a token")
	flag.StringVar(&resource, "resource", "",
		"RFC 8707 resource indicator (default: -mcp-url); must appear "+
			"byte-for-byte in Signet's CIMD_ALLOWED_RESOURCES")
	flag.StringVar(&cimdAddr, "cimd-addr", ":9443",
		"HTTPS listen address for the client metadata origin")
	flag.StringVar(&cimdURL, "cimd-url", "https://localhost:9443/oauth/client.json",
		"public URL of the client metadata document — this IS the OAuth client_id; "+
			"must be https and reachable by Signet")
	flag.StringVar(&certFile, "cert", "localhost.pem",
		"TLS certificate for the metadata origin (mkcert localhost)")
	flag.StringVar(&keyFile, "key", "localhost-key.pem",
		"TLS private key for the metadata origin (mkcert localhost)")
	flag.StringVar(&clientName, "client-name", "CIMD Workshop Client",
		"client_name in the metadata document (display only — the consent page "+
			"identifies the client by the document's domain, not this string)")
	flag.StringVar(&scopes, "scopes", "openid profile email",
		"space-separated scopes; Signet intersects CIMD scopes with its "+
			"user-safe set (openid profile email offline_access)")
	flag.IntVar(&callbackPort, "callback-port", 8085,
		"local TCP port for the OAuth callback server; the resulting redirect "+
			"URI is baked into the metadata document, so it must be a fixed port")
	flag.BoolVar(&connect, "connect", true,
		"after obtaining a token, connect to -mcp-url and call who_am_i")
	flag.StringVar(&logLevel, "log-level", "INFO", "log level: DEBUG, INFO, WARN, ERROR")
	flag.Parse()

	logger.NewWithLevel(logLevel)

	if resource == "" {
		resource = mcpURL
	}
	// The redirect URI is baked into the published document and compared by
	// exact match, so an ephemeral port (0) would advertise
	// http://127.0.0.1:0/callback while the listener binds somewhere else.
	if callbackPort < 1 || callbackPort > 65535 {
		slog.Error("callback-port must be a fixed port in 1..65535 — it is baked "+
			"into the metadata document as the redirect URI", "callback_port", callbackPort)
		os.Exit(2)
	}

	return &config{
		authServer:   strings.TrimRight(authServer, "/"),
		mcpURL:       mcpURL,
		resource:     resource,
		cimdAddr:     cimdAddr,
		cimdURL:      cimdURL,
		certFile:     certFile,
		keyFile:      keyFile,
		clientName:   clientName,
		scopes:       strings.Fields(scopes),
		callbackPort: callbackPort,
		connect:      connect,
	}
}

func run() error {
	cfg := parseFlags()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", cfg.callbackPort)

	// The document must exist before the browser opens: Signet fetches the
	// client_id URL while handling the authorization request.
	doc, err := buildClientMetadata(cfg.cimdURL, cfg.clientName, redirectURI, cfg.scopes)
	if err != nil {
		return err
	}
	if err := validateOriginPort(cfg.cimdAddr, cfg.cimdURL); err != nil {
		return err
	}
	stopOrigin, err := startMetadataOrigin(cfg, doc)
	if err != nil {
		return err
	}
	defer stopOrigin()

	if err := preflightMetadata(ctx, cfg.cimdURL, doc); err != nil {
		return fmt.Errorf("metadata self-check failed — Signet would hit the same "+
			"error when it fetches the client_id URL: %w", err)
	}
	slog.Info("client metadata document published",
		"client_id", cfg.cimdURL, "redirect_uri", redirectURI)

	// RFC 8414 discovery of the AS the client was told to trust.
	metaURL := cfg.authServer + "/.well-known/oauth-authorization-server"
	meta, err := oauthex.GetAuthServerMeta(ctx, metaURL, cfg.authServer, http.DefaultClient)
	if err != nil {
		return fmt.Errorf("discover authorization server %s: %w", cfg.authServer, err)
	}
	if meta == nil || meta.AuthorizationEndpoint == "" || meta.TokenEndpoint == "" {
		return fmt.Errorf("AS metadata missing authorize/token endpoint: %+v", meta)
	}
	// Without this capability the AS would treat the URL-shaped client_id as an
	// unknown regular client; fail before the browser round-trip.
	if !meta.ClientIDMetadataDocumentSupported {
		return fmt.Errorf("authorization server %s does not advertise "+
			"client_id_metadata_document_supported — enable CIMD_ENABLED=true on "+
			"Signet or use a pre-registered client instead", cfg.authServer)
	}

	slog.Info("discovered authorization server",
		"issuer", meta.Issuer,
		"authorize", meta.AuthorizationEndpoint,
		"token", meta.TokenEndpoint,
		"cimd_supported", meta.ClientIDMetadataDocumentSupported,
		"iss_parameter_supported", meta.AuthorizationResponseIssParameterSupported,
	)

	token, err := runAuthCodeFlow(ctx, cfg, meta, redirectURI)
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
	// has been ticking during the interactive browser login.
	mcpCtx, mcpCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer mcpCancel()
	if err := callMCP(mcpCtx, cfg, token); err != nil {
		return fmt.Errorf("call MCP: %w", err)
	}
	slog.Info("client run complete")
	return nil
}

// startMetadataOrigin serves the metadata document over HTTPS at the path of
// cfg.cimdURL and returns a shutdown func. Signet refuses non-https client_ids
// outright, so unlike the callback server this listener cannot fall back to
// plain HTTP.
func startMetadataOrigin(cfg *config, doc []byte) (func(), error) {
	u, err := url.Parse(cfg.cimdURL)
	if err != nil {
		return nil, fmt.Errorf("parse CIMD URL: %w", err)
	}
	// Load the key pair up front instead of letting ListenAndServeTLS discover a
	// missing or mismatched file asynchronously: this validates the certificate,
	// the key, and that the two belong together, all before anything is served.
	cert, err := tls.LoadX509KeyPair(cfg.certFile, cfg.keyFile)
	if err != nil {
		return nil, fmt.Errorf("load TLS key pair (cert %q, key %q) — generate one with "+
			"`mkcert localhost` (and run `mkcert -install` once): %w",
			cfg.certFile, cfg.keyFile, err)
	}

	srv := &http.Server{
		Handler:           metadataHandler(u.Path, doc),
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		},
	}

	// Bind synchronously so "address already in use" is reported here, with the
	// address in the message, rather than surfacing moments later as a confusing
	// preflight "connection refused".
	listener, err := net.Listen("tcp", cfg.cimdAddr)
	if err != nil {
		return nil, fmt.Errorf("listen on %s for the metadata origin: %w", cfg.cimdAddr, err)
	}
	go func() {
		if serveErr := srv.ServeTLS(listener, "", ""); serveErr != nil &&
			!errors.Is(serveErr, http.ErrServerClosed) {
			slog.Error("metadata origin stopped serving",
				"addr", cfg.cimdAddr, "err", serveErr)
		}
	}()

	slog.Info("metadata origin listening", "addr", cfg.cimdAddr, "path", u.Path)
	return func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			_ = srv.Close()
		}
	}, nil
}

// preflightMetadata performs the CIMD doc's own verification step against the
// published URL: a direct 200, no redirects, and the exact document this run
// built. Comparing the full body rather than just client_id also pins
// redirect_uris and scope, so a cache or another process answering this URL is
// caught here instead of after the browser round-trip. It uses the system trust
// store like Signet does, so an untrusted certificate fails here first with an
// actionable message.
func preflightMetadata(ctx context.Context, cimdURL string, want []byte) error {
	client := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cimdURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("fetch %s (is the certificate trusted? run `mkcert -install`): %w",
			cimdURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s returned %d, want 200 with no redirect",
			cimdURL, resp.StatusCode)
	}

	// Signet caps the document at 64 KiB; read one byte past that so an
	// oversized document is reported as a size mismatch rather than truncated.
	body, err := io.ReadAll(io.LimitReader(resp.Body, (64<<10)+1))
	if err != nil {
		return fmt.Errorf("read metadata response: %w", err)
	}
	if !bytes.Equal(body, want) {
		return fmt.Errorf("the document served at %s is not the one this client just "+
			"published (got %d bytes, want %d) — another process or a cache is "+
			"answering this URL, so Signet would resolve a different client",
			cimdURL, len(body), len(want))
	}
	return nil
}

// validateIssuerResponse is the RFC 9207 client check: the iss returned on the
// authorization response must match the issuer discovered from AS metadata,
// byte-for-byte. Signet advertises authorization_response_iss_parameter_supported,
// so a missing iss is also a failure.
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

// authResult carries the parameters returned on the authorization response
// callback, including the RFC 9207 iss.
type authResult struct {
	code string
	iss  string
}

func runAuthCodeFlow(
	ctx context.Context,
	cfg *config,
	meta *oauthex.AuthServerMeta,
	redirectURI string,
) (*tokenResponse, error) {
	pkce, err := authflow.NewPKCE()
	if err != nil {
		return nil, fmt.Errorf("generate PKCE: %w", err)
	}
	state, err := randomState()
	if err != nil {
		return nil, err
	}

	// The port is fixed (not :0): the redirect URI inside the already-published
	// metadata document must match this listener exactly.
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", cfg.callbackPort))
	if err != nil {
		return nil, fmt.Errorf("listen on callback port %d: %w", cfg.callbackPort, err)
	}

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
	slog.Info("opening browser for authorization — Signet will now fetch the "+
		"metadata document to resolve the client", "url", authURL)
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

	// RFC 9207 checkpoint: never send the code to a token endpoint before the
	// issuer on the response has been verified.
	if err := validateIssuerResponse(
		res.iss, meta.Issuer, meta.AuthorizationResponseIssParameterSupported,
	); err != nil {
		return nil, fmt.Errorf("RFC 9207 issuer validation failed: %w", err)
	}
	slog.Info("iss OK — issuer matches the discovered authorization server",
		"iss", res.iss)

	return exchangeCode(ctx, cfg, meta.TokenEndpoint, res.code, redirectURI, pkce.Verifier)
}

func buildAuthorizeURL(
	endpoint string,
	cfg *config,
	redirectURI, state, codeChallenge string,
) string {
	q := url.Values{
		"response_type": {"code"},
		// The CIMD URL is the client_id — no registration happened before this.
		"client_id":             {cfg.cimdURL},
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
	// CIMD clients authenticate with PKCE only — there is no client_secret and
	// the document pins token_endpoint_auth_method to "none".
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {cfg.cimdURL},
		"code_verifier": {codeVerifier},
		"resource":      {cfg.resource},
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

// bearerRoundTripper attaches a fixed Bearer token to requests for the MCP
// resource this token was issued for.
//
// The origin check is not optional: net/http strips Authorization when a
// redirect crosses to another host, but that happens before the RoundTripper
// runs, so a transport that sets the header unconditionally re-adds it on every
// hop and would hand the access token to whatever host -mcp-url redirects to.
type bearerRoundTripper struct {
	base   http.RoundTripper
	token  string
	origin string // scheme://host the token may be sent to
}

func (b *bearerRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	if origin := r.URL.Scheme + "://" + r.URL.Host; origin != b.origin {
		slog.Warn("withholding bearer token: request origin differs from the MCP "+
			"endpoint (redirect to another host?)", "origin", origin, "expected", b.origin)
		return b.base.RoundTrip(r)
	}
	req := r.Clone(r.Context())
	req.Header.Set("Authorization", "Bearer "+b.token)
	return b.base.RoundTrip(req)
}

func callMCP(ctx context.Context, cfg *config, token *tokenResponse) error {
	mcpURL, err := url.Parse(cfg.mcpURL)
	if err != nil {
		return fmt.Errorf("parse MCP URL %q: %w", cfg.mcpURL, err)
	}
	httpClient := &http.Client{
		Transport: &bearerRoundTripper{
			base:   http.DefaultTransport,
			token:  token.AccessToken,
			origin: mcpURL.Scheme + "://" + mcpURL.Host,
		},
		// No Client.Timeout: it also bounds reading the response body, which
		// would tear down the streamable transport's long-lived SSE stream
		// mid-session. The call is bounded by ctx instead.
	}

	transport := &mcp.StreamableClientTransport{
		Endpoint:   cfg.mcpURL,
		HTTPClient: httpClient,
	}
	client := mcp.NewClient(&mcp.Implementation{
		Name:    "cimd-client",
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
		b, err := json.MarshalIndent(r.StructuredContent, "", "  ")
		if err != nil {
			slog.Error("tool structured content is not marshalable",
				"tool", name, "err", err)
			return
		}
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
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", rawURL)
	case "linux":
		cmd = exec.Command("xdg-open", rawURL)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	// The launcher exits immediately; reap it so it does not sit as a zombie for
	// the rest of the interactive flow.
	go func() { _ = cmd.Wait() }()
	return nil
}

func writeCallbackHTML(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, "<html><body><h1>%s</h1><script>window.close();</script></body></html>",
		html.EscapeString(msg))
}
