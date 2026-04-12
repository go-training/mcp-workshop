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
and validates each incoming Bearer token via RFC 7662 token introspection.

- **[Client Credentials Implementation](client-credentials/README.md)**
- **Key Features**:
  - Bearer-token protected MCP endpoint via `auth.RequireBearerToken`
  - RFC 7662 token introspection against an external authorization server
  - RFC 9728 protected-resource metadata for client auto-discovery
  - No user / browser required — for background services and CI/CD

## Quick Start

```bash
# DCR (interactive, authorization-code + PKCE)
cd dcr/oauth-server
go run . -client_id=<your-id> -client_secret=<your-secret>

cd dcr/oauth-client
go run .

# Client Credentials (machine-to-machine) — validates tokens issued by an external AS (e.g. AuthGate)
go run ./client-credentials \
  -auth-server http://localhost:8080 \
  -introspect-client-id mcp-resource \
  -introspect-client-secret rs-secret
```
