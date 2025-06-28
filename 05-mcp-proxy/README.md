# MCP Proxy

## Introduction

**MCP Proxy** is a server that **aggregates multiple MCP resource servers** behind a single HTTP endpoint. It helps clients access all tools and data streams from various MCP servers through one simple connection.

---

## Why Use MCP Proxy?

Connecting directly to several MCP resource servers can quickly become **complex, difficult to manage, and insecure** as systems grow. MCP Proxy acts as a single bridge or entry point, so clients only need to connect once to reach all the backend resources.

---

## Features

- **Unified Access:** Connect to many MCP resource servers (stdio, SSE, or HTTP) through just one proxy.
- **Live Data Streaming:** Supports real-time updates with Server-Sent Events (SSE) or HTTP streaming.
- **Flexible Configuration:** Easily add, remove, or reconfigure backend servers without changing client code.
- **Simplified Deployment & Security:** Only the proxy is exposed to clients. All resource servers remain safely hidden.

---

## Architecture

This diagram shows how MCP Proxy sits between the client and multiple resource servers:

```mermaid
flowchart TD
    subgraph Clients
      UserClient[Client App<br>(HTTP/SSE)]
    end

    subgraph MCP_Proxy["MCP Proxy Server"]
      Proxy[Proxy<br>(Aggregates & Streams Data)]
    end

    subgraph Backends["MCP Resource Servers"]
      StdioMCP1[Resource Server A<br/>(stdio)]
      SSEMCP2[Resource Server B<br/>(SSE)]
      HttpMCP3[Resource Server C<br/>(HTTP/streamable-http)]
    end

    UserClient -->|Single Connection| Proxy
    Proxy -->|Fan-out| StdioMCP1
    Proxy --> SSEMCP2
    Proxy --> HttpMCP3
    StdioMCP1 -.->|Stream Events| Proxy
    SSEMCP2 -.-> Proxy
    HttpMCP3 -.-> Proxy
    Proxy -->|Streams Updates| UserClient
```

*Clients only connect to the MCP Proxy, which forwards requests and collects data from all backend MCP Resource Servers.*

---

## How It Works

1. **Client establishes a single connection** to MCP Proxy (using HTTP or SSE).
2. **MCP Proxy connects to multiple MCP resource servers** (stdio, SSE, or HTTP).
3. **Proxy aggregates all server responses** and streams tools, tasks, logs, or events back to the client in real time.
4. **Backend servers remain hidden**, improving security and manageability.

---

## Example Use Cases

- **Centralized AI Agent Control:** Manage and monitor AI agents running on multiple backends through a single web client.
- **Observability & Monitoring:** Collect logs or metrics from distributed MCP servers in one real-time dashboard.
- **Multi-domain Integration:** Orchestrate workflows or services that cross physical or network boundaries, without requiring clients to directly access each backend.

---

## Getting Started

> Coming soon: Configuration and setup instructions for running your own MCP Proxy server.

---

## Summary

MCP Proxy helps you **integrate, scale, and secure** access to your distributed MCP resource servers, combining their capabilities into one easy-to-use, real-time interface.
