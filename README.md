# mcp-workshop

This workshop guides you through building both MCP ([Model Context Protocol][1]) servers and clients using the [Go programming][2] language. You will learn how to leverage MCP to enhance your workflow and improve your development environment.

## Table of Contents

- [mcp-workshop](#mcp-workshop)
  - [Table of Contents](#table-of-contents)
  - [MCP Inspector](#mcp-inspector)
  - [OAuth in MCP](#oauth-in-mcp)
  - [MCP Vulnerabilities](#mcp-vulnerabilities)

![cover](./images/cover.png)

[1]:https://modelcontextprotocol.io/introduction
[2]:https://go.dev

## MCP Inspector

The MCP inspector is a developer tool for testing and debugging MCP servers. Similar to Postman, it allows you to send requests to MCP servers and view the responses. It is a valuable tool for developers working with MCP.

![inspector](./images/inspector.png)

## OAuth in MCP

![oauth](./images/oauth-flow-01.png)

Sequence diagram showing communication with each role separately.

![oauth-sequence](./images/oauth-flow-02.png)

## MCP Vulnerabilities

![vulnerabilities](./images/vulnerabilities.gif)

- Command Injection (Impact: Moderate 🟡 )
- Tool Poisoning (Impact: Severe 🔴 )
- Open Connections via SSE (Impact: Moderate 🟠)
- Privilege Escalation (Impact: Severe 🔴 )
- Persistent Context Misuse (Impact: Low, but risky 🟡 )
- Server Data Takeover/spoofing (Impact: Severe 🔴 )

More details can be found in the [MCP Vulnerabilities][11].

[11]: https://www.linkedin.com/posts/eordax_ai-mcp-genai-activity-7333057511651954688-sbNO
