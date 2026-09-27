package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"embyproxy/internal/config"
	"embyproxy/internal/spaceship"
	"embyproxy/internal/storage"
)

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
