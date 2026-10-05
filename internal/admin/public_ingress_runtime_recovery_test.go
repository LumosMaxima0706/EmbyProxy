package admin

import (
	"context"
	"embyproxy/internal/config"
	"embyproxy/internal/spaceship"
	"embyproxy/internal/storage"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestAutomaticHealthNeverRollsBackToDeadNode(t *testing.T) {
	h := newAuthTestHandler(t, config.Config{PublicIngressHost: "stream.example.com"})
	s := newPublicIngressSwitcher(h)
	state := publicIngressState{OperationID: "dead-predecessor", Trigger: "automatic_health", Mode: "preferred", ActiveNodeID: "reinstalled", PreviousAddress: "1.1.1.1", DesiredAddress: "2.2.2.2"}
	// No provider: any attempt to read/write DNS would panic.
	got, err := s.rollback(context.Background(), state, "recursive_verification_failed", nil)
	if err == nil || got.Phase != "failover_pending" || got.RollbackAttempted {
		t.Fatalf("unsafe rollback: %+v %v", got, err)
	}
}

func TestAutomaticHealthRecoveryRejectsUnknownDNSAndManual(t *testing.T) {
	h := newAuthTestHandler(t, config.Config{PublicIngressHost: "stream.example.com"})
	puts := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			puts++
			w.WriteHeader(204)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"items": []spaceship.Record{{Name: "stream", Type: "A", Address: "9.9.9.9", TTL: 60}}, "total": 1})
	}))
	defer provider.Close()
	h.dnsAutomation = &spaceship.Client{BaseURL: provider.URL, APIKey: "k", APISecret: "s", ManagedDomain: "example.com"}
	s := newPublicIngressSwitcher(h)
	state := publicIngressState{OperationID: "stuck", Phase: "rollback_failed", Trigger: "automatic_health", Mode: "preferred", RecordName: s.record, ActiveNodeID: "dead", PreviousAddress: "1.1.1.1", DesiredAddress: "2.2.2.2"}
	if err := s.save(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	if err := s.reconcile(context.Background()); err == nil || !strings.Contains(err.Error(), "changed_concurrently") {
		t.Fatalf("unknown DNS accepted %v", err)
	}
	state.Trigger = "admin_manual"
	s.save(context.Background(), state)
	if err := s.reconcile(context.Background()); err == nil || !strings.Contains(err.Error(), "recovery_required") {
		t.Fatalf("manual unlocked %v", err)
	}
	if puts != 0 {
		t.Fatalf("wrote unknown DNS %d", puts)
	}
}

func TestAutomaticHealthNoCandidatesRetriesAfterRestart(t *testing.T) {
	h := newAuthTestHandler(t, config.Config{PublicIngressHost: "stream.example.com"})
	puts := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			puts++
			w.WriteHeader(204)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"items": []spaceship.Record{{Name: "stream", Type: "A", Address: "1.1.1.1", TTL: 60}}, "total": 1})
	}))
	defer provider.Close()
	h.dnsAutomation = &spaceship.Client{BaseURL: provider.URL, APIKey: "k", APISecret: "s", ManagedDomain: "example.com"}
	s := newPublicIngressSwitcher(h)
	state := publicIngressState{OperationID: "stuck", Phase: "rollback_failed", Trigger: "automatic_health", Mode: "preferred", RecordName: s.record, ActiveNodeID: "dead", PreviousAddress: "1.1.1.1", DesiredAddress: "2.2.2.2"}
	s.save(context.Background(), state)
	for i := 0; i < 2; i++ {
		s = newPublicIngressSwitcher(h)
		if err := s.reconcileWithWarning(context.Background()); err == nil || err.Error() != "no_eligible_public_ingress_candidate" {
			t.Fatalf("cycle=%d err=%v", i, err)
		}
		if got := s.status(context.Background()); got.Phase != "failover_pending" {
			t.Fatalf("locked %+v", got)
		}
		if got := s.schedulerStatus(context.Background()); got.LastAttemptAt == 0 || got.NextRunAt == 0 {
			t.Fatalf("watchdog missing %+v", got)
		}
	}
	if puts != 0 {
		t.Fatalf("no candidates wrote DNS %d", puts)
	}
}

