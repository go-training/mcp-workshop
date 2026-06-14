// Package main is a verification client for the dcr/ MCP server. It discovers
// the authorization server from the MCP resource server per RFC 9728
// (unauthenticated probe → 401 → Protected Resource Metadata), then performs
// OAuth 2.1 Authorization Code + PKCE against that authorization server
// (e.g. AuthGate), persists the resulting tokens via
// github.com/go-authgate/sdk-go/credstore, and then exercises the MCP
// resource server using github.com/modelcontextprotocol/go-sdk.
//
// The PKCE flow is hand-rolled (not authflow.RunAuthCodeFlow) because
// sdk-go v0.11.0 does not expose the RFC 8707 `resource=` parameter on
// either the authorize URL or the token request. RFC 8707 binding is
// required: it is what makes the issued JWT's `aud` claim match the MCP
// server's resource URL, which the server enforces on every call.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/go-training/mcp-workshop/pkg/logger"

	"github.com/go-authgate/sdk-go/authflow"
	"github.com/go-authgate/sdk-go/credstore"
	"github.com/go-authgate/sdk-go/discovery"
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
	mcpURL       string
	authServer   string
	resource     string
	clientID     string
	clientSecret string
	scopes       []string
	tokenFile    string
	callbackPort int
	forceReauth  bool
}

func parseFlags() *config {
	var (
		mcpURL       string
		authServer   string
		resource     string
		clientID     string
		clientSecret string
		scopes       string
		tokenFile    string
		callbackPort int
		forceReauth  bool
		logLevel     string
	)
	flag.StringVar(&mcpURL, "mcp-url", "http://localhost:8095/mcp",
		"MCP streamable HTTP endpoint")
	flag.StringVar(&authServer, "auth-server", "http://localhost:8080",
		"OAuth 2.0 authorization server issuer URL (e.g. AuthGate)")
	flag.StringVar(&resource, "resource", "",
		"RFC 8707 resource indicator sent on /oauth/authorize and /oauth/token "+
			"(default: -mcp-url; binds the issued JWT's aud claim)")
	flag.StringVar(&clientID, "client_id", "",
		"OAuth client_id registered with the AS (required)")
	flag.StringVar(&clientSecret, "client_secret", "",
		"OAuth client_secret (omit for public clients — PKCE is always used)")
	flag.StringVar(&scopes, "scopes", "openid profile email",
		"space-separated scopes to request from the AS")
	flag.StringVar(&tokenFile, "token-file", "",
		"path to the on-disk token cache (default: ~/.cache/dcr-mcp-client/token.json)")
	flag.IntVar(&callbackPort, "callback-port", 8085,
		"local TCP port for the OAuth callback server")
	flag.BoolVar(&forceReauth, "force-reauth", false,
		"ignore any cached token and always run the interactive flow")
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
	if tokenFile == "" {
		tokenFile = defaultTokenFile(clientID)
	}

	return &config{
		mcpURL:       mcpURL,
		authServer:   authServer,
		resource:     resource,
		clientID:     clientID,
		clientSecret: clientSecret,
		scopes:       strings.Fields(scopes),
		tokenFile:    tokenFile,
		callbackPort: callbackPort,
		forceReauth:  forceReauth,
	}
}

func defaultTokenFile(clientID string) string {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "dcr-mcp-client", clientID+".json")
}

