package operation

import (
	"github.com/go-training/mcp-workshop/pkg/operation/caculator"
	"github.com/go-training/mcp-workshop/pkg/operation/echo"
	"github.com/go-training/mcp-workshop/pkg/operation/oauth"
	"github.com/go-training/mcp-workshop/pkg/operation/token"

	"github.com/mark3labs/mcp-go/server"
)

/*
RegisterCommonTool registers general (non-authentication) tools to the specified MCPServer instance.

Parameters:
  - s: Pointer to the MCPServer instance where the tools will be registered.

This function registers echo and calculator tools to the MCPServer.
*/
func RegisterCommonTool(s *server.MCPServer) {
	tool := &Tool{}

	tool.RegisterRead(server.ServerTool{
		Tool:    echo.EchoMessageTool,
		Handler: echo.HandleEchoMessageTool,
	})
	tool.RegisterWrite(server.ServerTool{
		Tool:    caculator.AddNumbersTool,
		Handler: caculator.HandleAddNumbersTool,
	})

	s.AddTools(tool.Tools()...)
}

/*
RegisterAuthTool registers authentication-related tools to the specified MCPServer instance.

Parameters:
  - s: Pointer to the MCPServer instance where the tools will be registered.

This function registers token tools (authenticated request, show token) to the MCPServer.
*/
func RegisterAuthTool(s *server.MCPServer) {
	tool := &Tool{}

	tool.RegisterRead(server.ServerTool{
		Tool:    token.MakeAuthenticatedRequestTool,
		Handler: token.HandleMakeAuthenticatedRequestTool,
	})
	tool.RegisterRead(server.ServerTool{
		Tool:    token.ShowAuthTokenTool,
		Handler: token.HandleShowAuthTokenTool,
	})
	tool.RegisterRead(server.ServerTool{
		Tool:    oauth.ListOAuthClientsTool,
		Handler: oauth.HandleListOAuthClientsTool,
	})

	s.AddTools(tool.Tools()...)
}

/*
Tool manages collections of tools to be registered with an MCPServer.

Fields:
  - write: Stores all ServerTools registered as write operations.
  - read: Stores all ServerTools registered as read operations.
*/
type Tool struct {
	write []server.ServerTool
	read  []server.ServerTool
}

/*
RegisterWrite registers a ServerTool as a write operation.

Parameters:
  - s: The ServerTool instance to register.

This method appends the tool to the write slice, indicating it is a write-type operation.
*/
func (t *Tool) RegisterWrite(s server.ServerTool) {
	t.write = append(t.write, s)
}

/*
RegisterRead registers a ServerTool as a read operation.

Parameters:
  - s: The ServerTool instance to register.

This method appends the tool to the read slice, indicating it is a read-type operation.
*/
func (t *Tool) RegisterRead(s server.ServerTool) {
	t.read = append(t.read, s)
}

/*
Tools returns all registered ServerTools.

Returns:
  - []server.ServerTool: A slice containing all write and read tools, with write tools first followed by read tools.

This method combines all registered tools for convenient batch registration to the MCPServer.
*/
func (t *Tool) Tools() []server.ServerTool {
	tools := make([]server.ServerTool, 0, len(t.write)+len(t.read))
	tools = append(tools, t.write...)
	tools = append(tools, t.read...)
	return tools
}
