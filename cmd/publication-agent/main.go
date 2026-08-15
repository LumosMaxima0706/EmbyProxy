package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"embyproxy/internal/publicationagent"
)

func main() {
	mode := flag.String("mode", "agent", "agent or edge")
	configPath := flag.String("config", "/etc/embyproxy-publication-agent/config.json", "root-only configuration")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	var err error
	switch *mode {
	case "agent":
		var cfg publicationagent.AgentConfig
		cfg, err = publicationagent.LoadAgentConfig(*configPath)
		if err == nil {
			err = publicationagent.NewAgent(cfg).Run(ctx)
		}
	case "edge":
		var cfg publicationagent.EdgeConfig
		cfg, err = publicationagent.LoadEdgeConfig(*configPath)
		if err == nil {
			err = publicationagent.RunEdge(ctx, cfg, os.Stdin, os.Stdout)
		}
	default:
		err = fmt.Errorf("invalid mode")
	}
	if err != nil {
		_, _ = io.WriteString(os.Stderr, "publication agent failed\n")
		os.Exit(1)
	}
}