func run() error {
	cfg := parseFlags()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// RFC 9728: discover the authorization server from the MCP server itself
	// (unauthenticated request → 401 → Protected Resource Metadata) instead of
	// trusting -auth-server blindly. This is what the MCP authorization spec
	// mandates; the flag is only a fallback (see discoverAuthServer).
	authServer := discoverAuthServer(ctx, cfg)

	disco, err := discovery.NewClient(authServer)
	if err != nil {
		return fmt.Errorf("discovery client: %w", err)
	}
	meta, err := disco.Fetch(ctx)
	if err != nil {
		return fmt.Errorf("OIDC discovery from %s: %w", authServer, err)
	}
	if meta.AuthorizationEndpoint == "" || meta.TokenEndpoint == "" {
		return fmt.Errorf(
			"discovery response missing authorization/token endpoint: %+v", meta,
		)
	}
	slog.Info("discovered AS endpoints",
		"issuer", meta.Issuer,
		"authorize", meta.AuthorizationEndpoint,
		"token", meta.TokenEndpoint,
	)

	if err := os.MkdirAll(filepath.Dir(cfg.tokenFile), 0o700); err != nil {
		return fmt.Errorf("create token dir: %w", err)
	}
	store := credstore.NewTokenFileStore(cfg.tokenFile)

	token, err := loadValidToken(store, cfg)
	switch {
	case errors.Is(err, errNoCachedToken):
		slog.Info("no usable cached token — starting Authorization Code + PKCE flow",
			"token_file", cfg.tokenFile)
		token, err = runAuthCodeFlow(ctx, cfg, meta)
		if err != nil {
			return fmt.Errorf("auth code flow: %w", err)
		}
		if err := store.Save(cfg.clientID, *token); err != nil {
			return fmt.Errorf("save token to %s: %w", cfg.tokenFile, err)
		}
		slog.Info("token saved", "path", cfg.tokenFile,
			"expires_at", token.ExpiresAt.Format(time.RFC3339))
	case err != nil:
		return err
	default:
		slog.Info("using cached token",
			"path", cfg.tokenFile,
			"expires_at", token.ExpiresAt.Format(time.RFC3339))
	}

	if err := callMCP(ctx, cfg, token); err != nil {
		return fmt.Errorf("call MCP: %w", err)
	}
	slog.Info("client run complete")
	return nil
}

// discoverAuthServer implements the RFC 9728 authorization-server discovery
// that the MCP authorization spec mandates: probe the MCP server with no token,
// read the resource_metadata pointer from the 401 WWW-Authenticate header,
// fetch the Protected Resource Metadata, and return the authorization server it
// names. The -auth-server flag is only a fallback for when the probe or the
// metadata fetch cannot complete (e.g. the server omits the hint and is not at
// the conventional well-known path).
func discoverAuthServer(ctx context.Context, cfg *config) string {
	metaURL := probeResourceMetadataURL(ctx, cfg.mcpURL)
	if metaURL == "" {
		// RFC 9728 §3.1 well-known fallback when the 401 carries no hint.
		metaURL = wellKnownPRMURL(cfg.mcpURL)
		slog.Info("no resource_metadata hint on 401 — trying well-known URI",
			"resource_metadata_url", metaURL)
	}

	prm, err := oauthex.GetProtectedResourceMetadata(
		ctx, metaURL, cfg.resource, http.DefaultClient)
	if err != nil {
		slog.Warn("RFC 9728 discovery failed — falling back to -auth-server",
			"resource_metadata_url", metaURL,
			"auth_server", cfg.authServer, "err", err)
		return cfg.authServer
	}
	if len(prm.AuthorizationServers) == 0 {
		slog.Warn("protected resource metadata named no authorization_servers — "+
			"falling back to -auth-server", "auth_server", cfg.authServer)
		return cfg.authServer
	}

	as := prm.AuthorizationServers[0]
	slog.Info("discovered authorization server via RFC 9728",
		"resource_metadata_url", metaURL,
		"resource", prm.Resource,
		"authorization_server", as,
	)
	return as
}

// probeResourceMetadataURL makes one unauthenticated MCP request and returns
// the resource_metadata URL from the 401 WWW-Authenticate challenge
// (RFC 9728 §5.1). It returns "" if the server does not answer with that hint,
// so the caller can fall back to the well-known URI.
func probeResourceMetadataURL(ctx context.Context, mcpURL string) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, mcpURL,
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
	if err != nil {
		return ""
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Warn("unauthenticated probe failed", "err", err)
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		slog.Warn("unauthenticated probe did not return 401", "status", resp.StatusCode)
		return ""
	}

	headers := resp.Header.Values("WWW-Authenticate")
	slog.Info("unauthenticated probe returned 401 as expected",
		"www_authenticate", strings.Join(headers, "; "))

	challenges, err := oauthex.ParseWWWAuthenticate(headers)
	if err != nil {
		slog.Warn("could not parse WWW-Authenticate header", "err", err)
		return ""
	}
	for _, c := range challenges {
		if u := c.Params["resource_metadata"]; u != "" {
			return u
		}
	}
	return ""
}

// wellKnownPRMURL builds the RFC 9728 well-known Protected Resource Metadata
// URL at the origin of an MCP endpoint, used only when the 401 carried no
// resource_metadata hint.
func wellKnownPRMURL(mcpURL string) string {
	u, err := url.Parse(mcpURL)
	if err != nil {
		return strings.TrimRight(mcpURL, "/") + "/.well-known/oauth-protected-resource"
	}
	return u.Scheme + "://" + u.Host + "/.well-known/oauth-protected-resource"
}

