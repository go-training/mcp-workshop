# OAuth MCP Module

This module demonstrates OAuth 2.0 integration with MCP (Model Context Protocol) servers.

## Implementations

### Dynamic Client Registration (DCR)

The `dcr/` directory contains a complete OAuth 2.0 implementation with Dynamic Client Registration support.

- **[Dynamic Client Registration Implementation](dcr/README.md)**: Full documentation for the DCR-based OAuth flow
- **Key Features**:
  - Multi-provider OAuth (GitHub, GitLab, Gitea)
  - Dynamic client registration endpoint
  - PKCE (Proof Key for Code Exchange) support
  - Flexible storage backends (memory, Redis)

See [dcr/README.md](dcr/README.md) for complete documentation and usage instructions.

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
# DCR (interactive, authorization-code + PKCE)
cd dcr/oauth-server
go run . -client_id=<your-id> -client_secret=<your-secret>

cd dcr/oauth-client
go run .

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
