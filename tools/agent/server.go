package agent

import (
	"context"
	"fmt"
	"log"
	"strconv"

	agentloop "github.com/kidkuddy/galacta/agent"
	"github.com/kidkuddy/galacta/anthropic"
	"github.com/kidkuddy/galacta/events"
	"github.com/kidkuddy/galacta/permissions"
	"github.com/kidkuddy/galacta/team"
	"github.com/kidkuddy/galacta/toolcaller"
	"github.com/kidkuddy/galacta/tools/message"
	tasktool "github.com/kidkuddy/galacta/tools/task"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Deps holds dependencies needed to spawn sub-agents.
type Deps struct {
	Client      *anthropic.Client
	Caller      *toolcaller.Caller
	Emitter     *events.Emitter
	Model       string
	WorkingDir  string
	TeamManager *team.Manager
}

// NewServer creates an MCP server with the agent spawning tool.
func NewServer(deps *Deps) *server.MCPServer {
	srv := server.NewMCPServer(
		"galacta-agent",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	tool := mcp.NewTool("galacta_agent",
		mcp.WithDescription("Launch a sub-agent to handle a complex task autonomously. The sub-agent has access to tools and can perform multi-step work."),
		mcp.WithString("prompt", mcp.Required(), mcp.Description("The task for the sub-agent to perform")),
		mcp.WithString("description", mcp.Description("Short description of what the agent will do (3-5 words)")),
		mcp.WithString("subagent_type", mcp.Description("Agent type: general-purpose (default), Explore (read-only), Plan (read-only)")),
		mcp.WithString("model", mcp.Description("Optional model override for the sub-agent")),
		mcp.WithString("max_turns", mcp.Description("Maximum turns for the sub-agent (default 10)")),
		mcp.WithString("team_name", mcp.Description("Team to join (enables team messaging and shared tasks)")),
		mcp.WithString("name", mcp.Description("Agent name within the team")),
	)

	srv.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		prompt, _ := req.Params.Arguments["prompt"].(string)
		description, _ := req.Params.Arguments["description"].(string)
		agentType, _ := req.Params.Arguments["subagent_type"].(string)
		modelOverride, _ := req.Params.Arguments["model"].(string)
		maxTurnsStr, _ := req.Params.Arguments["max_turns"].(string)
		teamName, _ := req.Params.Arguments["team_name"].(string)
		agentName, _ := req.Params.Arguments["name"].(string)

		if prompt == "" {
			return mcp.NewToolResultError("prompt is required"), nil
		}
		if agentType == "" {
			agentType = "general-purpose"
		}

		maxTurns := 10
		if maxTurnsStr != "" {
			if n, err := strconv.Atoi(maxTurnsStr); err == nil && n > 0 {
				maxTurns = n
			}
		}

		model := deps.Model
		if modelOverride != "" {
			model = modelOverride
		}

		// Build tool filter based on subagent_type
		filter := buildSubAgentFilter(agentType)

		// Emit sub-agent start event
		deps.Emitter.EmitSubAgentStart(agentType, description)

		// Create a bypass gate — sub-agents auto-approve everything
		bypassGate := &permissions.BypassGate{}
		gate := permissions.NewInteractiveGate(bypassGate, deps.Emitter)

		loopOpts := &agentloop.AgentLoopOptions{}

		// Team wiring: if team_name + name are set, register on the bus
		var extraClients []client.MCPClient
		if teamName != "" && agentName != "" && deps.TeamManager != nil {
			at, ok := deps.TeamManager.Get(teamName)
			if !ok {
				deps.Emitter.EmitSubAgentEnd(agentType)
				return mcp.NewToolResultError(fmt.Sprintf("team %q not found", teamName)), nil
			}

			member := team.TeamMember{
				Name:      agentName,
				AgentID:   agentName,
				AgentType: agentType,
			}
			inboxCh, err := deps.TeamManager.AddMember(teamName, member)
			if err != nil {
				deps.Emitter.EmitSubAgentEnd(agentType)
				return mcp.NewToolResultError(fmt.Sprintf("failed to join team: %v", err)), nil
			}
			defer deps.TeamManager.RemoveMember(teamName, agentName)

			loopOpts.InboxCh = inboxCh

			// Add message tool scoped to this agent
			msgSrv := message.NewServer(&message.Deps{
				Manager:   deps.TeamManager,
				Emitter:   deps.Emitter,
				AgentName: agentName,
				TeamName:  teamName,
			})
			msgClient, err := client.NewInProcessClient(msgSrv)
			if err == nil {
				if err := msgClient.Start(ctx); err == nil {
					initReq := mcp.InitializeRequest{}
					initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
					initReq.Params.ClientInfo = mcp.Implementation{Name: "galacta", Version: "0.1.0"}
					if _, err := msgClient.Initialize(ctx, initReq); err == nil {
						extraClients = append(extraClients, msgClient)
					}
				}
			}

			// Add task tools backed by the team's shared TaskStore
			taskSrv := tasktool.NewServer(at.TaskStore)
			taskClient, err := client.NewInProcessClient(taskSrv)
			if err == nil {
				if err := taskClient.Start(ctx); err == nil {
					initReq := mcp.InitializeRequest{}
					initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
					initReq.Params.ClientInfo = mcp.Implementation{Name: "galacta", Version: "0.1.0"}
					if _, err := taskClient.Initialize(ctx, initReq); err == nil {
						extraClients = append(extraClients, taskClient)
					}
				}
			}
		}

		// Build caller for the sub-agent
		// Start with the parent caller's tools, then add any team-specific extras
		callerToUse := deps.Caller
		if len(extraClients) > 0 {
			// Create a new caller that includes the extra team tools
			registry := toolcaller.NewRegistry()
			subCaller := toolcaller.NewCaller(registry, 5)

			// Copy parent tool refs
			for _, ref := range deps.Caller.ListToolRefs() {
				registry.Add(ref.Name, ref.ToolRef)
			}

			// Add extra team clients
			for _, mc := range extraClients {
				if err := subCaller.AddClient(ctx, mc); err != nil {
					log.Printf("galacta: failed to add team tool client: %v", err)
				}
			}

			callerToUse = subCaller
		}

		// Build sub-agent loop (no DB persistence)
		loop := agentloop.NewAgentLoop(
			deps.Client,
			callerToUse,
			gate,
			deps.Emitter,
			nil, // no store — sub-agents don't persist
			model,
			fmt.Sprintf("You are a sub-agent of type %q. Complete the task described in the user message.", agentType),
			loopOpts,
		)

		// Run the sub-agent
		result, err := loop.RunSubAgent(ctx, prompt, maxTurns, filter)

		// Clean up extra clients
		for _, mc := range extraClients {
			mc.Close()
		}

		// Emit sub-agent end event
		deps.Emitter.EmitSubAgentEnd(agentType)

		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("sub-agent error: %v", err)), nil
		}

		return mcp.NewToolResultText(result), nil
	})

	return srv
}

// buildSubAgentFilter returns a ToolFilter for the given sub-agent type.
// All types deny galacta_agent to prevent recursion.
func buildSubAgentFilter(agentType string) *toolcaller.ToolFilter {
	deny := []string{"galacta_agent"}

	switch agentType {
	case "Explore", "Plan":
		// Read-only: only allow read/glob/grep/search/list/fetch tools
		return &toolcaller.ToolFilter{
			Deny:  deny,
			Globs: []string{"galacta_read", "galacta_glob", "galacta_grep", "galacta_web_fetch", "galacta_task_list", "galacta_task_get", "galacta_skill"},
		}
	default: // general-purpose
		return &toolcaller.ToolFilter{
			Deny: deny,
		}
	}
}
