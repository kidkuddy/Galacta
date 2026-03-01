package skill

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"text/template"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// NewServer creates an MCP server with the skill tool.
func NewServer(workingDir string) *server.MCPServer {
	srv := server.NewMCPServer(
		"galacta-skill",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	registry := NewRegistry(workingDir)

	skillNames := registry.List()
	desc := fmt.Sprintf("Execute a named skill. Available skills: %s", strings.Join(skillNames, ", "))

	tool := mcp.NewTool("galacta_skill",
		mcp.WithDescription(desc),
		mcp.WithString("skill", mcp.Required(), mcp.Description("The skill name to execute")),
		mcp.WithString("args", mcp.Description("Optional arguments for the skill")),
	)

	srv.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name, _ := req.Params.Arguments["skill"].(string)
		args, _ := req.Params.Arguments["args"].(string)

		if name == "" {
			return mcp.NewToolResultError("skill name is required"), nil
		}

		skillDef, ok := registry.Get(name)
		if !ok {
			return mcp.NewToolResultError(fmt.Sprintf("skill %q not found. Available: %s", name, strings.Join(skillNames, ", "))), nil
		}

		// Render template with args
		tmpl, err := template.New(name).Parse(skillDef.Prompt)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to parse skill template: %v", err)), nil
		}

		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, map[string]string{"Args": args}); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to render skill: %v", err)), nil
		}

		return mcp.NewToolResultText(buf.String()), nil
	})

	return srv
}
