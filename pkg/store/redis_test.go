package store

import (
	"context"
	"testing"
	"time"

	"github.com/go-training/mcp-workshop/pkg/core"

	"github.com/redis/rueidis"
)

// setupRedisStore creates a test Redis store connected to localhost:6379
// Skip tests if Redis is not available
func setupRedisStore(t *testing.T) *RedisStore {
	t.Helper()

	opts := rueidis.ClientOption{
		InitAddress: []string{"localhost:6379"},
	}

	store, err := NewRedisStoreFromClientOption(opts)
	if err != nil {
		t.Skipf("Redis not available, skipping test: %v", err)
	}

	// Test connection
	ctx := context.Background()
	cmd := store.client.B().Ping().Build()
	if err := store.client.Do(ctx, cmd).Error(); err != nil {
		store.Close()
		t.Skipf("Cannot connect to Redis, skipping test: %v", err)
	}

	// Clean up before test
	t.Cleanup(func() {
		// Clean up test keys
		cleanupRedisKeys(t, store)
		store.Close()
	})

	return store
}

// cleanupRedisKeys removes all test keys from Redis
func cleanupRedisKeys(t *testing.T, store *RedisStore) {
	t.Helper()
	ctx := context.Background()

	// Delete all auth codes
	scanCmd := store.client.B().Scan().Cursor(0).Match(authCodePrefix + "*").Count(100).Build()
	scanResult, err := store.client.Do(ctx, scanCmd).AsScanEntry()
	if err == nil {
		for _, key := range scanResult.Elements {
			delCmd := store.client.B().Del().Key(key).Build()
			_ = store.client.Do(ctx, delCmd).Error()
		}
	}

	// Delete all clients
	scanCmd = store.client.B().Scan().Cursor(0).Match(clientPrefix + "*").Count(100).Build()
	scanResult, err = store.client.Do(ctx, scanCmd).AsScanEntry()
	if err == nil {
		for _, key := range scanResult.Elements {
			delCmd := store.client.B().Del().Key(key).Build()
			_ = store.client.Do(ctx, delCmd).Error()
		}
	}
}

func TestRedisStore_SaveAuthorizationCode(t *testing.T) {
	store := setupRedisStore(t)
	ctx := context.Background()

	tests := []struct {
		name    string
		code    *core.AuthorizationCode
		wantErr bool
		errType error
	}{
		{
			name: "valid authorization code",
			code: &core.AuthorizationCode{
				Code:                "test-code-123",
				ClientID:            "test-client",
				RedirectURI:         "https://example.com/callback",
				Scope:               []string{"read", "write"},
				CodeChallenge:       "challenge",
				CodeChallengeMethod: "S256",
				ExpiresAt:           time.Now().Add(10 * time.Minute).Unix(),
				CreatedAt:           time.Now().Unix(),
			},
			wantErr: false,
		},
		{
			name:    "nil authorization code",
			code:    nil,
			wantErr: true,
			errType: ErrNilAuthorizationCode,
		},
		{
			name: "empty client ID",
			code: &core.AuthorizationCode{
				Code:      "test-code-456",
				ClientID:  "",
				ExpiresAt: time.Now().Add(10 * time.Minute).Unix(),
			},
			wantErr: true,
			errType: ErrEmptyClientID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := store.SaveAuthorizationCode(ctx, tt.code)
			if (err != nil) != tt.wantErr {
				t.Errorf("SaveAuthorizationCode() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.errType != nil && err != tt.errType {
				t.Errorf("SaveAuthorizationCode() error = %v, want %v", err, tt.errType)
			}
		})
	}
}

