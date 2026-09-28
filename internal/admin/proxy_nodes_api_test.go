package admin

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"embyproxy/internal/config"
	"embyproxy/internal/spaceship"
	"embyproxy/internal/storage"
)

func TestEdgeUsageRequiresCredentialAndDeduplicates(t *testing.T) {
	h := newAuthTestHandler(t, config.Config{AdminToken: "strong-admin-token"})
	ctx := context.Background()
	enrollment, token, err := h.store.CreateProxyNode(ctx, storage.ProxyNode{Name: "edge-usage", ResetDay: 1}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	node, credential, err := h.store.CompleteEnrollment(ctx, enrollment.ID, token, "v1", "test")
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/edge/usage/" + node.ID
	post := func(secret string, bytes int64) int {
		payload, _ := json.Marshal(map[string]any{"event_id": "event-123456789abcdef", "response_bytes": bytes, "sampled_at": time.Now().Unix()})
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(payload)))
		req.Header.Set("X-EmbyProxy-Node-Credential", secret)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	if code := post("wrong", 1024); code != http.StatusNotFound {
		t.Fatalf("invalid credential accepted: %d", code)
	}
	if code := post(credential, 1024); code != http.StatusOK {
		t.Fatalf("usage refused: %d", code)
	}
	if code := post(credential, 1024); code != http.StatusOK {
		t.Fatalf("retry refused: %d", code)
	}
	if code := post(credential, 2048); code != http.StatusConflict {
		t.Fatalf("conflict accepted: %d", code)
	}
	got, err := h.store.GetProxyNode(ctx, node.ID)
	if err != nil || got.UsedBytes != 1024 {
		t.Fatalf("usage=%+v err=%v", got, err)
	}
}

