# `oauth-client/` — Authorization Code + PKCE example client

Runs the full OAuth 2.1 **Authorization Code + PKCE** flow against an
external authorization server (e.g. Signet), persists the resulting tokens
on disk via
[`github.com/go-signet/sdk-go/credstore`](https://pkg.go.dev/github.com/go-signet/sdk-go/credstore),
and then exercises the dcr/ MCP resource server using
[`github.com/modelcontextprotocol/go-sdk`](https://github.com/modelcontextprotocol/go-sdk).

For the architecture diagram, Signet prerequisites, and the Gap A / Gap B
caveats that apply to the whole dcr/ split, see the parent
[`../README.md`](../README.md). This file is a short reference for the
client alone.

## Why the auth-code flow is hand-rolled

The natural choice would be
[`go-signet/sdk-go/authflow.RunAuthCodeFlow`](https://pkg.go.dev/github.com/go-signet/sdk-go/authflow#RunAuthCodeFlow) —
it already opens a browser, runs a local callback server, generates PKCE,
and exchanges the code. We do not use it because sdk-go v0.11.0 has no
extension point for the **RFC 8707 `resource=` parameter** on either the
authorize URL or the token POST. Without `resource=`, the issued JWT's
`aud` claim does not match the MCP server's resource URL, and the server's
`aud` check rejects the call. We therefore inline the flow:

- `authflow.NewPKCE()` is reused for verifier/challenge generation.
- `discovery.NewClient().Fetch()` resolves `authorization_endpoint` and
  `token_endpoint` from `/.well-known/openid-configuration`.
- `credstore.NewTokenFileStore()` persists tokens between runs.
- The authorize URL and the token POST are built locally so both can
  carry `resource=`.

The MCP call itself uses `mcp.StreamableClientTransport` with an
`http.Client` whose `RoundTripper` injects the bearer.

## Build & run

```bash
go build -o ./bin/oauth-client ./03-oauth-mcp/dcr/oauth-client

./bin/oauth-client \
  -auth-server http://localhost:8080 \
  -mcp-url     http://localhost:8095/mcp \
  -client_id   <your-registered-client-id> \
  -client_secret <client-secret-if-confidential> \
  -scopes      "openid profile email"
```

On first run: a browser opens, you log in on Signet (via the federated
provider), the browser redirects to `http://127.0.0.1:8085/callback`, the
client exchanges the code, and writes the token to
`~/.cache/dcr-mcp-client/<client_id>.json`.

On subsequent runs: the cached token is loaded if still valid and reused —
no browser opens. Pass `-force-reauth` to bypass the cache.

The client then calls `who_am_i` and `show_auth_token` on the MCP server
and prints the structured output.

## Files

- `client.go` — everything: parse flags, discover endpoints, load/refresh
  token, run interactive auth-code+PKCE flow with `resource=` binding,
  call the MCP server.

## Flags

See [`../README.md`](../README.md#client-flags-oauth-client).

## What the issued JWT looks like

After a successful exchange the JWT's payload includes:

```json
{
  "iss": "http://localhost:8080",
  "sub": "user-uuid-on-signet",
  "aud": ["http://localhost:8095/mcp"],
  "client_id": "<your-registered-client-id>",
  "scope": "openid profile email",
  "type": "access",
  "exp": 1740000000,
  "extra_uid": "alice",
  "extra_domain": "engineering"
}
```

The `aud` claim is what the MCP server enforces — that is the entire reason
this client must send `resource=` on `/oauth/authorize` and `/oauth/token`.
