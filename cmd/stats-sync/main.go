package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"embyproxy/internal/statslog"
)

type meterConfig struct {
	NOSLAMeter struct {
		User           string `json:"user"`
		Host           string `json:"host"`
		IdentityFile   string `json:"identity_file"`
		KnownHostsFile string `json:"known_hosts_file"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	} `json:"nosla_meter"`
}

type meterResponse struct {
	Snapshot json.RawMessage `json:"stats_snapshot"`
}

type snapshot struct {
	SourceNode string                 `json:"source_node"`
	Rows       []statslog.SnapshotRow `json:"rows"`
}

func main() {
	configPath := flag.String("config", "/etc/embyproxy-failover-policy/config.json", "root-only policy config")
	dbPath := flag.String("db", "/var/lib/embyproxy-gsy-sidecar/global-stats.db", "central stats database")
	dryRun := flag.Bool("dry-run", false, "fetch and validate without DB writes")
	flag.Parse()
	configRaw, err := os.ReadFile(*configPath)
	if err != nil {
		fatal("config read failed")
	}
	var cfg meterConfig
	if json.Unmarshal(configRaw, &cfg) != nil {
		fatal("config parse failed")
	}
	if cfg.NOSLAMeter.User == "" || cfg.NOSLAMeter.Host == "" || cfg.NOSLAMeter.IdentityFile == "" || cfg.NOSLAMeter.KnownHostsFile == "" {
		fatal("meter config incomplete")
	}
	timeout := cfg.NOSLAMeter.TimeoutSeconds
	if timeout < 1 || timeout > 60 {
		timeout = 10
	}
	args := []string{
		"-F", "/dev/null", "-i", cfg.NOSLAMeter.IdentityFile,
		"-o", "UserKnownHostsFile=" + cfg.NOSLAMeter.KnownHostsFile,
		"-o", "StrictHostKeyChecking=yes", "-o", "BatchMode=yes", "-o", "IdentitiesOnly=yes",
		"-o", fmt.Sprintf("ConnectTimeout=%d", timeout), "-T",
		cfg.NOSLAMeter.User + "@" + cfg.NOSLAMeter.Host,
	}
	command := exec.Command("ssh", args...)
	output, err := command.Output()
	if err != nil {
		fatal("restricted meter failed")
	}
	var response meterResponse
	if json.Unmarshal(output, &response) != nil || len(response.Snapshot) == 0 {
		fatal("stats snapshot unavailable")
	}
	var data snapshot
	if json.Unmarshal(response.Snapshot, &data) != nil || data.SourceNode != "nosla" {
		fatal("stats snapshot invalid")
	}
	if *dryRun {
		fmt.Printf("source_node=nosla rows=%d dry_run=true\n", len(data.Rows))
		return
	}
	store, err := statslog.Open(*dbPath)
	if err != nil {
		fatal("central stats open failed")
	}
	defer store.Close()
	if err := store.ImportSnapshot(context.Background(), data.Rows); err != nil {
		fatal("central stats import failed")
	}
	fmt.Printf("source_node=nosla rows=%d dry_run=false\n", len(data.Rows))
}

func fatal(message string) {
	// Deliberately omit command output and paths that could contain credentials.
	fmt.Fprintln(os.Stderr, strings.TrimSpace(message))
	os.Exit(1)
}
