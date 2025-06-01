package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
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

func main() {
	var clientID string
	var clientSecret string
	flag.StringVar(&clientID, "client_id", "", "OAuth 2.0 Client ID")
	flag.StringVar(&clientSecret, "client_secret", "", "OAuth 2.0 Client Secret")
	flag.Parse()

	// Create a token store to persist tokens
	tokenStore := client.NewMemoryTokenStore()

	// Create OAuth configuration
	oauthConfig := client.OAuthConfig{
		// Client ID can be empty if using dynamic registration
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURI:  redirectURI,
		Scopes:       []string{"mcp.read", "mcp.write"},
		TokenStore:   tokenStore,
		PKCEEnabled:  true, // Enable PKCE for public clients
	}

	// Create the client with OAuth support
	c, err := client.NewOAuthStreamableHttpClient(serverURL, oauthConfig)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	// Start the client
	if err := c.Start(context.Background()); err != nil {
		log.Fatalf("Failed to start client: %v", err)
	}
	defer c.Close()

	// Try to initialize the client
	result, err := c.Initialize(context.Background(), mcp.InitializeRequest{
		Params: struct {
			ProtocolVersion string                 `json:"protocolVersion"`
			Capabilities    mcp.ClientCapabilities `json:"capabilities"`
			ClientInfo      mcp.Implementation     `json:"clientInfo"`
		}{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo: mcp.Implementation{
				Name:    "mcp-go-oauth-example",
				Version: "0.1.0",
			},
		},
	})

	// Check if we need OAuth authorization
	if client.IsOAuthAuthorizationRequiredError(err) {
		fmt.Println("OAuth authorization required. Starting authorization flow...")

		// Get the OAuth handler from the error
		oauthHandler := client.GetOAuthHandler(err)

		// Start a local server to handle the OAuth callback
		callbackChan := make(chan map[string]string)
		server := startCallbackServer(callbackChan)
		defer server.Close()

		// Generate PKCE code verifier and challenge
		codeVerifier, err := client.GenerateCodeVerifier()
		if err != nil {
			log.Fatalf("Failed to generate code verifier: %v", err)
		}
		codeChallenge := client.GenerateCodeChallenge(codeVerifier)

		// Generate state parameter
		state, err := client.GenerateState()
		if err != nil {
			log.Fatalf("Failed to generate state: %v", err)
		}

		// Get the authorization URL
		authURL, err := oauthHandler.GetAuthorizationURL(context.Background(), state, codeChallenge)
		if err != nil {
			log.Fatalf("Failed to get authorization URL: %v", err)
		}

		// Open the browser to the authorization URL
		fmt.Printf("Opening browser to: %s\n", authURL)
		openBrowser(authURL)

		// Wait for the callback
		fmt.Println("Waiting for authorization callback...")
		params := <-callbackChan

		// Verify state parameter
		if params["state"] != state {
			log.Fatalf("State mismatch: expected %s, got %s", state, params["state"])
		}

		// Exchange the authorization code for a token
		code := params["code"]
		if code == "" {
			log.Fatalf("No authorization code received")
		}

		fmt.Println("Exchanging authorization code for token...")
		err = oauthHandler.ProcessAuthorizationResponse(context.Background(), code, state, codeVerifier)
		if err != nil {
			log.Fatalf("Failed to process authorization response: %v", err)
		}

		fmt.Println("Authorization successful!")

		// Try to initialize again with the token
		result, err = c.Initialize(context.Background(), mcp.InitializeRequest{
			Params: struct {
				ProtocolVersion string                 `json:"protocolVersion"`
				Capabilities    mcp.ClientCapabilities `json:"capabilities"`
				ClientInfo      mcp.Implementation     `json:"clientInfo"`
			}{
				ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
				ClientInfo: mcp.Implementation{
					Name:    "mcp-go-oauth-example",
					Version: "0.1.0",
				},
			},
		})
		if err != nil {
			log.Fatalf("Failed to initialize client after authorization: %v", err)
		}
	} else if err != nil {
		log.Fatalf("Failed to initialize client: %v", err)
	}

	fmt.Printf("Client initialized successfully! Server: %s %s\n",
		result.ServerInfo.Name,
		result.ServerInfo.Version)

	// Now you can use the client as usual
	// For example, list tools
	tools, err := c.ListTools(context.Background(), mcp.ListToolsRequest{})
	if err != nil {
		log.Fatalf("Failed to list tools: %v", err)
	}

	fmt.Println("Available tools:")
	for _, tool := range tools.Tools {
		fmt.Printf("- %s\n", tool.Name)
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
			log.Printf("Error writing response: %v", err)
		}
	})

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("HTTP server error: %v", err)
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
		err = fmt.Errorf("unsupported platform")
	}

	if err != nil {
		log.Printf("Failed to open browser: %v", err)
		fmt.Printf("Please open the following URL in your browser: %s\n", url)
	}
}
