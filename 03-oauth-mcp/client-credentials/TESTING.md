# Verification Manual

Step-by-step verification for the `client-credentials/` example after the
RFC 8707 (resource indicator) + RFC 8414 (authorization server metadata)
work. Run every scenario top to bottom; each one is independent and
self-cleaning.

## Table of contents

- [Prerequisites](#prerequisites)
- [Build](#build)
- [Scenario 1 — Happy path (introspection server + Go client)](#scenario-1--happy-path-introspection-server--go-client)
- [Scenario 2 — `aud` mismatch is rejected](#scenario-2--aud-mismatch-is-rejected)
- [Scenario 3 — Missing `resource` against an audience-required server](#scenario-3--missing-resource-against-an-audience-required-server)
  - [Negative control — `require-resource-binding=false` accepts the same token](#negative-control--require-resource-bindingfalse-accepts-the-same-token)
- [Scenario 4 — JWKS variant, happy path](#scenario-4--jwks-variant-happy-path)
- [Scenario 5 — JWKS variant rejects a refresh token used as Bearer](#scenario-5--jwks-variant-rejects-a-refresh-token-used-as-bearer)
  - [Path A — Authorization code flow yields a refresh JWT](#path-a--authorization-code-flow-yields-a-refresh-jwt)
  - [Path B — AuthGate issues opaque refresh tokens (degenerate case)](#path-b--authgate-issues-opaque-refresh-tokens-degenerate-case)
  - [Verifying the rejection (Path A)](#verifying-the-rejection-path-a)
- [Scenario 6 — Python client auto-derives `resource`](#scenario-6--python-client-auto-derives-resource)
- [Scenario 7 — Manual `curl` walkthrough (no Go or Python client)](#scenario-7--manual-curl-walkthrough-no-go-or-python-client)
  - [7.1 RFC 8414 discovery](#71-rfc-8414-discovery)
  - [7.2 RFC 8707 token request](#72-rfc-8707-token-request)
  - [7.3 Inspect the JWT](#73-inspect-the-jwt)
  - [7.4 Call the MCP server](#74-call-the-mcp-server)
- [Troubleshooting](#troubleshooting)
- [Full cleanup](#full-cleanup)
- [What "all scenarios passed" proves](#what-all-scenarios-passed-proves)

## Prerequisites

You need these in your `PATH`:

| Tool   | Used for                             | Check            |
| ------ | ------------------------------------ | ---------------- |
| `go`   | building servers/clients (≥ 1.25)    | `go version`     |
| `curl` | manual token + MCP requests          | `curl --version` |
| `jq`   | decoding the token endpoint response | `jq --version`   |
| `uv`   | Python client only — Scenario 6      | `uv --version`   |

**AuthGate** is assumed to already be running at
`http://localhost:8080` (plain HTTP, no TLS). The walkthrough does NOT
cover installing AuthGate.

Two OAuth clients must be registered on that AuthGate instance:

| Client ID      | Secret      | Purpose                                                              |
| -------------- | ----------- | -------------------------------------------------------------------- |
| `mcp-resource` | `rs-secret` | This MCP server's own credentials, used to call `/oauth/introspect`. |
| `my-service`   | `s3cr3t`    | The calling application, granted scopes `mcp:read mcp:write`.        |

If your AuthGate scope or registration naming differs, replace those
values consistently in every command below.

**Confirm AuthGate is reachable** before going further:

```bash
curl -fsS http://localhost:8080/.well-known/oauth-authorization-server | jq '.issuer, .token_endpoint, .introspection_endpoint'
```

Expected: three non-null strings. If this fails, fix AuthGate first; no
scenario will work without it.

## Build

From the repo root:

```bash
make
```

Expected: all 8 binaries build, including `bin/client-credentials`,
`bin/client`, and `bin/server-jwks`. If any binary fails, stop and fix it
before continuing.

## Scenario 1 — Happy path (introspection server + Go client)

**Goal:** prove the full chain works — Go client discovers `token_endpoint`
via RFC 8414, fetches a token bound to the MCP resource via RFC 8707, and
the introspection server validates `aud` end to end.

**Terminal A — start the server:**

```bash
./bin/client-credentials \
  -addr :8096 \
  -auth-server http://localhost:8080 \
  -resource https://mcp.example/mcp \
  -require-resource-binding=true \
  -introspect-client-id mcp-resource \
  -introspect-client-secret rs-secret \
  -log-level INFO
```

Expected startup log:

```
msg="client-credentials MCP server starting" addr=:8096 resource=https://mcp.example/mcp require_resource_binding=true ...
```

**Terminal B — run the client:**

```bash
./bin/client \
  -mcp-url http://localhost:8096/mcp \
  -resource https://mcp.example/mcp \
  -auth-server http://localhost:8080 \
  -client_id my-service \
  -client_secret s3cr3t \
  -scopes 'mcp:read mcp:write'
```

**Expected — client output:**

```
msg="discovered token endpoint via RFC 8414" auth_server=http://localhost:8080 token_endpoint=http://localhost:8080/oauth/token
msg="unauthenticated probe returned 401 as expected" www_authenticate="Bearer resource_metadata=..."
msg=connecting mcp_url=http://localhost:8096/mcp token_url=http://localhost:8080/oauth/token resource=https://mcp.example/mcp scopes=[mcp:read mcp:write]
msg=connected server_name=client-credentials-mcp-server
msg="tool structured content" tool=echo_message json={"client_id":"my-service","message":"hello from go-sdk","scopes":["mcp:read","mcp:write"]}
msg="tool structured content" tool=add_numbers json={"sum":42}
msg="verification complete"
```

**Expected — server log (Terminal A):**

```
msg="audience verified" expected_aud=https://mcp.example/mcp got_aud=[https://mcp.example/mcp]
```

The `audience verified` line should appear at least twice (once per MCP
request — the SDK makes several during init + tool calls).

**Pass criteria:**

- [X] Client prints `discovered token endpoint via RFC 8414` (proves RFC 8414).
- [X] Client prints `verification complete` with no errors.
- [X] Server logs `audience verified` with `got_aud=[https://mcp.example/mcp]`.

**Cleanup:** `Ctrl+C` in Terminal A.

## Scenario 2 — `aud` mismatch is rejected

**Goal:** prove a token bound to a different resource is rejected with
`401 invalid_token`.

**Terminal A — keep the same server running** as in Scenario 1
(`-resource https://mcp.example/mcp`, require-binding on).

**Terminal B — fetch a token bound to a different resource, then send it:**

```bash
TOKEN=$(curl -fsS -X POST http://localhost:8080/oauth/token \
  -u my-service:s3cr3t \
  -d 'grant_type=client_credentials' \
  -d 'scope=mcp:read mcp:write' \
  -d 'resource=https://other.example/mcp' \
  | jq -r .access_token)

echo "aud claim in token:"
echo "$TOKEN" | cut -d. -f2 | base64 --decode 2>/dev/null | jq .aud
```

Expected: `"https://other.example/mcp"` (or an array containing it).

Now present this token to the server:

```bash
curl -i -X POST http://localhost:8096/mcp \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"curl","version":"1.0"}}}'
```

**Expected — HTTP response:**

```
HTTP/1.1 401 Unauthorized
Www-Authenticate: Bearer error="invalid_token", resource_metadata="...", scope="mcp:read"
```

**Expected — server log (Terminal A):**

```
level=WARN msg="audience mismatch" expected_aud=https://mcp.example/mcp got_aud=[https://other.example/mcp]
```

**Pass criteria:**

- [X] HTTP status is `401`.
- [X] `WWW-Authenticate` header contains `error="invalid_token"`.
- [X] Server log shows `audience mismatch` with both `expected_aud` and `got_aud`.

**Cleanup:** none yet — keep the server running for Scenario 3.

## Scenario 3 — Missing `resource` against an audience-required server

**Goal:** prove that with `-require-resource-binding=true`, a token whose
introspection response carries no `aud` is rejected.

This scenario assumes your AuthGate, when called _without_ the `resource`
parameter, returns a JWT whose `aud` is the static fallback
`JWT_AUDIENCE` — and that the introspection response also omits or
returns that fallback value. If your AuthGate always populates `aud`
regardless, this scenario will pass with `audience mismatch` (Scenario 2
behaviour) instead of `audience missing or unbound`; either log line
proves the defence works.

**Terminal A — same server still running** as in Scenarios 1–2.

**Terminal B — fetch a token without `resource`:**

```bash
TOKEN_NO_RES=$(curl -fsS -X POST http://localhost:8080/oauth/token \
  -u my-service:s3cr3t \
  -d 'grant_type=client_credentials' \
  -d 'scope=mcp:read mcp:write' \
  | jq -r .access_token)

echo "$TOKEN_NO_RES" | cut -d. -f2 | base64 --decode 2>/dev/null | jq '{aud}'
```

Note what `aud` is in this JWT — it's whatever AuthGate's `JWT_AUDIENCE`
default is. It will **not** equal `https://mcp.example/mcp`.

Present the token:

```bash
curl -i -X POST http://localhost:8096/mcp \
  -H "Authorization: Bearer $TOKEN_NO_RES" \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"curl","version":"1.0"}}}'
```

**Expected:** `401 Unauthorized` with `error="invalid_token"`.

**Expected — server log (Terminal A) — one of:**

- `audience missing or unbound expected_aud=https://mcp.example/mcp`
  (if introspection returned no `aud`), or
- `audience mismatch expected_aud=https://mcp.example/mcp got_aud=[<jwt-audience>]`
  (if AuthGate populated the fallback `aud`).

**Pass criteria:**

- [X] HTTP status `401`.
- [X] One of the two log lines above appears.

**Cleanup:** `Ctrl+C` in Terminal A.

### Negative control — `require-resource-binding=false` accepts the same token

To prove the flag is what's blocking the request, restart the server
without the flag and send the same `TOKEN_NO_RES`:

```bash
./bin/client-credentials \
  -addr :8096 \
  -auth-server http://localhost:8080 \
  -resource https://mcp.example/mcp \
  -introspect-client-id mcp-resource \
  -introspect-client-secret rs-secret
```

Send the same `curl` from above. Two possible outcomes:

- If introspection returns no `aud`: request **succeeds**, server logs
  `WARN msg="token accepted without aud claim"`.
- If introspection returns AuthGate's fallback `aud`: request still **fails**
  with `audience mismatch` (the `aud` is present and wrong; require-binding
  has no effect).

Either result confirms `-require-resource-binding` controls only the "no
`aud` at all" branch.

`Ctrl+C` the server before moving on.

## Scenario 4 — JWKS variant, happy path

**Goal:** prove the alternate server (`server-jwks`) verifies JWTs
locally (no per-request issuer roundtrip) and enforces the same `aud`
contract.

**Terminal A — start the JWKS server on a different port** (`:8097` so it
can coexist with the introspection server if you want both):

```bash
./bin/server-jwks \
  -addr :8097 \
  -auth-server http://localhost:8080 \
  -resource https://mcp.example/mcp \
  -log-level DEBUG
```

Expected startup logs:

```
msg="client-credentials JWKS MCP server starting" addr=:8097 resource=https://mcp.example/mcp auth_server=http://localhost:8080 issuer=http://localhost:8080 ...
```

If you instead see `OIDC discovery failed`, AuthGate isn't reachable — fix
that first.

**Terminal B — run the same Go client against the new port:**

```bash
./bin/client \
  -mcp-url http://localhost:8097/mcp \
  -resource https://mcp.example/mcp \
  -auth-server http://localhost:8080 \
  -client_id my-service \
  -client_secret s3cr3t \
  -scopes 'mcp:read mcp:write'
```

**Expected — client output:** identical to Scenario 1 (ends with
`verification complete`).

**Expected — server log (Terminal A):**

```
level=DEBUG msg="jwt verified" iss=http://localhost:8080 sub=... aud=[https://mcp.example/mcp] exp=... client_id=my-service scopes=[mcp:read mcp:write]
msg="audience verified" expected_aud=https://mcp.example/mcp got_aud=[https://mcp.example/mcp]
```

The `jwt verified` line confirms local signature + `iss`/`exp`/`aud`/`nbf`
checks fired. There must be **no HTTP traffic from `server-jwks` to
AuthGate** during this scenario (after the one-shot startup discovery) —
you can confirm with `tcpdump` if you want:

```bash
# In a third terminal, BEFORE running the client:
sudo tcpdump -i any -nn 'port 8080' &
# ... run the client ...
# kill tcpdump; you should see only the initial OIDC discovery, no per-request traffic.
```

**Pass criteria:**

- [X] Client prints `verification complete`.
- [X] Server log shows `jwt verified` at DEBUG level.
- [X] Server log shows `audience verified`.
- [X] No per-request traffic from `server-jwks` to AuthGate (optional check).

**Cleanup:** leave the server running for Scenario 5.

## Scenario 5 — JWKS variant rejects a refresh token used as Bearer

**Goal:** prove the adapter's `type=="access"` check fires. AuthGate's
docs warn that without this check, a refresh JWT presented as a Bearer
would pass signature/`iss`/`aud`/`exp` checks unchanged.

The `client_credentials` grant does not issue refresh tokens — AuthGate
only issues them on user-bearing flows. AuthGate does **not** support the
Resource Owner Password Credentials grant (RFC 6749 §4.3), so a one-shot
`curl` cannot mint a refresh token. The only realistic path is the
authorization code flow, and only when AuthGate is configured to issue
refresh tokens as JWTs.

### Path A — Authorization code flow yields a refresh JWT

Run an authorization code + PKCE flow against AuthGate to obtain a token
response containing `refresh_token`. The sibling
[`03-oauth-mcp/dcr/oauth-client/`](../dcr/oauth-client/) example
implements this flow end to end against the workshop's own OAuth server
— adapt its issuer and client_id/client_secret to point at AuthGate, or
write a minimal curl-driven flow yourself.

Once you have the token response in `RESPONSE`:

```bash
REFRESH=$(echo "$RESPONSE" | jq -r .refresh_token)

# Confirm it is a JWT and carries type=refresh.
echo "$REFRESH" | cut -d. -f2 | base64 --decode 2>/dev/null | jq '{type, aud, iss, exp}'
```

Expected: `type` is `"refresh"`. If `jq` errors with "parse error" or
the value is not three base64 segments separated by `.`, your AuthGate
is issuing opaque refresh tokens — skip to Path B.

### Path B — AuthGate issues opaque refresh tokens (degenerate case)

If AuthGate's refresh tokens are not JWTs, the `type=="access"` defence
is moot for this issuer: the JWKS verifier rejects the token at the
signature parsing step, before reaching the `type` check. Confirm with
any non-JWT string:

```bash
curl -i -X POST http://localhost:8097/mcp \
  -H "Authorization: Bearer this-is-not-a-jwt" \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"curl","version":"1.0"}}}'
```

Expected: `401 invalid_token`. The server log will show a JWT parse
error, **not** `non-access token rejected`. This satisfies the threat
model (the refresh token is still rejected) but does not exercise the
`type` check itself.

### Verifying the rejection (Path A)

Once you have `REFRESH` set to a JWT whose payload contains
`"type":"refresh"`:

```bash
curl -i -X POST http://localhost:8097/mcp \
  -H "Authorization: Bearer $REFRESH" \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"curl","version":"1.0"}}}'
```

**Expected — HTTP:** `401 Unauthorized` with `error="invalid_token"`.

**Expected — server log (Terminal A):**

```
level=WARN msg="non-access token rejected" got_type=refresh subject=...
```

**Pass criteria:**

- [ ] HTTP status `401` on the refresh-as-Bearer request.
- [ ] **Path A only:** server log shows `non-access token rejected got_type=refresh`. Path B users cannot exercise this — the `type=="access"` defence is dormant against opaque-refresh issuers, so the JWKS signature step rejects first.

**Cleanup:** `Ctrl+C` in Terminal A.

## Scenario 6 — Python client auto-derives `resource`

**Goal:** prove the Python MCP SDK (`mcp >= 1.27`) emits the
RFC 8707 `resource` parameter automatically, derived from `--mcp-url`,
and that the resulting `aud` matches the server's `-resource`.

**Critical:** the Python SDK derives `resource` from the canonical form of
`--mcp-url`. The value you pass to `--mcp-url` must be byte-for-byte
the value the server is started with via `-resource`.

**Terminal A — start the introspection server, using a URL the Python
SDK won't reshape:**

```bash
./bin/client-credentials \
  -addr :8096 \
  -auth-server http://localhost:8080 \
  -resource http://localhost:8096/mcp \
  -require-resource-binding=true \
  -introspect-client-id mcp-resource \
  -introspect-client-secret rs-secret \
  -log-level DEBUG
```

Note `-resource http://localhost:8096/mcp` — we deliberately use the
same URL the Python client will pass as `--mcp-url`.

**Terminal B — install Python deps and run:**

```bash
cd 03-oauth-mcp/client-credentials/client-python
uv sync
uv run python client.py \
  --mcp-url http://localhost:8096/mcp \
  --client-id my-service \
  --client-secret s3cr3t \
  --scopes 'mcp:read mcp:write'
```

**Expected — Python output:**

```
connecting to http://localhost:8096/mcp ...
connected: client-credentials-mcp-server v1.0.0
available tools: ['add_numbers', 'echo_message']
[echo_message] structured: {"client_id": "my-service", "message": "hello from python-sdk", "scopes": ["mcp:read"]}
[add_numbers] structured: {"sum": 42}
verification complete
```

**Expected — server log (Terminal A):**

```
msg="audience verified" expected_aud=http://localhost:8096/mcp got_aud=[http://localhost:8096/mcp]
```

If you see `audience mismatch` instead, the Python SDK normalised the URL
differently than expected. Inspect what value it actually sent by adding
a DEBUG capture: re-run with `LOG_LEVEL=DEBUG` and watch the
introspection-response debug line on the server — the `aud` field is
exactly what the SDK requested.

**Pass criteria:**

- [ ] Python script prints `verification complete`.
- [ ] Server log shows `audience verified` with matching `expected_aud`/`got_aud`.

**Cleanup:** `Ctrl+C` in Terminal A.

## Scenario 7 — Manual `curl` walkthrough (no Go or Python client)

**Goal:** prove the full RFC 8707 + `aud` enforcement using nothing but
`curl` and `jq`, so a reader who is suspicious of the example clients
can replicate end to end.

**Terminal A — start the introspection server (same as Scenario 1):**

```bash
./bin/client-credentials \
  -addr :8096 \
  -auth-server http://localhost:8080 \
  -resource https://mcp.example/mcp \
  -require-resource-binding=true \
  -introspect-client-id mcp-resource \
  -introspect-client-secret rs-secret
```

**Terminal B — step through the flow:**

### 7.1 RFC 8414 discovery

```bash
META=$(curl -fsS http://localhost:8080/.well-known/oauth-authorization-server)
TOKEN_EP=$(echo "$META" | jq -r .token_endpoint)
echo "token_endpoint = $TOKEN_EP"
```

Expected: a URL like `http://localhost:8080/oauth/token`.

### 7.2 RFC 8707 token request

```bash
RESPONSE=$(curl -fsS -X POST "$TOKEN_EP" \
  -u my-service:s3cr3t \
  -d 'grant_type=client_credentials' \
  -d 'scope=mcp:read mcp:write' \
  -d 'resource=https://mcp.example/mcp')

TOKEN=$(echo "$RESPONSE" | jq -r .access_token)
```

### 7.3 Inspect the JWT

```bash
echo "$TOKEN" | cut -d. -f2 | base64 --decode 2>/dev/null | jq '{iss, aud, sub, scope, exp, type}'
```

**Expected:** `aud` is `"https://mcp.example/mcp"` (or an array containing
it), `type` is `"access"`.

### 7.4 Call the MCP server

```bash
curl -i -X POST http://localhost:8096/mcp \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"curl","version":"1.0"}}}'
```

**Expected:** `HTTP/1.1 200 OK` and an MCP `initialize` response in the
body.

**Expected — server log (Terminal A):**

```
msg="audience verified" expected_aud=https://mcp.example/mcp got_aud=[https://mcp.example/mcp]
```

**Pass criteria:**

- [ ] Token endpoint discovered via `/.well-known/oauth-authorization-server`.
- [ ] JWT's `aud` equals `https://mcp.example/mcp`.
- [ ] MCP server returns `200` and logs `audience verified`.

**Cleanup:** `Ctrl+C` in Terminal A.

## Troubleshooting

| Symptom                                                      | Likely cause                                                                          | Fix                                                                                             |
| ------------------------------------------------------------ | ------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------- |
| Client: `RFC 8414 discovery failed, using fallback`          | AuthGate's metadata endpoint isn't at the standard well-known path, or unreachable    | Pass `-token-url` explicitly to bypass discovery                                                |
| Server: `audience mismatch` when you expect verification     | Client sent a different `resource` than the server is configured with                 | Make `-resource` byte-for-byte equal on both sides                                              |
| Server: `audience mismatch` from Python client               | Python SDK normalised `--mcp-url` differently (lowercased host, stripped slash, etc.) | Use the actual URL the SDK sent (visible in server DEBUG log) as the server's `-resource` value |
| Server: `OIDC discovery failed` on `server-jwks` startup     | AuthGate not reachable at `-auth-server` URL during startup discovery                 | Start AuthGate first; verify with the curl from "Prerequisites"                                 |
| All requests log at INFO `audience verified` (very noisy)    | Working as designed — every accepted request emits this for traceability              | Demote to DEBUG by editing the verifier if production needs quieter logs                        |
| Test for `type==access` is degenerate (no JWT refresh token) | AuthGate is issuing opaque refresh tokens                                             | Documented in Scenario 5; the signature check still rejects non-JWTs                            |

## Full cleanup

If you've been running servers in the background, kill them all:

```bash
pkill -f 'bin/client-credentials' 2>/dev/null
pkill -f 'bin/server-jwks' 2>/dev/null
```

## What "all scenarios passed" proves

| Scenario | Proves                                                                    |
| -------- | ------------------------------------------------------------------------- |
| 1        | Happy path — RFC 8414 discovery + RFC 8707 resource binding + `aud` check |
| 2        | Tokens minted for other resources can't reach this MCP server             |
| 3        | `-require-resource-binding=true` blocks tokens with no `aud` at all       |
| 4        | JWKS variant verifies locally without per-request issuer traffic          |
| 5        | JWKS variant rejects refresh JWTs presented as Bearer (`type=="access"`)  |
| 6        | The Python MCP SDK auto-derives `resource` from `server_url`              |
| 7        | The whole flow works with `curl` alone — no language SDK assumed          |

When all seven pass, the plan's "Done definition" is satisfied for
runtime behaviour. Static checks (`go vet`, `golangci-lint`, `make`) are
covered separately by the existing CI workflow.