func TestProxyNodeAPIAutomaticDNSCreatesPendingWithoutHealth(t *testing.T) {
	h := newAuthTestHandler(t, config.Config{AdminToken: "strong-admin-token", EnrollmentControllerURL: "https://controller.149077530.xyz"})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Logf("dns request %s %s", r.Method, r.URL.Path)
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"records":[{"id":"r1","name":"rak","type":"A","address":"1.2.3.4","ttl":300}]}`))
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	h.dnsAutomation = &spaceship.Client{BaseURL: srv.URL, APIKey: "k", APISecret: "s", ManagedDomain: "example.com"}
	login := serveAdminJSON(t, h, http.MethodPost, "/admin/auth/login", map[string]any{"token": "strong-admin-token"}, nil)
	cookie := login.Result().Cookies()[0]
	created := serveAdminJSON(t, h, http.MethodPost, "/api/admin/proxy-nodes", map[string]any{"name": "edge-auto", "domain_prefix": "rak", "public_ipv4": "1.2.3.4", "reset_day": 1}, cookie)
	if created.Code != http.StatusCreated {
		t.Fatalf("create=%d %s", created.Code, created.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(created.Body.Bytes(), &body)
	nodeID := body["enrollment"].(map[string]any)["node_id"].(string)
	node, err := h.store.GetProxyNode(context.Background(), nodeID)
	if err != nil || node == nil || node.State != "pending" || node.PublicAddress != "https://rak.example.com" {
		t.Fatalf("node=%+v err=%v", node, err)
	}
}

func TestProxyNodeAPIAutomaticDNSAcceptsFreshVPSInputAndGeneratesOrigin(t *testing.T) {
	h := newAuthTestHandler(t, config.Config{AdminToken: "strong-admin-token", EnrollmentControllerURL: "https://controller.149077530.xyz"})
	createdRecord := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			if createdRecord {
				_, _ = w.Write([]byte(`{"items":[{"name":"rak-test","type":"A","address":"198.200.42.120","ttl":300}],"total":1}`))
			} else {
				_, _ = w.Write([]byte(`{"items":[],"total":0}`))
			}
			return
		}
		createdRecord = true
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	h.dnsAutomation = &spaceship.Client{BaseURL: srv.URL, APIKey: "k", APISecret: "s", ManagedDomain: "149077530.xyz"}
	login := serveAdminJSON(t, h, http.MethodPost, "/admin/auth/login", map[string]any{"token": "strong-admin-token"}, nil)
	cookie := login.Result().Cookies()[0]
	created := serveAdminJSON(t, h, http.MethodPost, "/api/admin/proxy-nodes", map[string]any{
		"name": "raksmart-test", "domain_prefix": "rak-test", "public_ipv4": "198.200.42.120",
		"quota_bytes": int64(1000) * 1_000_000_000, "reset_day": 7, "priority": 10,
	}, cookie)
	if created.Code != http.StatusCreated {
		t.Fatalf("create=%d %s", created.Code, created.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(created.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	nodeID := body["enrollment"].(map[string]any)["node_id"].(string)
	node, err := h.store.GetProxyNode(context.Background(), nodeID)
	if err != nil || node == nil || node.State != "pending" || node.PublicAddress != "https://rak-test.149077530.xyz" || node.ResetDay != 7 || node.Priority != 10 {
		t.Fatalf("node=%+v err=%v", node, err)
	}
}

func TestProxyNodeAPIDuplicateNameIsSpecificAndDoesNotWriteDNS(t *testing.T) {
	h := newAuthTestHandler(t, config.Config{AdminToken: "strong-admin-token", EnrollmentControllerURL: "https://controller.149077530.xyz"})
	if _, _, err := h.store.CreateProxyNode(context.Background(), storage.ProxyNode{Name: "raksmart", ResetDay: 7}, 15*time.Minute); err != nil {
		t.Fatal(err)
	}
	getCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		getCalls++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	h.dnsAutomation = &spaceship.Client{BaseURL: srv.URL, APIKey: "k", APISecret: "s", ManagedDomain: "149077530.xyz"}
	login := serveAdminJSON(t, h, http.MethodPost, "/admin/auth/login", map[string]any{"token": "strong-admin-token"}, nil)
	cookie := login.Result().Cookies()[0]
	resp := serveAdminJSON(t, h, http.MethodPost, "/api/admin/proxy-nodes", map[string]any{
		"name": "raksmart", "domain_prefix": "rak", "public_ipv4": "198.200.42.120", "quota_bytes": 1_000_000_000, "reset_day": 7, "priority": 10,
	}, cookie)
	if resp.Code != http.StatusConflict || !strings.Contains(resp.Body.String(), `"error":"NODE_NAME_EXISTS"`) {
		t.Fatalf("response=%d %s", resp.Code, resp.Body.String())
	}
	if getCalls != 0 {
		t.Fatalf("DNS was contacted before duplicate-name validation: %d", getCalls)
	}
}

func TestProxyNodeAPILocalValidationErrorsBeforeDNS(t *testing.T) {
	h := newAuthTestHandler(t, config.Config{AdminToken: "strong-admin-token", EnrollmentControllerURL: "https://controller.149077530.xyz"})
	getCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		getCalls++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	h.dnsAutomation = &spaceship.Client{BaseURL: srv.URL, APIKey: "k", APISecret: "s", ManagedDomain: "149077530.xyz"}
	login := serveAdminJSON(t, h, http.MethodPost, "/admin/auth/login", map[string]any{"token": "strong-admin-token"}, nil)
	cookie := login.Result().Cookies()[0]
	resp := serveAdminJSON(t, h, http.MethodPost, "/api/admin/proxy-nodes", map[string]any{
		"name": "raksmart-invalid", "domain_prefix": "rak", "public_ipv4": "not-an-ip", "reset_day": 7, "priority": 10,
	}, cookie)
	if resp.Code != http.StatusBadRequest || !strings.Contains(resp.Body.String(), `"error":"INVALID_PUBLIC_IPV4"`) {
		t.Fatalf("response=%d %s", resp.Code, resp.Body.String())
	}
	if getCalls != 0 {
		t.Fatalf("DNS was contacted before local validation: %d", getCalls)
	}
}

func TestProxyNodeAPICreatesOneTimeEnrollmentAndHeartbeat(t *testing.T) {
	h := newAuthTestHandler(t, config.Config{AdminToken: "strong-admin-token", EnrollmentControllerURL: "https://controller.149077530.xyz"})
	login := serveAdminJSON(t, h, http.MethodPost, "/admin/auth/login", map[string]any{"token": "strong-admin-token"}, nil)
	cookie := login.Result().Cookies()[0]
	created := serveAdminJSON(t, h, http.MethodPost, "/api/admin/proxy-nodes", map[string]any{"name": "edge-test", "public_address": "https://edge.example.net", "quota_bytes": 1000, "reset_day": 1, "reset_timezone": "Asia/Shanghai"}, cookie)
	if created.Code != http.StatusCreated || !strings.Contains(created.Body.String(), `"install_command"`) {
		t.Fatalf("created=%d %s", created.Code, created.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(created.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	enrollment := body["enrollment"].(map[string]any)
	command := body["install_command"].(string)
	commandURL := ""
	for _, field := range strings.Fields(command) {
		candidate := strings.Trim(field, "'")
		if strings.HasPrefix(candidate, "https://") {
			commandURL = candidate
			break
		}
	}
	if commandURL == "" {
		t.Fatal("enrollment URL missing")
	}
	parts := strings.Split(commandURL, "/")
	token := parts[len(parts)-1]
	id := enrollment["id"].(string)
	enroll := serveAdminJSON(t, h, http.MethodPost, "/api/edge/enroll/"+id+"/"+token, map[string]any{"version": "v1", "commit": "abc"}, nil)
	if enroll.Code != http.StatusOK {
		t.Fatalf("enroll=%d %s", enroll.Code, enroll.Body.String())
	}
	var enrolled map[string]any
	_ = json.Unmarshal(enroll.Body.Bytes(), &enrolled)
	beat := serveAdminJSON(t, h, http.MethodPost, "/api/edge/heartbeat/"+enrolled["node_id"].(string), map[string]any{"credential": enrolled["credential"], "version": "v1", "commit": "abc", "state": "healthy", "playbackHealthy": true, "configSynced": true}, nil)
	if beat.Code != http.StatusOK {
		t.Fatalf("heartbeat=%d %s", beat.Code, beat.Body.String())
	}
	list, err := h.store.ListProxyNodes(context.Background())
	if err != nil || len(list) != 1 || !list[0].PlaybackHealthy || list[0].IngressHealthy {
		t.Fatalf("list=%+v err=%v", list, err)
	}
}

func TestProxyNodeAPIPersistsPublicAddressUpdate(t *testing.T) {
	h := newAuthTestHandler(t, config.Config{AdminToken: "strong-admin-token", EnrollmentControllerURL: "https://controller.149077530.xyz"})
	login := serveAdminJSON(t, h, http.MethodPost, "/admin/auth/login", map[string]any{"token": "strong-admin-token"}, nil)
	cookie := login.Result().Cookies()[0]
	created := serveAdminJSON(t, h, http.MethodPost, "/api/admin/proxy-nodes", map[string]any{"name": "edge-address", "public_address": "https://edge-address.example.net", "reset_day": 1}, cookie)
	var body map[string]any
	if err := json.Unmarshal(created.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	nodeID := body["enrollment"].(map[string]any)["node_id"].(string)
	updated := serveAdminJSON(t, h, http.MethodPatch, "/api/admin/proxy-nodes/"+nodeID, map[string]any{"public_address": "https://edge-address-2.example.net"}, cookie)
	if updated.Code != http.StatusOK || !strings.Contains(updated.Body.String(), "edge-address-2.example.net") {
		t.Fatalf("update=%d %s", updated.Code, updated.Body.String())
	}
	node, err := h.store.GetProxyNode(context.Background(), nodeID)
	if err != nil || node == nil || node.PublicAddress != "https://edge-address-2.example.net" {
		t.Fatalf("node=%+v err=%v", node, err)
	}
}

func TestProxyNodeBootstrapIsNoStoreAndDoesNotExposeAdminSecret(t *testing.T) {
	h := newAuthTestHandler(t, config.Config{AdminToken: "strong-admin-token", EnrollmentControllerURL: "https://owner-admin.149077530.xyz"})
	login := serveAdminJSON(t, h, http.MethodPost, "/admin/auth/login", map[string]any{"token": "strong-admin-token"}, nil)
	cookie := login.Result().Cookies()[0]
	created := serveAdminJSON(t, h, http.MethodPost, "/api/admin/proxy-nodes", map[string]any{"name": "edge-bootstrap", "public_address": "https://edge.example.net", "reset_day": 1}, cookie)
	var body map[string]any
	if err := json.Unmarshal(created.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	enrollment := body["enrollment"].(map[string]any)
	command := body["install_command"].(string)
	if !strings.Contains(command, "https://owner-admin.149077530.xyz/api/edge/bootstrap/") || strings.Contains(command, "strong-admin-token") {
		t.Fatalf("unsafe command: %s", command)
	}
	parts := strings.Split(strings.TrimPrefix(command, "curl --fail --silent --show-error --proto '=https' --tlsv1.2 'https://owner-admin.149077530.xyz/api/edge/bootstrap/"), "' | sudo sh")
	if len(parts) != 2 {
		t.Fatalf("unexpected command format: %s", command)
	}
	path := "/api/edge/bootstrap/" + strings.TrimSpace(parts[0])
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("bootstrap=%d headers=%v", rec.Code, rec.Header())
	}
	if strings.Contains(rec.Body.String(), "strong-admin-token") || !strings.Contains(rec.Body.String(), "api/edge/enroll/") || !strings.Contains(rec.Body.String(), "EMBYPROXY_INSTALL_ROOT") || !strings.Contains(rec.Body.String(), "sha256sum") || !strings.Contains(rec.Body.String(), "edge agent checksum verification failed") {
		t.Fatal("bootstrap leaked secret or omitted enrollment endpoint")
	}
	if strings.Contains(rec.Body.String(), "/api/edge/enroll/false/false") || !strings.Contains(rec.Body.String(), "/api/edge/enroll/"+strings.TrimSpace(parts[0])) {
		t.Fatal("bootstrap enrollment URL did not preserve the generated enrollment identity")
	}
	if !strings.Contains(rec.Body.String(), "edge_isolated_media=${EMBYPROXY_ISOLATED_TEST_MEDIA:-true}") || !strings.Contains(rec.Body.String(), "edge_canary='/__isolated-media/canary'") {
		t.Fatal("bootstrap does not provide the built-in playback canary by default")
	}
	if strings.Index(rec.Body.String(), "invalid playback health configuration") > strings.Index(rec.Body.String(), "api/edge/enroll/") {
		t.Fatal("playback configuration validation must precede enrollment")
	}
	if !strings.Contains(rec.Body.String(), "edge_listen=${EMBYPROXY_EDGE_LISTEN_ADDR:-127.0.0.1:18080}") || !strings.Contains(rec.Body.String(), "edge_probe=${EMBYPROXY_EDGE_PROBE_ADDR:-127.0.0.1:18080}") {
		t.Fatal("bootstrap does not separate the loopback listener and probe addresses")
	}
	script := rec.Body.String()
	if !strings.Contains(script, "PERSISTED_EDGE_PUBLIC='https://edge.example.net'") || !strings.Contains(script, "EMBYPROXY_EDGE_INGRESS_MODE") || !strings.Contains(script, "systemctl enable caddy.service") || !strings.Contains(script, "systemctl restart caddy.service") || !strings.Contains(script, "respond @isolated 404") || !strings.Contains(script, "systemctl cat caddy.service") || !strings.Contains(script, "wait_http_200 'HTTPS edge ingress'") {
		t.Fatal("bootstrap does not bind to the persisted HTTPS edge ingress")
	}
	if !strings.Contains(script, "$edge_domain, stream.149077530.xyz {") {
		t.Fatal("installer must provision TLS and routing for the unified business ingress")
	}

	for _, required := range []string{
		"caddy_before_installed=false",
		"caddy_postinst_active=false",
		"caddy_unit_before=false",
		"caddy_config_before=false",
		"caddy_marker=\"$state_dir/caddy-managed\"",
		"caddy_managed=false",
		"managed_by=embyproxy-edge",
		"unmanaged Caddy/configuration already exists",
		"ports 80/443 are already occupied",
		"apt-get update",
		"ca-certificates curl gnupg debian-keyring debian-archive-keyring apt-transport-https",
		"caddy-stable-archive-keyring.gpg",
		"caddy-stable.list",
		"https://dl.cloudsmith.io/public/caddy/stable/gpg.key",
		"gpg --dearmor --yes",
		"signed-by=$caddy_keyring",
		"chown root:caddy \"$caddy_root\"",
		"chmod 0750 \"$caddy_root\"",
		"DEBIAN_FRONTEND=noninteractive apt-get install -y caddy",
		"systemctl stop caddy.service",
		"failed to stop caddy.service before configuration replacement",
		"caddy_tmp=$(mktemp \"$caddy_root/Caddyfile.embyproxy.XXXXXX\")",
		"caddy_backup=\"$caddy_marker.previous\"",
		"caddy_restore()",
		"systemctl stop caddy.service || true",
		"artifact_tmp=$(mktemp \"$bin_dir/embyproxy-edge-agent.XXXXXX\")",
		"artifact_headers=$(mktemp \"$state_dir/edge-agent.headers.XXXXXX\")",
		"-o \"$artifact_tmp\"",
		"mv -f \"$artifact_tmp\" \"$bin_dir/embyproxy-edge-agent\"",
		"chown root:caddy \"$caddy_tmp\"",
		"chmod 0640 \"$caddy_tmp\"",
		"\"$caddy_bin\" validate --config \"$caddy_tmp\" --adapter caddyfile",
		"cp -p \"$caddy_root/Caddyfile\" \"$caddy_backup\"",
		"mv -f \"$caddy_tmp\" \"$caddy_root/Caddyfile\"",
		"caddy_marker_tmp=$(mktemp \"$state_dir/caddy-managed.XXXXXX\")",
		"mv -f \"$caddy_marker_tmp\" \"$caddy_marker\"",
		"systemctl status caddy.service --no-pager",
		"journalctl -u caddy.service -n 80 --no-pager",
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("bootstrap omitted Caddy safety branch %q", required)
		}
	}
	for _, required := range []string{"/api/edge/ownership/$node_id", "X-EmbyProxy-Node-Credential: $credential", "ownership_attempt=1", "failed to record edge resource ownership", "edge-agent.ownership.XXXXXX"} {
		if !strings.Contains(script, required) {
			t.Fatalf("bootstrap omitted ownership reconciliation %q", required)
		}
	}
	if strings.Contains(script, "embyproxy-edge-caddy.service") {
		t.Fatal("bootstrap must not create a competing Caddy service")
	}
	if strings.Index(script, "cat > \"$caddy_tmp\"") > strings.Index(script, "\"$caddy_bin\" validate --config \"$caddy_tmp\" --adapter caddyfile") {
		t.Fatal("temporary Caddyfile must be validated after generation")
	}
	permissionIndex := strings.Index(script, "chmod 0640 \"$caddy_tmp\"")
	replaceIndex := strings.Index(script, "mv -f \"$caddy_tmp\" \"$caddy_root/Caddyfile\"")
	if permissionIndex < 0 || replaceIndex < 0 || permissionIndex > replaceIndex {
		t.Fatal("Caddy service-readable permissions must be set before atomic replacement")
	}
	if strings.Index(script, "\"$caddy_bin\" validate --config \"$caddy_tmp\" --adapter caddyfile") > strings.Index(script, "systemctl stop caddy.service || {") {
		t.Fatal("Caddy must be stopped only after temporary config validation")
	}
	if strings.Index(script, "DEBIAN_FRONTEND=noninteractive apt-get install -y caddy") > strings.Index(script, "systemctl stop caddy.service") {
		t.Fatal("postinst Caddy must be stopped after installation")
	}
	if strings.Index(script, "systemctl stop caddy.service") > strings.Index(script, "mv -f \"$caddy_tmp\" \"$caddy_root/Caddyfile\"") {
		t.Fatal("Caddy must be stopped before atomic replacement")
	}
	restartIndex := strings.Index(script, "systemctl restart caddy.service || {")
	if strings.Index(script, "mv -f \"$caddy_tmp\" \"$caddy_root/Caddyfile\"") > restartIndex {
		t.Fatal("atomic Caddy replacement must precede restart")
	}
	if restartIndex > strings.Index(script, "printf 'managed_by=embyproxy-edge") {
		t.Fatal("managed marker must be written only after restart")
	}
	if strings.Index(script, "printf 'managed_by=embyproxy-edge") > strings.Index(script, "mv -f \"$caddy_marker_tmp\" \"$caddy_marker\"") {
		t.Fatal("managed marker contents must be prepared before atomic marker replacement")
	}
	if strings.Index(script, "mv -f \"$caddy_marker_tmp\" \"$caddy_marker\"") > strings.Index(script, "systemctl enable embyproxy-edge.service") {
		t.Fatal("edge agent must start only after managed marker replacement")
	}
	if strings.Index(script, "\"$caddy_bin\" validate --config \"$caddy_tmp\" --adapter caddyfile") > restartIndex {
		t.Fatal("Caddy must be validated before restart")
	}
	if strings.Index(script, "systemctl restart caddy.service") > strings.LastIndex(script, "systemctl is-active --quiet caddy.service") {
		t.Fatal("Caddy must be active-checked after restart")
	}
	if strings.Index(script, "systemctl is-active --quiet caddy.service") > strings.Index(script, "systemctl enable embyproxy-edge.service") {
		t.Fatal("edge agent must start only after managed Caddy is active")
	}
	if !strings.Contains(script, "systemctl enable embyproxy-edge.service || {") || !strings.Contains(script, "systemctl restart embyproxy-edge.service || {") {
		t.Fatal("bootstrap must enable and restart the edge after replacing enrolled config")
	}
	if strings.Contains(script, "systemctl enable --now embyproxy-edge.service") {
		t.Fatal("bootstrap must use explicit enable and restart semantics")
	}
	if strings.Index(script, "systemctl daemon-reload") > strings.Index(script, "systemctl enable embyproxy-edge.service") {
		t.Fatal("edge must be started only after daemon-reload")
	}
	if strings.Index(script, "systemctl restart embyproxy-edge.service || {") > strings.Index(script, "wait_edge_local_health || exit 1") {
		t.Fatal("local health must be checked after edge startup")
	}
	for _, failure := range []string{
		"failed to enable embyproxy-edge.service",
		"failed to restart embyproxy-edge.service",
		"if [ \"$edge_state\" = failed ]; then",
		"edge agent startup timed out after 60 seconds",
		"edge_startup_diagnostics",
		"wait_edge_local_health || exit 1",
	} {
		if !strings.Contains(script, failure) {
			t.Fatalf("bootstrap omitted non-zero startup failure handling %q", failure)
		}
	}
	if strings.Count(script, "systemctl restart embyproxy-edge.service || {") != 1 {
		t.Fatal("fresh, inactive, and active installs must share one explicit edge restart")
	}
	if strings.Contains(script, "playbackHealthy:false") || !strings.Contains(script, "systemctl disable --now embyproxy-edge-heartbeat.timer") || !strings.Contains(script, "rm -f \"$unit_dir/embyproxy-edge-heartbeat.timer\"") {
		t.Fatal("bootstrap must remove the legacy synthetic heartbeat without reinstalling it")
	}
	artifactDownload := strings.Index(script, "-o \"$artifact_tmp\"")
	artifactMode := strings.Index(script, "chmod 0700 \"$artifact_tmp\"")
	artifactReplace := strings.Index(script, "mv -f \"$artifact_tmp\" \"$bin_dir/embyproxy-edge-agent\"")
	if artifactDownload < 0 || artifactMode < artifactDownload || artifactReplace < artifactMode {
		t.Fatal("edge artifact must be downloaded and checked in a temporary path before atomic replacement")
	}
	for _, retry := range []string{"wait_edge_local_health()", "edge_wait_timeout=60", "systemctl is-active --quiet embyproxy-edge.service", "http://$edge_probe/health", "EDGE STARTUP: PASS", "LOCAL HEALTH: PASS", "systemctl restart embyproxy-edge.service || {", "wait_http_200()", "wait_attempts=\"$3\"", "wait_connect_timeout=\"$4\"", "wait_max_time=\"$5\"", "wait_http_200 'HTTPS edge ingress' \"$edge_public/health\" 18 5 5 5 https", "--proto '=https' --tlsv1.2"} {
		if !strings.Contains(script, retry) {
			t.Fatalf("bootstrap omitted readiness retry contract %q", retry)
		}
	}
	if strings.Contains(script, "systemctl enable --now caddy.service") {
		t.Fatal("bootstrap must not use an unvalidated Caddy start")
	}
	for _, diagnostic := range []string{"failed to restart caddy.service", "Caddy configuration validation failed", "failed to enable caddy.service"} {
		if !strings.Contains(script, diagnostic) {
			t.Fatalf("bootstrap omitted Caddy failure diagnostic %q", diagnostic)
		}
	}
	scriptPath := filepath.Join(t.TempDir(), "bootstrap.sh")
	if err := os.WriteFile(scriptPath, rec.Body.Bytes(), 0700); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("sh", "-n", scriptPath).CombinedOutput(); err != nil {
		t.Fatalf("bootstrap shell syntax: %v: %s", err, out)
	}
	_ = enrollment
}

func TestProxyNodeCreationFailsClosedWithoutPublicControllerURL(t *testing.T) {
	h := newAuthTestHandler(t, config.Config{AdminToken: "strong-admin-token"})
	login := serveAdminJSON(t, h, http.MethodPost, "/admin/auth/login", map[string]any{"token": "strong-admin-token"}, nil)
	cookie := login.Result().Cookies()[0]
	created := serveAdminJSON(t, h, http.MethodPost, "/api/admin/proxy-nodes", map[string]any{"name": "edge-missing-controller", "public_address": "https://edge-missing.example.net", "reset_day": 1}, cookie)
	if created.Code != http.StatusServiceUnavailable || !strings.Contains(created.Body.String(), "CONTROLLER_PUBLIC_URL_NOT_CONFIGURED") {
		t.Fatalf("created=%d %s", created.Code, created.Body.String())
	}
	nodes, err := h.store.ListProxyNodes(context.Background())
	if err != nil || len(nodes) != 0 {
		t.Fatalf("nodes=%+v err=%v", nodes, err)
	}
}

func TestBuildEnrollmentCommandQuotesURLAndRejectsPlaceholders(t *testing.T) {
	if got := buildEnrollmentCommand("https://controller.149077530.xyz", "node/id", "token value"); !strings.Contains(got, "'https://controller.149077530.xyz/api/edge/bootstrap/node%2Fid/token%20value'") {
		t.Fatalf("command=%s", got)
	}
	for _, raw := range []string{"", "https://OWNER_CONTROLLER", "https://localhost", "https://controller.example", "http://10.0.0.1"} {
		if _, err := config.NormalizeEnrollmentControllerURL(raw, false); err == nil {
			t.Fatalf("accepted unsafe controller URL %q", raw)
		}
	}
}

func TestProxyNodeBootstrapRegenerationWorksForRevokedUnadmittedNode(t *testing.T) {
	h := newAuthTestHandler(t, config.Config{AdminToken: "strong-admin-token", EnrollmentControllerURL: "https://owner-admin.149077530.xyz"})
	login := serveAdminJSON(t, h, http.MethodPost, "/admin/auth/login", map[string]any{"token": "strong-admin-token"}, nil)
	cookie := login.Result().Cookies()[0]
	created := serveAdminJSON(t, h, http.MethodPost, "/api/admin/proxy-nodes", map[string]any{"name": "edge-revoked-bootstrap", "public_address": "https://edge-revoked.example.net", "reset_day": 1}, cookie)
	if created.Code != http.StatusCreated {
		t.Fatalf("create=%d %s", created.Code, created.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(created.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	nodeID := body["enrollment"].(map[string]any)["node_id"].(string)
	if revoked := serveAdminJSON(t, h, http.MethodPost, "/api/admin/proxy-nodes/"+nodeID+"/revoke?force=true", nil, cookie); revoked.Code != http.StatusOK {
		t.Fatalf("revoke=%d %s", revoked.Code, revoked.Body.String())
	}
	first := serveAdminJSON(t, h, http.MethodPost, "/api/admin/proxy-nodes/"+nodeID+"/bootstrap", nil, cookie)
	second := serveAdminJSON(t, h, http.MethodPost, "/api/admin/proxy-nodes/"+nodeID+"/bootstrap", nil, cookie)
	if first.Code != http.StatusOK || second.Code != http.StatusOK {
		t.Fatalf("first=%d second=%d", first.Code, second.Code)
	}
	var firstBody, secondBody map[string]any
	_ = json.Unmarshal(first.Body.Bytes(), &firstBody)
	_ = json.Unmarshal(second.Body.Bytes(), &secondBody)
	firstEnrollment := firstBody["enrollment"].(map[string]any)
	secondEnrollment := secondBody["enrollment"].(map[string]any)
	if firstEnrollment["id"] == secondEnrollment["id"] || firstBody["install_command"] == secondBody["install_command"] {
		t.Fatal("consecutive regeneration reused enrollment token")
	}
	for _, value := range []string{firstBody["install_command"].(string), secondBody["install_command"].(string)} {
		if !strings.Contains(value, "https://owner-admin.149077530.xyz/api/edge/bootstrap/") || strings.Contains(value, "OWNER_CONTROLLER") || strings.Contains(value, "localhost") || strings.Contains(value, "127.0.0.1") || strings.Contains(value, ".example") {
			t.Fatalf("unsafe command: %s", value)
		}
	}
	node, err := h.store.GetProxyNode(context.Background(), nodeID)
	if err != nil || node == nil || node.State != "registered" || node.Enabled || node.PlaybackHealthy || node.ConfigSynced {
		t.Fatalf("node=%+v err=%v", node, err)
	}
}

func TestProxyNodeLifecycleAPIRejectsEnableForRevokedAndCreatesJob(t *testing.T) {
	h := newAuthTestHandler(t, config.Config{AdminToken: "strong-admin-token", EnrollmentControllerURL: "https://owner-admin.149077530.xyz"})
	login := serveAdminJSON(t, h, http.MethodPost, "/admin/auth/login", map[string]any{"token": "strong-admin-token"}, nil)
	cookie := login.Result().Cookies()[0]
	created := serveAdminJSON(t, h, http.MethodPost, "/api/admin/proxy-nodes", map[string]any{"name": "edge-lifecycle-api", "public_address": "https://edge-lifecycle.example.net", "reset_day": 1}, cookie)
	var body map[string]any
	if err := json.Unmarshal(created.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	id := body["enrollment"].(map[string]any)["node_id"].(string)
	if resp := serveAdminJSON(t, h, http.MethodPost, "/api/admin/proxy-nodes/"+id+"/revoke?force=true", nil, cookie); resp.Code != http.StatusOK {
		t.Fatalf("revoke=%d %s", resp.Code, resp.Body.String())
	}
	if resp := serveAdminJSON(t, h, http.MethodPatch, "/api/admin/proxy-nodes/"+id, map[string]any{"enabled": true}, cookie); resp.Code != http.StatusConflict {
		t.Fatalf("enable revoked=%d %s", resp.Code, resp.Body.String())
	}
	decom := serveAdminJSON(t, h, http.MethodPost, "/api/admin/proxy-nodes/"+id+"/decommission", map[string]any{"confirm_name": "edge-lifecycle-api"}, cookie)
	if decom.Code != http.StatusAccepted || !strings.Contains(decom.Body.String(), "remote_cleanup_pending") {
		t.Fatalf("decommission=%d %s", decom.Code, decom.Body.String())
	}
	var jobBody map[string]any
	if err := json.Unmarshal(decom.Body.Bytes(), &jobBody); err != nil {
		t.Fatal(err)
	}
	job := jobBody["job"].(map[string]any)["id"].(string)
	if got := serveAdminJSON(t, h, http.MethodGet, "/api/admin/proxy-node-jobs/"+job, nil, cookie); got.Code != http.StatusOK {
		t.Fatalf("job=%d %s", got.Code, got.Body.String())
	}
}

func TestBootstrapAllowsCleanHostToChooseHTTPSIngressAtInstallTime(t *testing.T) {
	h := newAuthTestHandler(t, config.Config{AdminToken: "strong-admin-token", EnrollmentControllerURL: "https://owner-admin.149077530.xyz"})
	enrollment, token, err := h.store.CreateProxyNode(context.Background(), storage.ProxyNode{Name: "edge-no-ingress", PublicAddress: "https://edge-no-ingress.example.net", ResetDay: 1}, 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/edge/bootstrap/"+enrollment.ID+"/"+token, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "EMBYPROXY_EDGE_DOMAIN") || !strings.Contains(rec.Body.String(), "edge ingress") {
		t.Fatalf("bootstrap status=%d body=%q", rec.Code, rec.Body.String())
	}
	if err := h.store.ValidateEnrollment(context.Background(), enrollment.ID, token); err != nil {
		t.Fatalf("preflight failure consumed enrollment: %v", err)
	}
}

func TestProxyNodeQuotaSchedulePatchAndManualReset(t *testing.T) {
	h := newAuthTestHandler(t, config.Config{AdminToken: "strong-admin-token", EnrollmentControllerURL: "https://owner-admin.149077530.xyz"})
	login := serveAdminJSON(t, h, http.MethodPost, "/admin/auth/login", map[string]any{"token": "strong-admin-token"}, nil)
	cookie := login.Result().Cookies()[0]
	created := serveAdminJSON(t, h, http.MethodPost, "/api/admin/proxy-nodes", map[string]any{"name": "edge-quota", "public_address": "https://edge-quota.example.net", "quota_bytes": 1000, "reset_day": 1, "reset_timezone": "UTC"}, cookie)
	var body map[string]any
	if err := json.Unmarshal(created.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	id := body["enrollment"].(map[string]any)["node_id"].(string)
	before, err := h.store.GetProxyNode(context.Background(), id)
	if err != nil || before == nil {
		t.Fatal(err)
	}
	invalid := serveAdminJSON(t, h, http.MethodPatch, "/api/admin/proxy-nodes/"+id, map[string]any{"reset_timezone": "Not/AZone", "next_reset_at": "invalid"}, cookie)
	if invalid.Code != http.StatusBadRequest || !strings.Contains(invalid.Body.String(), "INVALID_RESET_SCHEDULE") {
		t.Fatalf("invalid schedule=%d %s", invalid.Code, invalid.Body.String())
	}
	patched := serveAdminJSON(t, h, http.MethodPatch, "/api/admin/proxy-nodes/"+id, map[string]any{"quota_bytes": 2000, "used_bytes": 900, "reset_day": 31, "reset_timezone": "Asia/Shanghai"}, cookie)
	if patched.Code != http.StatusOK {
		t.Fatalf("patch=%d %s", patched.Code, patched.Body.String())
	}
	node, err := h.store.GetProxyNode(context.Background(), id)
	if err != nil || node.QuotaBytes != 2000 || node.UsedBytes != 900 || node.ResetDay != 31 || node.ResetTimezone != "Asia/Shanghai" || node.NextResetAt == before.NextResetAt || node.NextResetAt <= time.Now().Unix() {
		t.Fatalf("patched node=%+v before=%+v err=%v", node, before, err)
	}
	reset := serveAdminJSON(t, h, http.MethodPost, "/api/admin/proxy-nodes/"+id+"/reset-usage", nil, cookie)
	if reset.Code != http.StatusOK {
		t.Fatalf("reset=%d %s", reset.Code, reset.Body.String())
	}
	node, err = h.store.GetProxyNode(context.Background(), id)
	if err != nil || node.UsedBytes != 0 || node.NextResetAt <= time.Now().Unix() {
		t.Fatalf("reset node=%+v err=%v", node, err)
	}
}

func TestProxyNodeTrafficThresholdCreatePatchAndValidation(t *testing.T) {
	h := newAuthTestHandler(t, config.Config{AdminToken: "strong-admin-token", EnrollmentControllerURL: "https://owner-admin.149077530.xyz"})
	login := serveAdminJSON(t, h, http.MethodPost, "/admin/auth/login", map[string]any{"token": "strong-admin-token"}, nil)
	cookie := login.Result().Cookies()[0]
	created := serveAdminJSON(t, h, http.MethodPost, "/api/admin/proxy-nodes", map[string]any{"name": "edge-threshold", "public_address": "https://edge-threshold.example.net", "quota_bytes": 1000, "reset_day": 1, "reset_timezone": "UTC"}, cookie)
	var body map[string]any
	if err := json.Unmarshal(created.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	id := body["enrollment"].(map[string]any)["node_id"].(string)
	node, _ := h.store.GetProxyNode(context.Background(), id)
	if node.ThresholdPercent != 95 {
		t.Fatalf("default threshold=%v", node.ThresholdPercent)
	}
	patched := serveAdminJSON(t, h, http.MethodPatch, "/api/admin/proxy-nodes/"+id, map[string]any{"threshold_percent": 80}, cookie)
	if patched.Code != http.StatusOK {
		t.Fatalf("patch=%d %s", patched.Code, patched.Body.String())
	}
	node, _ = h.store.GetProxyNode(context.Background(), id)
	if node.ThresholdPercent != 80 {
		t.Fatalf("patched threshold=%v", node.ThresholdPercent)
	}
	invalid := serveAdminJSON(t, h, http.MethodPatch, "/api/admin/proxy-nodes/"+id, map[string]any{"threshold_percent": 101}, cookie)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid=%d %s", invalid.Code, invalid.Body.String())
	}
}

func TestProxyNodeQuotaControlsRemainInAdminUI(t *testing.T) {
	for _, required := range []string{"proxyQuotaModal", "openProxyQuotaModal", "saveProxyQuota", "resetProxyUsage", "/reset-usage", "已用流量校准", "重置时区", "proxyThresholdPercent", "切换阈值"} {
		if !strings.Contains(indexHTML, required) {
			t.Fatalf("admin UI missing quota control %q", required)
		}
	}
}

func TestEdgeArtifactAndSnapshotRequireNodeCredential(t *testing.T) {
	artifact := filepath.Join(t.TempDir(), "edge-agent")
	payload := []byte("edge-agent-test-artifact")
	if err := os.WriteFile(artifact, payload, 0700); err != nil {
		t.Fatal(err)
	}
	h := newAuthTestHandler(t, config.Config{AdminToken: "strong-admin-token", EdgeAgentBinaryPath: artifact})
	enrollment, token, err := h.store.CreateProxyNode(context.Background(), storage.ProxyNode{Name: "edge-artifact", ResetDay: 1}, 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	node, credential, err := h.store.CompleteEnrollment(context.Background(), enrollment.ID, token, "v1", "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.store.SaveManagedRoute(context.Background(), storage.ManagedRoute{Slug: "demo", NodeName: "demo", Enabled: true, Public: true, DefaultLine: "main"}, []storage.ManagedRouteLine{{RouteSlug: "demo", LineSlug: "main", Target: "https://media.example", Enabled: true, Position: 1}}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/edge/artifact/" + node.ID + "/edge-agent", "/api/edge/config/" + node.ID} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("missing credential %s status=%d", path, rec.Code)
		}
		req.Header.Set("X-EmbyProxy-Node-Credential", "wrong")
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("wrong credential %s status=%d", path, rec.Code)
		}
	}
	artifactReq := httptest.NewRequest(http.MethodGet, "/api/edge/artifact/"+node.ID+"/edge-agent", nil)
	artifactReq.Header.Set("X-EmbyProxy-Node-Credential", credential)
	artifactRec := httptest.NewRecorder()
	h.ServeHTTP(artifactRec, artifactReq)
	sum := sha256.Sum256(payload)
	if artifactRec.Code != http.StatusOK || artifactRec.Header().Get("X-EmbyProxy-Artifact-SHA256") != fmt.Sprintf("%x", sum) || string(artifactRec.Body.Bytes()) != string(payload) {
		t.Fatalf("artifact response status=%d hash=%q body=%q", artifactRec.Code, artifactRec.Header().Get("X-EmbyProxy-Artifact-SHA256"), artifactRec.Body.String())
	}
	snapshotReq := httptest.NewRequest(http.MethodGet, "/api/edge/config/"+node.ID, nil)
	h.cfg.PublicMediaNodePaths = map[string]string{"uhd": "/https/v1.uhdnow.com/443"}
	h.cfg.PublicMediaExtraPaths = map[string]string{"uhd-vod2": "/https/v1-vod2.uhdnow.com/443"}
	snapshotReq.Header.Set("X-EmbyProxy-Node-Credential", credential)
	snapshotRec := httptest.NewRecorder()
	h.ServeHTTP(snapshotRec, snapshotReq)
	if snapshotRec.Code != http.StatusOK || !strings.Contains(snapshotRec.Body.String(), `"slug":"demo"`) || !strings.Contains(snapshotRec.Body.String(), `"uhd":"/https/v1.uhdnow.com/443"`) || strings.Contains(snapshotRec.Body.String(), "strong-admin-token") || strings.Contains(snapshotRec.Body.String(), credential) {
		t.Fatalf("snapshot response status=%d body=%s", snapshotRec.Code, snapshotRec.Body.String())
	}
	if !strings.Contains(snapshotRec.Body.String(), `"uhd-vod2":"/https/v1-vod2.uhdnow.com/443"`) {
		t.Fatalf("extra legacy alias missing from edge snapshot: %s", snapshotRec.Body.String())
	}
}

func TestEdgeRuntimeOwnershipRequiresNodeCredentialAndPreservesDNSOwnership(t *testing.T) {
	h := newAuthTestHandler(t, config.Config{AdminToken: "strong-admin-token"})
	enrollment, token, err := h.store.CreateProxyNode(context.Background(), storage.ProxyNode{Name: "edge-ownership", ResetDay: 1}, 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	node, credential, err := h.store.CompleteEnrollment(context.Background(), enrollment.ID, token, "v1", "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.store.SetProxyNodeOwnershipBound(context.Background(), node.ID, "spaceship", "acct", "example.com", "record-1", "A", true, false, false, false, false); err != nil {
		t.Fatal(err)
	}
	path := "/api/edge/ownership/" + node.ID
	payload := `{"caddy_installed_by_project":true,"caddy_config_owned":true,"tls_state_owned":true,"edge_unit_owned":true}`
	for _, value := range []string{"", "wrong"} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		if value != "" {
			req.Header.Set("X-EmbyProxy-Node-Credential", value)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("credential %q status=%d", value, rec.Code)
		}
	}
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-EmbyProxy-Node-Credential", credential)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("ownership update=%d %s", rec.Code, rec.Body.String())
	}
	updated, err := h.store.GetProxyNode(context.Background(), node.ID)
	if err != nil || updated == nil {
		t.Fatal(err)
	}
	if !updated.CaddyInstalledByProject || !updated.CaddyConfigOwned || !updated.TLSStateOwned || !updated.EdgeUnitOwned {
		t.Fatalf("runtime ownership not recorded: %+v", updated)
	}
	if !updated.DNSOwned || updated.DNSRecordID != "record-1" || updated.DNSProvider != "spaceship" {
		t.Fatalf("DNS ownership changed: %+v", updated)
	}
	downgrade := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
	downgrade.Header.Set("Content-Type", "application/json")
	downgrade.Header.Set("X-EmbyProxy-Node-Credential", credential)
	downgradeRec := httptest.NewRecorder()
	h.ServeHTTP(downgradeRec, downgrade)
	if downgradeRec.Code != http.StatusOK {
		t.Fatalf("ownership assertion retry=%d %s", downgradeRec.Code, downgradeRec.Body.String())
	}
	updated, err = h.store.GetProxyNode(context.Background(), node.ID)
	if err != nil || !updated.CaddyInstalledByProject || !updated.CaddyConfigOwned || !updated.TLSStateOwned || !updated.EdgeUnitOwned {
		t.Fatalf("runtime ownership was downgraded: %+v err=%v", updated, err)
	}
}
