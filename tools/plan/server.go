package plan

import (
	"context"
	"sync"

	"github.com/kidkuddy/galacta/events"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// PlanState tracks whether plan mode is active for a session.
type PlanState struct {
	mu     sync.Mutex
	active bool
}

// IsActive returns whether plan mode is currently active.
func (p *PlanState) IsActive() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.active
}

// SetActive sets the plan mode state.
func (p *PlanState) SetActive(v bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.active = v
}

// NewServer creates an MCP server with enter/exit plan mode tools.
func NewServer(state *PlanState, emitter *events.Emitter) *server.MCPServer {
	srv := server.NewMCPServer(
		"galacta-plan",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	enterTool := mcp.NewTool("galacta_enter_plan_mode",
		mcp.WithDescription("Enter plan mode. While active, only read-only tools are available. Use this to explore the codebase and design an implementation approach before writing code."),
	)

	srv.AddTool(enterTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		state.SetActive(true)
		emitter.EmitPlanModeChanged(true)
		return mcp.NewToolResultText("Plan mode activated. Only read-only tools are available until you call galacta_exit_plan_mode."), nil
	})

	exitTool := mcp.NewTool("galacta_exit_plan_mode",
		mcp.WithDescription("Exit plan mode and restore all tools. Call this when you have finished planning and are ready to implement."),
	)

	srv.AddTool(exitTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		state.SetActive(false)
		emitter.EmitPlanModeChanged(false)
		return mcp.NewToolResultText("Plan mode deactivated. All tools are now available."), nil
	})

	return srv
}
