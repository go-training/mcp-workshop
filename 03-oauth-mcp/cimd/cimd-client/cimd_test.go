//go:build !windows

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

func TestValidateCIMDURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr string // substring of the expected error; "" means valid
	}{
		{
			name: "valid https url with path",
			url:  "https://client.example.com/oauth/client.json",
		},
		{
			name: "valid loopback with port",
			url:  "https://localhost:9443/oauth/client.json",
		},
		{
			name: "uppercase scheme accepted",
			url:  "HTTPS://client.example.com/oauth/client.json",
		},
		{
			name:    "http scheme rejected",
			url:     "http://client.example.com/oauth/client.json",
			wantErr: "not https",
		},
		{
			name:    "missing hostname",
			url:     "https:///oauth/client.json",
			wantErr: "hostname",
		},
		{
			name:    "root path rejected",
			url:     "https://client.example.com/",
			wantErr: "more specific",
		},
		{
			name:    "empty path rejected",
			url:     "https://client.example.com",
			wantErr: "more specific",
		},
		{
			name:    "dot segment rejected",
			url:     "https://client.example.com/oauth/../client.json",
			wantErr: "segments",
		},
		{
			name:    "fragment rejected",
			url:     "https://client.example.com/oauth/client.json#frag",
			wantErr: "fragment",
		},
		{
			name:    "trailing empty fragment rejected",
			url:     "https://client.example.com/oauth/client.json#",
			wantErr: "fragment",
		},
		{
			name:    "userinfo rejected",
			url:     "https://user@client.example.com/oauth/client.json",
			wantErr: "userinfo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCIMDURL(tt.url)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validateCIMDURL(%q) = %v, want nil", tt.url, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("validateCIMDURL(%q) = nil, want error containing %q",
					tt.url, tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("validateCIMDURL(%q) = %v, want error containing %q",
					tt.url, err, tt.wantErr)
			}
		})
	}
}

func TestBuildClientMetadata(t *testing.T) {
	const (
		cimdURL     = "https://localhost:9443/oauth/client.json"
		redirectURI = "http://127.0.0.1:8085/callback"
	)

	body, err := buildClientMetadata(cimdURL, "Test Client", redirectURI,
		[]string{"openid", "profile"})
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
	if len(doc.RedirectURIs) != 1 || doc.RedirectURIs[0] != redirectURI {
		t.Errorf("redirect_uris = %v, want [%q]", doc.RedirectURIs, redirectURI)
	}
	// CIMD clients are public; anything but "none" is invalid.
	if doc.TokenEndpointAuthMethod != "none" {
		t.Errorf("token_endpoint_auth_method = %q, want %q",
			doc.TokenEndpointAuthMethod, "none")
	}
	if !slices.Contains(doc.GrantTypes, "authorization_code") {
		t.Errorf("grant_types = %v, must include authorization_code", doc.GrantTypes)
	}
	if doc.Scope != "openid profile" {
		t.Errorf("scope = %q, want %q", doc.Scope, "openid profile")
	}
}

func TestBuildClientMetadataRejectsBadInput(t *testing.T) {
	if _, err := buildClientMetadata(
		"http://localhost:9443/oauth/client.json", "n",
		"http://127.0.0.1:8085/callback", nil,
	); err == nil {
		t.Error("expected error for non-https CIMD URL")
	}
	if _, err := buildClientMetadata(
		"https://localhost:9443/oauth/client.json", "n", "", nil,
	); err == nil {
		t.Error("expected error for empty redirect URI")
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

func TestWriteCallbackHTMLEscapes(t *testing.T) {
	// The message embeds authorization-response parameters controlled by the AS
	// (e.g. "error"), so it must never reach the page unescaped.
	rec := httptest.NewRecorder()
	writeCallbackHTML(rec, `Authentication failed: <script>alert("xss")</script>`)

	got := rec.Body.String()
	if strings.Contains(got, "<script>alert") {
		t.Errorf("callback HTML contains unescaped script tag: %q", got)
	}
	if !strings.Contains(got, "&lt;script&gt;") {
		t.Errorf("callback HTML = %q, want the message HTML-escaped", got)
	}
}

func TestValidateIssuerResponse(t *testing.T) {
	const issuer = "http://localhost:8080"

	tests := []struct {
		name      string
		iss       string
		supported bool
		wantErr   bool
	}{
		{name: "supported and matching", iss: issuer, supported: true},
		{name: "supported but missing", iss: "", supported: true, wantErr: true},
		{name: "supported but mismatched", iss: "http://evil:9090", supported: true, wantErr: true},
		{name: "unsupported and absent", iss: "", supported: false},
		{name: "unsupported but present", iss: issuer, supported: false, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateIssuerResponse(tt.iss, issuer, tt.supported)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateIssuerResponse(%q, %q, %v) = %v, wantErr %v",
					tt.iss, issuer, tt.supported, err, tt.wantErr)
			}
		})
	}
}
