package toolcaller

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync"

	"github.com/kidkuddy/galacta/anthropic"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

// ToolRef maps a tool name to the MCP client that owns it and its schema.
type ToolRef struct {
	Client client.MCPClient
	Tool   mcp.Tool
}

// Registry holds discovered tools from all connected MCP servers.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]ToolRef
}

func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]ToolRef),
	}
}

// Discover calls ListTools on the given MCP client and registers all tools.
func (r *Registry) Discover(ctx context.Context, c client.MCPClient) error {
	result, err := c.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for _, t := range result.Tools {
		r.tools[t.Name] = ToolRef{
			Client: c,
			Tool:   t,
		}
	}
	return nil
}

// Get returns the ToolRef for a given tool name.
func (r *Registry) Get(name string) (ToolRef, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ref, ok := r.tools[name]
	return ref, ok
}

// ListAll returns all registered tools.
func (r *Registry) ListAll() []ToolRef {
	r.mu.RLock()
	defer r.mu.RUnlock()
	refs := make([]ToolRef, 0, len(r.tools))
	for _, ref := range r.tools {
		refs = append(refs, ref)
	}
	return refs
}

// ListNamed returns all registered tools with their names.
func (r *Registry) ListNamed() []NamedToolRef {
	r.mu.RLock()
	defer r.mu.RUnlock()
	refs := make([]NamedToolRef, 0, len(r.tools))
	for name, ref := range r.tools {
		refs = append(refs, NamedToolRef{Name: name, ToolRef: ref})
	}
	return refs
}

// Add registers a tool directly.
func (r *Registry) Add(name string, ref ToolRef) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[name] = ref
}

// ToolFilter controls which tools are visible to the model.
type ToolFilter struct {
	Allow []string // explicit allow list (if non-empty, only these tools are available)
	Deny  []string // explicit deny list
	Globs []string // glob patterns for allowed tools
}

// Matches returns true if the given tool name passes the filter.
func (f *ToolFilter) Matches(name string) bool {
	if f == nil {
		return true
	}

	// Check deny list first
	for _, d := range f.Deny {
		if d == name {
			return false
		}
	}

	// If allow list is set, tool must be in it
	if len(f.Allow) > 0 {
		for _, a := range f.Allow {
			if a == name {
				return true
			}
		}
		// Not in allow list, check globs
		if len(f.Globs) == 0 {
			return false
		}
	}

	// Check glob patterns
	if len(f.Globs) > 0 {
		for _, pattern := range f.Globs {
			if matched, _ := filepath.Match(pattern, name); matched {
				return true
			}
		}
		// If we have globs but no allow list, globs are the allow list
		if len(f.Allow) == 0 {
			return false
		}
	}

	return true
}

// FilteredTools returns Anthropic tools filtered by the given filter.
func (r *Registry) FilteredTools(filter *ToolFilter) []anthropic.Tool {
	if filter == nil {
		return r.ToAnthropicTools()
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	var tools []anthropic.Tool
	for _, ref := range r.tools {
		if !filter.Matches(ref.Tool.Name) {
			continue
		}
		schema, err := json.Marshal(ref.Tool.InputSchema)
		if err != nil {
			if ref.Tool.RawInputSchema != nil {
				schema = ref.Tool.RawInputSchema
			} else {
				continue
			}
		}
		if ref.Tool.RawInputSchema != nil {
			schema = ref.Tool.RawInputSchema
		}
		tools = append(tools, anthropic.Tool{
			Name:        ref.Tool.Name,
			Description: ref.Tool.Description,
			InputSchema: json.RawMessage(schema),
		})
	}
	return tools
}

// ToAnthropicTools converts all registered tools to the format expected by the
// Anthropic API.
func (r *Registry) ToAnthropicTools() []anthropic.Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tools := make([]anthropic.Tool, 0, len(r.tools))
	for _, ref := range r.tools {
		schema, err := json.Marshal(ref.Tool.InputSchema)
		if err != nil {
			// If RawInputSchema is set, use that instead
			if ref.Tool.RawInputSchema != nil {
				schema = ref.Tool.RawInputSchema
			} else {
				continue
			}
		}
		// Prefer RawInputSchema when available
		if ref.Tool.RawInputSchema != nil {
			schema = ref.Tool.RawInputSchema
		}
		tools = append(tools, anthropic.Tool{
			Name:        ref.Tool.Name,
			Description: ref.Tool.Description,
			InputSchema: json.RawMessage(schema),
		})
	}
	return tools
}
