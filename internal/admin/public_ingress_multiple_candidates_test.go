package admin

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"embyproxy/internal/config"
	"embyproxy/internal/spaceship"
)

func TestPublicIngressAutomaticTriesNextPreDNSCandidate(t *testing.T) {
	h := newAuthTestHandler(t, config.Config{PublicIngressHost: "stream.example.com"})
	ctx := context.Background()
	puts := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			puts++
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []spaceship.Record{{Name: "stream", Type: "A", Address: "1.1.1.1", TTL: 60}}, "total": 1})
	}))
	defer provider.Close()
	h.dnsAutomation = &spaceship.Client{BaseURL: provider.URL, APIKey: "k", APISecret: "s", ManagedDomain: "example.com"}
	s := newPublicIngressSwitcher(h)
	s.lookupHost = func(_ context.Context, _ string) ([]string, error) { return []string{"1.1.1.1"}, nil }
	public := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-EmbyProxy-Node-ID", "old-node")
		_, _ = w.Write([]byte("ok"))
	}))
	defer public.Close()
	endpoint, err := url.Parse(public.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := public.Client()
	transport := client.Transport.(*http.Transport).Clone()
	transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, endpoint.Host)
	}
	client.Transport = transport
	s.httpClient = client
	verified := publicIngressState{OperationID: "old", Phase: "verified", Mode: "preferred", ActiveNodeID: "old-node", RecordName: "stream.example.com", DesiredAddress: "1.1.1.1", RequestVerified: true}
	if err := s.save(ctx, verified); err != nil {
		t.Fatal(err)
	}
	failed := publicIngressState{OperationID: "failed", PriorVerifiedID: "old", Phase: "failed", Error: "target_not_eligible", Mode: "preferred", ActiveNodeID: "old-node"}
	if err := s.save(ctx, failed); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	// Insert in the opposite order to the configured failover priority.
	for _, candidate := range []struct {
		id       string
		priority int
	}{{"edge-two", 2}, {"edge-one", 0}} {
		if _, err := h.store.DB().ExecContext(ctx, `INSERT INTO proxy_nodes (id,name,public_address,enabled,state,priority,quota_bytes,used_bytes,reset_day,reset_timezone,next_reset_at,last_heartbeat_at,playback_healthy,ingress_healthy,config_synced,agent_version,agent_commit,credential_hash,last_error,created_at,updated_at) VALUES (?,?,?,1,'healthy',?,0,0,1,'UTC',0,?,1,1,1,'v','test','','',?,?)`, candidate.id, candidate.id, "https://127.0.0.1", candidate.priority, now, now, now); err != nil {
			t.Fatal(err)
		}
	}
	ordered, err := h.store.ListProxyNodes(ctx)
	if err != nil || len(ordered) != 2 || ordered[0].ID != "edge-one" || ordered[1].ID != "edge-two" {
		t.Fatalf("fallback priority order: %+v err=%v", ordered, err)
	}
	if err := h.store.SetProxyNodePriority(ctx, "edge-two", 0); err != nil {
		t.Fatal(err)
	}
	ordered, err = h.store.ListProxyNodes(ctx)
	if err != nil || ordered[0].ID != "edge-two" || ordered[0].Priority != 0 || ordered[1].ID != "edge-one" || ordered[1].Priority != 1 {
		t.Fatalf("updated fallback order: %+v err=%v", ordered, err)
	}
	var attempts []string
	if err := s.reconcile(ctx); err == nil {
		t.Fatal("two unreachable edges accepted")
	}
	if got := s.status(ctx); got.OperationID != verified.OperationID || got.ActiveNodeID != "old-node" || got.Phase != "verified" {
		t.Fatalf("original ingress lost: %+v", got)
	}
	if puts != 0 {
		t.Fatalf("preflight wrote DNS: %d", puts)
	}
	rows, err := h.store.DB().QueryContext(ctx, `SELECT v FROM proxy_kv WHERE k LIKE 'failover:public-ingress:operation:%' ORDER BY rowid`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seen := map[string]int{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var item publicIngressState
		if json.Unmarshal([]byte(raw), &item) == nil && item.Error == "target_preflight_failed" {
			seen[item.RequestedNodeID]++
			attempts = append(attempts, item.RequestedNodeID)
		}
	}
	if seen["edge-one"] != 1 || seen["edge-two"] != 1 {
		t.Fatalf("candidate attempts=%v", seen)
	}
	if len(attempts) != 2 || attempts[0] != "edge-two" || attempts[1] != "edge-one" {
		t.Fatalf("scheduler ignored updated order: %v", attempts)
	}
	unsafe := failed
	unsafe.OperationID, unsafe.PriorVerifiedID = "missing-history", "nonexistent"
	if err := s.save(ctx, unsafe); err != nil {
		t.Fatal(err)
	}
	if err := s.reconcile(ctx); err == nil {
		t.Fatal("unknown original history allowed automatic switching")
	}
	if got := s.status(ctx); got.OperationID != "missing-history" {
		t.Fatalf("missing history state overwritten: %+v", got)
	}
	if puts != 0 {
		t.Fatalf("missing history wrote DNS: %d", puts)
	}
}
