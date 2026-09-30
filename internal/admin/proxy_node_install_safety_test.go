package admin

import (
	"context"
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
	"embyproxy/internal/storage"
)

func installSafetyScript(t *testing.T) string {
	t.Helper()
	h := newAuthTestHandler(t, config.Config{EnrollmentControllerURL: "https://controller.example.net"})
	e, token, err := h.store.CreateProxyNode(context.Background(), storage.ProxyNode{Name: "install-safety", PublicAddress: "https://edge.example.net", ResetDay: 1}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.handleBootstrap(rec, httptest.NewRequest(http.MethodGet, "/", nil), e.ID, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("script=%d %s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

func TestInstallSafetyAPIAppendsUnlessExplicitAndNeverAutoEnables(t *testing.T) {
	h := newAuthTestHandler(t, config.Config{AdminToken: "strong-admin-token", EnrollmentControllerURL: "https://controller.example.net", PublicIngressHost: "stream.example.net"})
	ctx := context.Background()
	login := serveAdminJSON(t, h, http.MethodPost, "/admin/auth/login", map[string]any{"token": "strong-admin-token"}, nil)
	cookie := login.Result().Cookies()[0]
	for i, p := range []int{0, 15} {
		if _, _, err := h.store.CreateProxyNode(ctx, storage.ProxyNode{Name: fmt.Sprintf("peer-%d", i), Priority: p, ResetDay: 1}, time.Minute); err != nil {
			t.Fatal(err)
		}
	}
	s := newPublicIngressSwitcher(h)
	before := publicIngressState{OperationID: "keep-production", Phase: "verified", Mode: "preferred", ActiveNodeID: "current"}
	if err := s.save(ctx, before); err != nil {
		t.Fatal(err)
	}
	for i, want := range []int{16, 0} {
		payload := map[string]any{"name": fmt.Sprintf("new-%d", i), "public_address": "https://new.example.net", "reset_day": 1}
		if i == 1 {
			payload["priority"] = 0
		}
		rec := serveAdminJSON(t, h, http.MethodPost, "/api/admin/proxy-nodes", payload, cookie)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create=%d %s", rec.Code, rec.Body.String())
		}
		var body struct {
			Enrollment storage.Enrollment `json:"enrollment"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		n, err := h.store.GetProxyNode(ctx, body.Enrollment.NodeID)
		if err != nil || n.Enabled || n.Priority != want {
			t.Fatalf("node=%+v err=%v", n, err)
		}
		if _, err := h.store.DB().ExecContext(ctx, `UPDATE proxy_nodes SET state='healthy',last_heartbeat_at=?,playback_healthy=1,ingress_healthy=1,config_synced=1 WHERE id=?`, time.Now().Unix(), n.ID); err != nil {
			t.Fatal(err)
		}
	}
	nodes, err := h.store.ListProxyNodes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range nodes {
		if eligiblePublicIngressNode(node) {
			t.Fatalf("new node scheduled: %+v", node)
		}
	}
	if err := s.reconcile(ctx); err == nil || !strings.Contains(err.Error(), "no_eligible_public_ingress_candidate") {
		t.Fatalf("unexpected reconcile: %v", err)
	}
	if got := s.status(ctx); got.OperationID != before.OperationID || got.ActiveNodeID != before.ActiveNodeID || got.Mode != before.Mode {
		t.Fatalf("installation changed ingress: %+v", got)
	}
	for _, marker := range []string{"priorityInput === '' ? undefined", "proxyNodePriority').value=''"} {
		if !strings.Contains(indexHTML, marker) {
			t.Fatalf("UI default contract omitted %q", marker)
		}
	}
}

func TestBootstrapReadinessRuntimeAndAccurateSummary(t *testing.T) {
	script := installSafetyScript(t)
	for _, marker := range []string{"INSTALLATION: PASS", "INSTALLATION: STAGED", "ADMISSION: NOT CHECKED", "SCHEDULING: UNCHANGED", "business-domain TLS is not checked"} {
		if !strings.Contains(script, marker) {
			t.Fatalf("summary omitted %q", marker)
		}
	}
	if strings.Contains(script, "ADMISSION: PENDING") {
		t.Fatal("static pending status is misleading")
	}
	start := strings.Index(script, "  wait_edge_local_health() {")
	if start < 0 {
		t.Fatal("readiness function missing")
	}
	end := strings.Index(script[start:], "  wait_edge_local_health || exit 1") + start
	if start < 0 || end <= start {
		t.Fatal("readiness function missing")
	}
	function := script[start:end]
	for _, tc := range []struct {
		name                      string
		fail, timeout, failedUnit bool
	}{
		{"initial-refusal-then-success", false, false, false},
		{"persistent-refusal", true, false, false},
		{"deadline-expired", true, true, false},
		{"failed-service", true, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			mocks := fmt.Sprintf(`set -eu
state_dir='%s'
edge_probe=127.0.0.1:18080
date() {
  if [ ! -e "$state_dir/time" ]; then echo 100 > "$state_dir/time"; echo 100; return; fi
  n=$(cat "$state_dir/time")
  echo $((n + 1)) > "$state_dir/time"
  if [ '%t' = true ]; then echo 161; else echo "$n"; fi
}
systemctl() {
  if [ '%t' = true ]; then
    if [ "$1" = is-active ] && [ "${2:-}" = --quiet ]; then return 1; fi
    echo failed
  fi
  return 0
}
sleep() { :; }
edge_startup_diagnostics() { echo 'EDGE STARTUP: FAIL' >&2; }
curl() {
  if [ -e "$state_dir/tried" ] && [ '%t' = false ]; then printf 200; return 0; fi
  touch "$state_dir/tried"
  printf 000
  echo 'curl: (7) Connection refused' >&2
  return 7
}
`, dir, tc.timeout, tc.failedUnit, tc.fail)
			cmd := exec.Command("sh", "-c", mocks+function+"\nwait_edge_local_health || exit 1\n")
			out, err := cmd.CombinedOutput()
			if tc.fail {
				if err == nil || !strings.Contains(string(out), "EDGE STARTUP: FAIL") || strings.Contains(string(out), "LOCAL HEALTH: PASS") {
					t.Fatalf("failure hidden: err=%v out=%s", err, out)
				}
				if !tc.failedUnit && !tc.timeout && !strings.Contains(string(out), "curl: (7)") {
					t.Fatalf("last error omitted: %s", out)
				}
			} else if err != nil || !strings.Contains(string(out), "LOCAL HEALTH: PASS") || strings.Contains(string(out), "curl: (7)") {
				t.Fatalf("transient refusal leaked: err=%v out=%s", err, out)
			}
			leftovers, err := filepath.Glob(filepath.Join(dir, "startup-health.*"))
			if err != nil || len(leftovers) != 0 {
				t.Fatalf("health diagnostics left behind=%v err=%v", leftovers, err)
			}
		})
	}
	path := filepath.Join(t.TempDir(), "script.sh")
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("sh", "-n", path).CombinedOutput(); err != nil {
		t.Fatalf("shell syntax: %v %s", err, out)
	}
}
