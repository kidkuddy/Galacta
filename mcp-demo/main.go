// mcp-demo is a minimal MCP server with a single "echo" tool.
// Run via stdio. Register it in your MCP config as:
//
//	{"command": "/path/to/mcp-demo"}
package main

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func main() {
	s := server.NewMCPServer("mcp-demo", "0.1.0",
		server.WithToolCapabilities(false),
	)

	s.AddTool(
		mcp.NewTool("echo",
			mcp.WithDescription("Echoes back whatever you send it"),
			mcp.WithString("message",
				mcp.Required(),
				mcp.Description("The message to echo"),
			),
		),
		func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			msg, _ := req.Params.Arguments["message"].(string)
			return mcp.NewToolResultText(fmt.Sprintf("echo: %s", msg)), nil
		},
	)

	if err := server.ServeStdio(s); err != nil {
		panic(err)
	}
}
