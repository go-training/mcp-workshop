package store

import (
	"context"
	"errors"
	"sync"

	"github.com/go-training/mcp-workshop/pkg/core"
)

var (
	// ErrCodeNotFound is returned when an authorization code is not found in the store.
	ErrCodeNotFound = errors.New("authorization code not found")
	// ErrNilAuthorizationCode is returned when attempting to save a nil authorization code.
	ErrNilAuthorizationCode = errors.New("authorization code cannot be nil")
	// ErrEmptyCode is returned when the authorization code string is empty.
	ErrEmptyCode = errors.New("authorization code string cannot be empty")
	// ErrClientNotFound is returned when a client is not found in the store.
	ErrClientNotFound = errors.New("client not found")
	// ErrNilClient is returned when attempting to save a nil client.
	ErrNilClient = errors.New("client cannot be nil")
	// ErrEmptyClientID is returned when the client ID string is empty.
	ErrEmptyClientID = errors.New("client ID cannot be empty")
)

// MemoryStore implements the core.Store interface using an in-memory map.
// It provides thread-safe storage for authorization codes and clients.
type MemoryStore struct {
	mu      sync.RWMutex
	codes   map[string]*core.AuthorizationCode
	clients map[string]*core.Client
}

// NewMemoryStore creates a new instance of MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		codes:   make(map[string]*core.AuthorizationCode),
		clients: make(map[string]*core.Client),
	}
}

// SaveAuthorizationCode stores an authorization code in memory.
// It returns an error if the code is nil or the code string is empty.
func (m *MemoryStore) SaveAuthorizationCode(ctx context.Context, code *core.AuthorizationCode) error {
	if code == nil {
		return ErrNilAuthorizationCode
	}
	if code.Code == "" {
		return ErrEmptyCode
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.codes[code.Code] = code
	return nil
}

// GetAuthorizationCode retrieves an authorization code from memory by its code string.
// It returns ErrCodeNotFound if the code does not exist.
func (m *MemoryStore) GetAuthorizationCode(ctx context.Context, code string) (*core.AuthorizationCode, error) {
	if code == "" {
		return nil, ErrEmptyCode
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	authCode, exists := m.codes[code]
	if !exists {
		return nil, ErrCodeNotFound
	}

	return authCode, nil
}

// GetClient retrieves a client from memory by its client ID.
// It returns ErrClientNotFound if the client does not exist.
func (m *MemoryStore) GetClient(ctx context.Context, clientID string) (*core.Client, error) {
	if clientID == "" {
		return nil, ErrEmptyClientID
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	client, exists := m.clients[clientID]
	if !exists {
		return nil, ErrClientNotFound
	}

	return client, nil
}

// CreateClient stores a new client in memory.
// It returns an error if the client is nil or the client ID is empty.
func (m *MemoryStore) CreateClient(ctx context.Context, client *core.Client) error {
	if client == nil {
		return ErrNilClient
	}
	if client.ID == "" {
		return ErrEmptyClientID
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.clients[client.ID] = client
	return nil
}

// UpdateClient updates an existing client in memory.
// It returns an error if the client is nil, the client ID is empty, or the client does not exist.
func (m *MemoryStore) UpdateClient(ctx context.Context, client *core.Client) error {
	if client == nil {
		return ErrNilClient
	}
	if client.ID == "" {
		return ErrEmptyClientID
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.clients[client.ID]; !exists {
		return ErrClientNotFound
	}

	m.clients[client.ID] = client
	return nil
}

// DeleteClient removes a client from memory by its client ID.
// It returns ErrClientNotFound if the client does not exist.
func (m *MemoryStore) DeleteClient(ctx context.Context, clientID string) error {
	if clientID == "" {
		return ErrEmptyClientID
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.clients[clientID]; !exists {
		return ErrClientNotFound
	}

	delete(m.clients, clientID)
	return nil
}
