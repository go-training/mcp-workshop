# OAuth MCP Client

This application demonstrates an OAuth 2.0 client that integrates with an MCP-enabled OAuth server. It performs the OAuth Authorization Code flow (with PKCE), interacts with the MCP server using authenticated HTTP requests, and showcases dynamic registration, local callback handling, and tool discovery via the MCP protocol.

---

## Features

- **Full OAuth 2.0 Authorization Code Flow with PKCE:**
  - Launches your browser for user authentication.
  - Hosts a local server to receive the OAuth callback for capturing authorization codes.
  - Handles state, PKCE, and (optionally) dynamic client registration.
- **Token Management:** Stores received tokens in-memory (can be extended to persistent storage).
- **MCP Protocol Integration:** Initializes with the MCP server, manages OAuth handshake, and lists available tools with authorized requests.
- **Clear Logging:** All actions and errors are logged to the terminal for easy debugging.

---

## OAuth Client Flow

```mermaid
sequenceDiagram
    participant Client as Client Application
    participant AuthServer as Authorization Server
    participant ResourceServer as Resource Server
    participant User as End User

    Note over Client: 1. Prepare PKCE Parameters
    Client->>Client: GenerateCodeVerifier()<br/>Generate code_verifier (64 chars)
    Client->>Client: GenerateCodeChallenge(code_verifier)<br/>SHA256 + Base64URL encoding
    Client->>Client: GenerateState()<br/>Generate anti-CSRF state (32 chars)
    Client->>Client: ValidateRedirectURI(redirect_uri)<br/>Validate redirect URI security

    Note over Client,AuthServer: 2. Authorization Request
    Client->>User: Redirect to authorization page<br/>with code_challenge, state
    User->>AuthServer: User login and authorize
    AuthServer->>AuthServer: Store code_challenge and state
    AuthServer->>Client: Redirect back to client<br/>with authorization_code

    Note over Client,AuthServer: 3. Token Exchange
    Client->>AuthServer: Request access_token<br/>with authorization_code and code_verifier
    AuthServer->>AuthServer: Verify:<br/>SHA256(code_verifier) == code_challenge
    AuthServer->>Client: Return access_token

    Note over Client,ResourceServer: 4. Access Protected Resources
    Client->>ResourceServer: Request resources with access_token
    ResourceServer->>Client: Return protected resources
```

### High-Level MCP Client Flow

```mermaid
flowchart LR
    Start("Start") --> InitClient["Initialize MCP Client"]
    InitClient --"OAuth Required Error"--> BrowserFlow["Launch Browser to Authorization URL"]
    BrowserFlow --> CallbackServer["Local Callback Server Receives Code"]
    CallbackServer --> TokenExchange["Exchange Code for Access Token"]
    TokenExchange --> ReInit["Re-initialize MCP Client"]
    ReInit --> MCPAction["Call MCP API / List Tools"]
    InitClient --"No OAuth Required"--> MCPAction
```

---

## PKCE Implementation Details

The OAuth implementation uses PKCE (Proof Key for Code Exchange) for enhanced security. Here's how the cryptographic functions work:

```mermaid
flowchart TD
    A[Start OAuth Flow] --> B[GenerateRandomString(length)]
    
    B --> B1[Create byte array]
    B1 --> B2[crypto/rand.Read fills random bytes]
    B2 --> B3{Random generation successful?}
    B3 -->|No| B4[Return error]
    B3 -->|Yes| B5[Base64URL encode and truncate to length]
    B5 --> B6[Return random string]

    A --> C[GenerateCodeVerifier]
    C --> C1[Call GenerateRandomString(64)]
    C1 --> C2[Return 64-char code_verifier]

    C2 --> D[GenerateCodeChallenge]
    D --> D1[SHA256 hash the code_verifier]
    D1 --> D2[Base64URL encode the hash]
    D2 --> D3[Return code_challenge]

    A --> E[GenerateState]
    E --> E1[Call GenerateRandomString(32)]
    E1 --> E2[Return 32-char anti-CSRF state]

    A --> F[ValidateRedirectURI]
    F --> F1{Is URI empty?}
    F1 -->|Yes| F2[Return error: URI cannot be empty]
    F1 -->|No| F3[Parse URL]
    F3 --> F4{Parse successful?}
    F4 -->|No| F5[Return error: Invalid URI]
    F4 -->|Yes| F6{Scheme is HTTP?}
    
    F6 -->|Yes| F7{Hostname is localhost<br/>or 127.0.0.1?}
    F7 -->|Yes| F8[Validation passed]
    F7 -->|No| F9[Return error: HTTP must use localhost]
    
    F6 -->|No| F10{Scheme is HTTPS?}
    F10 -->|Yes| F8
    F10 -->|No| F11[Return error: Must use HTTP+localhost or HTTPS]

    style B fill:#e1f5fe
    style C fill:#f3e5f5
    style D fill:#e8f5e8
    style E fill:#fff3e0
    style F fill:#fce4ec
```

### PKCE Security Benefits

- **Code Verifier**: 64-character random string kept secret by the client
- **Code Challenge**: SHA256 hash of code verifier, sent to authorization server
- **State Parameter**: 32-character random string to prevent CSRF attacks
- **URI Validation**: Ensures redirect URIs use localhost HTTP or any HTTPS
- **Protection**: Prevents authorization code interception attacks

---

## Detailed Flow

1. **Initialize Client**  
   The client configures and starts with the MCP server, specifying server URL, redirect URI, and scopes. Initial handshake attempts a protocol initialization.

2. **OAuth Authorization Flow**  
   If the server indicates authorization is needed:
   - PKCE parameters and a state string are generated.
   - Dynamic registration is attempted if no client credentials are preset.
   - Authorization URL is opened in the browser.
   - A local HTTP server (`:8085`) waits for the OAuth callback with the code.

3. **Token Exchange**  
   After user authorization, the code is received, verified for the correct state, and exchanged for an access token using PKCE verifier.

4. **Re-initialize and Use MCP APIs**  
   With a valid token, the client re-initializes to the MCP server and lists available tools, demonstrating fully authorized API interaction.

---

## Getting Started

### Prerequisites

- Ensure the OAuth MCP server is running and reachable at the server URL.
- Optionally, register your client credentials in advance, or let the client perform dynamic registration.

### Usage

1. Change to the client directory:

    ```bash
    cd 03-oauth-mcp/client
    ```

2. Start the client:

    ```bash
    go run client.go
    ```

    - The client will open your default browser for OAuth authorization.
    - It will start a local HTTP server at `http://localhost:8085/oauth/callback` to handle the authorization code.
    - If successful, you will see a success message in your browser, and tool information in the terminal.

---

## References

- [MCP Documentation](https://mark3.ai/docs/mcp/)
- [OAuth 2.0 RFC6749](https://datatracker.ietf.org/doc/html/rfc6749)
- [Client Source Code](client.go)

---
