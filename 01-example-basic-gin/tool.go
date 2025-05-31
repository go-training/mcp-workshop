package main

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// handleEchoMessageTool is the handler function for the MCP tool "echo_message".
// Parameters:
//   - ctx: context.Context, the request context, used for cancellation, timeout, etc.
//   - req: mcp.CallToolRequest, the MCP tool call request object containing tool arguments.
//
// Returns:
//   - *mcp.CallToolResult: the result of the tool execution (here, a text message)
//   - error: returns an error if the argument is invalid or processing fails
func handleEchoMessageTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Retrieve the "message" argument from arguments and check its type
	message, ok := req.GetArguments()["message"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid message argument") // Argument type error
	}
	// Return the formatted message
	return mcp.NewToolResultText(fmt.Sprintf("Echo: %s", message)), nil
}

func handleAddNumbersTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Retrieve num1 and num2 arguments and check their types
	num1Val, ok1 := req.GetArguments()["num1"].(float64)
	num2Val, ok2 := req.GetArguments()["num2"].(float64)
	if !ok1 || !ok2 {
		return nil, fmt.Errorf("invalid num1 or num2 argument")
	}
	sum := num1Val + num2Val
	return mcp.NewToolResultText(fmt.Sprintf("Sum: %.0f", sum)), nil
}

func registerTool(s *server.MCPServer) {
	// Register the echo_message tool and specify the handler function
	s.AddTool(mcp.NewTool("echo_message",
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
	), handleEchoMessageTool)

	// Register the add_numbers tool
	s.AddTool(mcp.NewTool("add_numbers",
		mcp.WithDescription(`Add Numbers Tool

Description:
  Calculates the sum of two numbers. Useful for basic arithmetic operations.

Input Parameters:
  - num1 (number, required): The first addend
  - num2 (number, required): The second addend

Output:
  - Returns: "Sum: <num1 + num2>"

Example Usage:
  Request:
    {
      "num1": 42,
      "num2": 58
    }
  Response:
    "Sum: 100"

Error Conditions:
  - If num1 or num2 is missing or has the wrong type, an error is returned.

Use Cases:
  - Basic arithmetic operations
  - Summing values in automated workflows
  - Teaching or testing arithmetic functionality
`),
		mcp.WithNumber("num1",
			mcp.Description("The first number to add"),
			mcp.Required(),
		),
		mcp.WithNumber("num2",
			mcp.Description("The second number to add"),
			mcp.Required(),
		),
	), handleAddNumbersTool)
}
