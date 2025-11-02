package store

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/go-training/mcp-workshop/pkg/core"
)

func TestNewMemoryStore(t *testing.T) {
	store := NewMemoryStore()

	if store == nil {
		t.Fatal("NewMemoryStore() returned nil")
	}

	if store.codes == nil {
		t.Error("codes map should be initialized")
	}
}

func TestMemoryStore_SaveAuthorizationCode(t *testing.T) {
	tests := []struct {
		name    string
		code    *core.AuthorizationCode
		wantErr error
	}{
		{
			name: "valid authorization code",
			code: &core.AuthorizationCode{
				Code:        "test_code_123",
				ClientID:    "client_123",
				RedirectURI: "https://example.com/callback",
				Scope:       []string{"read", "write"},
				ExpiresAt:   time.Now().Add(10 * time.Minute).Unix(),
				CreatedAt:   time.Now().Unix(),
			},
			wantErr: nil,
		},
		{
			name: "valid code with PKCE",
			code: &core.AuthorizationCode{
				Code:                "pkce_code_456",
				ClientID:            "client_456",
				RedirectURI:         "https://example.com/callback",
				Scope:               []string{"read"},
				CodeChallenge:       "challenge_string",
				CodeChallengeMethod: "S256",
				ExpiresAt:           time.Now().Add(10 * time.Minute).Unix(),
				CreatedAt:           time.Now().Unix(),
			},
			wantErr: nil,
		},
		{
			name:    "nil authorization code",
			code:    nil,
			wantErr: ErrNilAuthorizationCode,
		},
		{
			name: "empty client ID",
			code: &core.AuthorizationCode{
				Code:        "test_code_789",
				ClientID:    "",
				RedirectURI: "https://example.com/callback",
				Scope:       []string{"read"},
				ExpiresAt:   time.Now().Add(10 * time.Minute).Unix(),
				CreatedAt:   time.Now().Unix(),
			},
			wantErr: ErrEmptyClientID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMemoryStore()
			ctx := context.Background()

			err := store.SaveAuthorizationCode(ctx, tt.code)

			if !errors.Is(err, tt.wantErr) {
				t.Errorf("SaveAuthorizationCode() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantErr == nil && tt.code != nil {
				savedCode, getErr := store.GetAuthorizationCode(ctx, tt.code.ClientID)
				if getErr != nil {
					t.Errorf("Failed to retrieve saved code: %v", getErr)
				}
				if savedCode.Code != tt.code.Code {
					t.Errorf(
						"Retrieved code mismatch: got %v, want %v",
						savedCode.Code,
						tt.code.Code,
					)
				}
			}
		})
	}
}

func TestMemoryStore_GetAuthorizationCode(t *testing.T) {
	tests := []struct {
		name         string
		setupCode    *core.AuthorizationCode
		searchClient string
		wantErr      error
		wantCode     bool
	}{
		{
			name: "existing code",
			setupCode: &core.AuthorizationCode{
				Code:        "existing_code",
				ClientID:    "client_123",
				RedirectURI: "https://example.com/callback",
				Scope:       []string{"read"},
				ExpiresAt:   time.Now().Add(10 * time.Minute).Unix(),
				CreatedAt:   time.Now().Unix(),
			},
			searchClient: "client_123",
			wantErr:      nil,
			wantCode:     true,
		},
		{
			name:         "non-existing client",
			setupCode:    nil,
			searchClient: "non_existing_client",
			wantErr:      ErrCodeNotFound,
			wantCode:     false,
		},
		{
			name: "empty client ID",
			setupCode: &core.AuthorizationCode{
				Code:        "some_code",
				ClientID:    "client_456",
				RedirectURI: "https://example.com/callback",
				Scope:       []string{"write"},
				ExpiresAt:   time.Now().Add(10 * time.Minute).Unix(),
				CreatedAt:   time.Now().Unix(),
			},
			searchClient: "",
			wantErr:      ErrEmptyClientID,
			wantCode:     false,
		},
		{
			name: "expired authorization code",
			setupCode: &core.AuthorizationCode{
				Code:        "expired_code",
				ClientID:    "client_expired",
				RedirectURI: "https://example.com/callback",
				Scope:       []string{"read"},
				ExpiresAt:   time.Now().Add(-10 * time.Minute).Unix(), // Expired 10 minutes ago
				CreatedAt:   time.Now().Add(-20 * time.Minute).Unix(),
			},
			searchClient: "client_expired",
			wantErr:      ErrCodeNotFound,
			wantCode:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMemoryStore()
			ctx := context.Background()

			if tt.setupCode != nil {
				if err := store.SaveAuthorizationCode(ctx, tt.setupCode); err != nil {
					t.Fatalf("Failed to setup test: %v", err)
				}
			}

			gotCode, err := store.GetAuthorizationCode(ctx, tt.searchClient)

			if !errors.Is(err, tt.wantErr) {
				t.Errorf("GetAuthorizationCode() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantCode && gotCode == nil {
				t.Error("Expected to get authorization code, but got nil")
			}

			if !tt.wantCode && gotCode != nil {
				t.Errorf("Expected no authorization code, but got %v", gotCode)
			}

			if tt.wantCode && gotCode != nil && tt.setupCode != nil &&
				gotCode.Code != tt.setupCode.Code {
				t.Errorf(
					"GetAuthorizationCode() code = %v, want %v",
					gotCode.Code,
					tt.setupCode.Code,
				)
			}
		})
	}
}

