package worktree

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/kidkuddy/galacta/db"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Deps holds dependencies for the worktree tool.
type Deps struct {
	WorkingDir string
	Store      *db.SessionDB
}

// NewServer creates an MCP server with the galacta_enter_worktree tool.
func NewServer(deps *Deps) *server.MCPServer {
	srv := server.NewMCPServer(
		"galacta-worktree",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	tool := mcp.NewTool("galacta_enter_worktree",
		mcp.WithDescription("Create a git worktree for isolated work. Creates a new branch and working directory."),
		mcp.WithString("name", mcp.Description("Optional name for the worktree (defaults to a short UUID)")),
	)

	srv.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name, _ := req.Params.Arguments["name"].(string)
		if name == "" {
			name = uuid.New().String()[:8]
		}

		wtPath := filepath.Join(deps.WorkingDir, ".galacta", "worktrees", name)
		branch := "galacta/worktree-" + name

		cmd := exec.CommandContext(ctx, "git", "worktree", "add", wtPath, "-b", branch)
		cmd.Dir = deps.WorkingDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("git worktree add failed: %v\n%s", err, strings.TrimSpace(string(out)))), nil
		}

		if deps.Store != nil {
			deps.Store.SetMeta("working_dir", wtPath)
		}

		return mcp.NewToolResultText(fmt.Sprintf("Worktree created:\n  path: %s\n  branch: %s", wtPath, branch)), nil
	})

	return srv
}
