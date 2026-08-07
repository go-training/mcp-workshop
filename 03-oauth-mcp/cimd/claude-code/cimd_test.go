//go:build !windows

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestBuildClientMetadataMultipleRedirects(t *testing.T) {
	const cimdURL = "https://localhost:9443/oauth/client.json"
	redirects := []string{
		"http://localhost:8085/callback",
		"http://127.0.0.1:8085/callback",
	}

	body, err := buildClientMetadata(cimdURL, "Claude Code", redirects,
		[]string{"openid", "profile", "email"})
	if err != nil {
		t.Fatalf("buildClientMetadata: %v", err)
	}

	var doc clientMetadata
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("document is not valid JSON: %v", err)
	}

	// The client_id member must be byte-identical to the document URL — Signet
	// binds the fetched document to the URL it fetched it from.
	if doc.ClientID != cimdURL {
		t.Errorf("client_id = %q, want %q", doc.ClientID, cimdURL)
	}
	if doc.ClientURI != "https://claude.ai" {
		t.Errorf("client_uri = %q, want %q", doc.ClientURI, "https://claude.ai")
	}
	// Redirect URIs are compared by exact match on the AS, so the document must
	// carry every callback variant the external client may use, in order.
	if !reflect.DeepEqual(doc.RedirectURIs, redirects) {
		t.Errorf("redirect_uris = %v, want %v", doc.RedirectURIs, redirects)
	}
	// CIMD clients are public; anything but "none" is invalid.
	if doc.TokenEndpointAuthMethod != "none" {
		t.Errorf("token_endpoint_auth_method = %q, want %q",
			doc.TokenEndpointAuthMethod, "none")
	}
	if !slices.Contains(doc.GrantTypes, "authorization_code") {
		t.Errorf("grant_types = %v, must include authorization_code", doc.GrantTypes)
	}
	if doc.Scope != "openid profile email" {
		t.Errorf("scope = %q, want %q", doc.Scope, "openid profile email")
	}
}

func TestBuildClientMetadataRejectsBadInput(t *testing.T) {
	const cimdURL = "https://localhost:9443/oauth/client.json"
	callback := []string{"http://localhost:8085/callback"}

	tests := []struct {
		name      string
		cimdURL   string
		redirects []string
		wantErr   string
	}{
		{
			name:      "non-https CIMD URL",
			cimdURL:   "http://localhost:9443/oauth/client.json",
			redirects: callback,
			wantErr:   "not https",
		},
		{
			name:    "no redirect URIs",
			cimdURL: cimdURL,
			wantErr: "at least one redirect URI",
		},
		{
			name:    "more than ten redirect URIs",
			cimdURL: cimdURL,
			redirects: func() []string {
				out := make([]string, 11)
				for i := range out {
					out[i] = fmt.Sprintf("http://localhost:%d/callback", 8000+i)
				}
				return out
			}(),
			wantErr: "at most 10",
		},
		{
			name:      "relative redirect URI",
			cimdURL:   cimdURL,
			redirects: []string{"/callback"},
			wantErr:   "not an absolute URL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := buildClientMetadata(tt.cimdURL, "n", tt.redirects, nil)
			if err == nil {
				t.Fatalf("buildClientMetadata = nil error, want error containing %q",
					tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("buildClientMetadata error = %v, want error containing %q",
					err, tt.wantErr)
			}
		})
	}
}

func TestMetadataHandler(t *testing.T) {
	const path = "/oauth/client.json"
	body := []byte(`{"client_id":"https://localhost:9443/oauth/client.json"}`)
	handler := metadataHandler(path, body)

	t.Run("GET returns document with headers", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler(rec, httptest.NewRequest(http.MethodGet, path, nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if got := rec.Body.String(); got != string(body) {
			t.Errorf("body = %q, want %q", got, body)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		// The document tracks this run's flags, so it must not be cached.
		if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
			t.Errorf("Cache-Control = %q, want %q", cc, "no-store")
		}
	})

	t.Run("POST is rejected", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler(rec, httptest.NewRequest(http.MethodPost, path, nil))

		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d, want 405", rec.Code)
		}
	})

	// The document is the client's identity: it must be served at exactly its
	// client_id URL, never as a subtree or wildcard match.
	t.Run("other paths are not served", func(t *testing.T) {
		for _, p := range []string{"/", "/oauth/", "/oauth/client.json/extra", "/oauth/other.json"} {
			rec := httptest.NewRecorder()
			handler(rec, httptest.NewRequest(http.MethodGet, p, nil))

			if rec.Code != http.StatusNotFound {
				t.Errorf("GET %s: status = %d, want 404", p, rec.Code)
			}
			if rec.Body.Len() > 0 && strings.Contains(rec.Body.String(), "client_id") {
				t.Errorf("GET %s served the metadata document", p)
			}
		}
	})
}

func TestSplitNonEmpty(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "two uris",
			in:   "http://localhost:8085/callback,http://127.0.0.1:8085/callback",
			want: []string{"http://localhost:8085/callback", "http://127.0.0.1:8085/callback"},
		},
		{
			name: "trailing comma and spaces dropped",
			in:   " http://localhost:8085/callback , ",
			want: []string{"http://localhost:8085/callback"},
		},
		{
			name: "empty input",
			in:   "",
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := splitNonEmpty(tt.in, ","); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("splitNonEmpty(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
