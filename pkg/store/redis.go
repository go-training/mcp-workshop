package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/go-training/mcp-workshop/pkg/core"
	"github.com/redis/rueidis"
)

const (
	// Key prefixes for Redis storage
	authCodePrefix = "auth_code:"
	clientPrefix   = "client:"
)

// RedisStore implements the core.Store interface using Redis via rueidis.
// It provides persistent storage for authorization codes and clients.
type RedisStore struct {
	client rueidis.Client
}

// NewRedisStore creates a new instance of RedisStore with the provided rueidis client.
func NewRedisStore(client rueidis.Client) *RedisStore {
	return &RedisStore{
		client: client,
	}
}

// RedisOptions contains configuration for Redis connection.
type RedisOptions struct {
	Addr     string
	Password string
	DB       int
}

// NewRedisStoreFromOptions creates a new RedisStore with simplified options.
func NewRedisStoreFromOptions(opts RedisOptions) (*RedisStore, error) {
	clientOpts := rueidis.ClientOption{
		InitAddress: []string{opts.Addr},
		Password:    opts.Password,
		SelectDB:    opts.DB,
	}
	client, err := rueidis.NewClient(clientOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to create redis client: %w", err)
	}
	return NewRedisStore(client), nil
}

// NewRedisStoreFromClientOption creates a new RedisStore with full rueidis client options.
func NewRedisStoreFromClientOption(opts rueidis.ClientOption) (*RedisStore, error) {
	client, err := rueidis.NewClient(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create redis client: %w", err)
	}
	return NewRedisStore(client), nil
}

// Close closes the Redis client connection.
func (r *RedisStore) Close() {
	r.client.Close()
}

// SaveAuthorizationCode stores an authorization code in Redis with TTL.
// It returns an error if the code is nil or the client ID is empty.
func (r *RedisStore) SaveAuthorizationCode(ctx context.Context, code *core.AuthorizationCode) error {
	if code == nil {
		return ErrNilAuthorizationCode
	}
	if code.ClientID == "" {
		return ErrEmptyClientID
	}

	// Serialize authorization code to JSON
	data, err := json.Marshal(code)
	if err != nil {
		return fmt.Errorf("failed to marshal authorization code: %w", err)
	}

	// Calculate TTL based on expiration time
	ttl := time.Until(time.Unix(code.ExpiresAt, 0))
	if ttl <= 0 {
		return errors.New("authorization code is already expired")
	}

	// Store in Redis with TTL
	key := authCodePrefix + code.ClientID
	cmd := r.client.B().Set().Key(key).Value(string(data)).ExSeconds(int64(ttl.Seconds())).Build()
	if err := r.client.Do(ctx, cmd).Error(); err != nil {
		return fmt.Errorf("failed to save authorization code to redis: %w", err)
	}

	return nil
}

// GetAuthorizationCode retrieves an authorization code from Redis by client ID.
// It returns ErrCodeNotFound if the code does not exist or has expired.
// Uses client-side caching with 10 second TTL for better performance.
func (r *RedisStore) GetAuthorizationCode(ctx context.Context, clientID string) (*core.AuthorizationCode, error) {
	if clientID == "" {
		return nil, ErrEmptyClientID
	}

	key := authCodePrefix + clientID
	cmd := r.client.B().Get().Key(key).Cache()
	result, err := r.client.DoCache(ctx, cmd, 10*time.Second).ToString()
	if err != nil {
		if rueidis.IsRedisNil(err) {
			return nil, ErrCodeNotFound
		}
		return nil, fmt.Errorf("failed to get authorization code from redis: %w", err)
	}

	var code core.AuthorizationCode
	if err := json.Unmarshal([]byte(result), &code); err != nil {
		return nil, fmt.Errorf("failed to unmarshal authorization code: %w", err)
	}

	// Double-check expiration (Redis TTL should handle this, but being explicit)
	if time.Now().Unix() > code.ExpiresAt {
		// Delete the expired code
		_ = r.DeleteAuthorizationCode(ctx, clientID)
		return nil, ErrCodeNotFound
	}

	return &code, nil
}

