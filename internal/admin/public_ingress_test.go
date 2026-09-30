package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"embyproxy/internal/config"
	"embyproxy/internal/spaceship"
	"embyproxy/internal/storage"
)

func TestPublicIngressOperationSurvivesDatabaseRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "controller.db")
	store, err := storage.New(path)
	if err != nil {
		t.Fatal(err)
	}
	h := &Handler{cfg: config.Config{PublicIngressHost: "stream.example.com"}, store: store}
	s := newPublicIngressSwitcher(h)
	ctx := context.Background()
	verified := publicIngressState{OperationID: "verified-old", Phase: "verified", Mode: "fixed", ActiveNodeID: "node-a", RequestVerified: true}
	if err := s.save(ctx, verified); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = storage.New(path)
	if err != nil {
		t.Fatal(err)
	}
	s = newPublicIngressSwitcher(&Handler{cfg: h.cfg, store: store})
	if got := s.status(ctx); got.OperationID != verified.OperationID || got.ActiveNodeID != verified.ActiveNodeID || !got.RequestVerified {
		t.Fatalf("verified operation lost: %+v", got)
	}
	stuck := publicIngressState{OperationID: "pending-dns", Phase: "submitting_dns", Mode: "fixed", ActiveNodeID: "node-a"}
	if err := s.save(ctx, stuck); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = storage.New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	s = newPublicIngressSwitcher(&Handler{cfg: h.cfg, store: store})
	if got := s.status(ctx); got.Phase != "submitting_dns" || got.OperationID != stuck.OperationID {
		t.Fatalf("in-flight operation lost on restart: %+v", got)
	}
	if got, err := s.switchTo(ctx, "node-b", "admin_manual", "preferred"); err == nil || got.OperationID != stuck.OperationID {
		t.Fatalf("in-flight state was overwritten: %+v err=%v", got, err)
	}
}
func TestPublicIngressRecoveryAndFixedMode(t *testing.T) {
	h := newAuthTestHandler(t, config.Config{PublicIngressHost: "stream.example.com"})
	s := newPublicIngressSwitcher(h)
	ctx := context.Background()
	old := publicIngressState{OperationID: "sw-old", Phase: "verified", Mode: "fixed", ActiveNodeID: "old", CompletedAt: time.Now().Unix()}
	if err := s.save(ctx, old); err != nil {
		t.Fatal(err)
	}
	failed, err := s.switchTo(ctx, "missing", "admin_manual", "preferred")
	if err == nil || failed.Phase != "failed" || failed.ActiveNodeID != "old" || failed.Mode != "fixed" {
		t.Fatalf("state=%+v err=%v", failed, err)
	}
	if err := s.reconcile(ctx); err != nil {
		t.Fatalf("fixed mode: %v", err)
	}
	stuck := publicIngressState{OperationID: "sw-stuck", Phase: "submitting_dns", Mode: "fixed", ActiveNodeID: "old"}
	if err := s.save(ctx, stuck); err != nil {
		t.Fatal(err)
	}
	s = newPublicIngressSwitcher(h)
	got, err := s.switchTo(ctx, "new", "admin_manual", "preferred")
	if err == nil || !strings.Contains(err.Error(), "recovery_required") || got.OperationID != stuck.OperationID {
		t.Fatalf("state=%+v err=%v", got, err)
	}
	if err := s.reconcile(ctx); err == nil || !strings.Contains(err.Error(), "recovery_required") {
		t.Fatalf("reconcile: %v", err)
	}
	var historical publicIngressState
	if ok, err := h.store.KV().GetJSON(ctx, publicIngressHistoryPrefix+stuck.OperationID, &historical); err != nil || !ok || historical.Phase != stuck.Phase {
		t.Fatalf("history=%+v ok=%v err=%v", historical, ok, err)
	}
}
func TestPublicIngressCooldownDoesNotHideHealthFailure(t *testing.T) {
	h := newAuthTestHandler(t, config.Config{PublicIngressHost: "stream.example.com", AdminToken: "strong-admin-token"})
	ctx := context.Background()
	enrollment, _, err := h.store.CreateProxyNode(ctx, storage.ProxyNode{Name: "active", PublicAddress: "https://edge.example.com", ResetDay: 1}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	s := newPublicIngressSwitcher(h)
	state := publicIngressState{OperationID: "sw-recent", Phase: "verified", Mode: "preferred", ActiveNodeID: enrollment.NodeID, CompletedAt: time.Now().Unix()}
	if err := s.save(ctx, state); err != nil {
		t.Fatal(err)
	}
	if err := s.reconcile(ctx); err == nil || !strings.Contains(err.Error(), "no_eligible_public_ingress_candidate") {
		t.Fatalf("unhealthy node held by cooldown: %v", err)
	}
	status := newPublicIngressSwitcher(h).schedulerStatus(ctx)
	if status.Error != "no_eligible_public_ingress_candidate" || status.Trigger != "automatic_health" || status.UpdatedAt == 0 {
		t.Fatalf("scheduler status was not persisted: %+v", status)
	}
	if got := s.status(ctx); got.OperationID != state.OperationID || got.Phase != "verified" {
		t.Fatalf("scheduler warning overwrote verified transaction: %+v", got)
	}
	h.SetDNSAutomationClient(&spaceship.Client{ManagedDomain: "example.com"})
	login := serveAdminJSON(t, h, http.MethodPost, "/admin/auth/login", map[string]any{"token": "strong-admin-token"}, nil)
	response := serveAdminJSON(t, h, http.MethodGet, "/api/admin/public-ingress/status", nil, login.Result().Cookies()[0])
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"error":"no_eligible_public_ingress_candidate"`) || !strings.Contains(response.Body.String(), state.OperationID) {
		t.Fatalf("status API did not expose warning and operation: %d %s", response.Code, response.Body.String())
	}
}
func TestPublicIngressThresholdCooldown(t *testing.T) {
	h := newAuthTestHandler(t, config.Config{PublicIngressHost: "stream.example.com"})
	ctx := context.Background()
	enrollment, _, err := h.store.CreateProxyNode(ctx, storage.ProxyNode{Name: "active-threshold", PublicAddress: "https://edge.example.com", ResetDay: 1}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	s := newPublicIngressSwitcher(h)
	state := publicIngressState{OperationID: "sw-threshold", Phase: "verified", Mode: "preferred", ActiveNodeID: enrollment.NodeID, CompletedAt: time.Now().Unix()}
	if err := s.save(ctx, state); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	if _, err := h.store.DB().ExecContext(ctx, `UPDATE proxy_nodes SET enabled=1,state='healthy',last_heartbeat_at=?,playback_healthy=1,ingress_healthy=1,config_synced=1,quota_bytes=100,used_bytes=100 WHERE id=?`, now, enrollment.NodeID); err != nil {
		t.Fatal(err)
	}
	if err := s.reconcile(ctx); err != nil {
		t.Fatalf("healthy threshold should respect cooldown: %v", err)
	}
	state.CompletedAt = time.Now().Add(-2 * time.Hour).Unix()
	if err := s.save(ctx, state); err != nil {
		t.Fatal(err)
	}
	if err := s.reconcile(ctx); err == nil || !strings.Contains(err.Error(), "no_eligible_public_ingress_candidate") {
		t.Fatalf("expired threshold cooldown should report no candidate: %v", err)
	}
	state.Mode = "fixed"
	state.CompletedAt = now
	if err := s.save(ctx, state); err != nil {
		t.Fatal(err)
	}
	if err := s.reconcile(ctx); err != nil {
		t.Fatalf("fixed mode must not auto-switch: %v", err)
	}
	if warning := s.schedulerStatus(ctx); warning.Error != "" {
		t.Fatalf("fixed mode retained automatic warning: %+v", warning)
	}
}

func TestPublicIngressRejectsIPv6Record(t *testing.T) {
	h := newAuthTestHandler(t, config.Config{PublicIngressHost: "stream.example.com"})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []spaceship.Record{{Name: "stream", Type: "AAAA", Address: "2001:db8::1"}}, "total": 1})
	}))
	defer srv.Close()
	h.dnsAutomation = &spaceship.Client{BaseURL: srv.URL, ManagedDomain: "example.com", APIKey: "k", APISecret: "s"}
	if err := newPublicIngressSwitcher(h).rejectIPv6(context.Background()); err == nil || !strings.Contains(err.Error(), "ipv6_record_present") {
		t.Fatalf("AAAA allowed: %v", err)
	}
}

func TestPublicIngressRollbackNeverOverwritesConcurrentDNS(t *testing.T) {
	h := newAuthTestHandler(t, config.Config{PublicIngressHost: "stream.example.com"})
	puts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			puts++
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []spaceship.Record{{Name: "stream", Type: "A", Address: "3.3.3.3", TTL: 60}}, "total": 1})
	}))
	defer srv.Close()
	h.dnsAutomation = &spaceship.Client{BaseURL: srv.URL, ManagedDomain: "example.com", APIKey: "k", APISecret: "s"}
	s := newPublicIngressSwitcher(h)
	state := publicIngressState{OperationID: "sw-rollback", Phase: "waiting_recursive", ActiveNodeID: "old", PreviousAddress: "1.1.1.1", DesiredAddress: "2.2.2.2"}
	if err := s.save(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	got, err := s.rollback(context.Background(), state, "verification_failed", nil)
	if err == nil || got.Phase != "rollback_failed" || puts != 0 {

		t.Fatalf("state=%+v err=%v writes=%d", got, err, puts)
	}
}

func TestPublicIngressUninitializedSchedulerDoesNotWriteDNS(t *testing.T) {
	h := newAuthTestHandler(t, config.Config{PublicIngressHost: "stream.example.com"})
	ctx := context.Background()
	_, _, err := h.store.CreateProxyNode(ctx, storage.ProxyNode{Name: "candidate", PublicAddress: "https://edge.example.com", ResetDay: 1, ThresholdPercent: 95}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := newPublicIngressSwitcher(h).reconcile(ctx); err == nil || !strings.Contains(err.Error(), "not_initialized") {
		t.Fatalf("scheduler unexpectedly ran: %v", err)
	}
}
