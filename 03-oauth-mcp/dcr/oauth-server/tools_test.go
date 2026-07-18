package main

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestWhoAmIHandler(t *testing.T) {
	t.Parallel()
	info := &auth.TokenInfo{
		UserID: "user-42",
		Scopes: []string{"mcp:read", "openid"},
		Extra: map[string]any{
			"client_id":     "client-abc",
			"iss":           "https://signet.example/",
			"aud":           []string{"http://localhost:8095/mcp"},
			"uid":           "alice",
			"domain":        "engineering",
			"project":       "atlas",
			"unknown_extra": "passthrough",
		},
	}
	req := &mcp.CallToolRequest{Extra: &mcp.RequestExtra{TokenInfo: info}}
	_, out, err := whoAmIHandler(context.Background(), req, WhoAmIInput{})
	if err != nil {
		t.Fatalf("whoAmIHandler error: %v", err)
	}
	if out.Subject != "user-42" {
		t.Errorf("Subject = %q, want user-42", out.Subject)
	}
	if out.ClientID != "client-abc" {
		t.Errorf("ClientID = %q, want client-abc", out.ClientID)
	}
	if out.Issuer != "https://signet.example/" {
		t.Errorf("Issuer = %q", out.Issuer)
	}
	if len(out.Audience) != 1 || out.Audience[0] != "http://localhost:8095/mcp" {
		t.Errorf("Audience = %v", out.Audience)
	}
	if len(out.Scopes) != 2 {
		t.Errorf("Scopes = %v", out.Scopes)
	}
	if out.UID != "alice" {
		t.Errorf("UID = %q", out.UID)
	}
	if out.Domain != "engineering" {
		t.Errorf("Domain = %q", out.Domain)
	}
}

func TestWhoAmIHandlerEmptyExtras(t *testing.T) {
	t.Parallel()
	info := &auth.TokenInfo{
		UserID: "user-x",
		Scopes: []string{},
		Extra:  map[string]any{},
	}
	req := &mcp.CallToolRequest{Extra: &mcp.RequestExtra{TokenInfo: info}}
	_, out, err := whoAmIHandler(context.Background(), req, WhoAmIInput{})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if out.Subject != "user-x" {
		t.Errorf("Subject = %q", out.Subject)
	}
	if out.ClientID != "" || out.UID != "" || out.Domain != "" {
		t.Errorf("expected empty optional fields, got %+v", out)
	}
}

func TestShowAuthTokenHandler(t *testing.T) {
	t.Parallel()
	info := &auth.TokenInfo{
		UserID: "user-123456",
		Extra:  map[string]any{"client_id": "client-abcdef"},
	}
	req := &mcp.CallToolRequest{Extra: &mcp.RequestExtra{TokenInfo: info}}
	_, out, err := showAuthTokenHandler(context.Background(), req, ShowAuthTokenInput{})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if out.Subject != "user-123456" {
		t.Errorf("Subject = %q", out.Subject)
	}
	if out.ClientID != "client-abcdef" {
		t.Errorf("ClientID = %q", out.ClientID)
	}
	if out.MaskedToken == "" || out.MaskedToken == info.UserID {
		t.Errorf("MaskedToken should be masked, got %q", out.MaskedToken)
	}
}
