# RFC 8707 Resource Indicator 驗證流程

本文件示範：**同一個 Client ID** 配對 **多個 MCP Resource Server**，由 MCP
Client 對不同的 `resource=` 取得**不同的 Access Token**，並驗證每張 token
只能被它指定的那一台 MCP Server 接受。這正是 RFC 8707 的設計目的——
防止 token 在共享同一個 AS 的多個 RS 之間被互打。

> 這份測試流程不需要修改 `dcr/` 的程式碼，純粹靠 flag 組合即可重現。

---

## 拓撲

```
                          ┌────────────────────┐
                          │ Signet (AS)      │
                          │ http://localhost   │
                          │       :8080        │
                          └──────────▲─────────┘
                                     │
              ┌──────────────────────┼──────────────────────┐
              │   one client_id      │                      │
              │                      │                      │
   ┌──────────┴──────────┐           │           ┌──────────┴──────────┐
   │ MCP Server A (RS_A) │           │           │ MCP Server B (RS_B) │
   │ :8095               │           │           │ :8096               │
   │ aud = .../8095/mcp  │           │           │ aud = .../8096/mcp  │
   └──────────▲──────────┘           │           └──────────▲──────────┘
              │                      │                      │
              │   token_A            │             token_B  │
              │   (aud=8095/mcp)     │             (aud=8096/mcp)
              │                      │                      │
              └─────────── MCP Client (one client_id) ──────┘
                          (sequentially obtains token_A,
                          then token_B with different
                          `-resource` flags)
```

關鍵：兩台 MCP Server 的 `-resource` 不同；MCP Client 跑兩次 auth-code 流程，
每次傳不同的 `-resource`，Signet 就會發出兩張 `aud` 不同的 JWT。

---

## 前置條件

1. **Signet** 已跑在 `http://localhost:8080`，並完成 GitHub（或 Gitea /
   Microsoft）federation 設定。
2. **註冊一個 OAuth client** in Signet（或啟用 DCR 由 client 自註冊），
   redirect URI 包含 `http://127.0.0.1:8085/callback`。把回傳的
   `client_id` / `client_secret` 記為 `$CID` / `$CSECRET`。
3. 兩個 `dcr/` server 變體已 build 過：

   ```bash
   make
   # 或單獨：
   go build -o bin/oauth-server            ./03-oauth-mcp/dcr/oauth-server
   go build -o bin/oauth-server-introspect ./03-oauth-mcp/dcr/oauth-server-introspect
   go build -o bin/oauth-client            ./03-oauth-mcp/dcr/oauth-client
   ```

---

## 步驟一：啟動兩台 MCP Server

兩台用 **JWKS 變體**最簡單（無 RS shared secret）。開兩個 terminal：

### Server A（aud = `http://localhost:8095/mcp`）

```bash
./bin/oauth-server \
  -addr :8095 \
  -resource http://localhost:8095/mcp \
  -auth-server http://localhost:8080 \
  -log-level DEBUG
```

啟動 log 應該看到：

```
dcr JWKS MCP server starting addr=:8095 resource=http://localhost:8095/mcp ...
```

### Server B（aud = `http://localhost:8096/mcp`）

```bash
./bin/oauth-server \
  -addr :8096 \
  -resource http://localhost:8096/mcp \
  -auth-server http://localhost:8080 \
  -log-level DEBUG
```

兩台 server **共用同一個 Signet**，只是各自宣告不同的 `aud`。

---

## 步驟二：用同一個 client_id 換兩張不同 aud 的 token

`oauth-client` 的 token 預設快取在 `~/.cache/dcr-mcp-client/<client_id>.json`，
為了避免互蓋，這裡用 `-token-file` 把兩張 token 分開存。

### 取得 token_A（resource = 8095/mcp）

```bash
./bin/oauth-client \
  -client_id   "$CID" \
  -client_secret "$CSECRET" \
  -auth-server http://localhost:8080 \
  -mcp-url     http://localhost:8095/mcp \
  -resource    http://localhost:8095/mcp \
  -token-file  /tmp/token_A.json \
  -scopes      "mcp:read" \
  -log-level   INFO
```

預期：

- 開啟瀏覽器→Signet 同意頁→callback 完成。
- Client 印出 `who_am_i` / `show_auth_token` 結果。
- Server A log 出現 `audience verified expected_aud=http://localhost:8095/mcp got_aud=[http://localhost:8095/mcp]`。
- `/tmp/token_A.json` 內含 `access_token`。

