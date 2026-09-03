package admin

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"embyproxy/internal/buildinfo"
	"embyproxy/internal/capture"
	"embyproxy/internal/config"
	"embyproxy/internal/requestlog"
	"embyproxy/internal/storage"
)

func (h *Handler) handleProxyNodesAPI(w http.ResponseWriter, r *http.Request, path string) {
	ctx := r.Context()
	if r.Method == http.MethodGet && path == "/api/admin/proxy-nodes" {
		items, err := h.store.ListProxyNodes(ctx)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "NODE_LIST_FAILED"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "nodes": items})
		return
	}
	if r.Method == http.MethodPost && path == "/api/admin/proxy-nodes" {
		var body struct {
			Name          string `json:"name"`
			PublicAddress string `json:"public_address"`
			QuotaBytes    int64  `json:"quota_bytes"`
			ResetDay      int    `json:"reset_day"`
			ResetTimezone string `json:"reset_timezone"`
			Priority      int    `json:"priority"`
		}
		if !decodeAuthJSON(w, r, &body) {
			return
		}
		controllerURL, err := config.NormalizeEnrollmentControllerURL(h.cfg.EnrollmentControllerURL, false)
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "CONTROLLER_PUBLIC_URL_NOT_CONFIGURED"})
			return
		}
		enrollment, token, err := h.store.CreateProxyNode(ctx, storage.ProxyNode{Name: body.Name, PublicAddress: body.PublicAddress, QuotaBytes: body.QuotaBytes, ResetDay: body.ResetDay, ResetTimezone: body.ResetTimezone, Priority: body.Priority}, 15*time.Minute)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "INVALID_NODE"})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "enrollment": enrollment, "install_command": buildEnrollmentCommand(controllerURL, enrollment.ID, token)})
		return
	}
	if r.Method == http.MethodPost && path == "/api/admin/proxy-nodes/reorder" {
		var body struct {
			IDs []string `json:"ids"`
		}
		if !decodeAuthJSON(w, r, &body) {
			return
		}
		if len(body.IDs) == 0 || len(body.IDs) > 100 {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "INVALID_ORDER"})
			return
		}
		if err := h.store.ReorderProxyNodes(ctx, body.IDs); err != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "ORDER_FAILED"})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true})
		return
	}
	prefix := "/api/admin/proxy-nodes/"
	if strings.HasPrefix(path, prefix) {
		parts := strings.Split(strings.TrimPrefix(path, prefix), "/")
		id := parts[0]
		if id == "" {
			http.NotFound(w, r)
			return
		}
		if r.Method == http.MethodPost && len(parts) == 2 && parts[1] == "revoke" {
			force := r.URL.Query().Get("force") == "true"
			if err := h.store.RevokeProxyNode(ctx, id, force); err != nil {
				writeJSON(w, 404, map[string]any{"ok": false, "error": "NODE_NOT_FOUND"})
				return
			}
			writeJSON(w, 200, map[string]any{"ok": true, "state": map[bool]string{true: "revoked", false: "draining"}[force], "force": force})
			return
		}
		if r.Method == http.MethodPost && len(parts) == 2 && parts[1] == "bootstrap" {
			controllerURL, err := config.NormalizeEnrollmentControllerURL(h.cfg.EnrollmentControllerURL, false)
			if err != nil {
				writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "CONTROLLER_PUBLIC_URL_NOT_CONFIGURED"})
				return
			}
			enrollment, token, err := h.store.RegenerateProxyNodeEnrollment(ctx, id, 15*time.Minute)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "BOOTSTRAP_REGENERATION_FAILED"})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "enrollment": enrollment, "install_command": buildEnrollmentCommand(controllerURL, enrollment.ID, token)})
			return
		}
		if r.Method == http.MethodPatch && len(parts) == 1 {
			n, err := h.store.GetProxyNode(ctx, id)
			if err != nil || n == nil {
				writeJSON(w, 404, map[string]any{"ok": false, "error": "NODE_NOT_FOUND"})
				return
			}
			var body map[string]any
			if !decodeAuthJSON(w, r, &body) {
				return
			}
			if v, ok := body["enabled"].(bool); ok {
				n.Enabled = v
			}
			if v, ok := body["public_address"].(string); ok {
				n.PublicAddress = strings.TrimSpace(v)
			}
			if v, ok := body["priority"].(float64); ok {
				n.Priority = int(v)
			}
			if v, ok := body["quota_bytes"].(float64); ok {
				n.QuotaBytes = int64(v)
			}
			if v, ok := body["reset_day"].(float64); ok {
				n.ResetDay = int(v)
			}
			if v, ok := body["reset_timezone"].(string); ok {
				n.ResetTimezone = v
			}
			if v, ok := body["used_bytes"].(float64); ok && v >= 0 {
				n.UsedBytes = int64(v)
			}
			if v, ok := body["next_reset_at"].(float64); ok && v >= 0 {
				n.NextResetAt = int64(v)
			}
			if err := h.store.UpdateProxyNode(ctx, *n); err != nil {
				writeJSON(w, 400, map[string]any{"ok": false, "error": "INVALID_NODE"})
				return
			}
			writeJSON(w, 200, map[string]any{"ok": true, "node": n})
			return
		}
		if r.Method == http.MethodPost && len(parts) == 2 && parts[1] == "usage" {
			var body struct {
				UsedBytes int64 `json:"used_bytes"`
			}
			if !decodeAuthJSON(w, r, &body) {
				return
			}
			if err := h.store.RecordProxyNodeUsage(ctx, id, body.UsedBytes, time.Now()); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "USAGE_UPDATE_FAILED"})
				return
			}
			n, _ := h.store.GetProxyNode(ctx, id)
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "node": n})
			return
		}
	}
	http.NotFound(w, r)
}

