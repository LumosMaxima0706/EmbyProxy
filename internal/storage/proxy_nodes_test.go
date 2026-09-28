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
	if err = store.HeartbeatProxyNodeWithCapability(context.Background(), node.ID, credential, "v1", "abc", "healthy", true, true, "", true); err != nil {
		t.Fatal(err)
	}
	updated, err := store.GetProxyNode(context.Background(), node.ID)
	if err != nil || updated == nil || !updated.DecommissionCapable {
		t.Fatalf("decommission capability not persisted: node=%+v err=%v", updated, err)
	}
	if err = store.HeartbeatProxyNode(context.Background(), node.ID, "wrong", "v1", "abc", "healthy", true, true, ""); err == nil {
		t.Fatal("wrong node credential accepted")
	}
}

func TestProxyNodeTrafficThresholdPersistsAndDefaults(t *testing.T) {
	ctx := context.Background()
	store, err := New(filepath.Join(t.TempDir(), "proxy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	first, _, err := store.CreateProxyNode(ctx, ProxyNode{Name: "edge-threshold-default", ResetDay: 1}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	node, err := store.GetProxyNode(ctx, first.NodeID)
	if err != nil || node.ThresholdPercent != 100 {
		t.Fatalf("default node=%+v err=%v", node, err)
	}
	second, _, err := store.CreateProxyNode(ctx, ProxyNode{Name: "edge-threshold-custom", ThresholdPercent: 82.5, ResetDay: 1}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	node, err = store.GetProxyNode(ctx, second.NodeID)
	if err != nil || node.ThresholdPercent != 82.5 {
		t.Fatalf("custom node=%+v err=%v", node, err)
	}
	node.ThresholdPercent = 77
	if err := store.UpdateProxyNode(ctx, *node); err != nil {
		t.Fatal(err)
	}
	node, err = store.GetProxyNode(ctx, second.NodeID)
	if err != nil || node.ThresholdPercent != 77 {
		t.Fatalf("updated node=%+v err=%v", node, err)
	}
}

func TestProxyNodeIngressHealthIsIndependentFromPlayback(t *testing.T) {
	ctx := context.Background()
	store, err := New(filepath.Join(t.TempDir(), "proxy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	enrollment, token, err := store.CreateProxyNode(ctx, ProxyNode{Name: "edge-ingress", PublicAddress: "https://edge.example.net", ResetDay: 1}, 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	node, credential, err := store.CompleteEnrollment(ctx, enrollment.ID, token, "v1", "abc")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.HeartbeatProxyNode(ctx, node.ID, credential, "v1", "abc", "healthy", true, true, ""); err != nil {
		t.Fatal(err)
	}
	nodeView, err := store.GetProxyNode(ctx, node.ID)
	if err != nil || nodeView.PlaybackHealthy != true || nodeView.IngressHealthy {
		t.Fatalf("before ingress probe node=%+v err=%v", node, err)
	}
	if err := store.SetProxyNodeIngressHealth(ctx, node.ID, true, ""); err != nil {
		t.Fatal(err)
	}
	nodeView, err = store.GetProxyNode(ctx, node.ID)
	if err != nil || !nodeView.PlaybackHealthy || !nodeView.IngressHealthy || nodeView.State != "healthy" {
		t.Fatalf("after ingress probe node=%+v err=%v", node, err)
	}
	if err := store.SetProxyNodeIngressHealth(ctx, node.ID, false, "ingress_probe_unreachable"); err != nil {
		t.Fatal(err)
	}
	nodeView, err = store.GetProxyNode(ctx, node.ID)
	if err != nil || !nodeView.PlaybackHealthy || nodeView.IngressHealthy || nodeView.State != "degraded" {
		t.Fatalf("after ingress failure node=%+v err=%v", node, err)
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

func TestRegenerateProxyNodeEnrollmentAllowsHealthyNodeWithoutChangingIdentity(t *testing.T) {
	ctx := context.Background()
	store, err := New(filepath.Join(t.TempDir(), "proxy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	enrollment, token, err := store.CreateProxyNode(ctx, ProxyNode{Name: "edge-healthy-regen", ResetDay: 1}, 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, credential, err := store.CompleteEnrollment(ctx, enrollment.ID, token, "v1", "test"); err != nil {
		t.Fatal(err)
	} else if err := store.HeartbeatProxyNode(ctx, enrollment.NodeID, credential, "v1", "test", "healthy", true, true, ""); err != nil {
		t.Fatal(err)
	}
	if err := store.SetProxyNodeIngressHealth(ctx, enrollment.NodeID, true, ""); err != nil {
		t.Fatal(err)
	}
	fresh, _, err := store.RegenerateProxyNodeEnrollment(ctx, enrollment.NodeID, 15*time.Minute)
	if err != nil || fresh.NodeID != enrollment.NodeID {
		t.Fatalf("fresh=%+v err=%v", fresh, err)
	}
	node, err := store.GetProxyNode(ctx, enrollment.NodeID)
	if err != nil || node == nil || node.State != "healthy" || !node.PlaybackHealthy || !node.IngressHealthy {
		t.Fatalf("node=%+v err=%v", node, err)
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

func TestLifecycleDrainKeepsIdentityAndDisabledCanReenable(t *testing.T) {
	ctx := context.Background()
	store, err := New(filepath.Join(t.TempDir(), "proxy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	enrollment, token, err := store.CreateProxyNode(ctx, ProxyNode{Name: "edge-lifecycle", ResetDay: 1}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = store.CompleteEnrollment(ctx, enrollment.ID, token, "v1", "test"); err != nil {
		t.Fatal(err)
	}
	if err = store.BeginProxyNodeDrain(ctx, enrollment.NodeID); err != nil {
		t.Fatal(err)
	}
	node, _ := store.GetProxyNode(ctx, enrollment.NodeID)
	if node.State != "draining" || node.Enabled {
		t.Fatalf("drain=%+v", node)
	}
	if err = store.SetProxyNodeScheduling(ctx, enrollment.NodeID, true); err != nil {
		t.Fatal(err)
	}
	node, _ = store.GetProxyNode(ctx, enrollment.NodeID)
	if node.State != "registered" || !node.Enabled {
		t.Fatalf("reenable=%+v", node)
	}
}

func TestDecommissionCreatesPartialJobForUnreachableNodeAndRetryCompletes(t *testing.T) {
	ctx := context.Background()
	store, err := New(filepath.Join(t.TempDir(), "proxy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	enrollment, _, err := store.CreateProxyNode(ctx, ProxyNode{Name: "edge-decommission", ResetDay: 1}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.SetProxyNodeOwnershipBound(ctx, enrollment.NodeID, "spaceship", "acct", "example.com", "dns-1", "A", true, true, true, true, true); err != nil {
		t.Fatal(err)
	}
	job, err := store.DecommissionProxyNode(ctx, enrollment.NodeID, false)
	if err != nil || job == nil {
		t.Fatalf("job=%+v err=%v", job, err)
	}
	if job.State != "partial" || !job.RemoteCleanupPending || job.CleanupCommand == "" {
		t.Fatalf("job=%+v", job)
	}
	node, _ := store.GetProxyNode(ctx, enrollment.NodeID)
	if node.State != "removed" || node.Enabled || node.DNSRecordID != "dns-1" || !node.DNSOwned {
		t.Fatalf("node=%+v", node)
	}
	if !node.CaddyConfigOwned || !node.TLSStateOwned || !node.EdgeUnitOwned {
		t.Fatalf("ownership metadata was changed: %+v", node)
	}
	if _, err = store.RetryProxyNodeDecommission(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	job, _ = store.GetProxyNodeDecommissionJob(ctx, job.ID)
	if job.State != "partial" || !job.RemoteCleanupPending {
		t.Fatalf("retry must remain pending until edge completion: %+v", job)
	}
}

func TestSignedDecommissionCompletionIsSingleUse(t *testing.T) {
	ctx := context.Background()
	store, err := New(filepath.Join(t.TempDir(), "proxy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	enrollment, _, err := store.CreateProxyNode(ctx, ProxyNode{Name: "edge-online", ResetDay: 1}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	cred := "node-credential"
	if _, err := store.DB().Exec(`UPDATE proxy_nodes SET credential_hash=?,last_heartbeat_at=?,state='healthy',playback_healthy=1,config_synced=1 WHERE id=?`, nodeHash(cred), time.Now().Unix(), enrollment.NodeID); err != nil {
		t.Fatal(err)
	}
	job, err := store.DecommissionProxyNode(ctx, enrollment.NodeID, false)
	if err != nil {
		t.Fatal(err)
	}
	key, err := store.ControllerDecommissionKey(ctx)
	if err != nil {
		t.Fatal(err)
	}
	job, err = store.IssueProxyNodeDecommissionJob(ctx, job.ID, key, 15*time.Minute)
	if err != nil || job.SignedJob == nil {
		t.Fatalf("issue=%+v err=%v", job, err)
	}
	if err := store.AcceptProxyNodeDecommissionJob(ctx, job.ID, job.SignedJob.CompletionToken); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CompleteProxyNodeDecommission(ctx, job.ID, job.SignedJob.CompletionToken); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CompleteProxyNodeDecommission(ctx, job.ID, job.SignedJob.CompletionToken); err != nil {
		t.Fatal(err)
	}
	final, _ := store.GetProxyNodeDecommissionJob(ctx, job.ID)
	if final.State != "complete" || !final.CompletionConsumed {
		t.Fatalf("final=%+v", final)
	}
}

func TestOwnedDNSDeletionIsRecordScopedAndIdempotent(t *testing.T) {
	ctx := context.Background()
	store, err := New(filepath.Join(t.TempDir(), "proxy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	a, _, err := store.CreateProxyNode(ctx, ProxyNode{Name: "edge-dns-owned", ResetDay: 1}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := store.CreateProxyNode(ctx, ProxyNode{Name: "edge-dns-shared", ResetDay: 1}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetProxyNodeOwnershipBound(ctx, a.NodeID, "spaceship", "acct", "example.com", "record-owned", "A", true, false, false, false, true); err != nil {
		t.Fatal(err)
	}
	if err := store.SetProxyNodeOwnership(ctx, b.NodeID, "record-shared", false, false, false, false, true); err != nil {
		t.Fatal(err)
	}
	var deleted []string
	store.SetProxyNodeDNSDeleter(func(_ context.Context, provider, account, zone, nodeID, id, typ, fqdn, expectedIP string) error {
		deleted = append(deleted, id+":"+typ)
		return nil
	})
	if err := store.DeleteOwnedProxyNodeDNS(ctx, a.NodeID); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteOwnedProxyNodeDNS(ctx, b.NodeID); err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 1 || deleted[0] != "record-owned:A" {
		t.Fatalf("deleted=%v", deleted)
	}
	if err := store.SetProxyNodeOwnershipBound(ctx, b.NodeID, "spaceship", "acct", "example.com", "record-v6", "AAAA", true, false, false, false, true); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteOwnedProxyNodeDNS(ctx, b.NodeID); err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 2 || deleted[1] != "record-v6:AAAA" {
		t.Fatalf("deleted=%v", deleted)
	}
}

func TestProxyNodeDNSDeletionFallsBackToOwnedNameAndAddress(t *testing.T) {
	ctx := context.Background()
	store, err := New(filepath.Join(t.TempDir(), "proxy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	enrollment, _, err := store.CreateProxyNode(ctx, ProxyNode{Name: "edge-no-record-id", PublicAddress: "https://edge.example.com", ResetDay: 1}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetProxyNodeOwnershipBound(ctx, enrollment.NodeID, "spaceship", "acct", "example.com", "", "A", true, false, false, false, true); err != nil {
		t.Fatal(err)
	}
	if err := store.SetProxyNodeDNSMetadata(ctx, enrollment.NodeID, "edge.example.com", "1.2.3.4", "pending", ""); err != nil {
		t.Fatal(err)
	}
	called := false
	store.SetProxyNodeDNSDeleter(func(_ context.Context, provider, account, zone, nodeID, id, typ, fqdn, expectedIP string) error {
		called = provider == "spaceship" && account == "acct" && zone == "example.com" && nodeID == enrollment.NodeID && id == "" && typ == "A" && fqdn == "edge.example.com" && expectedIP == "1.2.3.4"
		return nil
	})
	if err := store.DeleteOwnedProxyNodeDNS(ctx, enrollment.NodeID); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("owned name/address fallback was not used")
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
func TestProxyNodeUsageEventDedupAndCycle(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "usage.db")
	store, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	enrollment, _, err := store.CreateProxyNode(ctx, ProxyNode{Name: "metered-edge", ResetDay: 1}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		err := store.RecordProxyNodeUsageEvent(ctx, enrollment.NodeID, "event-123456789abcdef", 1024, time.Now())
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = New(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecordProxyNodeUsageEvent(ctx, enrollment.NodeID, "event-123456789abcdef", 1024, time.Now()); err != nil {
		t.Fatalf("retry after controller restart: %v", err)
	}
	node, _ := store.GetProxyNode(ctx, enrollment.NodeID)
	if node.UsedBytes != 1024 {
		t.Fatalf("duplicate counted: %d", node.UsedBytes)
	}
	if err := store.RecordProxyNodeUsageEvent(ctx, enrollment.NodeID, "event-123456789abcdef", 2048, time.Now()); err == nil {
		t.Fatal("conflicting duplicate accepted")
	}
	if err := store.RecordProxyNodeUsageEvent(ctx, enrollment.NodeID, "event-222222222222222", 500, time.Now().AddDate(0, -1, 0)); err == nil {
		t.Fatal("old cycle event accepted")
	}
	if err := store.RecordProxyNodeUsageEvent(ctx, enrollment.NodeID, "event-333333333333333", -1, time.Now()); err == nil {
		t.Fatal("negative bytes accepted")
	}
	node, _ = store.GetProxyNode(ctx, enrollment.NodeID)
	if node.UsedBytes != 1024 {
		t.Fatalf("bad event changed usage: %d", node.UsedBytes)
	}
}
func TestEdgeUsageOutboxSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "edge.db")
	store, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := store.InitEdgeUsageOutbox(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.QueueEdgeUsage(ctx, 3, time.Now()); err != nil {
		t.Fatal(err)
	}
	first, err := store.NextEdgeUsage(ctx)
	if err != nil || first == nil {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	if err := store.DelayEdgeUsage(ctx, first.ID, 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := store.QueueEdgeUsage(ctx, 5, time.Now()); err != nil {
		t.Fatal(err)
	}
	second, err := store.NextEdgeUsage(ctx)
	if err != nil || second == nil || second.Bytes != 5 {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	if err := store.AcknowledgeEdgeUsage(ctx, second.ID); err != nil {
		t.Fatal(err)
	}
	if pending, err := store.EdgeUsagePending(ctx); err != nil || pending != 1 {
		t.Fatalf("pending=%d err=%v", pending, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.InitEdgeUsageOutbox(ctx); err != nil {
		t.Fatal(err)
	}
	if pending, err := store.EdgeUsagePending(ctx); err != nil || pending != 1 {
		t.Fatalf("pending after restart=%d err=%v", pending, err)
	}
	if event, err := store.NextEdgeUsage(ctx); err != nil || event != nil {
		t.Fatalf("deferred event uploaded early: %+v %v", event, err)
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE edge_usage_outbox SET next_attempt_at=0`); err != nil {
		t.Fatal(err)
	}
	event, err := store.NextEdgeUsage(ctx)
	if err != nil || event == nil || event.Bytes != 3 {
		t.Fatalf("event=%+v err=%v", event, err)
	}
	if err := store.AcknowledgeEdgeUsage(ctx, event.ID); err != nil {
		t.Fatal(err)
	}
	if pending, err := store.EdgeUsagePending(ctx); err != nil || pending != 0 {
		t.Fatalf("pending after ack=%d err=%v", pending, err)
	}
	event, err = store.NextEdgeUsage(ctx)
	if err != nil || event != nil {
		t.Fatalf("ack event=%+v err=%v", event, err)
	}
}
func TestEdgeUsageOutboxMigratesLegacySchema(t *testing.T) {
	store, err := New(filepath.Join(t.TempDir(), "legacy-edge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if _, err := store.DB().ExecContext(ctx, `CREATE TABLE edge_usage_outbox (event_id TEXT PRIMARY KEY, response_bytes INTEGER NOT NULL, sampled_at INTEGER NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO edge_usage_outbox VALUES('legacy-event-123456',7,?)`, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := store.InitEdgeUsageOutbox(ctx); err != nil {
			t.Fatalf("migration pass %d: %v", i, err)
		}
	}
	event, err := store.NextEdgeUsage(ctx)
	if err != nil || event == nil || event.Bytes != 7 {
		t.Fatalf("migrated event=%+v err=%v", event, err)
	}
}
