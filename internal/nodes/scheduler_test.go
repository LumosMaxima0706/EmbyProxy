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
