package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestNextMonthlyResetUsesExplicitTimezone(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	got, err := NextMonthlyReset(now, 1, "Asia/Shanghai")
	if err != nil || got.Location().String() != "Asia/Shanghai" || got.Day() != 1 || got.Month() != time.October {
		t.Fatalf("reset=%v err=%v", got, err)
	}
}

func TestProxyNodeEnrollmentIsSingleUseAndCredentialScoped(t *testing.T) {
	store, err := New(filepath.Join(t.TempDir(), "proxy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	enrollment, token, err := store.CreateProxyNode(context.Background(), ProxyNode{Name: "edge-a", QuotaBytes: 1000, ResetDay: 1}, 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	node, credential, err := store.CompleteEnrollment(context.Background(), enrollment.ID, token, "v1", "abc")
	if err != nil || node.State != "installing" || credential == "" {
		t.Fatalf("node=%+v credential=%v err=%v", node, credential, err)
	}
	if _, _, err = store.CompleteEnrollment(context.Background(), enrollment.ID, token, "v1", "abc"); err == nil {
		t.Fatal("reused enrollment token accepted")
	}
	if err = store.HeartbeatProxyNode(context.Background(), node.ID, credential, "v1", "abc", "healthy", true, true, ""); err != nil {
		t.Fatal(err)
	}
	if err = store.HeartbeatProxyNode(context.Background(), node.ID, "wrong", "v1", "abc", "healthy", true, true, ""); err == nil {
		t.Fatal("wrong node credential accepted")
	}
}

func TestRegenerateProxyNodeEnrollmentRevokesPreviousUnconsumedToken(t *testing.T) {
	ctx := context.Background()
	store, err := New(filepath.Join(t.TempDir(), "proxy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	first, firstToken, err := store.CreateProxyNode(ctx, ProxyNode{Name: "edge-regen", ResetDay: 1}, 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	second, secondToken, err := store.RegenerateProxyNodeEnrollment(ctx, first.NodeID, 15*time.Minute)
	if err != nil || second.ID == first.ID || firstToken == secondToken {
		t.Fatalf("first=%+v second=%+v err=%v", first, second, err)
	}
	if err := store.ValidateEnrollment(ctx, first.ID, firstToken); err == nil {
		t.Fatal("superseded enrollment remained valid")
	}
	if err := store.ValidateEnrollment(ctx, second.ID, secondToken); err != nil {
		t.Fatalf("new enrollment rejected: %v", err)
	}
	if _, _, err := store.RegenerateProxyNodeEnrollment(ctx, first.NodeID, 15*time.Minute); err != nil {
		t.Fatalf("second regeneration: %v", err)
	}
	if _, _, err := store.RegenerateProxyNodeEnrollment(ctx, first.NodeID, 15*time.Minute); err != nil {
		t.Fatalf("third regeneration: %v", err)
	}
}

func TestRegenerateProxyNodeEnrollmentReopensRevokedUnadmittedNode(t *testing.T) {
	ctx := context.Background()
	store, err := New(filepath.Join(t.TempDir(), "proxy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	enrollment, _, err := store.CreateProxyNode(ctx, ProxyNode{Name: "edge-revoked-regen", ResetDay: 1}, 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RevokeProxyNode(ctx, enrollment.NodeID, true); err != nil {
		t.Fatal(err)
	}
	fresh, token, err := store.RegenerateProxyNodeEnrollment(ctx, enrollment.NodeID, 15*time.Minute)
	if err != nil || token == "" {
		t.Fatalf("fresh=%+v token=%q err=%v", fresh, token, err)
	}
	node, err := store.GetProxyNode(ctx, enrollment.NodeID)
	if err != nil || node == nil || node.State != "registered" || node.PlaybackHealthy || node.ConfigSynced {
		t.Fatalf("node=%+v err=%v", node, err)
	}
	if err := store.ValidateEnrollment(ctx, fresh.ID, token); err != nil {
		t.Fatalf("fresh enrollment invalid: %v", err)
	}
}

func TestProxyNodeOrderAndRevokePersist(t *testing.T) {
	store, err := New(filepath.Join(t.TempDir(), "proxy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	a, _, err := store.CreateProxyNode(context.Background(), ProxyNode{Name: "edge-a", ResetDay: 1}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := store.CreateProxyNode(context.Background(), ProxyNode{Name: "edge-b", ResetDay: 1}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.ReorderProxyNodes(context.Background(), []string{b.NodeID, a.NodeID}); err != nil {
		t.Fatal(err)
	}
	list, err := store.ListProxyNodes(context.Background())
	if err != nil || list[0].ID != b.NodeID {
		t.Fatalf("list=%+v err=%v", list, err)
	}
	if err = store.RevokeProxyNode(context.Background(), b.NodeID, false); err != nil {
		t.Fatal(err)
	}
	n, err := store.GetProxyNode(context.Background(), b.NodeID)
	if err != nil || n.State != "revoked" || n.Enabled {
		t.Fatalf("node=%+v err=%v", n, err)
	}
	if err = store.RevokeProxyNode(context.Background(), b.NodeID, true); err != nil {
		t.Fatal(err)
	}
	n, err = store.GetProxyNode(context.Background(), b.NodeID)
	if err != nil || n.State != "revoked" || n.Enabled {
		t.Fatalf("node=%+v err=%v", n, err)
	}
	if err = store.RecordProxyNodeUsage(context.Background(), a.NodeID, 500, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err = store.RecordProxyNodeUsage(context.Background(), a.NodeID, 100, time.Now()); err != nil {
		t.Fatal(err)
	}
	n, err = store.GetProxyNode(context.Background(), a.NodeID)
	if err != nil || n.UsedBytes != 500 {
		t.Fatalf("usage=%+v err=%v", n, err)
	}
}

func TestProxyNodeDrainWaitsForActiveConnections(t *testing.T) {
	store, err := New(filepath.Join(t.TempDir(), "proxy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	_, _, err = store.CreateProxyNode(ctx, ProxyNode{Name: "edge-drain", ResetDay: 1}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := store.ListProxyNodes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	id := nodes[0].ID
	if err := store.BeginProxyNodeConnection(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := store.DrainProxyNode(ctx, id); err != nil {
		t.Fatal(err)
	}
	if active, err := store.ProxyNodeActiveConnections(ctx, id); err != nil || active != 1 {
		t.Fatalf("active=%d err=%v", active, err)
	}
	if err := store.BeginProxyNodeConnection(ctx, id); err == nil {
		t.Fatal("draining node accepted a new connection")
	}
	if err := store.EndProxyNodeConnection(ctx, id); err != nil {
		t.Fatal(err)
	}
	n, err := store.GetProxyNode(ctx, id)
	if err != nil || n.State != "revoked" || n.Enabled {
		t.Fatalf("node after drain=%+v err=%v", n, err)
	}
}

func TestProxyNodeDrainWithoutConnectionsRevokesImmediately(t *testing.T) {
	ctx := context.Background()
	store, err := New(filepath.Join(t.TempDir(), "proxy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	enrollment, _, err := store.CreateProxyNode(ctx, ProxyNode{Name: "edge-empty-drain", ResetDay: 1}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DrainProxyNode(ctx, enrollment.NodeID); err != nil {
		t.Fatal(err)
	}
	node, err := store.GetProxyNode(ctx, enrollment.NodeID)
	if err != nil || node == nil || node.State != "revoked" || node.Enabled {
		t.Fatalf("node=%+v err=%v", node, err)
	}
}

func TestProxyNodeUsageAdvancesExpiredBillingCycle(t *testing.T) {
	ctx := context.Background()
	store, err := New(filepath.Join(t.TempDir(), "proxy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	enrollment, _, err := store.CreateProxyNode(ctx, ProxyNode{Name: "edge-cycle", ResetDay: 1, ResetTimezone: "UTC"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`UPDATE proxy_nodes SET used_bytes=900,next_reset_at=? WHERE id=?`, time.Now().Add(-time.Hour).Unix(), enrollment.NodeID); err != nil {
		t.Fatal(err)
	}
	if err := store.AddProxyNodeUsage(ctx, enrollment.NodeID, 100, time.Now()); err != nil {
		t.Fatal(err)
	}
	node, err := store.GetProxyNode(ctx, enrollment.NodeID)
	if err != nil || node == nil || node.UsedBytes != 100 || node.NextResetAt <= time.Now().Unix() {
		t.Fatalf("node=%+v err=%v", node, err)
	}
}

func TestProxyNodeSchemaBackfillsResetSchedule(t *testing.T) {
	ctx := context.Background()
	store, err := New(filepath.Join(t.TempDir(), "proxy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	enrollment, _, err := store.CreateProxyNode(ctx, ProxyNode{Name: "edge-backfill", ResetDay: 1, ResetTimezone: "UTC"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`UPDATE proxy_nodes SET next_reset_at=0 WHERE id=?`, enrollment.NodeID); err != nil {
		t.Fatal(err)
	}
	if err := store.backfillProxyNodeResetSchedules(ctx, time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	node, err := store.GetProxyNode(ctx, enrollment.NodeID)
	if err != nil || node == nil || node.NextResetAt == 0 {
		t.Fatalf("node=%+v err=%v", node, err)
	}
}
