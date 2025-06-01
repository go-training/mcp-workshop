package echo

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
)

var EchoMessageTool = mcp.NewTool("echo_message",
	mcp.WithDescription(`Echo Message Tool

Description:
  Returns the input message as a response, prefixed with "Echo: ". This tool is useful for testing connectivity, debugging, or verifying tool integration.

Input Parameters:
  - message (string, required): The message to be echoed back in the response.
    Constraints: Must be a non-empty string. Recommended max length: 500 characters.

Output:
  - Returns a text result in the format: "Echo: <message>"

Example Usage:
  Request:
    {
      "message": "Hello, world!"
    }
  Response:
    "Echo: Hello, world!"

Error Conditions:
  - If the "message" parameter is missing or not a string, an error is returned.
  - If the message exceeds the allowed length or is empty, an error may be returned.

Use Cases:
  - Testing tool invocation and argument passing.
  - Verifying server responsiveness.
  - Demonstrating basic tool structure in MCP.`), // Tool description
	mcp.WithString("message", // Tool argument name
		mcp.Description("The message to echo back in the response."), // Argument description
		mcp.Required(), // Argument is required
	),
)

// handleEchoMessageTool is the handler function for the MCP tool "echo_message".
// Parameters:
//   - ctx: context.Context, the request context, used for cancellation, timeout, etc.
//   - req: mcp.CallToolRequest, the MCP tool call request object containing tool arguments.
//
// Returns:
//   - *mcp.CallToolResult: the result of the tool execution (here, a text message)
//   - error: returns an error if the argument is invalid or processing fails
func HandleEchoMessageTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Retrieve the "message" argument from arguments and check its type
	message, ok := req.GetArguments()["message"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid message argument") // Argument type error
	}
	// Return the formatted message
	return mcp.NewToolResultText(fmt.Sprintf("Echo: %s", message)), nil
}
