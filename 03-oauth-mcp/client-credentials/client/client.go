// Package main is a verification client for the client-credentials MCP
// server, built with github.com/modelcontextprotocol/go-sdk v1.5.0.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-training/mcp-workshop/pkg/logger"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/oauth2/clientcredentials"
)

func main() {
	if err := run(); err != nil {
		slog.Error("verification failed", "err", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		mcpURL       string
		authServer   string
		tokenURL     string
		clientID     string
		clientSecret string
		scopes       string
		skipUnauth   bool
		logLevel     string
	)
	flag.StringVar(&mcpURL, "mcp-url", "http://localhost:8096/mcp",
		"MCP streamable HTTP endpoint")
	flag.StringVar(&authServer, "auth-server", "http://localhost:8080",
		"OAuth 2.0 authorization server issuer URL (e.g. AuthGate)")
	flag.StringVar(&tokenURL, "token-url", "",
		"OAuth 2.0 token endpoint (defaults to <auth-server>/oauth/token)")
	flag.StringVar(&clientID, "client_id", "my-service", "OAuth client_id")
	flag.StringVar(&clientSecret, "client_secret", "s3cr3t", "OAuth client_secret")
	flag.StringVar(&scopes, "scopes", "mcp:read mcp:write",
		"space-separated scopes to request from the authorization server")
	flag.BoolVar(&skipUnauth, "skip-unauth-check", false,
		"skip the 'no token' 401 probe at startup")
	flag.StringVar(&logLevel, "log-level", "INFO", "log level: DEBUG, INFO, WARN, ERROR")
	flag.Parse()

	logger.NewWithLevel(logLevel)

	if tokenURL == "" {
		tokenURL = strings.TrimRight(authServer, "/") + "/oauth/token"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Probe exists only to prove the server's auth middleware is wired up;
	// it is not part of the normal client-credentials flow.
	if !skipUnauth {
		if err := probeUnauthenticated(ctx, mcpURL); err != nil {
			slog.Warn("unauthenticated probe", "err", err)
		}
	}

	cfg := &clientcredentials.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		TokenURL:     tokenURL,
		Scopes:       strings.Fields(scopes),
	}

	transport := &mcp.StreamableClientTransport{
		Endpoint:   mcpURL,
		HTTPClient: cfg.Client(ctx),
	}
	client := mcp.NewClient(&mcp.Implementation{
		Name:    "client-credentials-mcp-client",
		Version: "1.0.0",
	}, nil)

	slog.Info("connecting", "mcp_url", mcpURL, "token_url", tokenURL, "scopes", cfg.Scopes)
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

	echoResult, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "echo_message",
		Arguments: map[string]any{"message": "hello from go-sdk"},
	})
	if err != nil {
		return fmt.Errorf("call echo_message: %w", err)
	}
	printToolResult("echo_message", echoResult)

	addResult, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "add_numbers",
		Arguments: map[string]any{"a": 21, "b": 21},
	})
	if err != nil {
		return fmt.Errorf("call add_numbers: %w", err)
	}
	printToolResult("add_numbers", addResult)

	slog.Info("verification complete")
	return nil
}

func probeUnauthenticated(ctx context.Context, mcpURL string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, mcpURL,
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		return fmt.Errorf("expected 401, got %d", resp.StatusCode)
	}
	slog.Info("unauthenticated probe returned 401 as expected",
		"www_authenticate", resp.Header.Get("WWW-Authenticate"))
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
