package exectools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// NewServer creates an MCP server with command execution tools.
func NewServer(workingDir string) *server.MCPServer {
	srv := server.NewMCPServer(
		"galacta-exec",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	registerBash(srv, workingDir)

	return srv
}

func registerBash(srv *server.MCPServer, workingDir string) {
	tool := mcp.NewTool("galacta_bash",
		mcp.WithDescription("Execute a bash command"),
		mcp.WithString("command", mcp.Required(), mcp.Description("Bash command to execute")),
		mcp.WithNumber("timeout", mcp.Description("Timeout in seconds (default 120, max 600)")),
		mcp.WithString("description", mcp.Description("Human-readable description of what the command does")),
	)
	srv.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		command := mcp.ParseString(req, "command", "")
		timeoutSec := mcp.ParseInt(req, "timeout", 120)

		if command == "" {
			return mcp.NewToolResultError("command is required"), nil
		}

		if timeoutSec > 600 {
			timeoutSec = 600
		}
		if timeoutSec < 1 {
			timeoutSec = 1
		}

		timeout := time.Duration(timeoutSec) * time.Second
		cmdCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		cmd := exec.CommandContext(cmdCtx, "/bin/bash", "-c", command)
		cmd.Dir = workingDir

		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		err := cmd.Run()

		var sb strings.Builder

		if stdout.Len() > 0 {
			sb.WriteString(stdout.String())
		}

		if stderr.Len() > 0 {
			if sb.Len() > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString("STDERR:\n")
			sb.WriteString(stderr.String())
		}

		if err != nil {
			if cmdCtx.Err() == context.DeadlineExceeded {
				if sb.Len() > 0 {
					sb.WriteString("\n")
				}
				sb.WriteString(fmt.Sprintf("Command timed out after %d seconds", timeoutSec))
				return mcp.NewToolResultText(sb.String()), nil
			}

			if exitErr, ok := err.(*exec.ExitError); ok {
				if sb.Len() > 0 {
					sb.WriteString("\n")
				}
				sb.WriteString(fmt.Sprintf("Exit code: %d", exitErr.ExitCode()))
				return mcp.NewToolResultText(sb.String()), nil
			}

			return mcp.NewToolResultError(fmt.Sprintf("executing command: %s", err.Error())), nil
		}

		if sb.Len() == 0 {
			return mcp.NewToolResultText("(no output)"), nil
		}

		return mcp.NewToolResultText(sb.String()), nil
	})
}
