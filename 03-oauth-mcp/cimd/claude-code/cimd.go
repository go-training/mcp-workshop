//go:build !windows

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// clientMetadata is the Client ID Metadata Document this origin publishes on
// behalf of Claude Code. The document is the client's registration: its URL is
// the OAuth client_id, and Signet fetches it during the authorization request.
// Fields mirror the subset of draft-ietf-oauth-client-id-metadata-document
// that Signet consumes.
type clientMetadata struct {
	ClientID                string   `json:"client_id"`
	ClientName              string   `json:"client_name"`
	ClientURI               string   `json:"client_uri"`
	RedirectURIs            []string `json:"redirect_uris"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	GrantTypes              []string `json:"grant_types"`
	Scope                   string   `json:"scope"`
}

// validateCIMDURL mirrors Signet's IsCIMDClientID predicate so a bad URL fails
// here with a readable error instead of an opaque unauthorized_client after
// the browser round-trip: https scheme, a hostname, a path more specific than
// "/" with no dot segments, no fragment (including a trailing empty "#"), and
// no userinfo.
func validateCIMDURL(raw string) error {
	if strings.Contains(raw, "#") {
		return errors.New("must not contain a fragment")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("not a valid URL: %w", err)
	}
	if !strings.EqualFold(u.Scheme, "https") {
		return fmt.Errorf("scheme %q is not https — Signet only treats https URLs as "+
			"CIMD client_ids, with no development-mode exception; use mkcert for "+
			"local TLS", u.Scheme)
	}
	if u.Hostname() == "" {
		return errors.New("missing hostname")
	}
	if u.Path == "" || u.Path == "/" {
		return errors.New("path must be more specific than \"/\"")
	}
	for seg := range strings.SplitSeq(u.Path, "/") {
		if seg == "." || seg == ".." {
			return errors.New("path must not contain \".\" or \"..\" segments")
		}
	}
	if u.User != nil {
		return errors.New("must not contain userinfo")
	}
	return nil
}

// buildClientMetadata renders the document served at cimdURL. The client_id
// member must be byte-for-byte identical to the URL Signet fetched, so it is
// always set to cimdURL itself. Unlike the cimd-client sample, the redirect
// URIs belong to an external OAuth client (Claude Code), so several may be
// listed — CIMD allows 1–10, compared by exact match.
func buildClientMetadata(cimdURL, name string, redirectURIs, scopes []string) ([]byte, error) {
	if err := validateCIMDURL(cimdURL); err != nil {
		return nil, fmt.Errorf("invalid CIMD URL %q: %w", cimdURL, err)
	}
	if len(redirectURIs) == 0 {
		return nil, errors.New("at least one redirect URI is required")
	}
	if len(redirectURIs) > 10 {
		return nil, fmt.Errorf("%d redirect URIs — CIMD allows at most 10", len(redirectURIs))
	}
	for _, r := range redirectURIs {
		u, err := url.Parse(r)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return nil, fmt.Errorf("redirect URI %q is not an absolute URL", r)
		}
	}
	doc := clientMetadata{
		ClientID:     cimdURL,
		ClientName:   name,
		ClientURI:    "https://claude.ai",
		RedirectURIs: redirectURIs,
		// CIMD clients are always public: there is no registration step at
		// which a secret could be exchanged, so "none" is the only valid value.
		TokenEndpointAuthMethod: "none",
		GrantTypes:              []string{"authorization_code", "refresh_token"},
		Scope:                   strings.Join(scopes, " "),
	}
	body, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal client metadata: %w", err)
	}
	return body, nil
}

// metadataHandler serves the fixed document bytes the way the CIMD spec
// requires from an origin: a direct 200 on GET, no auth, no redirects.
//
// The path is matched here rather than by a ServeMux pattern: the document is
// the client's identity and must be served at exactly its client_id URL, while
// a ServeMux pattern built from a user-supplied path would treat a trailing
// "/" as a subtree and "{...}" as a wildcard (or panic outright on "{" and
// whitespace), publishing the registration at URLs that are not the client_id.
func metadataHandler(path string, body []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != path {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		// The document's contents follow this run's flags (redirect_uris and
		// -scopes), so a cached copy would hand the AS a stale registration on
		// the next run: no-store, not max-age.
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}
}

// validateOriginPort ensures the port the metadata origin binds (-addr)
// matches the port advertised in the CIMD URL (-cimd-url). Hosts are not
// compared — binding a wildcard address like ":9443" while advertising
// https://localhost:9443/… is a legitimate deployment — but a port mismatch
// always means Signet would fetch a URL nothing answers, so catch it at
// startup instead of as a confusing SSRF-looking failure.
func validateOriginPort(addr, cimdURL string) error {
	_, listenPort, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid -addr %q: %w", addr, err)
	}
	u, err := url.Parse(cimdURL)
	if err != nil {
		return fmt.Errorf("invalid CIMD URL %q: %w", cimdURL, err)
	}
	urlPort := u.Port()
	if urlPort == "" {
		urlPort = "443"
	}
	if listenPort != urlPort {
		return fmt.Errorf("-addr listens on port %s but -cimd-url advertises "+
			"port %s — the client_id would point at an address the origin is not "+
			"serving", listenPort, urlPort)
	}
	return nil
}
