package failover

import (
	"context"
	"testing"
)

func TestMockHealthProbeAndTracker(t *testing.T) {
	tracker := NewHealthTracker(testNodes(), DefaultPolicyConfig())
	probe := NewMockHealthProbe()
	probe.Set(HealthResultAt("nosla", "mock_http", true, 200, 0, ""))
	results := tracker.Check(context.Background(), probe)
	if len(results) != 2 {
		t.Fatalf("results = %+v", results)
	}
	snapshot := tracker.Snapshot()
	for _, node := range snapshot {
		if node.ID == "nosla" && (node.HealthStatus != HealthHealthy || node.ConsecutiveSuccesses != 1) {
			t.Fatalf("nosla = %+v", node)
		}
	}
}

func TestHealthTrackerIsRaceSafeForConcurrentRecords(t *testing.T) {
	tracker := NewHealthTracker(testNodes(), DefaultPolicyConfig())
	done := make(chan struct{})
	for i := 0; i < 8; i++ {
		go func() {
			tracker.Record(HealthResultAt("nosla", "mock", true, 200, 0, ""))
			done <- struct{}{}
		}()
	}
	for i := 0; i < 8; i++ {
		<-done
	}
	for _, node := range tracker.Snapshot() {
		if node.ID == "nosla" && node.ConsecutiveSuccesses != 8 {
			t.Fatalf("nosla = %+v", node)
		}
	}
}

func TestHealthTrackerDistinguishesProbeFailureFromFailoverEligibility(t *testing.T) {
	tracker := NewHealthTracker(testNodes(), DefaultPolicyConfig())
	tracker.Record(HealthResultAt("nosla", "mock", false, 503, 0, "unavailable"))
	for _, node := range tracker.Snapshot() {
		if node.ID == "nosla" && node.HealthStatus != HealthDegraded {
			t.Fatalf("node = %+v", node)
		}
	}
}

func TestHealthTrackerUsesConfiguredFailureThreshold(t *testing.T) {
	cfg := DefaultPolicyConfig()
	cfg.FailureThreshold = 5
	tracker := NewHealthTracker(testNodes(), cfg)
	for i := 0; i < 4; i++ {
		tracker.Record(HealthResultAt("nosla", "mock", false, 503, 0, "unavailable"))
	}
	for _, node := range tracker.Snapshot() {
		if node.ID == "nosla" && node.HealthStatus == HealthFailed {
			t.Fatalf("node failed before configured threshold: %+v", node)
		}
	}
	tracker.Record(HealthResultAt("nosla", "mock", false, 503, 0, "unavailable"))
	for _, node := range tracker.Snapshot() {
		if node.ID == "nosla" && node.HealthStatus != HealthFailed {
			t.Fatalf("node did not fail at configured threshold: %+v", node)
		}
	}
}