func TestRedisStore_GetAuthorizationCode(t *testing.T) {
	store := setupRedisStore(t)
	ctx := context.Background()

	// Setup test data
	testCode := &core.AuthorizationCode{
		Code:                "test-code-get",
		ClientID:            "test-client-get",
		RedirectURI:         "https://example.com/callback",
		Scope:               []string{"read"},
		CodeChallenge:       "challenge",
		CodeChallengeMethod: "S256",
		ExpiresAt:           time.Now().Add(10 * time.Minute).Unix(),
		CreatedAt:           time.Now().Unix(),
	}
	_ = store.SaveAuthorizationCode(ctx, testCode)

	tests := []struct {
		name     string
		clientID string
		wantErr  bool
		errType  error
	}{
		{
			name:     "existing authorization code",
			clientID: "test-client-get",
			wantErr:  false,
		},
		{
			name:     "non-existent authorization code",
			clientID: "non-existent-client",
			wantErr:  true,
			errType:  ErrCodeNotFound,
		},
		{
			name:     "empty client ID",
			clientID: "",
			wantErr:  true,
			errType:  ErrEmptyClientID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := store.GetAuthorizationCode(ctx, tt.clientID)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetAuthorizationCode() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.errType != nil && err != tt.errType {
				t.Errorf("GetAuthorizationCode() error = %v, want %v", err, tt.errType)
			}
			if !tt.wantErr && got == nil {
				t.Error("GetAuthorizationCode() returned nil code without error")
			}
			if !tt.wantErr && got.Code != testCode.Code {
				t.Errorf("GetAuthorizationCode() code = %v, want %v", got.Code, testCode.Code)
			}
		})
	}
}

func TestRedisStore_GetAuthorizationCode_Expired(t *testing.T) {
	store := setupRedisStore(t)
	ctx := context.Background()

	// Create an expired code
	expiredCode := &core.AuthorizationCode{
		Code:      "expired-code",
		ClientID:  "expired-client",
		ExpiresAt: time.Now().Add(1 * time.Second).Unix(),
		CreatedAt: time.Now().Unix(),
	}
	_ = store.SaveAuthorizationCode(ctx, expiredCode)

	// Wait for expiration
	time.Sleep(2 * time.Second)

	// Try to get the expired code
	_, err := store.GetAuthorizationCode(ctx, "expired-client")
	if err != ErrCodeNotFound {
		t.Errorf("GetAuthorizationCode() error = %v, want %v", err, ErrCodeNotFound)
	}
}

func TestRedisStore_DeleteAuthorizationCode(t *testing.T) {
	store := setupRedisStore(t)
	ctx := context.Background()

	// Setup test data
	testCode := &core.AuthorizationCode{
		Code:      "test-code-delete",
		ClientID:  "test-client-delete",
		ExpiresAt: time.Now().Add(10 * time.Minute).Unix(),
		CreatedAt: time.Now().Unix(),
	}
	_ = store.SaveAuthorizationCode(ctx, testCode)

	tests := []struct {
		name     string
		clientID string
		wantErr  bool
		errType  error
	}{
		{
			name:     "delete existing authorization code",
			clientID: "test-client-delete",
			wantErr:  false,
		},
		{
			name:     "delete non-existent authorization code",
			clientID: "non-existent-client",
			wantErr:  true,
			errType:  ErrCodeNotFound,
		},
		{
			name:     "empty client ID",
			clientID: "",
			wantErr:  true,
			errType:  ErrEmptyClientID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := store.DeleteAuthorizationCode(ctx, tt.clientID)
			if (err != nil) != tt.wantErr {
				t.Errorf("DeleteAuthorizationCode() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.errType != nil && err != tt.errType {
				t.Errorf("DeleteAuthorizationCode() error = %v, want %v", err, tt.errType)
			}
		})
	}
}

func TestRedisStore_CreateClient(t *testing.T) {
	store := setupRedisStore(t)
	ctx := context.Background()

	tests := []struct {
		name    string
		client  *core.Client
		wantErr bool
		errType error
	}{
		{
			name: "valid client",
			client: &core.Client{
				ID:                    "test-client-create",
				Secret:                "secret123",
				RedirectURIs:          []string{"https://example.com/callback"},
				GrantTypes:            []string{"authorization_code"},
				ResponseTypes:         []string{"code"},
				TokenAuthMethod:       "client_secret_basic",
				Scope:                 "read write",
				IssuedAt:              time.Now().Unix(),
				ClientSecretExpiresAt: 0,
			},
			wantErr: false,
		},
		{
			name:    "nil client",
			client:  nil,
			wantErr: true,
			errType: ErrNilClient,
		},
		{
			name: "empty client ID",
			client: &core.Client{
				ID:     "",
				Secret: "secret456",
			},
			wantErr: true,
			errType: ErrEmptyClientID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := store.CreateClient(ctx, tt.client)
			if (err != nil) != tt.wantErr {
				t.Errorf("CreateClient() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.errType != nil && err != tt.errType {
				t.Errorf("CreateClient() error = %v, want %v", err, tt.errType)
			}
		})
	}
}

