package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/mark3labs/mcp-go/client/transport"
)

const (
	giteaAuthorizePath = "/login/oauth/authorize"
	giteaTokenPath     = "/login/oauth/access_token"
	giteaUserAPIPath   = "/api/v1/user"
)

// GiteaProvider implements OAuthProvider for Gitea.
// It supports self-hosted Gitea instances by allowing a custom host.
type GiteaProvider struct {
	host       string
	httpClient *http.Client
}

// NewGiteaProvider creates a new Gitea provider for a specific Gitea host.
func NewGiteaProvider(host string) *GiteaProvider {
	return &GiteaProvider{
		host: host,
		httpClient: &http.Client{
			Timeout: requestTimeout,
		},
	}
}

// GetAuthorizeURL generates the authorization URL for Gitea.
func (g *GiteaProvider) GetAuthorizeURL(
	clientID, state, redirectURI, scopes, codeChallenge, codeChallengeMethod string,
) (string, error) {
	u, err := url.Parse(g.host + giteaAuthorizePath)
	if err != nil {
		return "", fmt.Errorf("failed to parse gitea authorize URL: %w", err)
	}
	values := url.Values{}
	values.Set("client_id", clientID)
	values.Set("state", state)
	values.Set("response_type", "code")
	if redirectURI != "" {
		values.Set("redirect_uri", redirectURI)
	}
	if scopes != "" {
		values.Set("scope", scopes)
	}
	if codeChallenge != "" {
		values.Set("code_challenge", codeChallenge)
		values.Set("code_challenge_method", codeChallengeMethod)
	}
	u.RawQuery = values.Encode()
	return u.String(), nil
}

// ExchangeToken exchanges an authorization code for an access token.
func (g *GiteaProvider) ExchangeToken(
	clientID, clientSecret, code, redirectURI, codeVerifier string,
) (*Token, error) {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	tokenURL := g.host + giteaTokenPath
	reqBody := url.Values{}
	reqBody.Set("client_id", clientID)
	reqBody.Set("client_secret", clientSecret)
	reqBody.Set("code", code)
	reqBody.Set("grant_type", "authorization_code")
	if redirectURI != "" {
		reqBody.Set("redirect_uri", redirectURI)
	}
	if codeVerifier != "" {
		reqBody.Set("code_verifier", codeVerifier)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		"POST",
		tokenURL,
		bytes.NewBufferString(reqBody.Encode()),
	)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to exchange gitea token: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read gitea token response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(
			"gitea token exchange failed with status %d: %s",
			resp.StatusCode,
			string(body),
		)
	}

	var tokenResp transport.Token
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal gitea token response: %w", err)
	}

	return &Token{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		TokenType:    tokenResp.TokenType,
		ExpiresIn:    tokenResp.ExpiresIn,
		Scope:        tokenResp.Scope,
		ExpiresAt:    tokenResp.ExpiresAt,
	}, nil
}

// FetchUserInfo fetches user information from the Gitea API.
func (g *GiteaProvider) FetchUserInfo(accessToken string) (*UserInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	userAPIURL := g.host + giteaUserAPIPath
	req, err := http.NewRequestWithContext(ctx, "GET", userAPIURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch gitea user info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read gitea error response: %w", err)
		}
		return nil, fmt.Errorf(
			"failed to fetch gitea user info with status %d: %s",
			resp.StatusCode,
			string(body),
		)
	}

	// Read and debug log the raw JSON body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read gitea user info body: %w", err)
	}
	slog.Debug("Gitea user info response", "raw_body", string(body))

	var user struct {
		Login     string `json:"login"`
		Email     string `json:"email"`
		Name      string `json:"name"`
		AvatarURL string `json:"avatar_url"`
		FullName  string `json:"full_name"`
	}
	if err := json.Unmarshal(body, &user); err != nil {
		return nil, fmt.Errorf("failed to decode gitea user info: %w", err)
	}

	return &UserInfo{
		Name:      user.FullName,
		Email:     user.Email,
		Login:     user.Login,
		AvatarURL: user.AvatarURL,
	}, nil
}
