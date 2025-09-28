package main

import "time"

// Token represents a generic OAuth token response.
type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"` // Optional, may not be present in all responses
	TokenType    string    `json:"token_type"`              // e.g., "Bearer"
	ExpiresIn    int64     `json:"expires_in,omitempty"`    // Duration in seconds
	Scope        string    `json:"scope,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
}

// OAuthProvider defines the methods for any OAuth provider.
type OAuthProvider interface {
	GetAuthorizeURL(clientID, state, redirectURI, scopes string) (string, error)
	ExchangeToken(clientID, clientSecret, code, redirectURI string) (*Token, error)
	FetchUserInfo(accessToken string) (map[string]interface{}, error)
}
