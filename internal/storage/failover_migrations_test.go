package storage

import (
	"context"
	"path/filepath"
	"testing"
)

func TestFailoverSchemaMigrationIsIdempotent(t *testing.T) {
	store, err := New(filepath.Join(t.TempDir(), "proxy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.InitFailoverSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"failover_nodes", "failover_state", "failover_health_checks", "failover_traffic_samples", "failover_events", "dns_update_runs", "traffic_quota_policies"} {
		ok, err := store.FailoverTableExists(context.Background(), table)
		if err != nil || !ok {
			t.Fatalf("table %s exists=%v err=%v", table, ok, err)
		}
	}
}

func TestFailoverSamplesAndHealthChecksPersistWithoutSecrets(t *testing.T) {
	store, err := New(filepath.Join(t.TempDir(), "proxy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if err := store.RecordFailoverHealthCheck(ctx, "nosla", 100, "mock", false, 0, 5, 1, 0, "token=hidden"); err != nil {
		t.Fatal(err)
	}
	var healthError string
	if err := store.db.QueryRowContext(ctx, `SELECT last_error FROM failover_health_checks`).Scan(&healthError); err != nil {
		t.Fatal(err)
	}
	if healthError != "redacted" {
		t.Fatalf("last_error = %q", healthError)
	}
	if err := store.RecordFailoverTrafficSample(ctx, TrafficSampleRecord{NodeID: "nosla", SampledAt: 100, CycleKey: "cycle-a", SourceType: "mock", Quality: "unknown"}); err != nil {
		t.Fatal(err)
	}
	var total any
	if err := store.db.QueryRowContext(ctx, `SELECT total_bytes FROM failover_traffic_samples`).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if total != nil {
		t.Fatalf("unknown traffic total = %v, want NULL", total)
	}
}

func TestFailoverStateAndEventsRoundTrip(t *testing.T) {
	store, err := New(filepath.Join(t.TempDir(), "proxy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if err := store.SaveFailoverState(ctx, "nosla", "bwg", "nosla", "auto", "cycle-a", 11, 12, 13, false); err != nil {
		t.Fatal(err)
	}
	state, ok, err := store.LoadFailoverState(ctx)
	if err != nil || !ok || state.ActiveNodeID != "nosla" || state.CooldownUntil != 11 {
		t.Fatalf("state=%+v ok=%v err=%v", state, ok, err)
	}
	if err := store.AppendFailoverEvent(ctx, FailoverEventRecord{CreatedAt: 20, EventType: "switch", Mode: "auto", ReasonCode: "health_failure", Success: true}); err != nil {
		t.Fatal(err)
	}
	events, err := store.LoadFailoverEvents(ctx, 10)
	if err != nil || len(events) != 1 || events[0].EventType != "switch" {
		t.Fatalf("events=%+v err=%v", events, err)
	}
}
