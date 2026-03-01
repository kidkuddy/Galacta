package task

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/kidkuddy/galacta/db"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// NewServer creates an MCP server with task management tools.
func NewServer(store *db.SessionDB) *server.MCPServer {
	srv := server.NewMCPServer(
		"galacta-task",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	registerCreate(srv, store)
	registerGet(srv, store)
	registerUpdate(srv, store)
	registerList(srv, store)

	return srv
}

func registerCreate(srv *server.MCPServer, store *db.SessionDB) {
	tool := mcp.NewTool("galacta_task_create",
		mcp.WithDescription("Create a new task to track work in the current session."),
		mcp.WithString("subject", mcp.Required(), mcp.Description("Brief imperative title for the task")),
		mcp.WithString("description", mcp.Required(), mcp.Description("Detailed description of what needs to be done")),
		mcp.WithString("activeForm", mcp.Description("Present continuous form shown when in_progress (e.g., 'Running tests')")),
	)

	srv.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		subject, _ := req.Params.Arguments["subject"].(string)
		description, _ := req.Params.Arguments["description"].(string)
		activeForm, _ := req.Params.Arguments["activeForm"].(string)

		if subject == "" {
			return mcp.NewToolResultError("subject is required"), nil
		}

		t, err := store.CreateTask(subject, description, activeForm)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to create task: %v", err)), nil
		}

		out, _ := json.Marshal(t)
		return mcp.NewToolResultText(string(out)), nil
	})
}

func registerGet(srv *server.MCPServer, store *db.SessionDB) {
	tool := mcp.NewTool("galacta_task_get",
		mcp.WithDescription("Retrieve a task by its ID to see full details."),
		mcp.WithString("taskId", mcp.Required(), mcp.Description("The ID of the task to retrieve")),
	)

	srv.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		idStr, _ := req.Params.Arguments["taskId"].(string)
		id, err := strconv.Atoi(idStr)
		if err != nil {
			return mcp.NewToolResultError("taskId must be a valid integer"), nil
		}

		t, err := store.GetTask(id)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("task not found: %v", err)), nil
		}

		out, _ := json.Marshal(t)
		return mcp.NewToolResultText(string(out)), nil
	})
}

func registerUpdate(srv *server.MCPServer, store *db.SessionDB) {
	tool := mcp.NewTool("galacta_task_update",
		mcp.WithDescription("Update an existing task's status, subject, description, owner, or dependencies."),
		mcp.WithString("taskId", mcp.Required(), mcp.Description("The ID of the task to update")),
		mcp.WithString("status", mcp.Description("New status: pending, in_progress, completed, or deleted")),
		mcp.WithString("subject", mcp.Description("New subject for the task")),
		mcp.WithString("description", mcp.Description("New description")),
		mcp.WithString("activeForm", mcp.Description("Present continuous form for spinner")),
		mcp.WithString("owner", mcp.Description("New owner for the task")),
	)

	srv.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		idStr, _ := req.Params.Arguments["taskId"].(string)
		id, err := strconv.Atoi(idStr)
		if err != nil {
			return mcp.NewToolResultError("taskId must be a valid integer"), nil
		}

		opts := db.TaskUpdateOpts{}

		if v, ok := req.Params.Arguments["status"].(string); ok && v != "" {
			opts.Status = &v
		}
		if v, ok := req.Params.Arguments["subject"].(string); ok && v != "" {
			opts.Subject = &v
		}
		if v, ok := req.Params.Arguments["description"].(string); ok && v != "" {
			opts.Description = &v
		}
		if v, ok := req.Params.Arguments["activeForm"].(string); ok && v != "" {
			opts.ActiveForm = &v
		}
		if v, ok := req.Params.Arguments["owner"].(string); ok && v != "" {
			opts.Owner = &v
		}
		if v, ok := req.Params.Arguments["addBlocks"].([]any); ok {
			for _, item := range v {
				if s, ok := item.(string); ok {
					if n, err := strconv.Atoi(s); err == nil {
						opts.AddBlocks = append(opts.AddBlocks, n)
					}
				}
			}
		}
		if v, ok := req.Params.Arguments["addBlockedBy"].([]any); ok {
			for _, item := range v {
				if s, ok := item.(string); ok {
					if n, err := strconv.Atoi(s); err == nil {
						opts.AddBlockedBy = append(opts.AddBlockedBy, n)
					}
				}
			}
		}
		if v, ok := req.Params.Arguments["metadata"].(map[string]any); ok {
			opts.Metadata = v
		}

		t, err := store.UpdateTask(id, opts)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to update task: %v", err)), nil
		}

		out, _ := json.Marshal(t)
		return mcp.NewToolResultText(string(out)), nil
	})
}

func registerList(srv *server.MCPServer, store *db.SessionDB) {
	tool := mcp.NewTool("galacta_task_list",
		mcp.WithDescription("List all tasks in the current session. Returns a summary of each task."),
	)

	srv.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		tasks, err := store.ListTasks()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to list tasks: %v", err)), nil
		}

		// Return summary format
		type taskSummary struct {
			ID        int    `json:"id"`
			Subject   string `json:"subject"`
			Status    string `json:"status"`
			Owner     string `json:"owner,omitempty"`
			BlockedBy []int  `json:"blockedBy,omitempty"`
		}

		summaries := make([]taskSummary, len(tasks))
		for i, t := range tasks {
			summaries[i] = taskSummary{
				ID:        t.ID,
				Subject:   t.Subject,
				Status:    t.Status,
				Owner:     t.Owner,
				BlockedBy: t.BlockedBy,
			}
		}

		out, _ := json.Marshal(summaries)
		return mcp.NewToolResultText(string(out)), nil
	})
}
