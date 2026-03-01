package team

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/kidkuddy/galacta/events"
	teamcore "github.com/kidkuddy/galacta/team"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Deps holds dependencies for team tools.
type Deps struct {
	Manager *teamcore.Manager
	Emitter *events.Emitter
}

// NewServer creates an MCP server with team_create and team_delete tools.
func NewServer(deps *Deps) *server.MCPServer {
	srv := server.NewMCPServer(
		"galacta-team",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	registerCreate(srv, deps)
	registerDelete(srv, deps)

	return srv
}

func registerCreate(srv *server.MCPServer, deps *Deps) {
	tool := mcp.NewTool("galacta_team_create",
		mcp.WithDescription("Create a new team to coordinate multiple agents working on a project."),
		mcp.WithString("team_name", mcp.Required(), mcp.Description("Name for the new team")),
		mcp.WithString("description", mcp.Description("Team description/purpose")),
		mcp.WithString("agent_type", mcp.Description("Type/role of the team lead")),
	)

	srv.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name, _ := req.Params.Arguments["team_name"].(string)
		desc, _ := req.Params.Arguments["description"].(string)
		agentType, _ := req.Params.Arguments["agent_type"].(string)

		if name == "" {
			return mcp.NewToolResultError("team_name is required"), nil
		}

		cfg, err := deps.Manager.Create(name, desc, agentType)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to create team: %v", err)), nil
		}

		deps.Emitter.EmitTeamCreated(name)

		out, _ := json.Marshal(cfg)
		return mcp.NewToolResultText(string(out)), nil
	})
}

func registerDelete(srv *server.MCPServer, deps *Deps) {
	tool := mcp.NewTool("galacta_team_delete",
		mcp.WithDescription("Delete a team after all agents have shut down."),
		mcp.WithString("team_name", mcp.Required(), mcp.Description("Name of the team to delete")),
	)

	srv.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name, _ := req.Params.Arguments["team_name"].(string)

		if name == "" {
			return mcp.NewToolResultError("team_name is required"), nil
		}

		if err := deps.Manager.Delete(name); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to delete team: %v", err)), nil
		}

		deps.Emitter.EmitTeamDeleted(name)

		return mcp.NewToolResultText(fmt.Sprintf("team %q deleted", name)), nil
	})
}