func TestMemoryStore_ConcurrentAccess(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	const numGoroutines = 100
	var wg sync.WaitGroup
	wg.Add(numGoroutines * 2)

	// Concurrent writes
	for i := 0; i < numGoroutines; i++ {
		go func(index int) {
			defer wg.Done()
			code := &core.AuthorizationCode{
				Code:        "concurrent_code_" + string(rune('A'+index)),
				ClientID:    "client_" + string(rune('A'+index)),
				RedirectURI: "https://example.com/callback",
				Scope:       []string{"read"},
				ExpiresAt:   time.Now().Add(10 * time.Minute).Unix(),
				CreatedAt:   time.Now().Unix(),
			}
			if err := store.SaveAuthorizationCode(ctx, code); err != nil {
				t.Errorf("Failed to save code concurrently: %v", err)
			}
		}(i)
	}

	// Concurrent reads
	for i := 0; i < numGoroutines; i++ {
		go func(index int) {
			defer wg.Done()
			searchClient := "client_" + string(rune('A'+index))
			_, _ = store.GetAuthorizationCode(ctx, searchClient)
		}(i)
	}

	wg.Wait()
}

func TestMemoryStore_UpdateExistingCode(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	// For the same client, saving a new authorization code should replace the old one
	originalCode := &core.AuthorizationCode{
		Code:        "original_code_123",
		ClientID:    "client_123",
		RedirectURI: "https://original.com/callback",
		Scope:       []string{"read"},
		ExpiresAt:   time.Now().Add(10 * time.Minute).Unix(),
		CreatedAt:   time.Now().Unix(),
	}

	err := store.SaveAuthorizationCode(ctx, originalCode)
	if err != nil {
		t.Fatalf("Failed to save original code: %v", err)
	}

	// Save a new code for the same client
	updatedCode := &core.AuthorizationCode{
		Code:        "updated_code_456",
		ClientID:    "client_123",
		RedirectURI: "https://updated.com/callback",
		Scope:       []string{"read", "write"},
		ExpiresAt:   time.Now().Add(20 * time.Minute).Unix(),
		CreatedAt:   time.Now().Unix(),
	}

	err = store.SaveAuthorizationCode(ctx, updatedCode)
	if err != nil {
		t.Fatalf("Failed to update code: %v", err)
	}

	retrievedCode, err := store.GetAuthorizationCode(ctx, "client_123")
	if err != nil {
		t.Fatalf("Failed to retrieve updated code: %v", err)
	}

	if retrievedCode.Code != "updated_code_456" {
		t.Errorf(
			"Code was not updated. Got Code %v, want %v",
			retrievedCode.Code,
			"updated_code_456",
		)
	}

	if retrievedCode.RedirectURI != "https://updated.com/callback" {
		t.Errorf(
			"RedirectURI was not updated. Got %v, want %v",
			retrievedCode.RedirectURI,
			"https://updated.com/callback",
		)
	}

	if len(retrievedCode.Scope) != 2 {
		t.Errorf("Scope was not updated. Got %v scopes, want 2", len(retrievedCode.Scope))
	}
}

