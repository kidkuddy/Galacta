package message

import (
	"context"
	"fmt"

	"github.com/kidkuddy/galacta/events"
	"github.com/kidkuddy/galacta/team"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Deps holds dependencies for the send_message tool, scoped to one agent.
type Deps struct {
	Manager   *team.Manager
	Emitter   *events.Emitter
	AgentName string
	TeamName  string
}

// NewServer creates an MCP server with the galacta_send_message tool.
func NewServer(deps *Deps) *server.MCPServer {
	srv := server.NewMCPServer(
		"galacta-message",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	tool := mcp.NewTool("galacta_send_message",
		mcp.WithDescription("Send a message to a teammate or broadcast to all team members."),
		mcp.WithString("type", mcp.Required(), mcp.Description("Message type: message, broadcast, shutdown_request, shutdown_response")),
		mcp.WithString("recipient", mcp.Description("Agent name of the recipient (required for message, shutdown_request)")),
		mcp.WithString("content", mcp.Description("Message text or reason")),
		mcp.WithString("summary", mcp.Description("5-10 word summary shown as preview")),
		mcp.WithString("request_id", mcp.Description("Request ID to respond to (for shutdown_response)")),
		mcp.WithBoolean("approve", mcp.Description("Whether to approve the request (for shutdown_response)")),
	)

	srv.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		msgType, _ := req.Params.Arguments["type"].(string)
		recipient, _ := req.Params.Arguments["recipient"].(string)
		content, _ := req.Params.Arguments["content"].(string)
		summary, _ := req.Params.Arguments["summary"].(string)
		requestID, _ := req.Params.Arguments["request_id"].(string)

		if msgType == "" {
			return mcp.NewToolResultError("type is required"), nil
		}

		at, ok := deps.Manager.Get(deps.TeamName)
		if !ok {
			return mcp.NewToolResultError(fmt.Sprintf("team %q not found", deps.TeamName)), nil
		}

		msg := team.TeamMessage{
			Type:      msgType,
			From:      deps.AgentName,
			Recipient: recipient,
			Content:   content,
			Summary:   summary,
			RequestID: requestID,
		}

		if v, ok := req.Params.Arguments["approve"].(bool); ok {
			msg.Approve = &v
		}

		switch msgType {
		case "message", "shutdown_request":
			if recipient == "" {
				return mcp.NewToolResultError("recipient is required for message/shutdown_request"), nil
			}
			if err := at.Bus.Send(msg); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("send failed: %v", err)), nil
			}
			deps.Emitter.EmitTeamMessage(deps.AgentName, recipient, summary, msgType)
			return mcp.NewToolResultText(fmt.Sprintf("message sent to %s", recipient)), nil

		case "broadcast":
			at.Bus.Broadcast(deps.AgentName, msg)
			deps.Emitter.EmitTeamMessage(deps.AgentName, "", summary, msgType)
			return mcp.NewToolResultText("broadcast sent to all team members"), nil

		case "shutdown_response":
			if requestID == "" {
				return mcp.NewToolResultError("request_id is required for shutdown_response"), nil
			}
			if err := at.Bus.Send(msg); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("send failed: %v", err)), nil
			}
			deps.Emitter.EmitTeamMessage(deps.AgentName, recipient, "shutdown response", msgType)
			return mcp.NewToolResultText("shutdown response sent"), nil

		default:
			return mcp.NewToolResultError(fmt.Sprintf("unknown message type: %s", msgType)), nil
		}
	})

	return srv
}
