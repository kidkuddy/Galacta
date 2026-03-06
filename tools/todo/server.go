package todo

import (
	"context"
	"fmt"
	"strings"

	"github.com/kidkuddy/galacta/db"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// NewServer creates an MCP server with todo tracking tools.
func NewServer(store *db.SessionDB) *server.MCPServer {
	srv := server.NewMCPServer(
		"galacta-todo",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	registerWrite(srv, store)
	registerRead(srv, store)

	return srv
}

func registerWrite(srv *server.MCPServer, store *db.SessionDB) {
	tool := mcp.NewTool("galacta_todo_write",
		mcp.WithDescription("Update the todo list for the current session. Use proactively to track progress and pending tasks. Ensure at least one task is in_progress at all times. Always provide both content (imperative) and status for each task."),
		mcp.WithArray("todos",
			mcp.Required(),
			mcp.Description("List of todo items"),
		),
	)

	srv.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		rawTodos, ok := req.Params.Arguments["todos"].([]any)
		if !ok {
			return mcp.NewToolResultError("todos must be an array"), nil
		}

		todos := make([]db.Todo, 0, len(rawTodos))
		for i, raw := range rawTodos {
			item, ok := raw.(map[string]any)
			if !ok {
				return mcp.NewToolResultError(fmt.Sprintf("todo[%d] must be an object", i)), nil
			}

			id, _ := item["id"].(string)
			content, _ := item["content"].(string)
			status, _ := item["status"].(string)
			priority, _ := item["priority"].(string)

			if id == "" {
				return mcp.NewToolResultError(fmt.Sprintf("todo[%d]: id is required", i)), nil
			}
			if content == "" {
				return mcp.NewToolResultError(fmt.Sprintf("todo[%d]: content is required", i)), nil
			}
			if status == "" {
				return mcp.NewToolResultError(fmt.Sprintf("todo[%d]: status is required", i)), nil
			}
			if status != "pending" && status != "in_progress" && status != "completed" {
				return mcp.NewToolResultError(fmt.Sprintf("todo[%d]: status must be pending, in_progress, or completed", i)), nil
			}
			if priority == "" {
				priority = "medium"
			}
			if priority != "high" && priority != "medium" && priority != "low" {
				return mcp.NewToolResultError(fmt.Sprintf("todo[%d]: priority must be high, medium, or low", i)), nil
			}

			todos = append(todos, db.Todo{
				ID:       id,
				Content:  content,
				Status:   status,
				Priority: priority,
			})
		}

		if err := store.ReplaceTodos(todos); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to update todos: %v", err)), nil
		}

		return mcp.NewToolResultText(formatTodos(todos)), nil
	})
}

func registerRead(srv *server.MCPServer, store *db.SessionDB) {
	tool := mcp.NewTool("galacta_todo_read",
		mcp.WithDescription("Read the current todo list for this session without modifying it."),
	)

	srv.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		todos, err := store.ListTodos()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to read todos: %v", err)), nil
		}

		if len(todos) == 0 {
			return mcp.NewToolResultText("No todos found."), nil
		}

		return mcp.NewToolResultText(formatTodos(todos)), nil
	})
}

func formatTodos(todos []db.Todo) string {
	var b strings.Builder
	for _, t := range todos {
		var icon string
		switch t.Status {
		case "pending":
			icon = "[ ]"
		case "in_progress":
			icon = "[~]"
		case "completed":
			icon = "[x]"
		}
		fmt.Fprintf(&b, "%s %s (id: %s, priority: %s)\n", icon, t.Content, t.ID, t.Priority)
	}
	return b.String()
}