// DeleteAuthorizationCode removes an authorization code from Redis by client ID.
// It returns ErrCodeNotFound if the code does not exist.
func (r *RedisStore) DeleteAuthorizationCode(ctx context.Context, clientID string) error {
	if clientID == "" {
		return ErrEmptyClientID
	}

	key := authCodePrefix + clientID
	cmd := r.client.B().Del().Key(key).Build()
	result, err := r.client.Do(ctx, cmd).AsInt64()
	if err != nil {
		return fmt.Errorf("failed to delete authorization code from redis: %w", err)
	}

	if result == 0 {
		return ErrCodeNotFound
	}

	return nil
}

// GetClient retrieves a client from Redis by its client ID.
// It returns ErrClientNotFound if the client does not exist.
// Uses client-side caching with 60 second TTL since clients change infrequently.
func (r *RedisStore) GetClient(ctx context.Context, clientID string) (*core.Client, error) {
	if clientID == "" {
		return nil, ErrEmptyClientID
	}

	key := clientPrefix + clientID
	cmd := r.client.B().Get().Key(key).Cache()
	result, err := r.client.DoCache(ctx, cmd, 60*time.Second).ToString()
	if err != nil {
		if rueidis.IsRedisNil(err) {
			return nil, ErrClientNotFound
		}
		return nil, fmt.Errorf("failed to get client from redis: %w", err)
	}

	var client core.Client
	if err := json.Unmarshal([]byte(result), &client); err != nil {
		return nil, fmt.Errorf("failed to unmarshal client: %w", err)
	}

	return &client, nil
}

// CreateClient stores a new client in Redis.
// It returns an error if the client is nil or the client ID is empty.
func (r *RedisStore) CreateClient(ctx context.Context, client *core.Client) error {
	if client == nil {
		return ErrNilClient
	}
	if client.ID == "" {
		return ErrEmptyClientID
	}

	// Serialize client to JSON
	data, err := json.Marshal(client)
	if err != nil {
		return fmt.Errorf("failed to marshal client: %w", err)
	}

	// Store in Redis (clients don't expire by default)
	key := clientPrefix + client.ID
	cmd := r.client.B().Set().Key(key).Value(string(data)).Build()
	if err := r.client.Do(ctx, cmd).Error(); err != nil {
		return fmt.Errorf("failed to create client in redis: %w", err)
	}

	return nil
}

// UpdateClient updates an existing client in Redis.
// It returns an error if the client is nil, the client ID is empty, or the client does not exist.
func (r *RedisStore) UpdateClient(ctx context.Context, client *core.Client) error {
	if client == nil {
		return ErrNilClient
	}
	if client.ID == "" {
		return ErrEmptyClientID
	}

	// Check if client exists
	key := clientPrefix + client.ID
	existsCmd := r.client.B().Exists().Key(key).Build()
	exists, err := r.client.Do(ctx, existsCmd).AsInt64()
	if err != nil {
		return fmt.Errorf("failed to check client existence in redis: %w", err)
	}
	if exists == 0 {
		return ErrClientNotFound
	}

	// Serialize client to JSON
	data, err := json.Marshal(client)
	if err != nil {
		return fmt.Errorf("failed to marshal client: %w", err)
	}

	// Update in Redis
	cmd := r.client.B().Set().Key(key).Value(string(data)).Build()
	if err := r.client.Do(ctx, cmd).Error(); err != nil {
		return fmt.Errorf("failed to update client in redis: %w", err)
	}

	return nil
}

// DeleteClient removes a client from Redis by its client ID.
// It returns ErrClientNotFound if the client does not exist.
func (r *RedisStore) DeleteClient(ctx context.Context, clientID string) error {
	if clientID == "" {
		return ErrEmptyClientID
	}

	key := clientPrefix + clientID
	cmd := r.client.B().Del().Key(key).Build()
	result, err := r.client.Do(ctx, cmd).AsInt64()
	if err != nil {
		return fmt.Errorf("failed to delete client from redis: %w", err)
	}

	if result == 0 {
		return ErrClientNotFound
	}

	return nil
}
