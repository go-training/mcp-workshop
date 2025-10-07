# Store Package

This package provides storage implementations for OAuth 2.0 authorization codes and clients.

## Factory Pattern

The package provides a factory pattern for creating store instances, making it easy to switch between different store implementations.

### Basic Usage

```go
import "github.com/go-training/mcp-workshop/pkg/store"

// Option 1: Using Config struct
config := store.Config{
    Type: store.StoreTypeMemory,
}
store, err := store.NewStore(config)
if err != nil {
    log.Fatal(err)
}

// Option 2: Using convenience functions
store := store.MustCreate(store.MemoryConfig())

// Option 3: From command-line flags
storeType := "redis" // from flag.StringVar
redisAddr := "localhost:6379" // from flag.StringVar
store, err := store.NewStoreFromType(storeType, store.RedisOptions{
    Addr: redisAddr,
})
```

### Configuration Helpers

```go
// Memory store configuration
config := store.MemoryConfig()

// Redis store configuration
config := store.RedisConfig(store.RedisOptions{
    Addr:     "localhost:6379",
    Password: "secret",
    DB:       1,
})

// Default configuration (memory)
config := store.DefaultConfig()
```

### Factory Instance

```go
// Create a reusable factory
factory := store.NewFactory(store.Config{
    Type: store.StoreTypeRedis,
    Redis: store.RedisOptions{
        Addr: "localhost:6379",
    },
})

// Create multiple store instances
store1, _ := factory.Create()
store2, _ := factory.Create()
```

## Available Implementations

### 1. MemoryStore

In-memory storage implementation with thread-safe operations using sync.RWMutex.

**Usage:**

```go
import "github.com/go-training/mcp-workshop/pkg/store"

store := store.NewMemoryStore()

// Save authorization code
code := &core.AuthorizationCode{
    Code:      "test-code",
    ClientID:  "test-client",
    ExpiresAt: time.Now().Add(10 * time.Minute).Unix(),
}
err := store.SaveAuthorizationCode(ctx, code)
```

**Pros:**

- No external dependencies
- Fast access
- Simple setup

**Cons:**

- Data lost on restart
- Not suitable for distributed systems
- Limited by available memory

### 2. RedisStore

Redis-based storage implementation using rueidis client.

**Usage:**

```go
import (
    "github.com/go-training/mcp-workshop/pkg/store"
    "github.com/redis/rueidis"
)

// Option 1: Create with simplified options (recommended)
store, err := store.NewRedisStoreFromOptions(store.RedisOptions{
    Addr:     "localhost:6379",
    Password: "your-password",
    DB:       0,
})
if err != nil {
    log.Fatal(err)
}
defer store.Close()

// Option 2: Create with full rueidis client options
store, err := store.NewRedisStoreFromClientOption(rueidis.ClientOption{
    InitAddress: []string{"localhost:6379"},
    Password:    "your-password",
    SelectDB:    0,
})
if err != nil {
    log.Fatal(err)
}
defer store.Close()

// Option 3: Create with rueidis client directly
client, err := rueidis.NewClient(rueidis.ClientOption{
    InitAddress: []string{"localhost:6379"},
})
if err != nil {
    log.Fatal(err)
}
store := store.NewRedisStore(client)
defer store.Close()

// Save authorization code
code := &core.AuthorizationCode{
    Code:      "test-code",
    ClientID:  "test-client",
    ExpiresAt: time.Now().Add(10 * time.Minute).Unix(),
}
err = store.SaveAuthorizationCode(ctx, code)
```

**Pros:**

- Persistent storage
- Suitable for distributed systems
- Automatic expiration via TTL
- High performance

**Cons:**

- Requires Redis server
- Network dependency
- Additional operational complexity

## Key Features

### Authorization Code Storage

- **SaveAuthorizationCode**: Store authorization codes with automatic TTL based on expiration time
- **GetAuthorizationCode**: Retrieve authorization codes by client ID
- **DeleteAuthorizationCode**: Remove authorization codes (e.g., after token exchange)

### Client Management

- **CreateClient**: Register new OAuth clients
- **GetClient**: Retrieve client information by ID
- **UpdateClient**: Update existing client configuration
- **DeleteClient**: Remove client registrations

## Redis Configuration

### Basic Configuration

```go
opts := rueidis.ClientOption{
    InitAddress: []string{"localhost:6379"},
}
```

### Cluster Configuration

```go
opts := rueidis.ClientOption{
    InitAddress: []string{
        "localhost:7000",
        "localhost:7001",
        "localhost:7002",
    },
}
```

### With Authentication

```go
opts := rueidis.ClientOption{
    InitAddress: []string{"localhost:6379"},
    Password:    "your-redis-password",
    SelectDB:    0,
}
```

### With TLS

```go
opts := rueidis.ClientOption{
    InitAddress: []string{"localhost:6379"},
    TLSConfig: &tls.Config{
        // TLS configuration
    },
}
```

## Testing

The package includes comprehensive tests for both implementations.

### Running Tests

```bash
# Run all store tests
go test ./pkg/store/... -v

# Run only memory store tests
go test ./pkg/store/... -v -run TestMemoryStore

# Run only redis store tests (requires Docker)
go test ./pkg/store/... -v -run TestRedisStore
```

### Redis Test Requirements

Redis tests use [testcontainers-go](https://golang.testcontainers.org/) to automatically start and manage Redis containers. This requires:

1. **Docker** must be installed and running
2. **Docker daemon** must be accessible

If Docker is not available, the tests will be automatically skipped with an informative message.

#### Manual Redis Testing

If you prefer to test against a manually started Redis instance:

```bash
# Using Docker
docker run -d -p 6379:6379 redis:alpine

# Or using docker-compose
docker-compose up -d redis
```

Then modify the test to connect to localhost:6379 instead of using testcontainers.

#### Testcontainers Benefits

- **Automatic setup**: No manual Redis installation required
- **Isolation**: Each test run uses a fresh Redis instance
- **Cleanup**: Containers are automatically removed after tests
- **CI/CD friendly**: Works in any environment with Docker

## Error Handling

The package defines standard errors:

- `ErrCodeNotFound`: Authorization code not found or expired
- `ErrNilAuthorizationCode`: Attempting to save nil authorization code
- `ErrEmptyCode`: Authorization code string is empty
- `ErrClientNotFound`: Client not found
- `ErrNilClient`: Attempting to save nil client
- `ErrEmptyClientID`: Client ID is empty

## Best Practices

1. **Choose the right store**:

   - Use MemoryStore for development/testing
   - Use RedisStore for production/distributed systems

2. **Handle expiration**:

   - Authorization codes automatically expire based on `ExpiresAt` field
   - Redis TTL handles cleanup automatically
   - MemoryStore checks expiration on retrieval

3. **Close connections**:

   ```go
   defer store.Close() // For RedisStore
   ```

4. **Use context**:

   ```go
   ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
   defer cancel()

   err := store.SaveAuthorizationCode(ctx, code)
   ```

5. **Error handling**:

   ```go
   code, err := store.GetAuthorizationCode(ctx, clientID)
   if errors.Is(err, store.ErrCodeNotFound) {
       // Handle not found case
   } else if err != nil {
       // Handle other errors
   }
   ```