func TestRedisStore_GetClient(t *testing.T) {
	store := setupRedisStore(t)
	ctx := context.Background()

	// Setup test data
	testClient := &core.Client{
		ID:                    "test-client-get",
		Secret:                "secret123",
		RedirectURIs:          []string{"https://example.com/callback"},
		GrantTypes:            []string{"authorization_code"},
		ResponseTypes:         []string{"code"},
		TokenAuthMethod:       "client_secret_basic",
		Scope:                 "read write",
		IssuedAt:              time.Now().Unix(),
		ClientSecretExpiresAt: 0,
	}
	_ = store.CreateClient(ctx, testClient)

	tests := []struct {
		name     string
		clientID string
		wantErr  bool
		errType  error
	}{
		{
			name:     "existing client",
			clientID: "test-client-get",
			wantErr:  false,
		},
		{
			name:     "non-existent client",
			clientID: "non-existent-client",
			wantErr:  true,
			errType:  ErrClientNotFound,
		},
		{
			name:     "empty client ID",
			clientID: "",
			wantErr:  true,
			errType:  ErrEmptyClientID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := store.GetClient(ctx, tt.clientID)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetClient() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.errType != nil && err != tt.errType {
				t.Errorf("GetClient() error = %v, want %v", err, tt.errType)
			}
			if !tt.wantErr && got == nil {
				t.Error("GetClient() returned nil client without error")
			}
			if !tt.wantErr && got.ID != testClient.ID {
				t.Errorf("GetClient() ID = %v, want %v", got.ID, testClient.ID)
			}
		})
	}
}

func TestRedisStore_UpdateClient(t *testing.T) {
	store := setupRedisStore(t)
	ctx := context.Background()

	// Setup test data
	testClient := &core.Client{
		ID:                    "test-client-update",
		Secret:                "secret123",
		RedirectURIs:          []string{"https://example.com/callback"},
		GrantTypes:            []string{"authorization_code"},
		ResponseTypes:         []string{"code"},
		TokenAuthMethod:       "client_secret_basic",
		Scope:                 "read",
		IssuedAt:              time.Now().Unix(),
		ClientSecretExpiresAt: 0,
	}
	_ = store.CreateClient(ctx, testClient)

	tests := []struct {
		name    string
		client  *core.Client
		wantErr bool
		errType error
	}{
		{
			name: "update existing client",
			client: &core.Client{
				ID:                    "test-client-update",
				Secret:                "newsecret123",
				RedirectURIs:          []string{"https://example.com/callback2"},
				GrantTypes:            []string{"authorization_code", "refresh_token"},
				ResponseTypes:         []string{"code"},
				TokenAuthMethod:       "client_secret_post",
				Scope:                 "read write",
				IssuedAt:              time.Now().Unix(),
				ClientSecretExpiresAt: 0,
			},
			wantErr: false,
		},
		{
			name:    "nil client",
			client:  nil,
			wantErr: true,
			errType: ErrNilClient,
		},
		{
			name: "empty client ID",
			client: &core.Client{
				ID:     "",
				Secret: "secret456",
			},
			wantErr: true,
			errType: ErrEmptyClientID,
		},
		{
			name: "non-existent client",
			client: &core.Client{
				ID:     "non-existent-client",
				Secret: "secret789",
			},
			wantErr: true,
			errType: ErrClientNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := store.UpdateClient(ctx, tt.client)
			if (err != nil) != tt.wantErr {
				t.Errorf("UpdateClient() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.errType != nil && err != tt.errType {
				t.Errorf("UpdateClient() error = %v, want %v", err, tt.errType)
			}

			// Verify the update
			if !tt.wantErr && tt.client != nil {
				updated, err := store.GetClient(ctx, tt.client.ID)
				if err != nil {
					t.Errorf("GetClient() after update failed: %v", err)
				}
				if updated.Secret != tt.client.Secret {
					t.Errorf("UpdateClient() secret = %v, want %v", updated.Secret, tt.client.Secret)
				}
			}
		})
	}
}

