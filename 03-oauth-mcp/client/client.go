package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"runtime"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

const (
	// Replace with your MCP server URL
	serverURL = "http://localhost:8080/mcp"
	// Use a localhost redirect URI for this example
	redirectURI = "http://localhost:8085/oauth/callback"
)

// fatalError logs an error message and exits the program with status code 1
// If errors are provided, the first error will be logged with the message
func fatalError(message string, errors ...error) {
	if len(errors) > 0 && errors[0] != nil {
		slog.Error(message, "err", errors[0])
	} else {
		slog.Error(message)
	}
	os.Exit(1)
}

func main() {
	// Create a token store to persist tokens
	tokenStore := client.NewMemoryTokenStore()

	// Create OAuth configuration
	oauthConfig := client.OAuthConfig{
		// Client ID can be empty if using dynamic registration
		ClientID:     os.Getenv("MCP_CLIENT_ID"),
		ClientSecret: os.Getenv("MCP_CLIENT_SECRET"),
		RedirectURI:  redirectURI,
		Scopes:       []string{"mcp.read", "mcp.write"},
		TokenStore:   tokenStore,
		PKCEEnabled:  true, // Enable PKCE for public clients
	}

	// Create the client with OAuth support
	c, err := client.NewOAuthStreamableHttpClient(serverURL, oauthConfig)
	if err != nil {
		fatalError("Failed to create client", err)
	}

	// Start the client
	if err := c.Start(context.Background()); err != nil {
		fatalError("Failed to start client", err)
	}
	defer c.Close()

	// Try to initialize the client
	result, err := c.Initialize(context.Background(), mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo: mcp.Implementation{
				Name:    "mcp-oauth-client-example",
				Version: "1.0.0",
			},
		},
	})

	// Check if we need OAuth authorization
	if client.IsOAuthAuthorizationRequiredError(err) {
		slog.Info("OAuth authorization required. Starting authorization flow...")

		// Get the OAuth handler from the error
		oauthHandler := client.GetOAuthHandler(err)

		// Start a local server to handle the OAuth callback
		callbackChan := make(chan map[string]string)
		server := startCallbackServer(callbackChan)
		defer server.Close()

		err = oauthHandler.RegisterClient(context.Background(), "mcp-go-oauth-example")
		if err != nil {
			fatalError("Failed to register client", err)
		}

		// Generate PKCE code verifier and challenge
		codeVerifier, err := client.GenerateCodeVerifier()
		if err != nil {
			fatalError("Failed to generate code verifier", err)
		}
		codeChallenge := client.GenerateCodeChallenge(codeVerifier)

		// Generate state parameter
		state, err := client.GenerateState()
		if err != nil {
			fatalError("Failed to generate state", err)
		}

		// Get the authorization URL
		authURL, err := oauthHandler.GetAuthorizationURL(context.Background(), state, codeChallenge)
		if err != nil {
			fatalError("Failed to get authorization URL", err)
		}

		// Open the browser to the authorization URL
		slog.Info("Opening browser to authorization URL", "authURL", authURL)
		openBrowser(authURL)

		// Wait for the callback
		slog.Info("Waiting for authorization callback...")
		params := <-callbackChan

		// Verify state parameter
		if params["state"] != state {
			fatalError("State mismatch: expected " + state + ", got " + params["state"])
		}

		// Exchange the authorization code for a token
		code := params["code"]
		if code == "" {
			fatalError("No authorization code received", nil)
		}

		slog.Info("Exchanging authorization code for token...")
		err = oauthHandler.ProcessAuthorizationResponse(context.Background(), code, state, codeVerifier)
		if err != nil {
			fatalError("Failed to process authorization response", err)
		}

		slog.Info("Authorization successful!")

		// Ping the server to verify connection
		slog.Info("Pinging server to verify connection...")
		err = c.Ping(context.Background())
		if err != nil {
			fatalError("Failed to ping server", err)
		}
		slog.Info("Server ping successful!")

		// Try to initialize again with the token
		result, err = c.Initialize(context.Background(), mcp.InitializeRequest{
			Params: mcp.InitializeParams{
				ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
				ClientInfo: mcp.Implementation{
					Name:    "mcp-go-oauth-example",
					Version: "0.1.0",
				},
			},
		})
		if err != nil {
			fatalError("Failed to initialize client after authorization", err)
		}
	} else if err != nil {
		fatalError("Failed to initialize client", err)
	}

	slog.Info("Client initialized successfully!",
		"server", result.ServerInfo.Name,
		"version", result.ServerInfo.Version)

	// Now you can use the client as usual
	// For example, list tools
	if result.Capabilities.Tools != nil {
		tools, err := c.ListTools(context.Background(), mcp.ListToolsRequest{
			PaginatedRequest: mcp.PaginatedRequest{
				Params: mcp.PaginatedParams{
					Cursor: "",
				},
			},
		})
		if err != nil {
			fatalError("Failed to list tools", err)
		}

		for _, tool := range tools.Tools {
			slog.Info("Available Tool", "name", tool.Name)
		}

		// Example: Call a tool
		toolName := "show_auth_token" // Replace with an actual tool name
		slog.Info("Calling tool", "name", toolName)
		toolResult, err := c.CallTool(context.Background(), mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name: toolName,
				// Add any required parameters for the tool here
			},
		})
		if err != nil {
			fatalError("Failed to call tool", err)
		}
		printToolResult(toolResult)
	}
}

// Helper function to print tool results
func printToolResult(result *mcp.CallToolResult) {
	for _, content := range result.Content {
		if textContent, ok := content.(mcp.TextContent); ok {
			slog.Info("Tool Result", "text", textContent.Text)
		} else {
			jsonBytes, _ := json.MarshalIndent(content, "", "  ")
			slog.Info("Tool Result", "json", string(jsonBytes))
		}
	}
}

// startCallbackServer starts a local HTTP server to handle the OAuth callback
func startCallbackServer(callbackChan chan<- map[string]string) *http.Server {
	server := &http.Server{
		Addr: ":8085",
	}

	http.HandleFunc("/oauth/callback", func(w http.ResponseWriter, r *http.Request) {
		// Extract query parameters
		params := make(map[string]string)
		for key, values := range r.URL.Query() {
			if len(values) > 0 {
				params[key] = values[0]
			}
		}

		// Send parameters to the channel
		callbackChan <- params

		// Respond to the user
		w.Header().Set("Content-Type", "text/html")
		_, err := w.Write([]byte(`
			<html>
				<body>
					<h1>Authorization Successful</h1>
					<p>You can now close this window and return to the application.</p>
					<script>window.close();</script>
				</body>
			</html>
		`))
		if err != nil {
			slog.Error("Error writing response", "err", err)
		}
	})

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("HTTP server error", "err", err)
		}
	}()

	return server
}

// openBrowser opens the default browser to the specified URL
func openBrowser(url string) {
	var err error

	switch runtime.GOOS {
	case "linux":
		err = exec.Command("xdg-open", url).Start()
	case "windows":
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		err = exec.Command("open", url).Start()
	default:
		err = errors.New("unsupported platform")
	}

	if err != nil {
		slog.Error("Failed to open browser", "err", err)
		slog.Info("Please open the following URL in your browser", "url", url)
	}
}
