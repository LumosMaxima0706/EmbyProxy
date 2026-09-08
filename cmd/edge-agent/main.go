package main

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	projectconfig "embyproxy/internal/config"
	"embyproxy/internal/edgecleanup"
	"embyproxy/internal/edgecontrol"
	"embyproxy/internal/mediaproxy"
	"embyproxy/internal/proxyadapter"
	"embyproxy/internal/storage"
)

type config struct {
	ListenAddr              string `json:"listen_addr"`
	ProbeAddr               string `json:"probe_addr"`
	DBPath                  string `json:"db_path"`
	Controller              string `json:"controller"`
	NodeID                  string `json:"node_id"`
	Credential              string `json:"credential"`
	Version                 string `json:"version"`
	Commit                  string `json:"commit"`
	CanaryPath              string `json:"canary_path"`
	AllowPrivate            bool   `json:"allow_private_targets"`
	IsolatedTestMedia       bool   `json:"isolated_test_media"`
	DecommissionPublicKey   string `json:"decommission_public_key"`
	CaddyInstalledByProject bool   `json:"caddy_installed_by_project"`
	CaddyConfigOwned        bool   `json:"caddy_config_owned"`
	TLSStateOwned           bool   `json:"tls_state_owned"`
	EdgeUnitOwned           bool   `json:"edge_unit_owned"`
}

type snapshot struct {
	NodeID string              `json:"node_id"`
	Nodes  []storage.ProxyNode `json:"nodes"`
	Routes []struct {
		Route storage.ManagedRoute       `json:"route"`
		Lines []storage.ManagedRouteLine `json:"lines"`
	} `json:"routes"`
	RedirectEndpoints map[string][]storage.ProxyRedirectEndpoint `json:"redirect_endpoints,omitempty"`
}

func normalizePlaybackConfig(cfg *config) error {
	cfg.CanaryPath = strings.TrimSpace(cfg.CanaryPath)
	if cfg.IsolatedTestMedia && cfg.CanaryPath == "" {
		cfg.CanaryPath = projectconfig.DefaultEdgePlaybackCanaryPath
	}
	if cfg.CanaryPath == "" {
		return errors.New("invalid playback health configuration: no playback canary configured")
	}
	if !strings.HasPrefix(cfg.CanaryPath, "/") || strings.ContainsAny(cfg.CanaryPath, "\x00\r\n\t\"'?#") {
		return errors.New("invalid playback health configuration: canary path must be an absolute URI path")
	}
	return nil
}

func playbackHealth(ctx context.Context, client *http.Client, cfg config) bool {
	if cfg.CanaryPath == "" {
		return false
	}
	probeAddr := cfg.ProbeAddr
	if strings.TrimSpace(probeAddr) == "" {
		probeAddr = "127.0.0.1:18080"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+probeAddr+cfg.CanaryPath, nil)
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
	if err = normalizePlaybackConfig(&cfg); err != nil {
		panic(err)
	}
	store, err := storage.New(cfg.DBPath)
	if err != nil {
		panic(err)
	}
	defer store.Close()
	client := &http.Client{Timeout: 15 * time.Second}
	decommissionKey, _ := edgecontrol.DecodePublicKey(cfg.DecommissionPublicKey)
	nonceStore := &edgecontrol.PersistentNonceStore{Path: filepath.Join(filepath.Dir(*path), "decommission-nonces.json")}
	configPath := *path
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
		if err = store.ReplaceProxyRedirectEndpoints(ctx, body.RedirectEndpoints); err != nil {
			return err
		}
		return nil
	}
	// Healthy means the agent can sync routes and serve its configured local
	// playback canary. Public ingress/TLS reachability is a separate concern.
	heartbeat := func(ctx context.Context, synced, playback bool, lastErr string) {
		body, _ := json.Marshal(map[string]any{"credential": cfg.Credential, "version": cfg.Version, "commit": cfg.Commit, "decommissionCapable": len(decommissionKey) == ed25519.PublicKeySize, "state": map[bool]string{true: "healthy", false: "degraded"}[synced && playback], "playbackHealthy": playback, "configSynced": synced, "lastError": lastErr})
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
			playback := ok && playbackHealth(context.Background(), client, cfg)
			if ok && !playback {
				message = "playback_canary_failed"
			}
			heartbeat(context.Background(), ok, playback, message)
			time.Sleep(30 * time.Second)
		}
	}()
	if len(decommissionKey) == ed25519.PublicKeySize {
		go func() {
			for {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(cfg.Controller, "/")+"/api/edge/decommission/"+cfg.NodeID, nil)
				if reqErr == nil {
					req.Header.Set("X-EmbyProxy-Node-Credential", cfg.Credential)
					res, getErr := client.Do(req)
					if getErr == nil && res.StatusCode == http.StatusOK {
						var body struct {
							Job edgecontrol.Job `json:"job"`
						}
						if json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&body) == nil && nonceStore.Verify(body.Job, cfg.NodeID, decommissionKey, time.Now(), 20*time.Minute) == nil {
							payload, _ := json.Marshal(map[string]string{"job_id": body.Job.JobID, "completion_token": body.Job.CompletionToken})
							acceptReq, _ := http.NewRequest(http.MethodPost, strings.TrimRight(cfg.Controller, "/")+"/api/edge/decommission/"+cfg.NodeID+"/accept", strings.NewReader(string(payload)))
							acceptReq.Header.Set("Content-Type", "application/json")
							acceptReq.Header.Set("X-EmbyProxy-Cleanup-Token", body.Job.CompletionToken)
							accepted, acceptErr := client.Do(acceptReq)
							if accepted != nil {
								_ = accepted.Body.Close()
							}
							if acceptErr == nil && accepted.StatusCode >= 200 && accepted.StatusCode < 300 {
								script, scriptErr := edgecleanup.Script(cfg.NodeID, body.Job.JobID, cfg.Controller, body.Job.CompletionToken, edgecleanup.Ownership{CaddyInstalledByProject: cfg.CaddyInstalledByProject, CaddyConfigOwned: cfg.CaddyConfigOwned, TLSStateOwned: cfg.TLSStateOwned, EdgeUnitOwned: cfg.EdgeUnitOwned})
								if scriptErr == nil {
									helper := filepath.Join(filepath.Dir(configPath), "decommission-"+body.Job.JobID+".sh")
									if os.WriteFile(helper, []byte(script), 0700) == nil {
										// A separate transient unit avoids systemd killing the
										// completion helper with the edge service cgroup.
										unit := "embyproxy-edge-cleanup-" + strings.ReplaceAll(body.Job.JobID, "/", "-")
										_ = exec.Command("systemd-run", "--unit="+unit, "--collect", "/bin/sh", helper).Start()
									}
								}
							}
						}
					}
					if res != nil {
						_ = res.Body.Close()
					}
				}
				cancel()
				time.Sleep(15 * time.Second)
			}
		}()
	}
	router := proxyadapter.NewEdgeRouter(proxyadapter.NewStorageResolver(store, "admin"), mediaproxy.NewExecutor(mediaproxy.Config{AllowPrivateTargets: cfg.AllowPrivate}), mediaproxy.Config{AllowPrivateTargets: cfg.AllowPrivate}, http.NotFoundHandler())
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
