package admin

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"embyproxy/internal/config"
	"embyproxy/internal/spaceship"
	"time"
)

func TestPreDNSFailureRechecksOriginalIngress(t *testing.T) {
	h := newAuthTestHandler(t, config.Config{PublicIngressHost: "stream.example.com"})
	ctx := context.Background()
	s := newPublicIngressSwitcher(h)
	address := "1.1.1.1"
	puts := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			puts++
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []spaceship.Record{{Name: "stream", Type: "A", Address: address, TTL: 60}}, "total": 1})
	}))
	defer provider.Close()
	h.dnsAutomation = &spaceship.Client{BaseURL: provider.URL, APIKey: "k", APISecret: "s", ManagedDomain: "example.com"}
	answers := []string{"1.1.1.1"}
	s.lookupHost = func(_ context.Context, _ string) ([]string, error) { return answers, nil }
	observed := "old-node"
	public := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-EmbyProxy-Node-ID", observed)
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
	verified := publicIngressState{OperationID: "old-op", Phase: "verified", Mode: "preferred", ActiveNodeID: "old-node", RecordName: "stream.example.com", DesiredAddress: "1.1.1.1", RequestVerified: true}
	if err := s.save(ctx, verified); err != nil {
		t.Fatal(err)
	}
	failed := publicIngressState{OperationID: "failed-op", PriorVerifiedID: "old-op", Phase: "failed", Error: "target_not_eligible", Mode: "preferred", ActiveNodeID: "old-node"}
	if err := s.save(ctx, failed); err != nil {
		t.Fatal(err)
	}
	address = "9.9.9.9"
	if err := s.reconcile(ctx); err == nil {
		t.Fatal("changed provider accepted")
	}
	if got := s.status(ctx); got.OperationID != "failed-op" {
		t.Fatalf("provider mismatch overwrote failure: %+v", got)
	}
	address = "1.1.1.1"
	answers = []string{"1.1.1.1", "9.9.9.9"}
	if err := s.reconcile(ctx); err == nil {
		t.Fatal("mixed recursive answers accepted")
	}
	if got := s.status(ctx); got.OperationID != "failed-op" {
		t.Fatalf("mixed answers overwrote failure: %+v", got)
	}
	answers = []string{"1.1.1.1"}
	observed = "wrong-node"
	if err := s.reconcile(ctx); err == nil {
		t.Fatal("wrong public node accepted")
	}
	if got := s.status(ctx); got.OperationID != "failed-op" {
		t.Fatalf("wrong node overwrote failure: %+v", got)
	}
	observed = "old-node"
	if err := s.reconcile(ctx); err == nil {
		t.Fatal("missing candidate should be reported")
	}
	if got := s.status(ctx); got.OperationID != "old-op" || got.Phase != "verified" || got.ActiveNodeID != "old-node" {
		t.Fatalf("verified state not restored: %+v", got)
	}
	var historical publicIngressState
	if ok, err := h.store.KV().GetJSON(ctx, publicIngressHistoryPrefix+failed.OperationID, &historical); err != nil || !ok || historical.Phase != "failed" {
		t.Fatalf("failed history erased: %+v ok=%v err=%v", historical, ok, err)
	}
	if puts != 0 {
		t.Fatalf("pre-DNS recovery wrote provider: %d", puts)
	}
	now := time.Now().Unix()
	if _, err := h.store.DB().ExecContext(ctx, `INSERT INTO proxy_nodes (id,name,public_address,enabled,state,priority,quota_bytes,used_bytes,reset_day,reset_timezone,next_reset_at,last_heartbeat_at,playback_healthy,ingress_healthy,config_synced,agent_version,agent_commit,credential_hash,last_error,created_at,updated_at) VALUES ('backup','backup','https://127.0.0.1',1,'healthy',1,0,0,1,'UTC',0,?,1,1,1,'v','test','','',?,?)`, now, now, now); err != nil {
		t.Fatal(err)
	}
	if err := s.save(ctx, failed); err != nil {
		t.Fatal(err)
	}
	if err := s.reconcile(ctx); err == nil {
		t.Fatal("unreachable backup accepted")
	}
	if got := s.status(ctx); got.OperationID != "old-op" || got.Phase != "verified" || got.ActiveNodeID != "old-node" {
		t.Fatalf("original ingress not retained: %+v", got)
	}
	if puts != 0 {
		t.Fatalf("backup preflight wrote provider: %d", puts)
	}
	unsafe := failed
	unsafe.OperationID = "unsafe-after-dns"
	unsafe.Error = "provider_update_unverified"
	if err := s.save(ctx, unsafe); err != nil {
		t.Fatal(err)
	}
	if err := s.reconcile(ctx); err == nil {
		t.Fatal("post-DNS failure resumed automatically")
	}
	if got := s.status(ctx); got.OperationID != unsafe.OperationID {
		t.Fatalf("post-DNS state overwritten: %+v", got)
	}
	unsafe.OperationID, unsafe.Error, unsafe.PriorVerifiedID = "missing-history", "target_not_eligible", "unknown-history"
	if err := s.save(ctx, unsafe); err != nil {
		t.Fatal(err)
	}
	if err := s.reconcile(ctx); err == nil {
		t.Fatal("missing verified history resumed")
	}
	if got := s.status(ctx); got.OperationID != unsafe.OperationID {
		t.Fatalf("missing history overwrote state: %+v", got)
	}
}