func TestMemoryStore_DeleteAuthorizationCode(t *testing.T) {
	tests := []struct {
		name      string
		setupCode *core.AuthorizationCode
		deleteID  string
		wantErr   error
	}{
		{
			name: "delete existing code",
			setupCode: &core.AuthorizationCode{
				Code:        "delete_code_123",
				ClientID:    "client_delete",
				RedirectURI: "https://example.com/callback",
				Scope:       []string{"read"},
				ExpiresAt:   time.Now().Add(10 * time.Minute).Unix(),
				CreatedAt:   time.Now().Unix(),
			},
			deleteID: "client_delete",
			wantErr:  nil,
		},
		{
			name:      "delete non-existing code",
			setupCode: nil,
			deleteID:  "non_existing_client",
			wantErr:   ErrCodeNotFound,
		},
		{
			name: "delete with empty client ID",
			setupCode: &core.AuthorizationCode{
				Code:        "some_code",
				ClientID:    "client_123",
				RedirectURI: "https://example.com/callback",
				Scope:       []string{"read"},
				ExpiresAt:   time.Now().Add(10 * time.Minute).Unix(),
				CreatedAt:   time.Now().Unix(),
			},
			deleteID: "",
			wantErr:  ErrEmptyClientID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMemoryStore()
			ctx := context.Background()

			if tt.setupCode != nil {
				if err := store.SaveAuthorizationCode(ctx, tt.setupCode); err != nil {
					t.Fatalf("Failed to setup test: %v", err)
				}
			}

			err := store.DeleteAuthorizationCode(ctx, tt.deleteID)

			if !errors.Is(err, tt.wantErr) {
				t.Errorf("DeleteAuthorizationCode() error = %v, wantErr %v", err, tt.wantErr)
			}

			// Verify deletion for successful cases
			if tt.wantErr == nil && tt.setupCode != nil {
				_, getErr := store.GetAuthorizationCode(ctx, tt.deleteID)
				if !errors.Is(getErr, ErrCodeNotFound) {
					t.Errorf("Code should have been deleted but still exists")
				}
			}
		})
	}
}

func TestMemoryStore_DeleteAuthorizationCode_Concurrent(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	const numGoroutines = 50
	var wg sync.WaitGroup
	wg.Add(numGoroutines * 2)

	// Concurrent writes
	for i := 0; i < numGoroutines; i++ {
		go func(index int) {
			defer wg.Done()
			code := &core.AuthorizationCode{
				Code:        "delete_concurrent_code_" + string(rune('A'+index)),
				ClientID:    "delete_client_" + string(rune('A'+index)),
				RedirectURI: "https://example.com/callback",
				Scope:       []string{"read"},
				ExpiresAt:   time.Now().Add(10 * time.Minute).Unix(),
				CreatedAt:   time.Now().Unix(),
			}
			_ = store.SaveAuthorizationCode(ctx, code)
		}(i)
	}

	// Concurrent deletes
	for i := 0; i < numGoroutines; i++ {
		go func(index int) {
			defer wg.Done()
			clientID := "delete_client_" + string(rune('A'+index))
			_ = store.DeleteAuthorizationCode(ctx, clientID)
		}(i)
	}

	wg.Wait()
}

func TestMemoryStore_GetClient(t *testing.T) {
	tests := []struct {
		name         string
		setupClient  *core.Client
		searchClient string
		wantErr      error
		wantClient   bool
	}{
		{
			name: "get existing client",
			setupClient: &core.Client{
				ID:                    "client_get_123",
				Secret:                "secret_123",
				RedirectURIs:          []string{"https://example.com/callback"},
				GrantTypes:            []string{"authorization_code"},
				ResponseTypes:         []string{"code"},
				TokenAuthMethod:       "client_secret_basic",
				Scope:                 "read write",
				IssuedAt:              time.Now().Unix(),
				ClientSecretExpiresAt: 0,
			},
			searchClient: "client_get_123",
			wantErr:      nil,
			wantClient:   true,
		},
		{
			name:         "get non-existing client",
			setupClient:  nil,
			searchClient: "non_existing_client",
			wantErr:      ErrClientNotFound,
			wantClient:   false,
		},
		{
			name: "get with empty client ID",
			setupClient: &core.Client{
				ID:     "client_456",
				Secret: "secret_456",
			},
			searchClient: "",
			wantErr:      ErrEmptyClientID,
			wantClient:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMemoryStore()
			ctx := context.Background()

			if tt.setupClient != nil {
				if err := store.CreateClient(ctx, tt.setupClient); err != nil {
					t.Fatalf("Failed to setup test: %v", err)
				}
			}

			gotClient, err := store.GetClient(ctx, tt.searchClient)

			if !errors.Is(err, tt.wantErr) {
				t.Errorf("GetClient() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantClient && gotClient == nil {
				t.Error("Expected to get client, but got nil")
			}

			if !tt.wantClient && gotClient != nil {
				t.Errorf("Expected no client, but got %v", gotClient)
			}

			if tt.wantClient && gotClient != nil && tt.setupClient != nil {
				if gotClient.ID != tt.setupClient.ID {
					t.Errorf("GetClient() ID = %v, want %v", gotClient.ID, tt.setupClient.ID)
				}
				if gotClient.Secret != tt.setupClient.Secret {
					t.Errorf(
						"GetClient() Secret = %v, want %v",
						gotClient.Secret,
						tt.setupClient.Secret,
					)
				}
			}
		})
	}
}

