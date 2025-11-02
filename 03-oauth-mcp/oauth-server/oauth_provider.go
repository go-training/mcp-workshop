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

// UserInfo represents the user information returned from OAuth providers.
type UserInfo struct {
	Email     string `json:"email"`
	Name      string `json:"name"`
	Login     string `json:"login"`
	AvatarURL string `json:"avatar_url,omitempty"`
}

type OAuthProvider interface {
	GetAuthorizeURL(
		clientID, state, redirectURI, scopes, codeChallenge, codeChallengeMethod string,
	) (string, error)
	ExchangeToken(clientID, clientSecret, code, redirectURI, codeVerifier string) (*Token, error)
	FetchUserInfo(accessToken string) (*UserInfo, error)
}
