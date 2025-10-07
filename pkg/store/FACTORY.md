# Store Factory Pattern

The store package implements the Factory Pattern to provide a unified interface for creating different types of storage backends.

## Design

### Problem

The application needs to support multiple storage backends (memory, Redis) without requiring changes to application code when switching between them.

### Solution

The Factory Pattern provides:

1. A single interface (`core.Store`) for all storage implementations
2. Factory functions to create store instances based on configuration
3. Type-safe configuration with compile-time checks
4. Easy extension for new storage backends

## Architecture

```
┌─────────────────────────────────────────────┐
│           Application Code                   │
│  (uses core.Store interface)                │
└──────────────┬──────────────────────────────┘
               │
               ▼
┌─────────────────────────────────────────────┐
│         Store Factory                        │
│  ┌─────────────────────────────┐            │
│  │  NewStore(config)            │            │
│  │  NewStoreFromType(...)       │            │
│  │  MustCreate(config)          │            │
│  └─────────────┬────────────────┘            │
└────────────────┼─────────────────────────────┘
                 │
         ┌───────┴───────┐
         ▼               ▼
┌────────────────┐ ┌──────────────┐
│  MemoryStore   │ │  RedisStore  │
└────────────────┘ └──────────────┘
```

## Components

### 1. Store Interface (`core.Store`)

Defines the contract all stores must implement:

```go
type Store interface {
    SaveAuthorizationCode(ctx context.Context, code *AuthorizationCode) error
    GetAuthorizationCode(ctx context.Context, clientID string) (*AuthorizationCode, error)
    DeleteAuthorizationCode(ctx context.Context, clientID string) error
    GetClient(ctx context.Context, clientID string) (*Client, error)
    CreateClient(ctx context.Context, client *Client) error
    UpdateClient(ctx context.Context, client *Client) error
    DeleteClient(ctx context.Context, clientID string) error
}
```

### 2. Configuration (`store.Config`)

Type-safe configuration structure:

```go
type Config struct {
    Type  StoreType      // memory or redis
    Redis RedisOptions   // Redis-specific config
}

type StoreType string

const (
    StoreTypeMemory StoreType = "memory"
    StoreTypeRedis  StoreType = "redis"
)
```

### 3. Factory Functions

#### NewStore

Creates a store from configuration:

```go
store, err := store.NewStore(store.Config{
    Type: store.StoreTypeMemory,
})
```

#### NewStoreFromType

Convenient for CLI flag parsing:

```go
store, err := store.NewStoreFromType("redis", store.RedisOptions{
    Addr: "localhost:6379",
})
```

#### MustCreate

Panics on error (for initialization code):

```go
store := store.MustCreate(store.MemoryConfig())
```

### 4. Configuration Helpers

Pre-built configurations for common scenarios:

```go
// Default (memory)
config := store.DefaultConfig()

// Memory store
config := store.MemoryConfig()

// Redis store
config := store.RedisConfig(store.RedisOptions{
    Addr:     "localhost:6379",
    Password: "secret",
    DB:       1,
})
```

### 5. Factory Instance

Reusable factory for creating multiple stores:

```go
factory := store.NewFactory(config)
store1, _ := factory.Create()
store2, _ := factory.Create()
```

## Usage Patterns

### Pattern 1: Command-Line Application

```go
func main() {
    var storeType string
    var redisAddr string
    flag.StringVar(&storeType, "store", "memory", "Store type")
    flag.StringVar(&redisAddr, "redis-addr", "localhost:6379", "Redis address")
    flag.Parse()

    store, err := store.NewStoreFromType(storeType, store.RedisOptions{
        Addr: redisAddr,
    })
    if err != nil {
        log.Fatal(err)
    }

    // Use store...
}
```

### Pattern 2: Configuration File

```go
type AppConfig struct {
    Store struct {
        Type     string `yaml:"type"`
        RedisURL string `yaml:"redis_url"`
    } `yaml:"store"`
}

func initStore(cfg AppConfig) (core.Store, error) {
    config := store.Config{
        Type: store.ParseStoreType(cfg.Store.Type),
        Redis: store.RedisOptions{
            Addr: cfg.Store.RedisURL,
        },
    }
    return store.NewStore(config)
}
```

### Pattern 3: Environment Variables

```go
func initStoreFromEnv() (core.Store, error) {
    storeType := os.Getenv("STORE_TYPE")
    if storeType == "" {
        storeType = "memory"
    }

    return store.NewStoreFromType(storeType, store.RedisOptions{
        Addr:     os.Getenv("REDIS_ADDR"),
        Password: os.Getenv("REDIS_PASSWORD"),
        DB:       0,
    })
}
```

