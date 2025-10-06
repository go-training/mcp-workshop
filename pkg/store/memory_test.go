package store

import (
	"context"
	"errors"
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
			name: "empty code string",
			code: &core.AuthorizationCode{
				Code:        "",
				ClientID:    "client_789",
				RedirectURI: "https://example.com/callback",
				Scope:       []string{"read"},
				ExpiresAt:   time.Now().Add(10 * time.Minute).Unix(),
				CreatedAt:   time.Now().Unix(),
			},
			wantErr: ErrEmptyCode,
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
				savedCode, getErr := store.GetAuthorizationCode(ctx, tt.code.Code)
				if getErr != nil {
					t.Errorf("Failed to retrieve saved code: %v", getErr)
				}
				if savedCode.Code != tt.code.Code {
					t.Errorf("Retrieved code mismatch: got %v, want %v", savedCode.Code, tt.code.Code)
				}
			}
		})
	}
}

func TestMemoryStore_GetAuthorizationCode(t *testing.T) {
	tests := []struct {
		name       string
		setupCode  *core.AuthorizationCode
		searchCode string
		wantErr    error
		wantCode   bool
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
			searchCode: "existing_code",
			wantErr:    nil,
			wantCode:   true,
		},
		{
			name:       "non-existing code",
			setupCode:  nil,
			searchCode: "non_existing_code",
			wantErr:    ErrCodeNotFound,
			wantCode:   false,
		},
		{
			name: "empty search string",
			setupCode: &core.AuthorizationCode{
				Code:        "some_code",
				ClientID:    "client_456",
				RedirectURI: "https://example.com/callback",
				Scope:       []string{"write"},
				ExpiresAt:   time.Now().Add(10 * time.Minute).Unix(),
				CreatedAt:   time.Now().Unix(),
			},
			searchCode: "",
			wantErr:    ErrEmptyCode,
			wantCode:   false,
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

			gotCode, err := store.GetAuthorizationCode(ctx, tt.searchCode)

			if !errors.Is(err, tt.wantErr) {
				t.Errorf("GetAuthorizationCode() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantCode && gotCode == nil {
				t.Error("Expected to get authorization code, but got nil")
			}

			if !tt.wantCode && gotCode != nil {
				t.Errorf("Expected no authorization code, but got %v", gotCode)
			}

			if tt.wantCode && gotCode != nil && gotCode.Code != tt.searchCode {
				t.Errorf("GetAuthorizationCode() code = %v, want %v", gotCode.Code, tt.searchCode)
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
			searchCode := "concurrent_code_" + string(rune('A'+index))
			_, _ = store.GetAuthorizationCode(ctx, searchCode)
		}(i)
	}

	wg.Wait()
}

func TestMemoryStore_UpdateExistingCode(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	originalCode := &core.AuthorizationCode{
		Code:        "update_test_code",
		ClientID:    "client_original",
		RedirectURI: "https://original.com/callback",
		Scope:       []string{"read"},
		ExpiresAt:   time.Now().Add(10 * time.Minute).Unix(),
		CreatedAt:   time.Now().Unix(),
	}

	err := store.SaveAuthorizationCode(ctx, originalCode)
	if err != nil {
		t.Fatalf("Failed to save original code: %v", err)
	}

	updatedCode := &core.AuthorizationCode{
		Code:        "update_test_code",
		ClientID:    "client_updated",
		RedirectURI: "https://updated.com/callback",
		Scope:       []string{"read", "write"},
		ExpiresAt:   time.Now().Add(20 * time.Minute).Unix(),
		CreatedAt:   time.Now().Unix(),
	}

	err = store.SaveAuthorizationCode(ctx, updatedCode)
	if err != nil {
		t.Fatalf("Failed to update code: %v", err)
	}

	retrievedCode, err := store.GetAuthorizationCode(ctx, "update_test_code")
	if err != nil {
		t.Fatalf("Failed to retrieve updated code: %v", err)
	}

	if retrievedCode.ClientID != "client_updated" {
		t.Errorf("Code was not updated. Got ClientID %v, want %v", retrievedCode.ClientID, "client_updated")
	}

	if retrievedCode.RedirectURI != "https://updated.com/callback" {
		t.Errorf("RedirectURI was not updated. Got %v, want %v", retrievedCode.RedirectURI, "https://updated.com/callback")
	}

	if len(retrievedCode.Scope) != 2 {
		t.Errorf("Scope was not updated. Got %v scopes, want 2", len(retrievedCode.Scope))
	}
}
