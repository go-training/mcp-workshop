package store

import (
	"fmt"
	"strings"

	"github.com/go-training/mcp-workshop/pkg/core"
)

// StoreType represents the type of store backend.
type StoreType string

const (
	// StoreTypeMemory represents in-memory storage.
	StoreTypeMemory StoreType = "memory"
	// StoreTypeRedis represents Redis storage.
	StoreTypeRedis StoreType = "redis"
)

// Config contains configuration for creating a store.
type Config struct {
	// Type specifies the store type (memory or redis).
	Type StoreType
	// Redis contains Redis-specific configuration.
	Redis RedisOptions
}

// Factory creates store instances based on configuration.
type Factory struct {
	config Config
}

// NewFactory creates a new store factory with the provided configuration.
func NewFactory(config Config) *Factory {
	return &Factory{
		config: config,
	}
}

// Create creates and returns a new store instance based on the factory configuration.
// Returns an error if the store type is invalid or if store creation fails.
func (f *Factory) Create() (core.Store, error) {
	switch f.config.Type {
	case StoreTypeMemory:
		return NewMemoryStore(), nil
	case StoreTypeRedis:
		return NewRedisStoreFromOptions(f.config.Redis)
	default:
		return nil, fmt.Errorf("unsupported store type: %s", f.config.Type)
	}
}

// NewStore is a convenience function that creates a store directly from configuration.
// It's equivalent to NewFactory(config).Create().
func NewStore(config Config) (core.Store, error) {
	factory := NewFactory(config)
	return factory.Create()
}

// NewStoreFromType creates a store from a type string and optional Redis configuration.
// This is useful for command-line flag parsing.
func NewStoreFromType(storeType string, redisOpts RedisOptions) (core.Store, error) {
	config := Config{
		Type:  ParseStoreType(storeType),
		Redis: redisOpts,
	}
	return NewStore(config)
}

// ParseStoreType parses a string into a StoreType.
// Returns StoreTypeMemory for invalid inputs.
func ParseStoreType(s string) StoreType {
	switch strings.ToLower(s) {
	case "memory":
		return StoreTypeMemory
	case "redis":
		return StoreTypeRedis
	default:
		return StoreTypeMemory
	}
}

// String returns the string representation of a StoreType.
func (t StoreType) String() string {
	return string(t)
}

// IsValid returns true if the StoreType is valid.
func (t StoreType) IsValid() bool {
	switch t {
	case StoreTypeMemory, StoreTypeRedis:
		return true
	default:
		return false
	}
}

// MustCreate creates a store and panics if creation fails.
// This is useful for initialization where store creation must succeed.
func MustCreate(config Config) core.Store {
	store, err := NewStore(config)
	if err != nil {
		panic(fmt.Sprintf("failed to create store: %v", err))
	}
	return store
}

// DefaultConfig returns the default store configuration (memory store).
func DefaultConfig() Config {
	return Config{
		Type: StoreTypeMemory,
	}
}

// RedisConfig creates a Redis store configuration with the provided options.
func RedisConfig(redisOpts RedisOptions) Config {
	return Config{
		Type:  StoreTypeRedis,
		Redis: redisOpts,
	}
}

// MemoryConfig creates a memory store configuration.
func MemoryConfig() Config {
	return Config{
		Type: StoreTypeMemory,
	}
}
