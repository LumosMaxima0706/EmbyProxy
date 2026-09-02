package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"embyproxy/internal/mediaproxy"
	"embyproxy/internal/proxyadapter"
	"embyproxy/internal/storage"
)

type config struct {
	ListenAddr   string `json:"listen_addr"`
	DBPath       string `json:"db_path"`
	Controller   string `json:"controller"`
	NodeID       string `json:"node_id"`
	Credential   string `json:"credential"`
	Version      string `json:"version"`
	Commit       string `json:"commit"`
	CanaryPath   string `json:"canary_path"`
	AllowPrivate bool   `json:"allow_private_targets"`
}

type snapshot struct {
	Routes []struct {
		Route storage.ManagedRoute       `json:"route"`
		Lines []storage.ManagedRouteLine `json:"lines"`
	} `json:"routes"`
}

func main() {
	path := flag.String("config", "", "root-only edge configuration")
	flag.Parse()
	if *path == "" {
		panic("config required")
	}
	raw, err := os.ReadFile(*path)
	if err != nil {
		panic(err)
	}
	var cfg config
	if err = json.Unmarshal(raw, &cfg); err != nil || cfg.ListenAddr == "" || cfg.DBPath == "" || cfg.Controller == "" || cfg.NodeID == "" || cfg.Credential == "" {
		panic("invalid edge config")
	}
	store, err := storage.New(cfg.DBPath)
	if err != nil {
		panic(err)
	}
	defer store.Close()
	client := &http.Client{Timeout: 15 * time.Second}
	sync := func(ctx context.Context) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(cfg.Controller, "/")+"/api/edge/config/"+cfg.NodeID, nil)
		if err != nil {
			return err
		}
		req.Header.Set("X-EmbyProxy-Node-Credential", cfg.Credential)
		res, err := client.Do(req)
		if err != nil {
			return err
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			return fmt.Errorf("snapshot status %d", res.StatusCode)
		}
		var body snapshot
		if err = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&body); err != nil {
			return err
		}
		for _, entry := range body.Routes {
			if err = store.SaveManagedRoute(ctx, entry.Route, entry.Lines); err != nil {
				return err
			}
		}
		return nil
	}
	health := func(ctx context.Context) bool {
		if cfg.CanaryPath == "" {
			return false
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+cfg.ListenAddr+cfg.CanaryPath, nil)
		if err != nil {
			return false
		}
		req.Header.Set("Range", "bytes=0-1023")
		res, err := client.Do(req)
		if err != nil {
			return false
		}
		defer res.Body.Close()
		return res.StatusCode == http.StatusPartialContent && res.Header.Get("Content-Range") != ""
	}
	heartbeat := func(ctx context.Context, synced, playback bool, lastErr string) {
		body, _ := json.Marshal(map[string]any{"credential": cfg.Credential, "version": cfg.Version, "commit": cfg.Commit, "state": map[bool]string{true: "healthy", false: "degraded"}[synced && playback], "playbackHealthy": playback, "configSynced": synced, "lastError": lastErr})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(cfg.Controller, "/")+"/api/edge/heartbeat/"+cfg.NodeID, strings.NewReader(string(body)))
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
			_, _ = client.Do(req)
		}
	}
	go func() {
		for {
			err := sync(context.Background())
			ok := err == nil
			message := ""
			if err != nil {
				message = "config_sync_failed"
			}
			playback := ok && health(context.Background())
			heartbeat(context.Background(), ok, playback, message)
			time.Sleep(30 * time.Second)
		}
	}()
	router := proxyadapter.NewProductionRouter(proxyadapter.NewStorageResolver(store, "admin"), mediaproxy.NewExecutor(mediaproxy.Config{AllowPrivateTargets: cfg.AllowPrivate}), mediaproxy.Config{AllowPrivateTargets: cfg.AllowPrivate}, http.NotFoundHandler())
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	mux.Handle("/", router)
	panic(http.ListenAndServe(cfg.ListenAddr, mux))
}
