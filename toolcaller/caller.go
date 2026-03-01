package toolcaller

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/kidkuddy/galacta/anthropic"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

// ToolCall represents a single tool invocation request.
type ToolCall struct {
	ID    string          // tool_use ID from Claude
	Name  string          // tool name
	Input json.RawMessage // tool input
}

// ToolCallResult represents the result of a tool invocation.
type ToolCallResult struct {
	ID         string // matches ToolCall.ID
	Name       string
	Output     string
	IsError    bool
	DurationMs int64
}

// Caller dispatches tool calls to the correct MCP client via the Registry.
type Caller struct {
	registry   *Registry
	maxWorkers int
}

func NewCaller(registry *Registry, maxWorkers int) *Caller {
	if maxWorkers < 1 {
		maxWorkers = 1
	}
	return &Caller{
		registry:   registry,
		maxWorkers: maxWorkers,
	}
}

// Call dispatches a single tool call.
func (c *Caller) Call(ctx context.Context, tc ToolCall) ToolCallResult {
	start := time.Now()

	ref, ok := c.registry.Get(tc.Name)
	if !ok {
		return ToolCallResult{
			ID:         tc.ID,
			Name:       tc.Name,
			Output:     fmt.Sprintf("tool %q not found", tc.Name),
			IsError:    true,
			DurationMs: time.Since(start).Milliseconds(),
		}
	}

	var args map[string]any
	if len(tc.Input) > 0 {
		if err := json.Unmarshal(tc.Input, &args); err != nil {
			return ToolCallResult{
				ID:         tc.ID,
				Name:       tc.Name,
				Output:     fmt.Sprintf("failed to unmarshal tool input: %v", err),
				IsError:    true,
				DurationMs: time.Since(start).Milliseconds(),
			}
		}
	}

	req := mcp.CallToolRequest{}
	req.Params.Name = tc.Name
	req.Params.Arguments = args

	result, err := ref.Client.CallTool(ctx, req)
	if err != nil {
		return ToolCallResult{
			ID:         tc.ID,
			Name:       tc.Name,
			Output:     fmt.Sprintf("tool call failed: %v", err),
			IsError:    true,
			DurationMs: time.Since(start).Milliseconds(),
		}
	}

	output := extractTextContent(result)

	return ToolCallResult{
		ID:         tc.ID,
		Name:       tc.Name,
		Output:     output,
		IsError:    result.IsError,
		DurationMs: time.Since(start).Milliseconds(),
	}
}

// CallMany dispatches multiple tool calls concurrently, bounded by maxWorkers.
func (c *Caller) CallMany(ctx context.Context, calls []ToolCall) []ToolCallResult {
	results := make([]ToolCallResult, len(calls))
	sem := make(chan struct{}, c.maxWorkers)
	var wg sync.WaitGroup

	for i, tc := range calls {
		wg.Add(1)
		go func(idx int, call ToolCall) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[idx] = c.Call(ctx, call)
		}(i, tc)
	}

	wg.Wait()
	return results
}

// AddClient discovers tools from the given MCP client and adds them to the registry.
func (c *Caller) AddClient(ctx context.Context, mc client.MCPClient) error {
	return c.registry.Discover(ctx, mc)
}

// ListTools returns all tools in Anthropic API format.
func (c *Caller) ListTools() []anthropic.Tool {
	return c.registry.ToAnthropicTools()
}

// ListToolRefs returns all raw ToolRef entries.
func (c *Caller) ListToolRefs() []NamedToolRef {
	return c.registry.ListNamed()
}

// NamedToolRef is a ToolRef with its registered name.
type NamedToolRef struct {
	Name string
	ToolRef
}

// extractTextContent concatenates all text content from a CallToolResult.
func extractTextContent(result *mcp.CallToolResult) string {
	if result == nil {
		return ""
	}
	var output string
	for _, c := range result.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			output += tc.Text
		} else if tc, ok := mcp.AsTextContent(c); ok {
			output += tc.Text
		} else {
			// Fall back to JSON marshaling for non-text content
			data, err := json.Marshal(c)
			if err == nil {
				output += string(data)
			}
		}
	}
	return output
}
