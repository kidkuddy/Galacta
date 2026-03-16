package mcpmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

// MCPConfig is the top-level config file format.
type MCPConfig struct {
	MCPServers map[string]MCPServerEntry `json:"mcpServers"`
}

// MCPServerEntry describes a single MCP server to connect to.
type MCPServerEntry struct {
	// For stdio servers
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`

	// For SSE and streamable-http servers
	Type string `json:"type,omitempty"` // "sse" or "streamable-http"
	URL  string `json:"url,omitempty"`
}

// ConnectedServer holds a connected MCP server client and its metadata.
type ConnectedServer struct {
	Name         string
	Client       client.MCPClient
	Instructions string // from InitializeResult, may be empty
	Transport    string // "stdio", "sse", "streamable-http"
}

// Manager manages connections to external MCP servers.
type Manager struct {
	mu      sync.Mutex
	servers map[string]*ConnectedServer
}

// New creates a new Manager.
func New() *Manager {
	return &Manager{
		servers: make(map[string]*ConnectedServer),
	}
}

// ConnectFromConfig reads a JSON config file and connects to all servers.
// Returns the list of successfully connected servers.
func (m *Manager) ConnectFromConfig(ctx context.Context, configPath string) ([]*ConnectedServer, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("reading MCP config: %w", err)
	}

	var config MCPConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parsing MCP config: %w", err)
	}

	return m.ConnectFromEntries(ctx, config.MCPServers)
}

// ConnectFromEntries connects to servers from a map of entries.
// Returns the list of successfully connected servers.
func (m *Manager) ConnectFromEntries(ctx context.Context, entries map[string]MCPServerEntry) ([]*ConnectedServer, error) {
	var connected []*ConnectedServer
	for name, entry := range entries {
		srv, err := m.Connect(ctx, name, entry)
		if err != nil {
			log.Printf("mcpmanager: failed to connect to %q: %v", name, err)
			continue
		}
		connected = append(connected, srv)
	}
	return connected, nil
}

// Connect connects to a single MCP server by name and entry.
func (m *Manager) Connect(ctx context.Context, name string, entry MCPServerEntry) (*ConnectedServer, error) {
	var (
		mc        client.MCPClient
		transport string
		err       error
	)

	switch {
	case entry.Command != "":
		transport = "stdio"
		env := mergeEnv(entry.Env)
		mc, err = client.NewStdioMCPClient(entry.Command, env, entry.Args...)
		if err != nil {
			return nil, fmt.Errorf("creating stdio client: %w", err)
		}

	case entry.Type == "sse":
		transport = "sse"
		sseClient, sseErr := client.NewSSEMCPClient(entry.URL)
		if sseErr != nil {
			return nil, fmt.Errorf("creating SSE client: %w", sseErr)
		}
		if err := sseClient.Start(ctx); err != nil {
			return nil, fmt.Errorf("starting SSE client: %w", err)
		}
		mc = sseClient

	case entry.Type == "streamable-http":
		transport = "streamable-http"
		httpClient, httpErr := client.NewStreamableHttpClient(entry.URL)
		if httpErr != nil {
			return nil, fmt.Errorf("creating streamable HTTP client: %w", httpErr)
		}
		if err := httpClient.Start(ctx); err != nil {
			return nil, fmt.Errorf("starting streamable HTTP client: %w", err)
		}
		mc = httpClient

	default:
		return nil, fmt.Errorf("unsupported server config (type=%q, command=%q)", entry.Type, entry.Command)
	}

	// Initialize the client.
	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{Name: "galacta", Version: "0.1.0"}

	result, err := mc.Initialize(ctx, initReq)
	if err != nil {
		mc.Close()
		return nil, fmt.Errorf("initializing MCP server %q: %w", name, err)
	}

	srv := &ConnectedServer{
		Name:      name,
		Client:    mc,
		Transport: transport,
	}
	if result != nil {
		srv.Instructions = result.Instructions
	}

	m.mu.Lock()
	// Close any existing server with the same name.
	if old, ok := m.servers[name]; ok {
		old.Client.Close()
	}
	m.servers[name] = srv
	m.mu.Unlock()

	log.Printf("mcpmanager: connected to %q via %s", name, transport)
	return srv, nil
}

// Disconnect closes a specific server by name.
func (m *Manager) Disconnect(name string) error {
	m.mu.Lock()
	srv, ok := m.servers[name]
	if ok {
		delete(m.servers, name)
	}
	m.mu.Unlock()

	if !ok {
		return fmt.Errorf("server %q not found", name)
	}

	return srv.Client.Close()
}

// DisconnectAll closes all connected servers.
func (m *Manager) DisconnectAll() {
	m.mu.Lock()
	servers := m.servers
	m.servers = make(map[string]*ConnectedServer)
	m.mu.Unlock()

	for name, srv := range servers {
		if err := srv.Client.Close(); err != nil {
			log.Printf("mcpmanager: error closing %q: %v", name, err)
		}
	}
}

// List returns all connected servers.
func (m *Manager) List() []*ConnectedServer {
	m.mu.Lock()
	defer m.mu.Unlock()

	list := make([]*ConnectedServer, 0, len(m.servers))
	for _, srv := range m.servers {
		list = append(list, srv)
	}
	return list
}

// Instructions returns all server instructions concatenated for prompt injection.
func (m *Manager) Instructions() string {
	m.mu.Lock()
	defer m.mu.Unlock()

	var sections []string
	for _, srv := range m.servers {
		if srv.Instructions == "" {
			continue
		}
		sections = append(sections, fmt.Sprintf("## %s\n%s", srv.Name, srv.Instructions))
	}

	if len(sections) == 0 {
		return ""
	}

	return "# MCP Server Instructions\n\nThe following MCP servers have provided usage instructions for their tools:\n\n" +
		strings.Join(sections, "\n\n")
}

// mergeEnv merges os.Environ() with the given overrides, returning a slice
// in "KEY=VALUE" format. Override values take precedence.
func mergeEnv(overrides map[string]string) []string {
	if len(overrides) == 0 {
		return os.Environ()
	}

	env := make(map[string]string)
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	for k, v := range overrides {
		env[k] = v
	}

	result := make([]string, 0, len(env))
	for k, v := range env {
		result = append(result, k+"="+v)
	}
	return result
}
