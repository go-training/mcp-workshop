package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/mark3labs/mcp-go/client/transport"
)

// GitHubProvider implements OAuthProvider for GitHub.
type GitHubProvider struct{}

func (g *GitHubProvider) GetAuthorizeURL(clientID, state, redirectURI, scopes string) (string, error) {
	u, err := url.Parse("https://github.com/login/oauth/authorize")
	if err != nil {
		return "", err
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
	u.RawQuery = values.Encode()
	return u.String(), nil
}

func (g *GitHubProvider) ExchangeToken(clientID, clientSecret, code, redirectURI string) (*Token, error) {
	tokenEndpoint := "https://github.com/login/oauth/access_token"
	reqBody := map[string]string{
		"client_id":     clientID,
		"client_secret": clientSecret,
		"code":          code,
		"redirect_uri":  redirectURI,
	}
	jsonBody, _ := json.Marshal(reqBody)
	req, err := http.NewRequest("POST", tokenEndpoint, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token exchange failed: %s", string(body))
	}
	var tokenResp transport.Token
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, err
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

func (g *GitHubProvider) FetchUserInfo(accessToken string) (map[string]interface{}, error) {
	req, err := http.NewRequest("GET", "https://api.github.com/user", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to fetch user info: %s", string(body))
	}
	var user map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, err
	}
	return user, nil
}
