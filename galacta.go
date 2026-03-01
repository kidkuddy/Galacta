package galacta

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/kidkuddy/galacta/anthropic"
	"github.com/kidkuddy/galacta/api"
	"github.com/kidkuddy/galacta/toolcaller"
	"github.com/mark3labs/mcp-go/client"
)

// Galacta is the top-level daemon instance.
type Galacta struct {
	cfg        *Config
	apiClient  *anthropic.Client
	caller     *toolcaller.Caller
	server     *api.Server
	extClients []client.MCPClient
}

// New creates and wires a Galacta instance.
func New(cfg *Config) (*Galacta, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("ANTHROPIC_API_KEY is required")
	}

	// Ensure data directory exists
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating data dir: %w", err)
	}

	apiClient := anthropic.NewClient(cfg.APIKey)

	// Global registry for external MCP servers only
	// Built-in tools (fs, exec, web) are created per-session in the handler
	registry := toolcaller.NewRegistry()
	caller := toolcaller.NewCaller(registry, cfg.MaxConcurrency)

	var extClients []client.MCPClient

	// Connect external MCP servers from config
	if cfg.MCPConfigPath != "" {
		clients, err := connectExternalMCP(cfg.MCPConfigPath)
		if err != nil {
			log.Printf("galacta: warning: failed to load MCP config: %v", err)
		} else {
			ctx := context.Background()
			for _, mc := range clients {
				if err := caller.AddClient(ctx, mc); err != nil {
					log.Printf("galacta: warning: failed to discover tools from MCP server: %v", err)
				}
				extClients = append(extClients, mc)
			}
		}
	}

	handler := api.NewHandler(cfg.DataDir, apiClient, caller, cfg.DefaultModel, cfg.MaxConcurrency)
	server := api.NewServer(handler, cfg.Port)

	return &Galacta{
		cfg:        cfg,
		apiClient:  apiClient,
		caller:     caller,
		server:     server,
		extClients: extClients,
	}, nil
}

// Start starts the HTTP server. Blocks until the server exits.
func (g *Galacta) Start() error {
	fmt.Println("READY")
	log.Printf("galacta: listening on :%d (data: %s)", g.cfg.Port, g.cfg.DataDir)
	return g.server.ListenAndServe()
}

// Shutdown gracefully shuts down the Galacta daemon.
func (g *Galacta) Shutdown() {
	for _, mc := range g.extClients {
		mc.Close()
	}
}

// mcpConfigFile is the JSON format for external MCP server configuration.
type mcpConfigFile struct {
	MCPServers map[string]mcpServerEntry `json:"mcpServers"`
}

type mcpServerEntry struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

func connectExternalMCP(configPath string) ([]client.MCPClient, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("reading MCP config: %w", err)
	}

	var config mcpConfigFile
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parsing MCP config: %w", err)
	}

	var clients []client.MCPClient
	for name, entry := range config.MCPServers {
		if entry.Type != "sse" {
			log.Printf("galacta: skipping MCP server %q: unsupported type %q (only 'sse' supported)", name, entry.Type)
			continue
		}

		mc, err := client.NewSSEMCPClient(entry.URL)
		if err != nil {
			log.Printf("galacta: failed to connect to MCP server %q at %s: %v", name, entry.URL, err)
			continue
		}

		log.Printf("galacta: connected to MCP server %q at %s", name, entry.URL)
		clients = append(clients, mc)
	}

	return clients, nil
}
