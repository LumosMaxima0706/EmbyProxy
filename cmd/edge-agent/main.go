package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"embyproxy/internal/mediaproxy"
	"embyproxy/internal/proxyadapter"
	"embyproxy/internal/storage"
)

type config struct {
	ListenAddr        string `json:"listen_addr"`
	DBPath            string `json:"db_path"`
	Controller        string `json:"controller"`
	NodeID            string `json:"node_id"`
	Credential        string `json:"credential"`
	Version           string `json:"version"`
	Commit            string `json:"commit"`
	CanaryPath        string `json:"canary_path"`
	AllowPrivate      bool   `json:"allow_private_targets"`
	IsolatedTestMedia bool   `json:"isolated_test_media"`
}

type snapshot struct {
	NodeID string              `json:"node_id"`
	Nodes  []storage.ProxyNode `json:"nodes"`
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
		if body.NodeID != cfg.NodeID {
			return fmt.Errorf("snapshot node mismatch")
		}
		if err = store.ReplaceProxyNodeSnapshot(ctx, body.Nodes); err != nil {
			return err
		}
		seen := make(map[string]struct{}, len(body.Routes))
		for _, entry := range body.Routes {
			if err = store.SaveManagedRoute(ctx, entry.Route, entry.Lines); err != nil {
				return err
			}
			seen[entry.Route.Slug] = struct{}{}
		}
		current, err := store.ListManagedRoutes(ctx)
		if err != nil {
			return err
		}
		for _, route := range current {
			if _, ok := seen[route.Slug]; !ok {
				if err = store.DeleteManagedRoute(ctx, route.Slug); err != nil {
					return err
				}
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
			res, err := client.Do(req)
			if err == nil && res != nil {
				_ = res.Body.Close()
			}
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
	if cfg.IsolatedTestMedia {
		mux.HandleFunc("/__isolated-media/", isolatedTestMedia)
	}
	mux.Handle("/", router)
	panic(http.ListenAndServe(cfg.ListenAddr, mux))
}

func isolatedTestMedia(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	body := []byte(strings.Repeat("embyproxy-isolated-media-", 4096))
	start, end := 0, len(body)-1
	partial := false
	if raw := r.Header.Get("Range"); strings.HasPrefix(raw, "bytes=") {
		if _, err := fmt.Sscanf(strings.TrimPrefix(raw, "bytes="), "%d-%d", &start, &end); err != nil || start < 0 || end < start || start >= len(body) {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", len(body)))
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		if end >= len(body) {
			end = len(body) - 1
		}
		partial = true
	}
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Length", strconv.Itoa(end-start+1))
	if partial {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(body)))
		w.WriteHeader(http.StatusPartialContent)
	}
	if r.Method != http.MethodHead {
		_, _ = w.Write(body[start : end+1])
	}
}
