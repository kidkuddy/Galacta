package skill

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
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

	// galacta_register_skill: create a new skill file at runtime
	registerTool := mcp.NewTool("galacta_register_skill",
		mcp.WithDescription("Register a new skill by writing a .claude/skills/{name}.md file. The skill becomes immediately available."),
		mcp.WithString("name", mcp.Required(), mcp.Description("Skill name (used as filename and /command name)")),
		mcp.WithString("description", mcp.Required(), mcp.Description("Short description of what the skill does")),
		mcp.WithString("prompt", mcp.Required(), mcp.Description("The prompt template body for the skill")),
	)

	srv.AddTool(registerTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name, _ := req.Params.Arguments["name"].(string)
		description, _ := req.Params.Arguments["description"].(string)
		prompt, _ := req.Params.Arguments["prompt"].(string)

		if name == "" || description == "" || prompt == "" {
			return mcp.NewToolResultError("name, description, and prompt are all required"), nil
		}

		// Ensure skills directory exists
		skillsDir := filepath.Join(workingDir, ".claude", "skills")
		if err := os.MkdirAll(skillsDir, 0755); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to create skills directory: %v", err)), nil
		}

		// Write skill file with frontmatter
		content := fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n%s\n", name, description, prompt)
		path := filepath.Join(skillsDir, name+".md")
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to write skill file: %v", err)), nil
		}

		// Register in memory for immediate availability
		registry.Register(SkillDef{
			Name:        name,
			Description: description,
			Prompt:      prompt,
		})

		return mcp.NewToolResultText(fmt.Sprintf("Skill %q registered at %s", name, path)), nil
	})

	return srv
}