func TestAutomaticHealthPreDNSRetryDoesNotRequireDeadPublicEntry(t *testing.T) {
	h := newAuthTestHandler(t, config.Config{PublicIngressHost: "stream.example.com"})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"items": []spaceship.Record{{Name: "stream", Type: "A", Address: "1.1.1.1", TTL: 60}}, "total": 1})
	}))
	defer provider.Close()
	h.dnsAutomation = &spaceship.Client{BaseURL: provider.URL, APIKey: "k", APISecret: "s", ManagedDomain: "example.com"}
	s := newPublicIngressSwitcher(h)
	s.lookupHost = func(context.Context, string) ([]string, error) { return []string{"1.1.1.1"}, nil }
	verified := publicIngressState{OperationID: "verified", Phase: "verified", Mode: "preferred", ActiveNodeID: "dead", RecordName: s.record, DesiredAddress: "1.1.1.1", RequestVerified: true}
	s.save(context.Background(), verified)
	failed := publicIngressState{OperationID: "failed", PriorVerifiedID: "verified", Phase: "failed", Trigger: "automatic_health", Mode: "preferred", ActiveNodeID: "dead"}
	if _, err := s.verifiedBeforeFailure(context.Background(), failed); err != nil {
		t.Fatal(err)
	}
}

func TestAutomaticHealthRecoveryKeepsPendingAfterCandidatesFail(t *testing.T) {
	h := newAuthTestHandler(t, config.Config{PublicIngressHost: "stream.example.com"})
	// TLS client and DNS provider are isolated. Preflight deliberately fails for
	// first candidate; retry stays pending instead of returning dead predecessor.
	puts := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			puts++
			w.WriteHeader(204)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"items": []spaceship.Record{{Name: "stream", Type: "A", Address: "2.2.2.2", TTL: 60}}, "total": 1})
	}))
	defer provider.Close()
	h.dnsAutomation = &spaceship.Client{BaseURL: provider.URL, APIKey: "k", APISecret: "s", ManagedDomain: "example.com"}
	s := newPublicIngressSwitcher(h)
	now := time.Now().Unix()
	for i, id := range []string{"candidate-first", "candidate-second"} {
		_, err := h.store.DB().Exec(`INSERT INTO proxy_nodes (id,name,public_address,enabled,state,priority,last_heartbeat_at,playback_healthy,ingress_healthy,config_synced,reset_day,reset_timezone,created_at,updated_at) VALUES (?,?,?,1,'healthy',?, ?,1,1,1,1,'UTC',?,?)`, id, id, "https://127.0.0.1", i, now, now, now)
		if err != nil {
			t.Fatal(err)
		}
	}
	state := publicIngressState{OperationID: "already-updated", Phase: "failover_pending", Trigger: "automatic_health", Mode: "preferred", RecordName: s.record, ActiveNodeID: "dead", PreviousAddress: "1.1.1.1", DesiredAddress: "2.2.2.2"}
	s.save(context.Background(), state)
	if err := s.reconcile(context.Background()); err == nil || err.Error() != "no_eligible_public_ingress_candidate" {
		t.Fatalf("err=%v", err)
	}
	if got := s.status(context.Background()); got.Phase != "failover_pending" || got.RollbackAttempted {
		t.Fatalf("unsafe %+v", got)
	}
	if puts != 0 {
		t.Fatalf("failed preflight wrote DNS %d", puts)
	}
}

// Reuse strict public identity validation during recovery without pooled connections.
func TestAutomaticRecoveryFreshRequest(t *testing.T) {
	h := newAuthTestHandler(t, config.Config{PublicIngressHost: "stream.example.com"})
	s := newPublicIngressSwitcher(h)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-EmbyProxy-Node-ID", "candidate")
		w.Write([]byte("ok"))
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	client := server.Client()
	tr := client.Transport.(*http.Transport).Clone()
	tr.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, u.Host)
	}
	client.Transport = tr
	s.httpClient = client
	if id, err := s.verifyPublicRequest(context.Background(), "candidate"); err != nil || id != "candidate" {
		t.Fatalf("%s %v", id, err)
	}
	if _, err := s.verifyPublicRequest(context.Background(), "wrong"); err == nil {
		t.Fatal("wrong identity accepted")
	}
}

func TestRuntimeMonitorDetectsMediaFailureAndStaleData(t *testing.T) {
	h := newAuthTestHandler(t, config.Config{PublicIngressHost: "stream.example.com"})
	h.playbackCredentials = &filePlaybackCredentialStore{dir: t.TempDir()}
	s := newPublicIngressSwitcher(h)
	node := storage.ProxyNode{ID: "live"}
	ctx := context.Background()
	for _, test := range []struct {
		age      int64
		failures int
		healthy  bool
	}{{0, 0, true}, {0, 1, true}, {0, 2, false}, {400, 0, false}} {
		h.store.KV().Put(ctx, "node-probe-monitor:live", nodeProbeMonitor{LastAttemptAt: time.Now().Unix() - test.age, Failures: test.failures})
		if got := s.runtimeIngressHealthy(ctx, node); got != test.healthy {
			t.Fatalf("case %+v got %v", test, got)
		}
	}
}