func buildEnrollmentCommand(controllerURL, id, token string) string {
	return "curl --fail --silent --show-error --proto '=https' --tlsv1.2 " + shellQuote(controllerURL+"/api/edge/bootstrap/"+url.PathEscape(id)+"/"+url.PathEscape(token)) + " | sudo sh"
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

// handleBootstrap returns a guarded, self-contained installer. It intentionally
// contains no administrator credential; the one-time enrollment token is
// exchanged by the installer over TLS and is never persisted by the server.
func (h *Handler) handleBootstrap(w http.ResponseWriter, r *http.Request, enrollmentID, token string) {
	if r.Method != http.MethodGet || enrollmentID == "" || token == "" {
		http.NotFound(w, r)
		return
	}
	if err := h.store.ValidateEnrollment(r.Context(), enrollmentID, token); err != nil {
		// Treat expired, consumed and unknown enrollment records alike.
		http.NotFound(w, r)
		return
	}
	// Do not disclose enrollment state in the generated script. The script
	// performs environment checks and posts the token only to the edge API.
	controller, err := config.NormalizeEnrollmentControllerURL(h.cfg.EnrollmentControllerURL, h.cfg.AllowInsecureLoopbackEnrollment)
	if err != nil {
		http.Error(w, "controller public URL is not configured", http.StatusServiceUnavailable)
		return
	}
	curlProtocol := "=https"
	if strings.HasPrefix(controller, "http://") && h.cfg.AllowInsecureLoopbackEnrollment {
		curlProtocol = "=http,https"
	}
	payload := map[string]string{"version": buildinfo.Current().Version, "commit": buildinfo.Current().Commit}
	body, _ := json.Marshal(payload)
	script := fmt.Sprintf(`#!/bin/sh
set -eu
command -v curl >/dev/null 2>&1 || { echo 'curl is required' >&2; exit 1; }
command -v sha256sum >/dev/null 2>&1 || { echo 'sha256sum is required' >&2; exit 1; }
install_root=${EMBYPROXY_INSTALL_ROOT:-}
if [ -n "$install_root" ]; then
  case "$install_root" in /*) ;; *) echo 'EMBYPROXY_INSTALL_ROOT must be absolute' >&2; exit 1;; esac
  case "$install_root" in *..*|*' '*|*'	'*) echo 'unsafe install root' >&2; exit 1;; esac
else
  command -v systemctl >/dev/null 2>&1 || { echo 'systemd is required' >&2; exit 1; }
fi
umask 077
CONTROLLER='%s'
cfg_dir="${install_root}/etc/embyproxy-edge"
state_dir="${install_root}/var/lib/embyproxy-edge"
lib_dir="${install_root}/usr/local/lib"
bin_dir="${install_root}/usr/local/bin"
unit_dir="${install_root}/etc/systemd/system"
install -d -m 0700 "$cfg_dir" "$state_dir" "$lib_dir" "$bin_dir" "$unit_dir"
	response=$(curl --fail --silent --show-error --proto '%s' --tlsv1.2 -H 'Content-Type: application/json' --data '%s' "$CONTROLLER/api/edge/enroll/%s/%s")
credential=$(printf '%%s' "$response" | sed -n 's/.*"credential":"\([^"]*\)".*/\1/p')
node_id=$(printf '%%s' "$response" | sed -n 's/.*"node_id":"\([^"]*\)".*/\1/p')
[ -n "$credential" ] && [ -n "$node_id" ] || { echo 'invalid enrollment response' >&2; exit 1; }
printf 'NODE_ID=%%s\nCREDENTIAL=%%s\nCONTROLLER=%%s\n' "$node_id" "$credential" '%s' > "$cfg_dir/identity.env"
chmod 0600 "$cfg_dir/identity.env"
edge_listen=${EMBYPROXY_EDGE_LISTEN_ADDR:-127.0.0.1:18080}
edge_canary=${EMBYPROXY_EDGE_CANARY_PATH:-}
edge_allow_private=${EMBYPROXY_EDGE_ALLOW_PRIVATE_TARGETS:-false}
edge_isolated_media=${EMBYPROXY_ISOLATED_TEST_MEDIA:-false}
artifact_headers="$state_dir/edge-agent.headers"
curl --fail --silent --show-error --proto '%s' --tlsv1.2 -D "$artifact_headers" -H "X-EmbyProxy-Node-Credential: $credential" "$CONTROLLER/api/edge/artifact/$node_id/edge-agent" -o "$bin_dir/embyproxy-edge-agent"
artifact_sha=$(sed -n 's/^[Xx]-[Ee]mby[Pp]roxy-[Aa]rtifact-[Ss][Hh][Aa]256: *\([0-9a-fA-F]\{64\}\).*$/\1/p' "$artifact_headers" | tail -n 1)
actual_sha=$(sha256sum "$bin_dir/embyproxy-edge-agent" | awk '{print $1}')
[ -n "$artifact_sha" ] && [ "$artifact_sha" = "$actual_sha" ] || { echo 'edge agent checksum verification failed' >&2; exit 1; }
rm -f "$artifact_headers"
chmod 0700 "$bin_dir/embyproxy-edge-agent"
printf '{"listen_addr":"%%s","db_path":"%%s/edge.db","controller":"%%s","node_id":"%%s","credential":"%%s","version":"bootstrap","commit":"%s","canary_path":"%%s","allow_private_targets":%%s,"isolated_test_media":%%s}\n' "$edge_listen" "$state_dir" "$CONTROLLER" "$node_id" "$credential" "$edge_canary" "$edge_allow_private" "$edge_isolated_media" > "$cfg_dir/edge-agent.json"
chmod 0600 "$cfg_dir/edge-agent.json"
cat > "$lib_dir/embyproxy-edge-heartbeat" <<HEARTBEAT
#!/bin/sh
set -eu
. "$cfg_dir/identity.env"
payload=\$(printf '{"credential":"%%s","version":"bootstrap","commit":"%s","state":"online","playbackHealthy":false,"configSynced":false}' "\$CREDENTIAL")
curl --fail --silent --show-error --proto '%s' --tlsv1.2 -H 'Content-Type: application/json' --data "\$payload" "\$CONTROLLER/api/edge/heartbeat/\$NODE_ID" >/dev/null
HEARTBEAT
chmod 0700 "$lib_dir/embyproxy-edge-heartbeat"
cat > "$unit_dir/embyproxy-edge-heartbeat.service" <<UNIT
[Unit]
Description=EmbyProxy enrolled edge heartbeat
Wants=network-online.target
After=network-online.target
[Service]
Type=oneshot
ExecStart=$lib_dir/embyproxy-edge-heartbeat
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=$cfg_dir
UNIT
cat > "$unit_dir/embyproxy-edge-heartbeat.timer" <<TIMER
[Unit]
Description=EmbyProxy enrolled edge heartbeat timer
[Timer]
OnBootSec=30s
OnUnitActiveSec=60s
RandomizedDelaySec=10s
[Install]
WantedBy=timers.target
TIMER
cat > "$unit_dir/embyproxy-edge.service" <<UNIT
[Unit]
Description=EmbyProxy enrolled edge agent
Wants=network-online.target
After=network-online.target
[Service]
Type=simple
ExecStart=$bin_dir/embyproxy-edge-agent --config $cfg_dir/edge-agent.json
Restart=on-failure
RestartSec=3s
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=$cfg_dir $state_dir
UNIT
if [ -z "$install_root" ]; then systemctl daemon-reload; systemctl enable --now embyproxy-edge-heartbeat.timer embyproxy-edge.service; fi
"$lib_dir/embyproxy-edge-heartbeat" || true
echo 'Edge identity enrolled. This host remains unadmitted until its data-plane configuration reports a passing playback canary.'
		`, controller, curlProtocol, string(body), url.PathEscape(enrollmentID), url.PathEscape(token), controller, curlProtocol, buildinfo.Current().Commit, curlProtocol, buildinfo.Current().Commit)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(script))
}