### 取得 token_B（resource = 8096/mcp）

```bash
./bin/oauth-client \
  -client_id   "$CID" \
  -client_secret "$CSECRET" \
  -auth-server http://localhost:8080 \
  -mcp-url     http://localhost:8096/mcp \
  -resource    http://localhost:8096/mcp \
  -token-file  /tmp/token_B.json \
  -scopes      "mcp:read" \
  -force-reauth \
  -log-level   INFO
```

`-force-reauth` 強制重跑一次互動式 flow（不沿用 token_A）。
預期 Server B log 出現對應的 `audience verified ... got_aud=[.../8096/mcp]`。

> 兩張 token 的 `client_id` claim 完全相同，差別只在 `aud`——這就是
> RFC 8707 的核心：**同一個 client 可以為不同 resource 取得 audience-bound
> 的 token**。

可以解碼 JWT 驗證。`-token-file` 寫出的是 credstore 格式
（`{ "data": { "<client_id>": "<stringified token JSON>" } }`），要先把
內層字串用 `fromjson` 拆出來；JWT payload 是 base64url 編碼，需要轉成標準
base64 並補上 `=` padding 才能 decode：

```bash
decode_jwt_payload() {
  local payload
  payload=$(jq -r '.data | to_entries[0].value | fromjson | .access_token' "$1" | cut -d. -f2)
  local pad=$(( (4 - ${#payload} % 4) % 4 ))
  printf '%s%*s' "$payload" "$pad" '' | tr ' ' '=' | tr '_-' '/+' | base64 -d
}

decode_jwt_payload /tmp/token_A.json | jq '{aud, client_id, sub, scope, type}'
decode_jwt_payload /tmp/token_B.json | jq '{aud, client_id, sub, scope, type}'
```

預期輸出：

```jsonc
// token_A
{ "aud": "http://localhost:8095/mcp", "client_id": "<CID>", "sub": "...", "type": "access" }
// token_B
{ "aud": "http://localhost:8096/mcp", "client_id": "<CID>", "sub": "...", "type": "access" }
```

---

## 步驟三：四象限交叉驗證

把兩張 token 抽成環境變數方便 curl（同樣要透過 credstore wrapper 取出
`access_token`）：

```bash
export TOKEN_A=$(jq -r '.data | to_entries[0].value | fromjson | .access_token' /tmp/token_A.json)
export TOKEN_B=$(jq -r '.data | to_entries[0].value | fromjson | .access_token' /tmp/token_B.json)
```

`initialize` 是不需要 session 的 MCP JSON-RPC 方法，最適合拿來測 bearer
驗證——只關心 401 vs 200，不關心後續 MCP 內容。

| #   | Token   | Server | 預期 HTTP | 預期 server log         |
| --- | ------- | ------ | --------- | ----------------------- |
| 1   | TOKEN_A | A:8095 | 200 OK    | `audience verified`     |
| 2   | TOKEN_B | B:8096 | 200 OK    | `audience verified`     |
| 3   | TOKEN_A | B:8096 | **401**   | `audience mismatch`     |
| 4   | TOKEN_B | A:8095 | **401**   | `audience mismatch`     |
| 5   | _(none)_ | A:8095 | **401**   | `WWW-Authenticate` hint |

### 1) Happy path：A→A

```bash
curl -i -X POST http://localhost:8095/mcp \
  -H "Authorization: Bearer $TOKEN_A" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"curl","version":"0"}}}'
```

### 2) Happy path：B→B

```bash
curl -i -X POST http://localhost:8096/mcp \
  -H "Authorization: Bearer $TOKEN_B" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"curl","version":"0"}}}'
```

### 3) Cross-RS 攻擊模擬：A 的 token 打 B

```bash
curl -i -X POST http://localhost:8096/mcp \
  -H "Authorization: Bearer $TOKEN_A" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"curl","version":"0"}}}'
```

預期 response header：

```
HTTP/1.1 401 Unauthorized
WWW-Authenticate: Bearer error="invalid_token",
  resource_metadata="http://localhost:8096/.well-known/oauth-protected-resource"
```

Server B log（DEBUG）：

