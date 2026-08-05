//go:build !windows

// Package main is a standalone client-metadata origin for testing Claude Code
// against the CIMD (Client ID Metadata Document) sample. It serves exactly one
// HTTPS resource — the metadata document at -cimd-url — and nothing else.
//
// In the cimd-client sample the OAuth client and the metadata origin are the
// same process. Claude Code cannot host its own document, so this binary takes
// over the origin role: it publishes a document whose redirect_uris point at
// Claude Code's local OAuth callback, and Claude Code then runs the
// authorization-code flow using the document's URL as its client_id.
//
// The process stays up for as long as the test runs: Signet re-fetches the
// client_id URL on every authorization request, so the origin must outlive a
// single flow (unlike cimd-client, which only needs it during its own run).
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-training/mcp-workshop/pkg/logger"
)

func main() {
	if err := run(); err != nil {
		slog.Error("metadata origin failed", "err", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		addr         string
		cimdURL      string
		certFile     string
		keyFile      string
		clientName   string
		redirectURIs string
		scopes       string
		logLevel     string
	)
	flag.StringVar(&addr, "addr", ":9443",
		"HTTPS listen address for the client metadata origin")
	flag.StringVar(&cimdURL, "cimd-url", "https://localhost:9443/oauth/client.json",
		"public URL of the client metadata document — this IS the OAuth client_id "+
			"Claude Code must present; must be https and reachable by Signet")
	flag.StringVar(&certFile, "cert", "localhost.pem",
		"TLS certificate for the metadata origin (mkcert localhost)")
	flag.StringVar(&keyFile, "key", "localhost-key.pem",
		"TLS private key for the metadata origin (mkcert localhost)")
	flag.StringVar(&clientName, "client-name", "Claude Code",
		"client_name in the metadata document (display only — the consent page "+
			"identifies the client by the document's domain, not this string)")
	flag.StringVar(&redirectURIs, "redirect-uris",
		"http://localhost:8085/callback,http://127.0.0.1:8085/callback",
		"comma-separated redirect URIs of the OAuth client this document "+
			"registers (Claude Code's local callback); compared by exact match, "+
			"so the callback port must be fixed — CIMD allows 1-10 entries")
	flag.StringVar(&scopes, "scopes", "openid profile email",
		"space-separated scopes; Signet intersects CIMD scopes with its "+
			"user-safe set (openid profile email offline_access)")
	flag.StringVar(&logLevel, "log-level", "INFO", "log level: DEBUG, INFO, WARN, ERROR")
	flag.Parse()

	logger.NewWithLevel(logLevel)

	uris := splitNonEmpty(redirectURIs, ",")
	doc, err := buildClientMetadata(cimdURL, clientName, uris, strings.Fields(scopes))
	if err != nil {
		return err
	}
	if err := validateOriginPort(addr, cimdURL); err != nil {
		return err
	}

	u, err := url.Parse(cimdURL)
	if err != nil {
		return fmt.Errorf("parse CIMD URL: %w", err)
	}

	// Load the key pair up front instead of letting ServeTLS discover a missing
	// or mismatched file asynchronously: this validates the certificate, the
	// key, and that the two belong together, all before anything is served.
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return fmt.Errorf("load TLS key pair (cert %q, key %q) — generate one with "+
			"`mkcert localhost` (and run `mkcert -install` once): %w",
			certFile, keyFile, err)
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
	// fetch failure on Signet's side.
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s for the metadata origin: %w", addr, err)
	}

	slog.Info("client metadata document published",
		"client_id", cimdURL,
		"redirect_uris", uris,
		"addr", addr,
		"path", u.Path,
	)
	slog.Info("use this client_id in Claude Code's MCP OAuth config", "client_id", cimdURL)

	go func() {
		if serveErr := srv.ServeTLS(listener, "", ""); serveErr != nil &&
			!errors.Is(serveErr, http.ErrServerClosed) {
			slog.Error("metadata origin stopped serving", "addr", addr, "err", serveErr)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	slog.Info("shutdown signal received, shutting down metadata origin...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		_ = srv.Close()
		return fmt.Errorf("forced shutdown: %w", err)
	}
	slog.Info("metadata origin shutdown gracefully")
	return nil
}

// splitNonEmpty splits s on sep and drops empty entries, so a trailing comma
// in -redirect-uris does not register an empty redirect URI.
func splitNonEmpty(s, sep string) []string {
	var out []string
	for part := range strings.SplitSeq(s, sep) {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
