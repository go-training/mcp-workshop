package caculator

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
)

var AddNumbersTool = mcp.NewTool("add_numbers",
	mcp.WithDescription("Calculates the sum of two numbers."),
	mcp.WithNumber("num1",
		mcp.Description("The first number to add"),
		mcp.Required(),
	),
	mcp.WithNumber("num2",
		mcp.Description("The second number to add"),
		mcp.Required(),
	),
)

func HandleAddNumbersTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Retrieve num1 and num2 arguments and check their types
	num1Val, ok1 := req.GetArguments()["num1"].(float64)
	num2Val, ok2 := req.GetArguments()["num2"].(float64)
	if !ok1 || !ok2 {
		return nil, fmt.Errorf("invalid num1 or num2 argument")
	}
	sum := num1Val + num2Val
	return mcp.NewToolResultText(fmt.Sprintf("Sum: %.0f", sum)), nil
}
