package main

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// WhoAmIInput is intentionally empty — the tool reads everything it needs
// from the verified JWT claims already attached to the request context.
type WhoAmIInput struct{}

// WhoAmIOutput surfaces a subset of the verified token's claims to the
// MCP caller. All fields come from the JWT — no upstream provider API call
// is made (see README "Gap A").
type WhoAmIOutput struct {
	Subject  string   `json:"subject"          jsonschema:"the JWT 'sub' (user id at the issuer)"`
	ClientID string   `json:"client_id"        jsonschema:"the OAuth client that obtained the token"`
	Issuer   string   `json:"issuer"           jsonschema:"the JWT 'iss' (authorization server URL)"`
	Audience []string `json:"audience"         jsonschema:"the JWT 'aud' (resource indicator(s))"`
	Scopes   []string `json:"scopes"           jsonschema:"OAuth scopes granted to this token"`
	UID      string   `json:"uid,omitempty"    jsonschema:"AuthGate-attested upstream username (extra_uid claim) when present"`
	Domain   string   `json:"domain,omitempty" jsonschema:"AuthGate-attested tenant/domain (extra_domain claim) when present"`
}

func whoAmIHandler(
	_ context.Context,
	req *mcp.CallToolRequest,
	_ WhoAmIInput,
) (*mcp.CallToolResult, WhoAmIOutput, error) {
	info := req.Extra.TokenInfo
	out := WhoAmIOutput{
		Subject: info.UserID,
		Scopes:  info.Scopes,
	}
	if cid, ok := info.Extra["client_id"].(string); ok {
		out.ClientID = cid
	}
	if iss, ok := info.Extra["iss"].(string); ok {
		out.Issuer = iss
	}
	if aud, ok := info.Extra["aud"].([]string); ok {
		out.Audience = aud
	}
	if uid, ok := info.Extra["uid"].(string); ok {
		out.UID = uid
	}
	if dom, ok := info.Extra["domain"].(string); ok {
		out.Domain = dom
	}
	return nil, out, nil
}

// ShowAuthTokenInput is empty — the tool reflects token metadata back to
// the caller. The raw bearer is never returned (only a masked form derived
// from the token's subject) so a malicious peer that calls this tool cannot
// exfiltrate the token itself.
type ShowAuthTokenInput struct{}

type ShowAuthTokenOutput struct {
	Subject     string `json:"subject"      jsonschema:"the JWT 'sub' claim"`
	ClientID    string `json:"client_id"    jsonschema:"the OAuth client that obtained the token"`
	MaskedToken string `json:"masked_token" jsonschema:"a partially-redacted hint derived from token metadata"`
}

func showAuthTokenHandler(
	_ context.Context,
	req *mcp.CallToolRequest,
	_ ShowAuthTokenInput,
) (*mcp.CallToolResult, ShowAuthTokenOutput, error) {
	info := req.Extra.TokenInfo
	cid, _ := info.Extra["client_id"].(string)
	return nil, ShowAuthTokenOutput{
		Subject:     info.UserID,
		ClientID:    cid,
		MaskedToken: maskHint(info.UserID, cid),
	}, nil
}

func maskHint(sub, clientID string) string {
	if sub == "" && clientID == "" {
		return "***"
	}
	return fmt.Sprintf("sub=%s client=%s", masked(sub), masked(clientID))
}

func masked(s string) string {
	if len(s) <= 4 {
		return "***"
	}
	return s[:2] + "***" + s[len(s)-2:]
}

func newMCPServer() *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "dcr-mcp-server",
		Version: "1.0.0",
	}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name: "who_am_i",
		Description: "Returns identity claims from the verified JWT: subject, client id, " +
			"audience, issuer, scopes, and AuthGate-attested extras (uid, domain).",
	}, whoAmIHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name: "show_auth_token",
		Description: "Returns a masked hint about the current bearer token (subject and " +
			"client id only — never the raw token).",
	}, showAuthTokenHandler)

	return server
}
