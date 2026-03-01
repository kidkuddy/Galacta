package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/kidkuddy/galacta"
)

func main() {
	cfg := galacta.LoadConfig()

	flag.IntVar(&cfg.Port, "port", cfg.Port, "HTTP port (env: GALACTA_PORT)")
	flag.StringVar(&cfg.DataDir, "data-dir", cfg.DataDir, "Data directory (env: GALACTA_DATA_DIR)")
	flag.StringVar(&cfg.MCPConfigPath, "mcp-config", cfg.MCPConfigPath, "MCP servers config JSON (env: GALACTA_MCP_CONFIG)")
	flag.Parse()

	if cfg.APIKey == "" {
		fmt.Fprintln(os.Stderr, "error: no API key found (set ANTHROPIC_API_KEY or log in with Claude Code)")
		os.Exit(1)
	}

	g, err := galacta.New(cfg)
	if err != nil {
		log.Fatalf("failed to initialize galacta: %v", err)
	}

	// Graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("galacta: shutting down...")
		g.Shutdown()
		os.Exit(0)
	}()

	if err := g.Start(); err != nil {
		log.Fatalf("galacta: server error: %v", err)
	}
}