func TestRedisStore_DeleteClient(t *testing.T) {
	store := setupRedisStore(t)
	ctx := context.Background()

	// Setup test data
	testClient := &core.Client{
		ID:     "test-client-delete",
		Secret: "secret123",
	}
	_ = store.CreateClient(ctx, testClient)

	tests := []struct {
		name     string
		clientID string
		wantErr  bool
		errType  error
	}{
		{
			name:     "delete existing client",
			clientID: "test-client-delete",
			wantErr:  false,
		},
		{
			name:     "delete non-existent client",
			clientID: "non-existent-client",
			wantErr:  true,
			errType:  ErrClientNotFound,
		},
		{
			name:     "empty client ID",
			clientID: "",
			wantErr:  true,
			errType:  ErrEmptyClientID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := store.DeleteClient(ctx, tt.clientID)
			if (err != nil) != tt.wantErr {
				t.Errorf("DeleteClient() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.errType != nil && err != tt.errType {
				t.Errorf("DeleteClient() error = %v, want %v", err, tt.errType)
			}
		})
	}
}

func TestRedisStore_ClientLifecycle(t *testing.T) {
	store := setupRedisStore(t)
	ctx := context.Background()

	// Create a client
	client := &core.Client{
		ID:                    "lifecycle-client",
		Secret:                "secret123",
		RedirectURIs:          []string{"https://example.com/callback"},
		GrantTypes:            []string{"authorization_code"},
		ResponseTypes:         []string{"code"},
		TokenAuthMethod:       "client_secret_basic",
		Scope:                 "read",
		IssuedAt:              time.Now().Unix(),
		ClientSecretExpiresAt: 0,
	}

	// Create
	if err := store.CreateClient(ctx, client); err != nil {
		t.Fatalf("CreateClient() failed: %v", err)
	}

	// Get
	retrieved, err := store.GetClient(ctx, client.ID)
	if err != nil {
		t.Fatalf("GetClient() failed: %v", err)
	}
	if retrieved.ID != client.ID {
		t.Errorf("Retrieved client ID = %v, want %v", retrieved.ID, client.ID)
	}

	// Update
	client.Scope = "read write"
	if err := store.UpdateClient(ctx, client); err != nil {
		t.Fatalf("UpdateClient() failed: %v", err)
	}

	// Verify update
	updated, err := store.GetClient(ctx, client.ID)
	if err != nil {
		t.Fatalf("GetClient() after update failed: %v", err)
	}
	if updated.Scope != "read write" {
		t.Errorf("Updated client scope = %v, want %v", updated.Scope, "read write")
	}

	// Delete
	if err := store.DeleteClient(ctx, client.ID); err != nil {
		t.Fatalf("DeleteClient() failed: %v", err)
	}

	// Verify deletion
	_, err = store.GetClient(ctx, client.ID)
	if err != ErrClientNotFound {
		t.Errorf("GetClient() after delete error = %v, want %v", err, ErrClientNotFound)
	}
}

func TestRedisStore_AuthorizationCodeLifecycle(t *testing.T) {
	store := setupRedisStore(t)
	ctx := context.Background()

	// Create an authorization code
	code := &core.AuthorizationCode{
		Code:                "lifecycle-code",
		ClientID:            "lifecycle-client",
		RedirectURI:         "https://example.com/callback",
		Scope:               []string{"read", "write"},
		CodeChallenge:       "challenge",
		CodeChallengeMethod: "S256",
		ExpiresAt:           time.Now().Add(10 * time.Minute).Unix(),
		CreatedAt:           time.Now().Unix(),
	}

	// Save
	if err := store.SaveAuthorizationCode(ctx, code); err != nil {
		t.Fatalf("SaveAuthorizationCode() failed: %v", err)
	}

	// Get
	retrieved, err := store.GetAuthorizationCode(ctx, code.ClientID)
	if err != nil {
		t.Fatalf("GetAuthorizationCode() failed: %v", err)
	}
	if retrieved.Code != code.Code {
		t.Errorf("Retrieved code = %v, want %v", retrieved.Code, code.Code)
	}

	// Delete
	if err := store.DeleteAuthorizationCode(ctx, code.ClientID); err != nil {
		t.Fatalf("DeleteAuthorizationCode() failed: %v", err)
	}

	// Verify deletion
	_, err = store.GetAuthorizationCode(ctx, code.ClientID)
	if err != ErrCodeNotFound {
		t.Errorf("GetAuthorizationCode() after delete error = %v, want %v", err, ErrCodeNotFound)
	}
}
