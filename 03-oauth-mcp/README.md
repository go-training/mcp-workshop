# OAuth MCP Module

This module demonstrates OAuth 2.0 integration with MCP (Model Context Protocol) servers.

## Implementations

### Authorization Code + PKCE (interactive user flow)

The `dcr/` directory contains an MCP **resource server** for the
[OAuth 2.1 Authorization Code + PKCE](https://datatracker.ietf.org/doc/html/draft-ietf-oauth-v2-1)
flow, built with the official
[Model Context Protocol Go SDK](https://github.com/modelcontextprotocol/go-sdk).

The server issues no tokens — it delegates all OAuth work to an external
authorization server (e.g. [AuthGate](https://github.com/go-authgate/authgate))
and validates each incoming Bearer. Federated identity providers
(GitHub, Gitea, Microsoft Entra ID, …) are configured on AuthGate, not here.
Dynamic Client Registration is also AuthGate's `/oauth/register`; the
`dcr/` MCP server does not host its own DCR endpoint.

- **[dcr Implementation](dcr/README.md)**: full documentation for the
  Authorization Code + PKCE split
- **Key Features**:
  - Two verifier variants: local JWKS ([`dcr/oauth-server/`](dcr/oauth-server/))
    and RFC 7662 introspection ([`dcr/oauth-server-introspect/`](dcr/oauth-server-introspect/))
  - Bearer-token protected MCP endpoint via `auth.RequireBearerToken`
  - RFC 9728 protected-resource metadata for client auto-discovery
  - **RFC 8707 resource indicator** binds the issued JWT to this MCP
    resource via the `aud` claim, preventing cross-RS token replay
  - Example client in [Go](dcr/oauth-client/) that performs auth-code+PKCE
    end-to-end with persistent token storage
  - Provider choice (GitHub / Gitea / Microsoft) is configured server-side
    on AuthGate; the MCP server is provider-agnostic

See [dcr/README.md](dcr/README.md) for complete documentation, including
Gap A (upstream provider tokens are not passed through to MCP tools) and
Gap B (GitLab is not currently a federated provider on AuthGate).

### Client Credentials (machine-to-machine)

The `client-credentials/` directory contains a minimal MCP **resource server**
that implements the
[OAuth 2.0 Client Credentials extension](https://modelcontextprotocol.io/extensions/auth/oauth-client-credentials),
built with the official [Model Context Protocol Go SDK](https://github.com/modelcontextprotocol/go-sdk) v1.5.0.

The server issues no tokens — it delegates all OAuth work to an external
authorization server such as [AuthGate](https://github.com/go-authgate/authgate)
and validates each incoming Bearer token.

- **[Client Credentials Implementation](client-credentials/README.md)**: Full documentation for the resource-server flow
- **Key Features**:
  - Two verifier variants: RFC 7662 token introspection ([introspection server](client-credentials/server.go)) and local JWKS signature verification ([JWKS server](client-credentials/server-jwks/))
  - Bearer-token protected MCP endpoint via `auth.RequireBearerToken`
  - RFC 9728 protected-resource metadata for client auto-discovery
  - **RFC 8707 resource indicator** binds tokens to a specific MCP resource via the JWT `aud` claim, preventing cross-RS token replay
  - Verification clients in [Go](client-credentials/client/) and [Python](client-credentials/client-python/)
  - No user / browser required — for background services and CI/CD

See [client-credentials/README.md](client-credentials/README.md) for complete documentation and usage instructions.

## Quick Start

```bash
# Authorization Code + PKCE (interactive, dcr/) — validates tokens issued by an external AS (e.g. AuthGate)
# JWKS variant (listens on :8095, local signature verification)
go run ./dcr/oauth-server \
  -auth-server http://localhost:8080 \
  -resource    http://localhost:8095/mcp

# Introspection variant (listens on :8095, RFC 7662 calls to AS on every request)
go run ./dcr/oauth-server-introspect \
  -auth-server http://localhost:8080 \
  -resource    http://localhost:8095/mcp \
  -introspect-client-id mcp-resource \
  -introspect-client-secret rs-secret

# Example client (runs auth-code+PKCE flow, persists token, calls who_am_i)
go run ./dcr/oauth-client \
  -auth-server http://localhost:8080 \
  -mcp-url     http://localhost:8095/mcp \
  -client_id   <your-registered-client-id>

# Client Credentials (machine-to-machine) — validates tokens issued by an external AS (e.g. AuthGate)
# Introspection variant (listens on :8096)
go run ./client-credentials \
  -auth-server https://authgate.local:8080 \
  -resource https://mcp.example/mcp \
  -require-resource-binding=true \
  -introspect-client-id mcp-resource \
  -introspect-client-secret rs-secret

# JWKS variant (listens on :8097, local signature verification, OIDC discovery at startup)
go run ./client-credentials/server-jwks \
  -auth-server https://authgate.local:8080 \
  -resource https://mcp.example/mcp

# Go verification client (targets :8096; swap port to :8097 to hit the JWKS variant)
go run ./client-credentials/client \
  -mcp-url http://localhost:8096/mcp \
  -resource https://mcp.example/mcp \
  -auth-server https://authgate.local:8080 \
  -client_id my-service -client_secret s3cr3t
```
