package galacta

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/kidkuddy/galacta/anthropic"
	"github.com/kidkuddy/galacta/api"
	"github.com/kidkuddy/galacta/mcpmanager"
	"github.com/kidkuddy/galacta/toolcaller"
)

// Galacta is the top-level daemon instance.
type Galacta struct {
	cfg       *Config
	apiClient *anthropic.Client
	caller    *toolcaller.Caller
	server    *api.Server
	mcpMgr    *mcpmanager.Manager
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

	apiClient := anthropic.NewClient(cfg.KeyFunc())

	// Global registry for external MCP servers only
	// Built-in tools (fs, exec, web) are created per-session in the handler
	registry := toolcaller.NewRegistry()
	caller := toolcaller.NewCaller(registry, cfg.MaxConcurrency)

	var mcpMgr *mcpmanager.Manager

	// Connect external MCP servers from config
	if cfg.MCPConfigPath != "" {
		mcpMgr = mcpmanager.New()
		ctx := context.Background()
		connected, err := mcpMgr.ConnectFromConfig(ctx, cfg.MCPConfigPath)
		if err != nil {
			log.Printf("galacta: warning: failed to load MCP config: %v", err)
		}
		for _, srv := range connected {
			if err := caller.AddClient(ctx, srv.Client); err != nil {
				log.Printf("galacta: warning: failed to discover tools from MCP server %q: %v", srv.Name, err)
			}
		}
	}

	handler := api.NewHandler(cfg.DataDir, apiClient, caller, cfg.DefaultModel, cfg.MaxConcurrency)
	server := api.NewServer(handler, cfg.Port)

	return &Galacta{
		cfg:       cfg,
		apiClient: apiClient,
		caller:    caller,
		server:    server,
		mcpMgr:    mcpMgr,
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
	if g.mcpMgr != nil {
		g.mcpMgr.DisconnectAll()
	}
}