func (h *Handler) handleEdgeEnrollment(w http.ResponseWriter, r *http.Request, path string) {
	// Enrollment and heartbeat payloads contain short-lived or long-lived node
	// credentials; keep them out of traffic capture and access logs.
	capture.Suppress(r)
	requestlog.SuppressAccessLog(r.Context())
	if r.Method == http.MethodGet && strings.HasPrefix(path, "/api/edge/bootstrap/") {
		parts := strings.Split(strings.TrimPrefix(path, "/api/edge/bootstrap/"), "/")
		if len(parts) != 2 {
			http.NotFound(w, r)
			return
		}
		h.handleBootstrap(w, r, parts[0], parts[1])
		return
	}
	if r.Method == http.MethodPost && strings.HasPrefix(path, "/api/edge/enroll/") {
		parts := strings.Split(strings.TrimPrefix(path, "/api/edge/enroll/"), "/")
		if len(parts) != 2 {
			http.NotFound(w, r)
			return
		}
		var body struct {
			Version string `json:"version"`
			Commit  string `json:"commit"`
		}
		if !decodeAuthJSON(w, r, &body) {
			return
		}
		node, credential, err := h.store.CompleteEnrollment(r.Context(), parts[0], parts[1], body.Version, body.Commit)
		if err != nil {
			writeJSON(w, 403, map[string]any{"ok": false, "error": "ENROLLMENT_DENIED"})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "node_id": node.ID, "credential": credential})
		return
	}
	if r.Method == http.MethodPost && strings.HasPrefix(path, "/api/edge/heartbeat/") {
		id := strings.TrimPrefix(path, "/api/edge/heartbeat/")
		var body struct {
			Credential, Version, Commit, State, LastError string
			PlaybackHealthy, ConfigSynced                 bool
		}
		if !decodeAuthJSON(w, r, &body) {
			return
		}
		if err := h.store.HeartbeatProxyNode(r.Context(), id, body.Credential, body.Version, body.Commit, body.State, body.PlaybackHealthy, body.ConfigSynced, body.LastError); err != nil {
			writeJSON(w, 403, map[string]any{"ok": false, "error": "HEARTBEAT_DENIED"})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true})
		return
	}
	if r.Method == http.MethodGet && strings.HasPrefix(path, "/api/edge/artifact/") {
		parts := strings.Split(strings.TrimPrefix(path, "/api/edge/artifact/"), "/")
		if len(parts) != 2 || h.cfg.EdgeAgentBinaryPath == "" {
			http.NotFound(w, r)
			return
		}
		credential := strings.TrimSpace(r.Header.Get("X-EmbyProxy-Node-Credential"))
		node, err := h.store.GetProxyNode(r.Context(), parts[0])
		if err != nil || node == nil || credential == "" || !h.store.ValidateProxyNodeCredential(r.Context(), parts[0], credential) {
			http.NotFound(w, r)
			return
		}
		file, err := os.Open(h.cfg.EdgeAgentBinaryPath)
		if err != nil {
			http.Error(w, "artifact unavailable", http.StatusServiceUnavailable)
			return
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 128<<20 {
			http.Error(w, "artifact unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", info.Size()))
		sum := sha256.New()
		if _, err := io.Copy(sum, file); err != nil {
			http.Error(w, "artifact unavailable", http.StatusServiceUnavailable)
			return
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			http.Error(w, "artifact unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("X-EmbyProxy-Artifact-SHA256", fmt.Sprintf("%x", sum.Sum(nil)))
		_, _ = io.Copy(w, io.LimitReader(file, 128<<20))
		return
	}
	if r.Method == http.MethodGet && strings.HasPrefix(path, "/api/edge/config/") {
		id := strings.TrimPrefix(path, "/api/edge/config/")
		credential := strings.TrimSpace(r.Header.Get("X-EmbyProxy-Node-Credential"))
		if id == "" || !h.store.ValidateProxyNodeCredential(r.Context(), id, credential) {
			http.NotFound(w, r)
			return
		}
		routes, err := h.store.ListManagedRoutes(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false})
			return
		}
		nodes, err := h.store.ListProxyNodes(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false})
			return
		}
		response := struct {
			NodeID string              `json:"node_id"`
			Nodes  []storage.ProxyNode `json:"nodes"`
			Routes []struct {
				Route storage.ManagedRoute       `json:"route"`
				Lines []storage.ManagedRouteLine `json:"lines"`
			} `json:"routes"`
		}{NodeID: id, Nodes: nodes, Routes: make([]struct {
			Route storage.ManagedRoute       `json:"route"`
			Lines []storage.ManagedRouteLine `json:"lines"`
		}, 0, len(routes))}
		for _, route := range routes {
			if !route.Enabled || !route.Public {
				continue
			}
			lines, listErr := h.store.ListManagedRouteLines(r.Context(), route.Slug)
			if listErr != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false})
				return
			}
			response.Routes = append(response.Routes, struct {
				Route storage.ManagedRoute       `json:"route"`
				Lines []storage.ManagedRouteLine `json:"lines"`
			}{Route: route, Lines: lines})
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, response)
		return
	}
	http.NotFound(w, r)
}
