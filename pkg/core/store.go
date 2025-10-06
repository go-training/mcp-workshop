package core

import "context"

// AuthorizationCode represents an OAuth 2.0 authorization code and its associated metadata.
type AuthorizationCode struct {
	Code                string   `json:"code"`
	ClientID            string   `json:"client_id"`
	RedirectURI         string   `json:"redirect_uri"`
	Scope               []string `json:"scope"`
	CodeChallenge       string   `json:"code_challenge,omitempty"`
	CodeChallengeMethod string   `json:"code_challenge_method,omitempty"`
	ExpiresAt           int64    `json:"expires_at"`
	CreatedAt           int64    `json:"created_at"`
}

// Client represents an OAuth 2.0 client application.
type Client struct {
	ID              string   `json:"client_id"`
	Secret          string   `json:"client_secret"`
	RedirectURIs    []string `json:"redirect_uris"`
	GrantTypes      []string `json:"grant_types"`
	ResponseTypes   []string `json:"response_types"`
	TokenAuthMethod string   `json:"token_endpoint_auth_method"`
	Scope           string   `json:"scope"`
	CreatedAt       int64    `json:"created_at"`
	UpdatedAt       int64    `json:"updated_at"`
}

// Store defines the interface for storing and retrieving authorization codes.
type Store interface {
	SaveAuthorizationCode(ctx context.Context, code *AuthorizationCode) error
	GetAuthorizationCode(ctx context.Context, code string) (*AuthorizationCode, error)

	GetClient(ctx context.Context, clientID string) (*Client, error)
	CreateClient(ctx context.Context, client *Client) error
	UpdateClient(ctx context.Context, client *Client) error
	DeleteClient(ctx context.Context, clientID string) error
}