```
WARN audience mismatch expected_aud=http://localhost:8096/mcp got_aud=[http://localhost:8095/mcp]
```

### 4) 反向：B 的 token 打 A

```bash
curl -i -X POST http://localhost:8095/mcp \
  -H "Authorization: Bearer $TOKEN_B" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"curl","version":"0"}}}'
```

預期 Server A 出 `audience mismatch ... got_aud=[.../8096/mcp]`。

### 5) 沒有帶 Bearer

```bash
curl -i -X POST http://localhost:8095/mcp \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}'
```

預期 401 + `WWW-Authenticate: Bearer realm=..., resource_metadata="..."`，
這是 RFC 9728 給 client 發現該找哪個 AS 的提示。

---

## 步驟四：用 MCP Client 端對端再跑一次

curl 只驗證 transport 層；要驗證**整個 MCP 對話**也會擋下：把 token_A 拿
去打 Server B 跑 `who_am_i`。

```bash
# 暫時把 token_A 偽裝成 server B 要用的 token：複製到 token_B 的位置
cp /tmp/token_A.json /tmp/token_A_replay_to_B.json

./bin/oauth-client \
  -client_id   "$CID" \
  -client_secret "$CSECRET" \
  -auth-server http://localhost:8080 \
  -mcp-url     http://localhost:8096/mcp \
  -resource    http://localhost:8095/mcp  `# 故意錯：宣稱要打 8095 但 mcp-url 指向 8096` \
  -token-file  /tmp/token_A_replay_to_B.json \
  -log-level   INFO
```

預期：MCP client `connect()` / `initialize` 失敗，server B log 顯示
`audience mismatch`。

---

## 用 introspection 變體做同一組測試

如果 deployment policy 要求每個請求都打 AS（為了即時撤銷），把 server 換成
introspection 變體即可，token 那邊完全不變：

```bash
# Server A
./bin/oauth-server-introspect \
  -addr :8095 \
  -resource http://localhost:8095/mcp \
  -auth-server http://localhost:8080 \
  -introspect-client-id     mcp-resource-a \
  -introspect-client-secret rs-secret-a \
  -log-level DEBUG

# Server B
./bin/oauth-server-introspect \
  -addr :8096 \
  -resource http://localhost:8096/mcp \
  -auth-server http://localhost:8080 \
  -introspect-client-id     mcp-resource-b \
  -introspect-client-secret rs-secret-b \
  -log-level DEBUG
```

introspection 變體的 `aud` 檢查在 `oauth-server-introspect/server.go:182`
（`(*introspector).checkAudience`），跟 JWKS 變體 `oauth-server/server.go:131`
（`checkAudience`）功能一致。記得在 Signet 上幫兩台 RS 各自註冊一組
introspection 用的 client credentials。

---

## 通過標準

- [ ] `TOKEN_A → Server A` 與 `TOKEN_B → Server B` 都回 200，且 server log
      有 `audience verified`。
- [ ] `TOKEN_A → Server B` 與 `TOKEN_B → Server A` 都回 401，且 server log
      有 `audience mismatch`，error body 為 `invalid_token`。
- [ ] response header `WWW-Authenticate` 含 `resource_metadata=...`
      （RFC 9728 hint）。
- [ ] 解碼後的兩張 JWT 具有**相同的 `client_id`**、**不同的 `aud`**。
- [ ] 換成 `oauth-server-introspect/` 變體，上述全部依然成立。

達成上述全部即代表 RFC 8707 resource indicator 綁定有生效。

---

## 為什麼不能省略 `aud` 檢查？

如果 RS 不檢 `aud`，那一張 Signet 簽出來的 token——只要 `iss` / `exp` /
簽章都對——就會被同一個 Signet 後面**所有** MCP server 接受，等於開後
門讓有惡意的 RS 拿到 user 給別人的 token 後可以橫向移動。`aud` 綁定把
「使用者同意把 token 交給誰」這件事從 client 的承諾升級為 RS 可以驗證的
契約。

參考：
- [RFC 8707 §2 Resource Parameter](https://datatracker.ietf.org/doc/html/rfc8707#section-2)
- [RFC 9728 Protected Resource Metadata](https://datatracker.ietf.org/doc/html/rfc9728)
- 程式碼：`oauth-server/server.go:131` `checkAudience`、
  `oauth-server-introspect/server.go:182` `(*introspector).checkAudience`。
