//go:build !windows

package main

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// WhoAmIInput is intentionally empty — the tool reads everything it needs from
// the verified JWT claims already attached to the request context.
type WhoAmIInput struct{}

// WhoAmIOutput surfaces a subset of the verified token's claims to the MCP
// caller. For a CIMD client the client_id field is the metadata document URL
// itself — visible proof that no pre-registered client row was involved.
type WhoAmIOutput struct {
	Subject  string   `json:"subject"   jsonschema:"the JWT 'sub' (user id at the issuer)"`
	ClientID string   `json:"client_id" jsonschema:"the OAuth client that obtained the token — a CIMD URL for this sample"`
	Issuer   string   `json:"issuer"    jsonschema:"the JWT 'iss' (authorization server URL)"`
	Audience []string `json:"audience"  jsonschema:"the JWT 'aud' (resource indicator(s))"`
	Scopes   []string `json:"scopes"    jsonschema:"OAuth scopes granted to this token"`
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
	return nil, out, nil
}

func newMCPServer() *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "cimd-mcp-server",
		Version: "1.0.0",
	}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name: "who_am_i",
		Description: "Returns identity claims from the verified JWT: subject, client id, " +
			"audience, issuer, and scopes.",
	}, whoAmIHandler)

	return server
}
