//go:build !windows

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// clientMetadata is the Client ID Metadata Document this client publishes.
// The document is the client's registration: its URL is the OAuth client_id,
// and Signet fetches it during the authorization request. Fields mirror the
// subset of draft-ietf-oauth-client-id-metadata-document that Signet consumes.
type clientMetadata struct {
	ClientID                string   `json:"client_id"`
	ClientName              string   `json:"client_name"`
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
// always set to cimdURL itself.
func buildClientMetadata(cimdURL, name, redirectURI string, scopes []string) ([]byte, error) {
	if err := validateCIMDURL(cimdURL); err != nil {
		return nil, fmt.Errorf("invalid CIMD URL %q: %w", cimdURL, err)
	}
	if redirectURI == "" {
		return nil, errors.New("redirect URI is required")
	}
	doc := clientMetadata{
		ClientID:     cimdURL,
		ClientName:   name,
		RedirectURIs: []string{redirectURI},
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
		// The document's contents follow this run's flags (redirect_uris carries
		// the callback port, scope carries -scopes), so a cached copy would hand
		// the AS a stale registration on the next run: no-store, not max-age.
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}
}