// errNoCachedToken means the on-disk credstore has no usable token for this
// client and the caller must run the interactive auth-code flow. Returned
// (rather than (nil, nil)) so callers can `errors.Is` it cleanly.
var errNoCachedToken = errors.New("no cached token")

func loadValidToken(
	store credstore.Store[credstore.Token],
	cfg *config,
) (*credstore.Token, error) {
	if cfg.forceReauth {
		return nil, errNoCachedToken
	}
	t, err := store.Load(cfg.clientID)
	if err != nil {
		if errors.Is(err, credstore.ErrNotFound) {
			return nil, errNoCachedToken
		}
		return nil, fmt.Errorf("load token from %s: %w", cfg.tokenFile, err)
	}
	if !t.IsValid() {
		slog.Info("cached token is expired",
			"expires_at", t.ExpiresAt.Format(time.RFC3339))
		return nil, errNoCachedToken
	}
	return &t, nil
}

// runAuthCodeFlow performs RFC 6749 §4.1 authorization code + RFC 7636 PKCE
// + RFC 8707 resource indicator. Hand-rolled rather than using
// authflow.RunAuthCodeFlow because the SDK v0.11.0 has no extension point
// for `resource=` on either the authorize URL or the token POST.
func runAuthCodeFlow(
	ctx context.Context,
	cfg *config,
	meta *discovery.Metadata,
) (*credstore.Token, error) {
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

	codeCh := make(chan string, 1)
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
			codeCh <- code
			writeCallbackHTML(w, "Authentication successful. You can close this window.")
		})
		if !handled {
			writeCallbackHTML(w, "Already processed. You can close this window.")
		}
	})

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
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
		meta.AuthorizationEndpoint, cfg, redirectURI, state, pkce.Challenge,
	)
	slog.Info("opening browser for authorization", "url", authURL)
	if err := openBrowser(authURL); err != nil {
		slog.Warn("could not open browser automatically — open this URL manually",
			"url", authURL, "err", err)
	}

	var code string
	select {
	case code = <-codeCh:
	case err := <-errCh:
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	return exchangeCode(ctx, cfg, meta.TokenEndpoint, code, redirectURI, pkce.Verifier)
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
) (*credstore.Token, error) {
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

	t := credstore.Token{
		AccessToken:  tr.AccessToken,
		RefreshToken: tr.RefreshToken,
		TokenType:    tr.TokenType,
		Scope:        tr.Scope,
		IDToken:      tr.IDToken,
		ClientID:     cfg.clientID,
	}
	if tr.ExpiresIn > 0 {
		t.ExpiresAt = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	}
	return &t, nil
}

// bearerRoundTripper attaches a fixed Bearer token to every request. We use
// a static token rather than a refreshing TokenSource because (a) the MCP
// session is short-lived, (b) the access token lives long enough for the
// run, and (c) a refresh on this flow would also need to thread `resource=`,
// which is again not in the SDK.
type bearerRoundTripper struct {
	base  http.RoundTripper
	token string
}

func (b *bearerRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	req := r.Clone(r.Context())
	req.Header.Set("Authorization", "Bearer "+b.token)
	return b.base.RoundTrip(req)
}

func callMCP(ctx context.Context, cfg *config, token *credstore.Token) error {
	httpClient := &http.Client{
		Transport: &bearerRoundTripper{
			base:  http.DefaultTransport,
			token: token.AccessToken,
		},
		Timeout: 30 * time.Second,
	}

	transport := &mcp.StreamableClientTransport{
		Endpoint:   cfg.mcpURL,
		HTTPClient: httpClient,
	}
	client := mcp.NewClient(&mcp.Implementation{
		Name:    "dcr-mcp-client",
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

	tools, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		return fmt.Errorf("list tools: %w", err)
	}
	names := make([]string, 0, len(tools.Tools))
	for _, t := range tools.Tools {
		names = append(names, t.Name)
	}
	slog.Info("available tools", "tools", names)

	for _, name := range []string{"who_am_i", "show_auth_token"} {
		res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name})
		if err != nil {
			return fmt.Errorf("call %s: %w", name, err)
		}
		printToolResult(name, res)
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
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL).Start()
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

func writeCallbackHTML(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, "<html><body><h1>%s</h1><script>window.close();</script></body></html>", msg)
}