func TestMemoryStore_CreateClient(t *testing.T) {
	tests := []struct {
		name    string
		client  *core.Client
		wantErr error
	}{
		{
			name: "create valid client",
			client: &core.Client{
				ID:                    "client_create_123",
				Secret:                "secret_123",
				RedirectURIs:          []string{"https://example.com/callback"},
				GrantTypes:            []string{"authorization_code"},
				ResponseTypes:         []string{"code"},
				TokenAuthMethod:       "client_secret_basic",
				Scope:                 "read write",
				IssuedAt:              time.Now().Unix(),
				ClientSecretExpiresAt: 0,
			},
			wantErr: nil,
		},
		{
			name:    "create nil client",
			client:  nil,
			wantErr: ErrNilClient,
		},
		{
			name: "create client with empty ID",
			client: &core.Client{
				ID:     "",
				Secret: "secret_456",
			},
			wantErr: ErrEmptyClientID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMemoryStore()
			ctx := context.Background()

			err := store.CreateClient(ctx, tt.client)

			if !errors.Is(err, tt.wantErr) {
				t.Errorf("CreateClient() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantErr == nil && tt.client != nil {
				gotClient, getErr := store.GetClient(ctx, tt.client.ID)
				if getErr != nil {
					t.Errorf("Failed to retrieve created client: %v", getErr)
				}
				if gotClient.ID != tt.client.ID {
					t.Errorf(
						"Retrieved client ID mismatch: got %v, want %v",
						gotClient.ID,
						tt.client.ID,
					)
				}
			}
		})
	}
}

func TestMemoryStore_UpdateClient(t *testing.T) {
	tests := []struct {
		name         string
		setupClient  *core.Client
		updateClient *core.Client
		wantErr      error
	}{
		{
			name: "update existing client",
			setupClient: &core.Client{
				ID:     "client_update_123",
				Secret: "original_secret",
				Scope:  "read",
			},
			updateClient: &core.Client{
				ID:     "client_update_123",
				Secret: "updated_secret",
				Scope:  "read write",
			},
			wantErr: nil,
		},
		{
			name:        "update non-existing client",
			setupClient: nil,
			updateClient: &core.Client{
				ID:     "non_existing_client",
				Secret: "secret",
			},
			wantErr: ErrClientNotFound,
		},
		{
			name: "update nil client",
			setupClient: &core.Client{
				ID:     "client_456",
				Secret: "secret_456",
			},
			updateClient: nil,
			wantErr:      ErrNilClient,
		},
		{
			name: "update with empty client ID",
			setupClient: &core.Client{
				ID:     "client_789",
				Secret: "secret_789",
			},
			updateClient: &core.Client{
				ID:     "",
				Secret: "updated_secret",
			},
			wantErr: ErrEmptyClientID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMemoryStore()
			ctx := context.Background()

			if tt.setupClient != nil {
				if err := store.CreateClient(ctx, tt.setupClient); err != nil {
					t.Fatalf("Failed to setup test: %v", err)
				}
			}

			err := store.UpdateClient(ctx, tt.updateClient)

			if !errors.Is(err, tt.wantErr) {
				t.Errorf("UpdateClient() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantErr == nil && tt.updateClient != nil {
				gotClient, getErr := store.GetClient(ctx, tt.updateClient.ID)
				if getErr != nil {
					t.Errorf("Failed to retrieve updated client: %v", getErr)
				}
				if gotClient.Secret != tt.updateClient.Secret {
					t.Errorf(
						"Client secret was not updated. Got %v, want %v",
						gotClient.Secret,
						tt.updateClient.Secret,
					)
				}
				if gotClient.Scope != tt.updateClient.Scope {
					t.Errorf(
						"Client scope was not updated. Got %v, want %v",
						gotClient.Scope,
						tt.updateClient.Scope,
					)
				}
			}
		})
	}
}

