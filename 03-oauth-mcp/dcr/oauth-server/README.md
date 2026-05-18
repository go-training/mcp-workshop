# `oauth-server/` — JWKS variant

MCP resource server for the Authorization Code + PKCE flow. Validates Bearer
tokens **locally via JWKS** using
[`github.com/go-authgate/sdk-go/jwksauth`](https://pkg.go.dev/github.com/go-authgate/sdk-go/jwksauth)
and never calls the AS on the hot path after the one-time discovery at
startup.

For an overview of the dcr/ split (JWKS vs introspection), the AuthGate
prerequisites, the Gap A / Gap B caveats, and the curl walkthrough, see the
parent [`../README.md`](../README.md). This file is a short reference for
the JWKS server alone.

## Files

- `server.go` — HTTP server, OIDC discovery, JWKS verifier, audience and
  `type==access` checks, RFC 9728 metadata, graceful shutdown.
- `tools.go` — the two MCP tools: `who_am_i` (claims dump) and
  `show_auth_token` (masked hint).
- `server_test.go` / `tools_test.go` — unit tests for the audience check,
  the JWT `type` adapter, and the two tools.

## Build & run

```bash
go build -o ./bin/oauth-server ./03-oauth-mcp/dcr/oauth-server

./bin/oauth-server \
  -auth-server http://localhost:8080 \
  -resource    http://localhost:8095/mcp \
  -addr        :8095
```

The server starts only after the upstream AS answers OIDC discovery on
`/.well-known/openid-configuration`. If discovery fails the server exits
with a non-zero status — fix `-auth-server` and retry.

## What the verifier enforces

For every request to `/mcp` with `Authorization: Bearer <jwt>`:

1. **JWT signature** against the JWKS published by the AS (cached in
   process; refetched on key-id miss).
2. **`iss`** equals the canonical issuer reported by OIDC discovery.
3. **`exp`** is in the future and **`nbf`** is in the past.
4. **`aud`** contains `-resource` (RFC 8707 binding).
5. **`type`** claim equals `"access"`. AuthGate signs refresh tokens with
   the same key, so without this check a refresh JWT presented as a
   Bearer would pass every other test. The SDK does not surface `type`
   on its parsed claims, so this server re-decodes the raw JWT payload to
   read it.

On rejection: HTTP 401 with `WWW-Authenticate: Bearer error="invalid_token",
resource_metadata="..."`, plus an INFO-level slog line naming the failure
reason (`audience mismatch`, `non-access token rejected`, …).

## Tools exposed

| Tool              | What it returns                                                                                                   |
| ----------------- | ----------------------------------------------------------------------------------------------------------------- |
| `who_am_i`        | `subject` (JWT `sub`), `client_id`, `issuer`, `audience` (`aud`), `scopes`, plus AuthGate extras `uid`, `domain`. |
| `show_auth_token` | `subject`, `client_id`, and a `masked_token` hint derived from those. Never returns the raw bearer.               |

See [`../README.md`](../README.md#mcp-tools) for the rationale.

## Flags

See [`../README.md`](../README.md#server-flags).
