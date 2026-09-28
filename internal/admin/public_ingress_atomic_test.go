package admin

import (
	"context"
	"testing"

	"embyproxy/internal/config"
)

func TestPublicIngressSaveIsAtomic(t *testing.T) {
	h := newAuthTestHandler(t, config.Config{PublicIngressHost: "stream.example.com"})
	ctx := context.Background()
	s := newPublicIngressSwitcher(h)
	old := publicIngressState{OperationID: "old", Phase: "verified", ActiveNodeID: "node-a"}
	if err := s.save(ctx, old); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.DB().ExecContext(ctx, `CREATE TRIGGER fail_current BEFORE UPDATE ON proxy_kv WHEN NEW.k='failover:public-ingress' BEGIN SELECT RAISE(FAIL, 'simulated failure'); END`); err != nil {
		t.Fatal(err)
	}
	next := publicIngressState{OperationID: "next", Phase: "submitting_dns", ActiveNodeID: "node-a"}
	if err := s.save(ctx, next); err == nil {
		t.Fatal("partial save accepted")
	}
	if got := s.status(ctx); got.OperationID != "old" || got.Phase != "verified" {
		t.Fatalf("current changed: %+v", got)
	}
	var history publicIngressState
	if ok, err := h.store.KV().GetJSON(ctx, publicIngressHistoryPrefix+next.OperationID, &history); err != nil || ok {
		t.Fatalf("partial history exists: ok=%v err=%v", ok, err)
	}
}