func TestMemoryStore_DeleteClient(t *testing.T) {
	tests := []struct {
		name        string
		setupClient *core.Client
		deleteID    string
		wantErr     error
	}{
		{
			name: "delete existing client",
			setupClient: &core.Client{
				ID:     "client_delete_123",
				Secret: "secret_123",
			},
			deleteID: "client_delete_123",
			wantErr:  nil,
		},
		{
			name:        "delete non-existing client",
			setupClient: nil,
			deleteID:    "non_existing_client",
			wantErr:     ErrClientNotFound,
		},
		{
			name: "delete with empty client ID",
			setupClient: &core.Client{
				ID:     "client_456",
				Secret: "secret_456",
			},
			deleteID: "",
			wantErr:  ErrEmptyClientID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMemoryStore()
			ctx := context.Background()

			if tt.setupClient != nil {
				if err := store.CreateClient(ctx, tt.setupClient); err != nil {
					t.Fatalf("Failed to setup test: %v", err)
				}
			}

			err := store.DeleteClient(ctx, tt.deleteID)

			if !errors.Is(err, tt.wantErr) {
				t.Errorf("DeleteClient() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantErr == nil && tt.setupClient != nil {
				_, getErr := store.GetClient(ctx, tt.deleteID)
				if !errors.Is(getErr, ErrClientNotFound) {
					t.Errorf("Client should have been deleted but still exists")
				}
			}
		})
	}
}

func TestMemoryStore_Client_Concurrent(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	const numGoroutines = 50
	var wg sync.WaitGroup
	wg.Add(numGoroutines * 3)

	// Concurrent creates
	for i := 0; i < numGoroutines; i++ {
		go func(index int) {
			defer wg.Done()
			client := &core.Client{
				ID:     "concurrent_client_" + string(rune('A'+index)),
				Secret: "secret_" + string(rune('A'+index)),
			}
			_ = store.CreateClient(ctx, client)
		}(i)
	}

	// Concurrent reads
	for i := 0; i < numGoroutines; i++ {
		go func(index int) {
			defer wg.Done()
			clientID := "concurrent_client_" + string(rune('A'+index))
			_, _ = store.GetClient(ctx, clientID)
		}(i)
	}

	// Concurrent updates
	for i := 0; i < numGoroutines; i++ {
		go func(index int) {
			defer wg.Done()
			client := &core.Client{
				ID:     "concurrent_client_" + string(rune('A'+index)),
				Secret: "updated_secret_" + string(rune('A'+index)),
			}
			_ = store.UpdateClient(ctx, client)
		}(i)
	}

	wg.Wait()
}

func TestMemoryStore_GetClients(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	// 1. Test with an empty store
	clients, err := store.GetClients(ctx)
	if err != nil {
		t.Fatalf("GetClients() on empty store failed: %v", err)
	}
	if len(clients) != 0 {
		t.Fatalf("Expected 0 clients, got %d", len(clients))
	}

	// 2. Add some clients
	client1 := &core.Client{ID: "client1", Secret: "secret1"}
	client2 := &core.Client{ID: "client2", Secret: "secret2"}
	if err := store.CreateClient(ctx, client1); err != nil {
		t.Fatalf("Failed to create client1: %v", err)
	}
	if err := store.CreateClient(ctx, client2); err != nil {
		t.Fatalf("Failed to create client2: %v", err)
	}

	// 3. Test with multiple clients
	clients, err = store.GetClients(ctx)
	if err != nil {
		t.Fatalf("GetClients() with multiple clients failed: %v", err)
	}
	if len(clients) != 2 {
		t.Fatalf("Expected 2 clients, got %d", len(clients))
	}

	// Check if the correct clients are returned (order is not guaranteed)
	found1 := false
	found2 := false
	for _, c := range clients {
		if c.ID == "client1" {
			found1 = true
		}
		if c.ID == "client2" {
			found2 = true
		}
	}
	if !found1 || !found2 {
		t.Errorf("Did not find all clients. Found1: %v, Found2: %v", found1, found2)
	}
}

func TestMemoryStore_GetClients_LargeNumber(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	numClients := 150

	// Create a large number of clients
	for i := 0; i < numClients; i++ {
		client := &core.Client{
			ID:     fmt.Sprintf("client-large-%d", i),
			Secret: "secret",
		}
		if err := store.CreateClient(ctx, client); err != nil {
			t.Fatalf("Failed to create client %d: %v", i, err)
		}
	}

	// Get all clients
	clients, err := store.GetClients(ctx)
	if err != nil {
		t.Fatalf("GetClients() with large number of clients failed: %v", err)
	}

	// Verify the number of clients retrieved
	if len(clients) != numClients {
		t.Errorf("Expected %d clients, but got %d", numClients, len(clients))
	}

	// Verify all clients are present
	clientMap := make(map[string]bool)
	for _, c := range clients {
		clientMap[c.ID] = true
	}

	for i := 0; i < numClients; i++ {
		clientID := fmt.Sprintf("client-large-%d", i)
		if !clientMap[clientID] {
			t.Errorf("Client %s was not found in the retrieved list", clientID)
		}
	}
}