### Pattern 4: Testing

```go
func TestMyFeature(t *testing.T) {
    // Use memory store for tests
    store := store.MustCreate(store.MemoryConfig())

    // Test code...
}
```

### Pattern 5: Dependency Injection

```go
type Server struct {
    store core.Store
}

func NewServer(config store.Config) (*Server, error) {
    s, err := store.NewStore(config)
    if err != nil {
        return nil, err
    }

    return &Server{store: s}, nil
}
```

## Benefits

### 1. Flexibility

Switch between storage backends without code changes:

```go
// Development
store := store.MustCreate(store.MemoryConfig())

// Production
store := store.MustCreate(store.RedisConfig(redisOpts))
```

### 2. Type Safety

Compile-time checking of store types:

```go
// This won't compile if StoreType changes
config := store.Config{
    Type: store.StoreTypeMemory,
}
```

### 3. Testability

Easy to mock or use in-memory store for tests:

```go
func TestHandler(t *testing.T) {
    store := store.MustCreate(store.MemoryConfig())
    handler := NewHandler(store)
    // Test...
}
```

### 4. Extensibility

Add new store types without breaking existing code:

```go
// Future: add PostgreSQL store
const StoreTypePostgres StoreType = "postgres"

func (f *Factory) Create() (core.Store, error) {
    switch f.config.Type {
    case StoreTypeMemory:
        return NewMemoryStore(), nil
    case StoreTypeRedis:
        return NewRedisStoreFromOptions(f.config.Redis)
    case StoreTypePostgres:  // New!
        return NewPostgresStore(f.config.Postgres)
    }
}
```

### 5. Configuration Validation

```go
storeType := store.ParseStoreType(userInput)
if !storeType.IsValid() {
    return fmt.Errorf("invalid store type")
}
```

## Best Practices

### 1. Use Config Helpers

```go
// Good: Type-safe and clear
config := store.RedisConfig(store.RedisOptions{...})

// Avoid: Manual config construction
config := store.Config{Type: "redis", ...} // Type is string, not StoreType
```

### 2. Handle Cleanup

```go
store, err := store.NewStore(config)
if err != nil {
    return err
}

// Clean up Redis connections
if rs, ok := store.(*store.RedisStore); ok {
    defer rs.Close()
}
```

### 3. Use MustCreate for Initialization

```go
var globalStore core.Store

func init() {
    // Panics are acceptable during initialization
    globalStore = store.MustCreate(store.MemoryConfig())
}
```

### 4. Parse User Input Safely

```go
// Always use ParseStoreType for user input
storeType := store.ParseStoreType(userInput)
// Invalid input defaults to memory store
```

### 5. Test Both Store Types

```go
func TestFeature(t *testing.T) {
    stores := []struct {
        name   string
        config store.Config
    }{
        {"memory", store.MemoryConfig()},
        {"redis", store.RedisConfig(redisOpts)},
    }

    for _, tc := range stores {
        t.Run(tc.name, func(t *testing.T) {
            s, err := store.NewStore(tc.config)
            // Test...
        })
    }
}
```

## Implementation Details

### Adding a New Store Type

1. Implement the `core.Store` interface:

```go
type MyStore struct {
    // fields
}

func (m *MyStore) SaveAuthorizationCode(ctx context.Context, code *core.AuthorizationCode) error {
    // implementation
}

// ... implement other methods
```

2. Add store type constant:

```go
const StoreTypeMyStore StoreType = "mystore"
```

3. Update factory:

```go
func (f *Factory) Create() (core.Store, error) {
    switch f.config.Type {
    case StoreTypeMemory:
        return NewMemoryStore(), nil
    case StoreTypeRedis:
        return NewRedisStoreFromOptions(f.config.Redis)
    case StoreTypeMyStore:
        return NewMyStore(f.config.MyStore), nil
    default:
        return nil, fmt.Errorf("unsupported store type: %s", f.config.Type)
    }
}
```

4. Add configuration helper:

```go
func MyStoreConfig(opts MyStoreOptions) Config {
    return Config{
        Type:    StoreTypeMyStore,
        MyStore: opts,
    }
}
```

5. Update `IsValid()` method:

```go
func (t StoreType) IsValid() bool {
    switch t {
    case StoreTypeMemory, StoreTypeRedis, StoreTypeMyStore:
        return true
    default:
        return false
    }
}
```

## See Also

- [Store Package Documentation](README.md)
- [Memory Store Implementation](memory.go)
- [Redis Store Implementation](redis.go)
- [Factory Pattern (Wikipedia)](https://en.wikipedia.org/wiki/Factory_method_pattern)
