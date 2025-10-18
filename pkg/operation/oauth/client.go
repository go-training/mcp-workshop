// Package oauth provides MCP tools for managing OAuth clients.
package oauth

import (
	"context"
	"encoding/json"

	"github.com/go-training/mcp-workshop/pkg/core"
	"github.com/mark3labs/mcp-go/mcp"
)

// ListOAuthClientsTool defines the MCP tool for listing all OAuth clients.
var ListOAuthClientsTool = mcp.NewTool("list_oauth_clients",
	mcp.WithDescription("List all OAuth clients"),
)

// HandleListOAuthClientsTool is an MCP tool handler that retrieves and returns
// a list of all registered OAuth clients from the store.
func HandleListOAuthClientsTool(
	ctx context.Context,
	_ mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	logger := core.LoggerFromCtx(ctx)
	logger.Info("Handling list_oauth_clients tool")

	store, err := core.StoreFromContext(ctx)
	if err != nil {
		logger.Error("Missing store from context", "error", err)
		return nil, err
	}

	clients, err := store.GetClients(ctx)
	if err != nil {
		logger.Error("Failed to get clients from store", "error", err)
		return nil, err
	}

	data, err := json.Marshal(clients)
	if err != nil {
		logger.Error("Failed to marshal clients to JSON", "error", err)
		return nil, err
	}

	logger.Info("Successfully retrieved OAuth clients")
	return mcp.NewToolResultText(string(data)), nil
}
