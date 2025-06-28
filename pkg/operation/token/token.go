// Package token provides MCP tools for authenticated HTTP request and showing auth tokens.
package token

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/go-training/mcp-workshop/pkg/core"
	"github.com/mark3labs/mcp-go/mcp"
)

// response represents the structure of the response from httpbin.org/anything.
type response struct {
	Args    map[string]any    `json:"args"`
	Headers map[string]string `json:"headers"`
}

// makeRequest sends a GET request to https://httpbin.org/anything, including
// the provided auth token in the Authorization header and the message as a query parameter.
// Returns the parsed response or an error.
func makeRequest(ctx context.Context, message, token string) (*response, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://httpbin.org/anything", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", token)
	query := req.URL.Query()
	query.Add("message", message)
	req.URL.RawQuery = query.Encode()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	// Check HTTP status code, return error if not 2xx
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("http request failed: status %d %s", resp.StatusCode, resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var r *response
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, err
	}
	return r, nil
}

// MakeAuthenticatedRequestTool defines the MCP tool for making authenticated HTTP requests.
var MakeAuthenticatedRequestTool = mcp.NewTool("make_authenticated_request",
	mcp.WithDescription("Make an authenticated HTTP request to httpbin.org/anything"),
	mcp.WithString("message",
		mcp.Description("The message to include in the request"),
		mcp.Required(),
	),
)

// ShowAuthTokenTool defines the MCP tool for displaying the current auth token.
var ShowAuthTokenTool = mcp.NewTool("show_auth_token",
	mcp.WithDescription("Show the current authentication token"),
)

// HandleMakeAuthenticatedRequestTool is an MCP tool handler that makes an
// authenticated HTTP request using the token from context and the provided message argument.
func HandleMakeAuthenticatedRequestTool(
	ctx context.Context,
	request mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	logger := core.LoggerFromCtx(ctx)
	logger.Info("Handling make_authenticated_request tool")
	message, ok := request.GetArguments()["message"].(string)
	if !ok {
		logger.Error("Missing message argument")
		return nil, fmt.Errorf("missing message")
	}
	token, err := core.TokenFromContext(ctx)
	if err != nil || token == "" {
		logger.Error("Missing token", "error", err)
		return nil, fmt.Errorf("missing token: %v", err)
	}
	// Make the HTTP request with the token, regardless of its source.
	resp, err := makeRequest(ctx, message, token)
	if err != nil {
		logger.Error("HTTP request failed", "error", err)
		return nil, err
	}
	logger.Info("HTTP request succeeded")
	return mcp.NewToolResultText(fmt.Sprintf("%+v", resp)), nil
}

// HandleShowAuthTokenTool is an MCP tool handler that returns the current
// auth token from context as a string.
func HandleShowAuthTokenTool(
	ctx context.Context,
	_ mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	token, err := core.TokenFromContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("missing token: %v", err)
	}
	// Mask the token: show only the first 4 and last 4 characters, hide the middle with asterisks for security
	masked := token
	if len(token) > 8 {
		masked = token[:6] + "****" + token[len(token)-2:]
	} else if len(token) > 0 {
		masked = "****"
	}
	return mcp.NewToolResultText(masked), nil
}
