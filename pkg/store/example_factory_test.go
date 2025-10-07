package store_test

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/go-training/mcp-workshop/pkg/core"
	"github.com/go-training/mcp-workshop/pkg/store"
)

// Example demonstrates basic usage of the store factory.
func Example() {
	// Create a memory store using the factory
	config := store.MemoryConfig()
	s, err := store.NewStore(config)
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	// Create a client
	client := &core.Client{
		ID:     "example-client",
		Secret: "secret",
	}
	if err := s.CreateClient(ctx, client); err != nil {
		log.Fatal(err)
	}

	// Retrieve the client
	retrieved, err := s.GetClient(ctx, "example-client")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(retrieved.ID)
	// Output: example-client
}

// Example_memoryStore demonstrates creating a memory store.
func Example_memoryStore() {
	// Method 1: Using helper function
	config := store.MemoryConfig()
	s, err := store.NewStore(config)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Store type: %T\n", s)
	// Output: Store type: *store.MemoryStore
}

// Example_redisStore demonstrates creating a Redis store (will fail if Redis is not available).
func Example_redisStore() {
	// Method 1: Using helper function
	config := store.RedisConfig(store.RedisOptions{
		Addr: "localhost:6379",
		DB:   0,
	})

	s, err := store.NewStore(config)
	if err != nil {
		fmt.Println("Redis not available (this is expected in tests)")
		return
	}
	defer func() {
		if rs, ok := s.(*store.RedisStore); ok {
			rs.Close()
		}
	}()

	fmt.Printf("Store type: %T\n", s)
}

// Example_factory demonstrates using the factory pattern.
func Example_factory() {
	// Create a factory
	factory := store.NewFactory(store.MemoryConfig())

	// Create multiple store instances
	store1, err := factory.Create()
	if err != nil {
		log.Fatal(err)
	}

	store2, err := factory.Create()
	if err != nil {
		log.Fatal(err)
	}

	// They are different instances
	fmt.Printf("Same instance: %v\n", store1 == store2)
	// Output: Same instance: false
}

// Example_parseStoreType demonstrates parsing store types from strings.
func Example_parseStoreType() {
	// Parse from string (useful for CLI flags)
	memoryType := store.ParseStoreType("memory")
	redisType := store.ParseStoreType("redis")
	invalidType := store.ParseStoreType("invalid")

	fmt.Printf("memory: %s (valid: %v)\n", memoryType, memoryType.IsValid())
	fmt.Printf("redis: %s (valid: %v)\n", redisType, redisType.IsValid())
	fmt.Printf("invalid: %s (valid: %v)\n", invalidType, invalidType.IsValid())

	// Output:
	// memory: memory (valid: true)
	// redis: redis (valid: true)
	// invalid: memory (valid: true)
}

// Example_fromCommandLineFlags demonstrates creating a store from command-line flags.
func Example_fromCommandLineFlags() {
	// Simulate command-line flags
	storeType := "memory" // from flag.StringVar
	redisAddr := "localhost:6379"
	redisPassword := ""
	redisDB := 0

	// Create store from flags
	s, err := store.NewStoreFromType(storeType, store.RedisOptions{
		Addr:     redisAddr,
		Password: redisPassword,
		DB:       redisDB,
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Store type: %T\n", s)
	// Output: Store type: *store.MemoryStore
}

// Example_mustCreate demonstrates the MustCreate helper for initialization.
func Example_mustCreate() {
	// Use MustCreate when store creation must succeed (e.g., in init functions)
	s := store.MustCreate(store.MemoryConfig())

	ctx := context.Background()

	// Use the store
	client := &core.Client{
		ID:     "must-create-client",
		Secret: "secret",
	}
	_ = s.CreateClient(ctx, client)

	retrieved, _ := s.GetClient(ctx, "must-create-client")
	fmt.Println(retrieved.ID)
	// Output: must-create-client
}

// Example_authorizationCode demonstrates working with authorization codes.
func Example_authorizationCode() {
	s := store.MustCreate(store.MemoryConfig())
	ctx := context.Background()

	// Create an authorization code
	code := &core.AuthorizationCode{
		Code:      "auth-code-123",
		ClientID:  "client-123",
		ExpiresAt: time.Now().Add(10 * time.Minute).Unix(),
		CreatedAt: time.Now().Unix(),
	}

	// Save the code
	if err := s.SaveAuthorizationCode(ctx, code); err != nil {
		log.Fatal(err)
	}

	// Retrieve the code
	retrieved, err := s.GetAuthorizationCode(ctx, "client-123")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(retrieved.Code)
	// Output: auth-code-123
}

// Example_switchingStores demonstrates how easy it is to switch between store types.
func Example_switchingStores() {
	// Function that works with any store
	useStore := func(s core.Store) error {
		ctx := context.Background()
		client := &core.Client{
			ID:     "test-client",
			Secret: "secret",
		}
		return s.CreateClient(ctx, client)
	}

	// Use with memory store
	memStore := store.MustCreate(store.MemoryConfig())
	if err := useStore(memStore); err != nil {
		log.Fatal(err)
	}
	fmt.Println("Memory store: OK")

	// Use with Redis store (if available)
	redisStore, err := store.NewStore(store.RedisConfig(store.RedisOptions{
		Addr: "localhost:6379",
	}))
	if err == nil {
		defer redisStore.(*store.RedisStore).Close()
		if err := useStore(redisStore); err != nil {
			log.Fatal(err)
		}
		fmt.Println("Redis store: OK")
	}

	// Output: Memory store: OK
}
