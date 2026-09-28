package admin

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"embyproxy/internal/config"
	"embyproxy/internal/spaceship"
)

func TestPublicIngressRollbackRequiresPublicNodeIdentity(t *testing.T) {
	h := newAuthTestHandler(t, config.Config{PublicIngressHost: "stream.example.com"})
	puts := 0
	providerAddress := "1.1.1.1"
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			puts++
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []spaceship.Record{{Name: "stream", Type: "A", Address: providerAddress, TTL: 60}}, "total": 1})
	}))
	defer provider.Close()
	h.dnsAutomation = &spaceship.Client{BaseURL: provider.URL, APIKey: "k", APISecret: "s", ManagedDomain: "example.com"}
	s := newPublicIngressSwitcher(h)
	s.lookupHost = func(_ context.Context, _ string) ([]string, error) { return []string{"1.1.1.1"}, nil }
	observedNode := "wrong-node"
	public := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-EmbyProxy-Node-ID", observedNode)
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
	state := publicIngressState{OperationID: "rollback-mismatch", Phase: "waiting_recursive", ActiveNodeID: "old-node", PreviousAddress: "1.1.1.1", DesiredAddress: "2.2.2.2"}
	got, err := s.rollback(context.Background(), state, "public_request_failed", nil)
	if err == nil || got.Phase != "rollback_failed" || got.RollbackVerified || got.ObservedNodeID != "wrong-node" || puts != 0 {
		t.Fatalf("rollback=%+v err=%v puts=%d", got, err, puts)
	}
	if !strings.Contains(err.Error(), "public_node_identity_mismatch") {
		t.Fatalf("wrong rollback error: %v", err)
	}
	observedNode = "old-node"
	state.OperationID = "rollback-correct-node"
	got, err = s.rollback(context.Background(), state, "public_request_failed", nil)
	if err == nil || got.Phase != "rolled_back" || !got.RollbackVerified || got.ObservedNodeID != "old-node" || puts != 0 {
		t.Fatalf("correct node rollback=%+v err=%v puts=%d", got, err, puts)
	}
	state.OperationID = "recover-locked"
	state.Phase = "rollback_failed"
	if err := s.save(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	if _, err := s.confirmPreviousIngress(context.Background(), "wrong-id"); err == nil {
		t.Fatal("wrong operation unlocked recovery")
	}
	if stored := s.status(context.Background()); stored.Phase != "rollback_failed" {
		t.Fatalf("wrong-id changed state: %+v", stored)
	}
	providerAddress = "9.9.9.9"
	if _, err := s.confirmPreviousIngress(context.Background(), state.OperationID); err == nil || !strings.Contains(err.Error(), "provider_not_original") {
		t.Fatalf("third-party provider accepted: %v", err)
	}
	if stored := s.status(context.Background()); stored.Phase != "rollback_failed" {
		t.Fatalf("provider mismatch changed state: %+v", stored)
	}
	providerAddress = "1.1.1.1"
	observedNode = "wrong-node"
	if _, err := s.confirmPreviousIngress(context.Background(), state.OperationID); err == nil {
		t.Fatal("wrong public node unlocked recovery")
	}
	if stored := s.status(context.Background()); stored.Phase != "rollback_failed" {
		t.Fatalf("mismatch changed state: %+v", stored)
	}
	observedNode = "old-node"
	got, err = s.confirmPreviousIngress(context.Background(), state.OperationID)
	if err != nil || got.Phase != "rolled_back" || !got.RollbackVerified || got.ObservedNodeID != "old-node" || puts != 0 {
		t.Fatalf("recovery=%+v err=%v puts=%d", got, err, puts)
	}
}
