# mcp-workshop

This workshop provides a comprehensive guide to building both MCP ([Model Context Protocol][1]) servers and clients using the [Go programming language][2]. You will learn how to leverage MCP to streamline your workflow and enhance your development environment.

## Table of Contents

- [mcp-workshop](#mcp-workshop)
  - [Table of Contents](#table-of-contents)
  - [MCP Inspector](#mcp-inspector)
  - [OAuth in MCP](#oauth-in-mcp)
  - [MCP Vulnerabilities](#mcp-vulnerabilities)

![cover](./images/cover.png)

[1]: https://modelcontextprotocol.io/introduction
[2]: https://go.dev

## MCP Inspector

The MCP Inspector is a developer tool designed for testing and debugging MCP servers. Similar to Postman, it enables you to send requests to MCP servers and view their responses. This tool is invaluable for developers working with MCP, providing a streamlined workflow for server interaction and troubleshooting.

![inspector](./images/inspector.png)

## OAuth in MCP

The following diagrams illustrate the OAuth flow within MCP, including the sequence of communication between each role.

![oauth](./images/oauth-flow-01.png)

Sequence diagram showing communication with each role separately.

![oauth-sequence](./images/oauth-flow-02.png)

For more information on OAuth in MCP, refer to the [Let's fix OAuth in MCP][3] or the [MCP Authorization][4] documentation.

[3]: https://aaronparecki.com/2025/04/03/15/oauth-for-model-context-protocol
[4]: https://modelcontextprotocol.io/specification/2025-03-26/basic/authorization

The expected flow for obtaining a valid access token via OAuth is depicted in the [MCP Specification](https://modelcontextprotocol.io/specification/draft/basic/authorization#authorization-flow-steps).  For convenience, we've embedded a copy of the authorization flow below.  Please study it carefully as the remainder of this document is written with this flow in mind.

```mermaid
sequenceDiagram
    participant B as User-Agent (Browser)
    participant C as Client
    participant M as MCP Server (Resource Server)
    participant A as Authorization Server

    C->>M: MCP request without token
    M->>C: HTTP 401 Unauthorized with WWW-Authenticate header
    Note over C: Extract resource_metadata URL from WWW-Authenticate

    C->>M: Request Protected Resource Metadata
    M->>C: Return metadata

    Note over C: Parse metadata and extract authorization server(s)<br/>Client determines AS to use

    C->>A: GET /.well-known/oauth-authorization-server
    A->>C: Authorization server metadata response

    alt Dynamic client registration
        C->>A: POST /register
        A->>C: Client Credentials
    end

    Note over C: Generate PKCE parameters
    C->>B: Open browser with authorization URL + code_challenge
    B->>A: Authorization request
    Note over A: User authorizes
    A->>B: Redirect to callback with authorization code
    B->>C: Authorization code callback
    C->>A: Token request + code_verifier
    A->>C: Access token (+ refresh token)
    C->>M: MCP request with access token
    M-->>C: MCP response
    Note over C,M: MCP communication continues with valid token
```

> [!NOTE]
> Dynamic Client Registration is NOT supported by Remote MCP Server at this time.

## MCP Vulnerabilities

The following are some known vulnerabilities in MCP implementations:

![vulnerabilities](./images/vulnerabilities.gif)

- Command Injection (Impact: Moderate 🟡)
- Tool Poisoning (Impact: Severe 🔴)
- Open Connections via SSE (Impact: Moderate 🟠)
- Privilege Escalation (Impact: Severe 🔴)
- Persistent Context Misuse (Impact: Low, but risky 🟡)
- Server Data Takeover/Spoofing (Impact: Severe 🔴)

For more information, refer to the [MCP Vulnerabilities][11].

[11]: https://www.linkedin.com/posts/eordax_ai-mcp-genai-activity-7333057511651954688-sbNO
