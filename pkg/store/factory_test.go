package store

import (
	"context"
	"testing"

	"github.com/go-training/mcp-workshop/pkg/core"
)

func TestParseStoreType(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected StoreType
	}{
		{
			name:     "parse memory lowercase",
			input:    "memory",
			expected: StoreTypeMemory,
		},
		{
			name:     "parse memory uppercase",
			input:    "MEMORY",
			expected: StoreTypeMemory,
		},
		{
			name:     "parse memory mixed case",
			input:    "Memory",
			expected: StoreTypeMemory,
		},
		{
			name:     "parse redis lowercase",
			input:    "redis",
			expected: StoreTypeRedis,
		},
		{
			name:     "parse redis uppercase",
			input:    "REDIS",
			expected: StoreTypeRedis,
		},
		{
			name:     "parse redis mixed case",
			input:    "ReDiS",
			expected: StoreTypeRedis,
		},
		{
			name:     "invalid input returns memory",
			input:    "invalid",
			expected: StoreTypeMemory,
		},
		{
			name:     "empty string returns memory",
			input:    "",
			expected: StoreTypeMemory,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseStoreType(tt.input)
			if result != tt.expected {
				t.Errorf("ParseStoreType(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestStoreType_String(t *testing.T) {
	tests := []struct {
		name     string
		storeType StoreType
		expected string
	}{
		{
			name:     "memory to string",
			storeType: StoreTypeMemory,
			expected: "memory",
		},
		{
			name:     "redis to string",
			storeType: StoreTypeRedis,
			expected: "redis",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.storeType.String()
			if result != tt.expected {
				t.Errorf("StoreType.String() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestStoreType_IsValid(t *testing.T) {
	tests := []struct {
		name     string
		storeType StoreType
		expected bool
	}{
		{
			name:     "memory is valid",
			storeType: StoreTypeMemory,
			expected: true,
		},
		{
			name:     "redis is valid",
			storeType: StoreTypeRedis,
			expected: true,
		},
		{
			name:     "invalid type",
			storeType: StoreType("invalid"),
			expected: false,
		},
		{
			name:     "empty type",
			storeType: StoreType(""),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.storeType.IsValid()
			if result != tt.expected {
				t.Errorf("StoreType.IsValid() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestNewFactory(t *testing.T) {
	config := Config{
		Type: StoreTypeMemory,
	}
	factory := NewFactory(config)

	if factory == nil {
		t.Fatal("NewFactory() returned nil")
	}
	if factory.config.Type != StoreTypeMemory {
		t.Errorf("NewFactory() config.Type = %v, want %v", factory.config.Type, StoreTypeMemory)
	}
}

func TestFactory_Create_Memory(t *testing.T) {
	config := Config{
		Type: StoreTypeMemory,
	}
	factory := NewFactory(config)

	store, err := factory.Create()
	if err != nil {
		t.Fatalf("Factory.Create() error = %v, want nil", err)
	}
	if store == nil {
		t.Fatal("Factory.Create() returned nil store")
	}

	// Verify it's a MemoryStore
	_, ok := store.(*MemoryStore)
	if !ok {
		t.Errorf("Factory.Create() returned %T, want *MemoryStore", store)
	}
}

func TestFactory_Create_Redis(t *testing.T) {
	ctx := context.Background()

	// Setup Redis container using testcontainers
	redisAddr, err := setupRedisContainer(ctx)
	if err != nil {
		t.Skipf("Failed to setup Redis container: %v", err)
	}

	// Clean up container on test completion
	defer func() {
		if redisContainer != nil {
			_ = redisContainer.Terminate(ctx)
			redisContainer = nil
		}
	}()

	config := Config{
		Type: StoreTypeRedis,
		Redis: RedisOptions{
			Addr: redisAddr,
		},
	}
	factory := NewFactory(config)

	store, err := factory.Create()

	// Skip test if Redis is not available
	if err != nil {
		t.Skipf("Redis not available, skipping test: %v", err)
	}

	if store == nil {
		t.Fatal("Factory.Create() returned nil store")
	}

	// Verify it's a RedisStore
	redisStore, ok := store.(*RedisStore)
	if !ok {
		t.Errorf("Factory.Create() returned %T, want *RedisStore", store)
	}

	// Clean up
	if redisStore != nil {
		redisStore.Close()
	}
}

func TestFactory_Create_InvalidType(t *testing.T) {
	config := Config{
		Type: StoreType("invalid"),
	}
	factory := NewFactory(config)

	store, err := factory.Create()
	if err == nil {
		t.Error("Factory.Create() with invalid type should return error")
	}
	if store != nil {
		t.Error("Factory.Create() with invalid type should return nil store")
	}
}

func TestNewStore(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
		wantType interface{}
	}{
		{
			name: "create memory store",
			config: Config{
				Type: StoreTypeMemory,
			},
			wantErr: false,
			wantType: (*MemoryStore)(nil),
		},
		{
			name: "invalid store type",
			config: Config{
				Type: StoreType("invalid"),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, err := NewStore(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewStore() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && store == nil {
				t.Error("NewStore() returned nil store without error")
			}
		})
	}
}

func TestNewStoreFromType(t *testing.T) {
	tests := []struct {
		name      string
		storeType string
		redisOpts RedisOptions
		wantErr   bool
		checkType func(core.Store) bool
	}{
		{
			name:      "create memory store from string",
			storeType: "memory",
			wantErr:   false,
			checkType: func(s core.Store) bool {
				_, ok := s.(*MemoryStore)
				return ok
			},
		},
		{
			name:      "create memory store from uppercase string",
			storeType: "MEMORY",
			wantErr:   false,
			checkType: func(s core.Store) bool {
				_, ok := s.(*MemoryStore)
				return ok
			},
		},
		{
			name:      "invalid type defaults to memory",
			storeType: "invalid",
			wantErr:   false,
			checkType: func(s core.Store) bool {
				_, ok := s.(*MemoryStore)
				return ok
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, err := NewStoreFromType(tt.storeType, tt.redisOpts)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewStoreFromType() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if store == nil {
					t.Error("NewStoreFromType() returned nil store without error")
					return
				}
				if tt.checkType != nil && !tt.checkType(store) {
					t.Errorf("NewStoreFromType() returned wrong store type: %T", store)
				}
			}
		})
	}
}

func TestMustCreate_Success(t *testing.T) {
	config := Config{
		Type: StoreTypeMemory,
	}

	// Should not panic
	store := MustCreate(config)
	if store == nil {
		t.Error("MustCreate() returned nil store")
	}
}

func TestMustCreate_Panic(t *testing.T) {
	config := Config{
		Type: StoreType("invalid"),
	}

	defer func() {
		if r := recover(); r == nil {
			t.Error("MustCreate() with invalid config should panic")
		}
	}()

	MustCreate(config)
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.Type != StoreTypeMemory {
		t.Errorf("DefaultConfig().Type = %v, want %v", config.Type, StoreTypeMemory)
	}
}

func TestRedisConfig(t *testing.T) {
	opts := RedisOptions{
		Addr:     "localhost:6379",
		Password: "secret",
		DB:       1,
	}
	config := RedisConfig(opts)

	if config.Type != StoreTypeRedis {
		t.Errorf("RedisConfig().Type = %v, want %v", config.Type, StoreTypeRedis)
	}
	if config.Redis.Addr != opts.Addr {
		t.Errorf("RedisConfig().Redis.Addr = %v, want %v", config.Redis.Addr, opts.Addr)
	}
	if config.Redis.Password != opts.Password {
		t.Errorf("RedisConfig().Redis.Password = %v, want %v", config.Redis.Password, opts.Password)
	}
	if config.Redis.DB != opts.DB {
		t.Errorf("RedisConfig().Redis.DB = %v, want %v", config.Redis.DB, opts.DB)
	}
}

func TestMemoryConfig(t *testing.T) {
	config := MemoryConfig()

	if config.Type != StoreTypeMemory {
		t.Errorf("MemoryConfig().Type = %v, want %v", config.Type, StoreTypeMemory)
	}
}

func TestFactory_Integration(t *testing.T) {
	// Test creating multiple stores from the same factory
	config := Config{
		Type: StoreTypeMemory,
	}
	factory := NewFactory(config)

	// Create first store
	store1, err := factory.Create()
	if err != nil {
		t.Fatalf("Factory.Create() first call error = %v", err)
	}
	if store1 == nil {
		t.Fatal("Factory.Create() first call returned nil")
	}

	// Create second store
	store2, err := factory.Create()
	if err != nil {
		t.Fatalf("Factory.Create() second call error = %v", err)
	}
	if store2 == nil {
		t.Fatal("Factory.Create() second call returned nil")
	}

	// Verify they are different instances
	if store1 == store2 {
		t.Error("Factory.Create() returned the same instance twice")
	}
}

func TestStoreType_ConfigRoundTrip(t *testing.T) {
	// Test that we can parse a string, create a config, and get back the same type
	tests := []struct {
		name  string
		input string
		want  StoreType
	}{
		{"memory roundtrip", "memory", StoreTypeMemory},
		{"redis roundtrip", "redis", StoreTypeRedis},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse string to StoreType
			storeType := ParseStoreType(tt.input)

			// Create config
			config := Config{Type: storeType}

			// Verify we got the expected type
			if config.Type != tt.want {
				t.Errorf("roundtrip failed: got %v, want %v", config.Type, tt.want)
			}

			// Verify string representation matches
			if config.Type.String() != tt.want.String() {
				t.Errorf("string representation mismatch: got %v, want %v",
					config.Type.String(), tt.want.String())
			}
		})
	}
}
