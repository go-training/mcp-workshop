# `oauth-server-introspect/` — RFC 7662 introspection variant

MCP resource server for the Authorization Code + PKCE flow. Validates Bearer
tokens by calling the **RFC 7662 introspection endpoint** on the upstream
AS for every request, instead of verifying signatures locally.

Use this variant when revocations must propagate immediately. For most
deployments, the [JWKS variant](../oauth-server/) is the better default
(zero network calls per request, no shared secret).

For the dcr/ split overview, Signet prerequisites, Gap A / Gap B caveats,
and the curl walkthrough, see the parent [`../README.md`](../README.md).

## Why we don't use `middleware.BearerAuth` directly

The natural choice would be
[`go-signet/sdk-go/middleware.BearerAuth`](https://pkg.go.dev/github.com/go-signet/sdk-go/middleware) —
the SDK package designed for exactly this case. We do not use it because
the SDK's `IntrospectionResult` struct does not surface the `aud` claim, so
a `BearerAuth` pipeline cannot enforce the RFC 8707 resource binding this
example requires. We therefore decode the introspection response into our
own struct that includes `aud`, while still using the SDK's
[`discovery`](https://pkg.go.dev/github.com/go-signet/sdk-go/discovery)
package to resolve the introspection endpoint at startup. When/if the
upstream SDK adds `aud` to `IntrospectionResult`, this file can switch to
`middleware.BearerAuth` with a small adapter to the MCP SDK's
`auth.TokenInfo`.

## Files

- `server.go` — HTTP server, OIDC discovery for the introspection endpoint,
  manual introspection POST + RFC 8707 `aud` enforcement, RFC 9728
  metadata, graceful shutdown.
- `tools.go` — the two MCP tools: `who_am_i` and `show_auth_token`.
- `server_test.go` — unit tests for the `aud` claim decoder, the audience
  check (table-driven), and the masking helper.

## Build & run

```bash
go build -o ./bin/oauth-server-introspect ./03-oauth-mcp/dcr/oauth-server-introspect

./bin/oauth-server-introspect \
  -auth-server http://localhost:8080 \
  -resource    http://localhost:8095/mcp \
  -introspect-client-id     mcp-resource \
  -introspect-client-secret rs-secret
```

`-introspect-client-id` / `-introspect-client-secret` are credentials this
RS uses to call `POST /oauth/introspect` with HTTP Basic auth, per RFC 7662
§2.2. Register a confidential client in Signet for this purpose.

## What the verifier enforces

For every request to `/mcp` with `Authorization: Bearer <jwt-or-opaque>`:

1. **POST** the token to `<auth-server>/oauth/introspect` with HTTP Basic
   auth (RFC 7662 §2.1).
2. Reject if `active != true`.
3. **`aud`** contains `-resource` (RFC 8707 binding). If the response has
   no `aud` claim and `-require-resource-binding=true` (the default), the
   token is rejected.
4. Surface `sub`, `client_id`, `iss`, `scope`, `exp`, `aud` into the
   request context for the MCP tools.

On rejection: HTTP 401 with `WWW-Authenticate: Bearer error="invalid_token",
resource_metadata="..."`. Server log lines name the reason
(`audience mismatch`, `audience missing or unbound`, `token is not active`).

## Tools exposed

| Tool              | What it returns                                                                                                |
| ----------------- | -------------------------------------------------------------------------------------------------------------- |
| `who_am_i`        | `subject` (`sub`), `username`, `client_id`, `issuer` (`iss`), `audience` (`aud`), `scopes`.                    |
| `show_auth_token` | `subject`, `client_id`, and a `masked_token` hint derived from those. Never returns the raw bearer.            |

## Flags

See [`../README.md`](../README.md#server-flags).
