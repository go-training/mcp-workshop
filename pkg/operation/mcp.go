package operation

import (
	"github.com/go-training/mcp-workshop/pkg/operation/caculator"
	"github.com/go-training/mcp-workshop/pkg/operation/echo"
	"github.com/mark3labs/mcp-go/server"
)

// RegisterTool registers custom tools to the given MCPServer instance.
// Parameter:
//
//	s - the MCPServer instance to which the tools will be registered.
func RegisterTool(s *server.MCPServer) {
	// Register the EchoMessageTool and its handler.
	s.AddTool(echo.EchoMessageTool, echo.HandleEchoMessageTool)
	// Register the AddNumbersTool and its handler.
	s.AddTool(caculator.AddNumbersTool, caculator.HandleAddNumbersTool)
}
