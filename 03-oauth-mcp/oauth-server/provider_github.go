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
	"time"

	"github.com/mark3labs/mcp-go/client/transport"
)

const (
	githubAuthorizeURL = "https://github.com/login/oauth/authorize"
	githubTokenURL     = "https://github.com/login/oauth/access_token"
	githubUserAPIURL   = "https://api.github.com/user"
	requestTimeout     = 30 * time.Second
)

// GitHubProvider implements OAuthProvider for GitHub.
type GitHubProvider struct {
	httpClient *http.Client
}

// NewGitHubProvider creates a new GitHub provider with configured HTTP client.
func NewGitHubProvider() *GitHubProvider {
	return &GitHubProvider{
		httpClient: &http.Client{
			Timeout: requestTimeout,
		},
	}
}

func (g *GitHubProvider) GetAuthorizeURL(clientID, state, redirectURI, scopes, codeChallenge, codeChallengeMethod string) (string, error) {
	u, err := url.Parse(githubAuthorizeURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse authorize URL: %w", err)
	}
	values := url.Values{}
	values.Set("client_id", clientID)
	values.Set("state", state)
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

func (g *GitHubProvider) ExchangeToken(clientID, clientSecret, code, redirectURI, codeVerifier string) (*Token, error) {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	reqBody := map[string]string{
		"client_id":     clientID,
		"client_secret": clientSecret,
		"code":          code,
		"redirect_uri":  redirectURI,
		"code_verifier": codeVerifier,
	}
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", githubTokenURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to exchange token: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token exchange failed with status %d: %s", resp.StatusCode, string(body))
	}
	var tokenResp transport.Token
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal token response: %w", err)
	}
	return &Token{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		TokenType:    tokenResp.TokenType,
		ExpiresIn:    tokenResp.ExpiresIn,
		Scope:        tokenResp.Scope,
		// ExpiresAt is the time when the token expires
		ExpiresAt: tokenResp.ExpiresAt,
	}, nil
}

func (g *GitHubProvider) FetchUserInfo(accessToken string) (*UserInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", githubUserAPIURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch user info: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read error response: %w", err)
		}
		return nil, fmt.Errorf("failed to fetch user info with status %d: %s", resp.StatusCode, string(body))
	}

	// Read and debug log the raw JSON body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read user info body: %w", err)
	}
	slog.Debug("GitHub user info response", "raw_body", string(body))

	var user struct {
		Login     string `json:"login"`
		Email     string `json:"email"`
		Name      string `json:"name"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := json.Unmarshal(body, &user); err != nil {
		return nil, fmt.Errorf("failed to decode user info: %w", err)
	}
	return &UserInfo{
		Name:      user.Name,
		Email:     user.Email,
		Login:     user.Login,
		AvatarURL: user.AvatarURL,
	}, nil
}
