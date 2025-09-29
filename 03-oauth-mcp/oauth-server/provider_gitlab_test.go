package main

import (
	"strings"
	"testing"
)

func TestGitLabProvider_NewGitLabProvider(t *testing.T) {
	// Test default host
	provider := NewGitLabProvider("")
	if provider.host != "https://gitlab.com" {
		t.Errorf("Expected default host to be https://gitlab.com, got %s", provider.host)
	}
	if provider.httpClient == nil {
		t.Error("Expected httpClient to be initialized")
	}

	// Test custom host
	customHost := "https://gitlab.example.com"
	provider = NewGitLabProvider(customHost)
	if provider.host != customHost {
		t.Errorf("Expected host to be %s, got %s", customHost, provider.host)
	}
}

func TestGitLabProvider_GetAuthorizeURL(t *testing.T) {
	provider := NewGitLabProvider("https://gitlab.com")

	url, err := provider.GetAuthorizeURL("test-client", "test-state", "https://example.com/callback", "read_user")
	if err != nil {
		t.Fatalf("GetAuthorizeURL failed: %v", err)
	}

	// Check that URL contains expected components
	expectedParts := []string{
		"https://gitlab.com/oauth/authorize",
		"client_id=test-client",
		"state=test-state",
		"response_type=code",
		"scope=read_user",
	}

	for _, part := range expectedParts {
		if !strings.Contains(url, part) {
			t.Errorf("URL missing expected part %q. Full URL: %s", part, url)
		}
	}
}

func TestGitLabProvider_GetAuthorizeURL_DefaultScopes(t *testing.T) {
	provider := NewGitLabProvider("https://gitlab.com")

	url, err := provider.GetAuthorizeURL("test-client", "test-state", "https://example.com/callback", "")
	if err != nil {
		t.Fatalf("GetAuthorizeURL failed: %v", err)
	}

	// Should have default scope
	if !strings.Contains(url, "scope=read_user") {
		t.Errorf("URL missing default scope 'read_user'. Full URL: %s", url)
	}
}
