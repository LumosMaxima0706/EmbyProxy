package nodes

import (
	"embyproxy/internal/storage"
	"testing"
	"time"
)

func TestSelectManualUsesEligibilityAndPriority(t *testing.T) {
	now := time.Now()
	base := func(id string, p int) storage.ProxyNode {
		return storage.ProxyNode{ID: id, Name: id, Priority: p, Enabled: true, State: "healthy", LastHeartbeatAt: now.Unix(), PlaybackHealthy: true, ConfigSynced: true}
	}
	nodes := []storage.ProxyNode{base("late", 2), base("first", 1), base("draining", 0)}
	nodes[2].State = "draining"
	got, ok := Select(nodes, "manual", "", now)
	if !ok || got.NodeID != "first" {
		t.Fatalf("got=%+v ok=%v", got, ok)
	}
}
func TestSelectSmartExcludesExhaustedAndStale(t *testing.T) {
	now := time.Now()
	good := storage.ProxyNode{ID: "good", Name: "good", Enabled: true, State: "healthy", LastHeartbeatAt: now.Unix(), PlaybackHealthy: true, ConfigSynced: true, QuotaBytes: 100, UsedBytes: 10}
	exhausted := good
	exhausted.ID = "bad"
	exhausted.UsedBytes = 100
	stale := good
	stale.ID = "stale"
	stale.LastHeartbeatAt = now.Add(-10 * time.Minute).Unix()
	got, ok := Select([]storage.ProxyNode{exhausted, stale, good}, "smart", "", now)
	if !ok || got.NodeID != "good" {
		t.Fatalf("got=%+v ok=%v", got, ok)
	}
}

func TestSelectWithPolicyHysteresisAndImmediateFailover(t *testing.T) {
	now := time.Unix(1700000000, 0)
	base := func(id string) storage.ProxyNode {
		return storage.ProxyNode{ID: id, Name: id, Enabled: true, State: "healthy", LastHeartbeatAt: now.Unix(), PlaybackHealthy: true, ConfigSynced: true, QuotaBytes: 100, UsedBytes: 10}
	}
	a, b := base("a"), base("b")
	a.UsedBytes = 90
	decision, ok := SelectWithPolicy([]storage.ProxyNode{a, b}, Policy{Mode: "smart", CurrentID: "a", CurrentSince: now, MinimumDwell: time.Hour, HysteresisScore: 0.1}, now.Add(time.Minute))
	if !ok || decision.NodeID != "a" || decision.Reason != "minimum_dwell" {
		t.Fatalf("decision=%+v ok=%v", decision, ok)
	}
	a.PlaybackHealthy = false
	decision, ok = SelectWithPolicy([]storage.ProxyNode{a, b}, Policy{Mode: "smart", CurrentID: "a", CurrentSince: now, MinimumDwell: time.Hour}, now.Add(time.Minute))
	if !ok || decision.NodeID != "b" {
		t.Fatalf("failover decision=%+v ok=%v", decision, ok)
	}
}
