# Testing runbook: RFC 9207 mix-up attack & defense

This runbook reproduces the three scenarios end-to-end against a running
[Signet](https://github.com/go-signet/signet) honest authorization server.
Everything runs on `localhost`, so plain HTTP is allowed by the SDK's
loopback exception.

## Prerequisites

1. **Signet running** as the honest AS at `http://localhost:8080` with at
   least one federated identity provider (e.g. GitHub) configured, exactly as
   the `dcr/` and `client-credentials/` examples require.

2. **A client registered at Signet** with:
   - a `client_id` you will pass as `-client_id`,
   - the redirect URI **`http://127.0.0.1:8085/callback`** (the client's local
     callback; the mix-up relies on the honest AS accepting this exact URI),
   - a `client_secret` only if the client is confidential (public clients use
     PKCE alone — omit `-client_secret`).

3. **Go toolchain** matching the repo's `go.mod`.

### Step 0 — verify Signet emits RFC 9207 `iss` (critical)

The defense scenario relies on the honest AS putting `iss` in its redirect and
advertising the flag. Confirm the flag before you start:

```bash
curl -s http://localhost:8080/.well-known/oauth-authorization-server \
  | grep -o '"authorization_response_iss_parameter_supported":[^,}]*'
```

Expected: `"authorization_response_iss_parameter_supported":true`.

- If it prints `true`, Signet stamps `iss` on the callback and scenario 3 will
  abort as designed.
- If the flag is **absent or `false`**, this Signet build does not implement
  RFC 9207, and the defense cannot trigger. In that case the sample's honest AS
  cannot demonstrate the defense; upgrade Signet, or stand up a small standalone
  honest AS that emits `iss` (left as a documented optional path — not built by
  default).

## Start the two long-running servers

Open two terminals from the repo root and leave them running.

**Terminal 1 — honest MCP resource server (`:8095`):**

```bash
go run ./03-oauth-mcp/issuer-identification/mcp-server \
  -auth-server http://localhost:8080 \
  -resource    http://localhost:8095/mcp \
  -log-level   INFO
```

It performs OIDC discovery against Signet at startup; if it exits with
`OIDC discovery failed`, Signet is not reachable at `-auth-server`.

**Terminal 2 — malicious authorization server (`:9090`):**

```bash
go run ./03-oauth-mcp/issuer-identification/evil-as \
  -issuer    http://localhost:9090 \
  -honest-as http://localhost:8080 \
  -log-level INFO
```

On startup it discovers Signet's authorize/token endpoints and logs
`evil-as impersonation target discovered`. Pass `-redeem=false` if you want it
to only log the capture and not mint a real stolen token.

---

## Scenario 1 — Happy path (defense on, honest AS)

Expected issuer = Signet. The client discovers Signet directly, `iss`
matches, the code is exchanged at Signet, and the client reaches the MCP tool.

```bash
go run ./03-oauth-mcp/issuer-identification/mcp-client \
  -auth-server http://localhost:8080 \
  -mcp-url     http://localhost:8095/mcp \
  -client_id   <your-registered-client-id> \
  -defense
```

A browser opens; authenticate and consent. **Expected client logs:**

- `discovered authorization server` with `iss_parameter_supported=true`
- `iss OK — issuer matches the discovered authorization server`
- `connected` to `issuer-identification-mcp-server`, then a `who_am_i`
  `tool structured content` result with your subject.

---

## Scenario 2 — Mix-up attack, defense OFF (code is stolen)

Expected issuer = `evil-as`. The client discovers `evil-as`, which redirects the
browser to Signet; Signet issues a valid code straight to the client
callback. With **no** `-defense`, the client posts that honest code to
`evil-as`'s token endpoint.

```bash
go run ./03-oauth-mcp/issuer-identification/mcp-client \
  -auth-server http://localhost:9090 \
  -mcp-url     http://localhost:8095/mcp \
  -client_id   <your-registered-client-id>
```

A browser opens; authenticate and consent. **Expected `evil-as` logs
(terminal 2):**

- `evil-as /authorize hit — performing mix-up redirect to honest AS`
- `CAPTURED authorization code at evil-as /token endpoint` (with the code and
  `code_verifier`)
- `STOLEN ACCESS TOKEN minted from captured code` (because `-redeem` defaults on)

The client log also warns `RFC 9207 iss validation is OFF …`. The attacker now
holds a working access token for the victim.

**Watch terminal 3 too — this is the insidious part.** Because `evil-as` redeems
the code and returns the honest token verbatim (and `-connect` defaults on), the
client's own terminal shows an apparently *successful* run: it obtains a token,
connects, and prints a normal `who_am_i` result. Nothing in the victim's terminal
signals the theft — the only evidence is the `CAPTURED` / `STOLEN` lines in
terminal 2. That silent success is exactly why the mix-up is dangerous and why
RFC 9207 (scenario 3) matters.

---

## Scenario 3 — Mix-up attack, defense ON (client aborts)

Same setup as scenario 2, but add `-defense`.

```bash
go run ./03-oauth-mcp/issuer-identification/mcp-client \
  -auth-server http://localhost:9090 \
  -mcp-url     http://localhost:8095/mcp \
  -client_id   <your-registered-client-id> \
  -defense
```

A browser opens; authenticate and consent. **Expected client logs:**

- `authorization response received at callback` with
  `expected_issuer=http://localhost:9090` and `received_iss=http://localhost:8080`
- `RFC 9207 issuer validation failed: issuer mismatch: got "…8080" want "…9090" — aborting`
- the client exits **before** contacting `evil-as`'s token endpoint.

**Expected `evil-as` logs (terminal 2):** the `/authorize` redirect line, but
**no** `CAPTURED authorization code` line — the attacker gets nothing.

---

## Automated check

```bash
go test ./03-oauth-mcp/issuer-identification/...
go vet  ./03-oauth-mcp/issuer-identification/...
```

`mcp-client/issuer_test.go` validates `validateIssuerResponse` across all RFC 9207
branches without needing any server running.
