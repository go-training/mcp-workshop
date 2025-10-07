# OAuth MCP Server

OAuth 2.0 authorization server with MCP integration, supporting multiple OAuth providers and storage backends.

## Features

- **Multiple OAuth Providers**: GitHub, Gitea, GitLab
- **Flexible Storage**: Choose between in-memory or Redis-backed storage
- **OAuth 2.0 with PKCE**: Full authorization code flow support
- **MCP Protocol**: Authenticated MCP tools integration

## Quick Start

### Basic Usage (Memory Store)

```bash
go run . -client_id=<your-client-id> -client_secret=<your-client-secret>
```

This will start the server on port 8095 using in-memory storage.

### Using Redis Store

```bash
# Start Redis (using Docker)
docker run -d -p 6379:6379 redis:alpine

# Run server with Redis
go run . \
  -client_id=<your-client-id> \
  -client_secret=<your-client-secret> \
  -store redis \
  -redis-addr localhost:6379
```

### Using Different Providers

```bash
# GitHub (default)
go run . -client_id=<id> -client_secret=<secret> -provider github

# GitLab
go run . -client_id=<id> -client_secret=<secret> -provider gitlab

# Gitea
go run . -client_id=<id> -client_secret=<secret> -provider gitea -gitea-host https://gitea.com
```

## Command-Line Flags

### Required Flags

- `-client_id`: OAuth 2.0 Client ID from your OAuth provider
- `-client_secret`: OAuth 2.0 Client Secret from your OAuth provider

### Optional Flags

#### Server Configuration

- `-addr`: Server address (default: `:8095`)
- `-log-level`: Logging level: DEBUG, INFO, WARN, ERROR (default: DEBUG)

#### OAuth Provider

- `-provider`: OAuth provider name (default: `github`)
  - Supported: `github`, `gitea`, `gitlab`
- `-gitea-host`: Gitea host URL (default: `https://gitea.com`)
- `-gitlab-host`: GitLab host URL (default: `https://gitlab.com`)

#### Storage Backend

- `-store`: Storage type (default: `memory`)
  - `memory`: In-memory storage (data lost on restart)
  - `redis`: Redis-backed persistent storage
- `-redis-addr`: Redis server address (default: `localhost:6379`)
  - Only used when `-store=redis`
- `-redis-password`: Redis password (optional)
  - Only used when `-store=redis`
- `-redis-db`: Redis database number (default: 0)
  - Only used when `-store=redis`

## Storage Options

### Memory Store

**Pros:**

- No external dependencies
- Fast access
- Simple setup
- Good for development/testing

**Cons:**

- Data lost on restart
- Not suitable for production with multiple instances
- Limited by available memory

**Usage:**

```bash
go run . -client_id=<id> -client_secret=<secret> -store memory
```

### Redis Store

**Pros:**

- Persistent storage
- Suitable for production
- Supports multiple server instances
- Automatic expiration handling

**Cons:**

- Requires Redis server
- Network dependency
- Additional operational complexity

**Usage:**

```bash
# Basic Redis connection
go run . -client_id=<id> -client_secret=<secret> -store redis

# Redis with authentication
go run . \
  -client_id=<id> \
  -client_secret=<secret> \
  -store redis \
  -redis-addr localhost:6379 \
  -redis-password mypassword \
  -redis-db 1
```

## Endpoints

- `GET /.well-known/oauth-authorization-server`: OAuth authorization server metadata
- `GET /.well-known/oauth-protected-resource`: Protected resource metadata
- `GET /authorize`: OAuth authorization endpoint
- `POST /token`: OAuth token endpoint
- `POST /register`: Dynamic client registration endpoint
- `POST /mcp`: MCP protocol endpoint (requires authentication)
- `GET /mcp`: MCP protocol SSE endpoint (requires authentication)
- `DELETE /mcp`: MCP protocol endpoint (requires authentication)

## Examples

### Development Setup

```bash
# Terminal 1: Start server with memory store
go run . -client_id=test-id -client_secret=test-secret

# Terminal 2: Run OAuth client example
cd ../oauth-client
go run .
```

### Production Setup with Redis

```bash
# 1. Start Redis
docker run -d --name redis \
  -p 6379:6379 \
  redis:alpine

# 2. Start OAuth server
go run . \
  -client_id=<production-client-id> \
  -client_secret=<production-client-secret> \
  -provider github \
  -store redis \
  -redis-addr localhost:6379 \
  -log-level INFO \
  -addr :8095
```

### Using Redis Cluster

```bash
go run . \
  -client_id=<id> \
  -client_secret=<secret> \
  -store redis \
  -redis-addr "node1:7000,node2:7001,node3:7002"
```

## Environment Variables

You can also configure Redis connection using environment variables in your code:

```go
// Example: Load from environment
store, err := store.NewRedisStoreFromOptions(store.RedisOptions{
    Addr:     os.Getenv("REDIS_ADDR"),     // default: "localhost:6379"
    Password: os.Getenv("REDIS_PASSWORD"), // default: ""
    DB:       0,                            // or parse from env
})
```

## Testing

```bash
# Test with curl
curl http://localhost:8095/.well-known/oauth-authorization-server

# Register a client
curl -X POST http://localhost:8095/register \
  -H "Content-Type: application/json" \
  -d '{
    "redirect_uris": ["http://localhost:8080/callback"],
    "grant_types": ["authorization_code"],
    "response_types": ["code"]
  }'
```

## Troubleshooting

### Redis Connection Issues

If you see "failed to create Redis store" error:

1. Verify Redis is running:

   ```bash
   redis-cli ping
   # Should return: PONG
   ```

2. Check Redis connection:

   ```bash
   redis-cli -h localhost -p 6379
   ```

3. Verify network connectivity:

   ```bash
   telnet localhost 6379
   ```

### Memory Store Issues

If authorization codes expire too quickly, they are automatically cleaned up. Default expiration is 10 minutes.

## See Also

- [Store Package Documentation](../../pkg/store/README.md)
- [OAuth Client Example](../oauth-client/)
- [MCP Protocol](https://github.com/mark3labs/mcp-go)
