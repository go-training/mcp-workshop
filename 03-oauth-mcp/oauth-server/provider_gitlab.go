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
	gitlabAuthorizePath = "/oauth/authorize"
	gitlabTokenPath     = "/oauth/token"
	gitlabUserAPIPath   = "/api/v4/user"
)

// GitLabProvider implements OAuthProvider for GitLab.
// It supports self-hosted GitLab instances by allowing a custom host.
type GitLabProvider struct {
	host       string
	httpClient *http.Client
}

// NewGitLabProvider creates a new GitLab provider for a specific GitLab host.
// Use "https://gitlab.com" for GitLab.com or your self-hosted instance URL.
func NewGitLabProvider(host string) *GitLabProvider {
	if host == "" {
		host = "https://gitlab.com"
	}
	return &GitLabProvider{
		host: host,
		httpClient: &http.Client{
			Timeout: requestTimeout,
		},
	}
}

// GetAuthorizeURL generates the authorization URL for GitLab OAuth.
func (g *GitLabProvider) GetAuthorizeURL(
	clientID, state, redirectURI, scopes, codeChallenge, codeChallengeMethod string,
) (string, error) {
	u, err := url.Parse(g.host + gitlabAuthorizePath)
	if err != nil {
		return "", fmt.Errorf("failed to parse GitLab authorize URL: %w", err)
	}

	values := url.Values{}
	values.Set("client_id", clientID)
	values.Set("response_type", "code")
	values.Set("state", state)

	if redirectURI != "" {
		values.Set("redirect_uri", redirectURI)
	}

	if scopes != "" {
		values.Set("scope", scopes)
	} else {
		// GitLab default scope
		values.Set("scope", "read_user")
	}

	if codeChallenge != "" {
		values.Set("code_challenge", codeChallenge)
		values.Set("code_challenge_method", codeChallengeMethod)
	}

	u.RawQuery = values.Encode()
	return u.String(), nil
}

// ExchangeToken exchanges an authorization code for an access token.
func (g *GitLabProvider) ExchangeToken(
	clientID, clientSecret, code, redirectURI, codeVerifier string,
) (*Token, error) {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	reqBody := map[string]string{
		"client_id":     clientID,
		"client_secret": clientSecret,
		"code":          code,
		"grant_type":    "authorization_code",
		"redirect_uri":  redirectURI,
	}
	if codeVerifier != "" {
		reqBody["code_verifier"] = codeVerifier
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal GitLab request body: %w", err)
	}

	tokenURL := g.host + gitlabTokenPath
	req, err := http.NewRequestWithContext(ctx, "POST", tokenURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to exchange GitLab token: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read GitLab token response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(
			"GitLab token exchange failed with status %d: %s",
			resp.StatusCode,
			string(body),
		)
	}

	var tokenResp transport.Token
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal GitLab token response: %w", err)
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

// FetchUserInfo fetches user information from the GitLab API.
func (g *GitLabProvider) FetchUserInfo(accessToken string) (*UserInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	userAPIURL := g.host + gitlabUserAPIPath
	req, err := http.NewRequestWithContext(ctx, "GET", userAPIURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch GitLab user info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read GitLab error response: %w", err)
		}
		return nil, fmt.Errorf(
			"failed to fetch GitLab user info with status %d: %s",
			resp.StatusCode,
			string(body),
		)
	}

	// Read and debug log the raw JSON body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read GitLab user info body: %w", err)
	}
	slog.Debug("GitLab user info response", "raw_body", string(body))

	var user struct {
		ID        int    `json:"id"`
		Username  string `json:"username"`
		Email     string `json:"email"`
		Name      string `json:"name"`
		AvatarURL string `json:"avatar_url"`
		State     string `json:"state"`
	}
	if err := json.Unmarshal(body, &user); err != nil {
		return nil, fmt.Errorf("failed to decode GitLab user info: %w", err)
	}

	return &UserInfo{
		Name:      user.Name,
		Email:     user.Email,
		Login:     user.Username,
		AvatarURL: user.AvatarURL,
	}, nil
}
