package storage

import (
	"context"
	"database/sql"
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

func TestFailoverRuntimeLoadsLatestHealthAndTraffic(t *testing.T) {
	store, err := New(filepath.Join(t.TempDir(), "proxy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if err := store.RecordFailoverHealthCheck(ctx, "nosla", 100, "mock", false, 503, 5, 2, 0, "down"); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordFailoverHealthCheck(ctx, "nosla", 101, "mock", true, 200, 5, 0, 1, ""); err != nil {
		t.Fatal(err)
	}
	value := int64(950)
	if err := store.RecordFailoverTrafficSample(ctx, TrafficSampleRecord{NodeID: "nosla", SampledAt: 100, CycleKey: "cycle-a", Quality: "known", TotalBytes: &value}); err != nil {
		t.Fatal(err)
	}
	runtime, err := store.LoadFailoverNodeRuntime(ctx)
	if err != nil || runtime["nosla"].ConsecutiveSuccesses != 1 || runtime["nosla"].Traffic.CycleKey != "cycle-a" {
		t.Fatalf("runtime=%+v err=%v", runtime, err)
	}
}

func TestLoadFailoverStateHandlesNullTimeColumns(t *testing.T) {
	store, err := New(filepath.Join(t.TempDir(), "proxy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if _, err := store.db.ExecContext(ctx, `INSERT INTO failover_state (scope, mode, updated_at) VALUES ('default', 'auto', 1)`); err != nil {
		t.Fatal(err)
	}
	state, ok, err := store.LoadFailoverState(ctx)
	if err != nil || !ok || state.ActiveNodeID != "" || state.CooldownUntil != 0 {
		t.Fatalf("state=%+v ok=%v err=%v", state, ok, err)
	}
}

func TestFailoverTransactionRollsBackRunAndEventWhenStateFails(t *testing.T) {
	store, err := New(filepath.Join(t.TempDir(), "proxy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if _, err := store.db.ExecContext(ctx, `CREATE TRIGGER failover_state_block BEFORE INSERT ON failover_state BEGIN SELECT RAISE(ABORT, 'blocked'); END`); err != nil {
		t.Fatal(err)
	}
	err = store.CommitDNSUpdate(ctx, DNSUpdateRunRecord{StartedAt: 1, CompletedAt: 2, ProviderKind: "mock", RecordName: "stream.example", RecordType: "A", Success: true}, FailoverStateRecord{ActiveNodeID: "bwg", Mode: "auto"}, FailoverEventRecord{CreatedAt: 2, EventType: "switch", Mode: "auto", Success: true})
	if err == nil {
		t.Fatal("expected transaction failure")
	}
	var count int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM dns_update_runs`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("dns runs after rollback = %d", count)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM failover_events`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("events after rollback = %d", count)
	}
}

func TestFailoverTransitionDoesNotWriteStateWhenEventFails(t *testing.T) {
	store, err := New(filepath.Join(t.TempDir(), "proxy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if _, err := store.db.ExecContext(ctx, `CREATE TRIGGER failover_event_block BEFORE INSERT ON failover_events BEGIN SELECT RAISE(ABORT, 'blocked'); END`); err != nil {
		t.Fatal(err)
	}
	err = store.CommitFailoverTransition(ctx, FailoverStateRecord{ActiveNodeID: "fallback", Mode: "auto"}, FailoverEventRecord{CreatedAt: 2, EventType: "switch", Mode: "auto", Success: true})
	if err == nil {
		t.Fatal("expected transaction failure")
	}
	var count int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM failover_state`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("state rows after rollback = %d", count)
	}
}

func TestDNSCommitRollsBackEventAndStateWhenRunFails(t *testing.T) {
	store, err := New(filepath.Join(t.TempDir(), "proxy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if _, err := store.db.ExecContext(ctx, `CREATE TRIGGER dns_run_block BEFORE INSERT ON dns_update_runs BEGIN SELECT RAISE(ABORT, 'blocked'); END`); err != nil {
		t.Fatal(err)
	}
	err = store.CommitDNSUpdate(ctx, DNSUpdateRunRecord{StartedAt: 1, CompletedAt: 2, ProviderKind: "mock", RecordName: "record.test", RecordType: "A", Success: true}, FailoverStateRecord{ActiveNodeID: "fallback", Mode: "auto"}, FailoverEventRecord{CreatedAt: 2, EventType: "switch", Mode: "auto", Success: true})
	if err == nil {
		t.Fatal("expected transaction failure")
	}
	for _, table := range []string{"failover_events", "failover_state"} {
		var count int
		if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("%s rows after rollback = %d", table, count)
		}
	}
}

func TestLatestDNSRunSurvivesStoreReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "proxy.db")
	store, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := store.RecordDNSUpdateRunRecord(ctx, DNSUpdateRunRecord{
		StartedAt: 1, CompletedAt: 2, ProviderKind: "mock", RecordName: "record.test", RecordType: "A",
		PreviousValue: "192.0.2.9", DesiredValue: "192.0.2.10", RollbackReady: true,
		DryRun: true, Success: true, ProviderResult: "mock", PropagationResult: "dry_run",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	run, ok, err := reopened.LoadLatestDNSUpdateRun(ctx)
	if err != nil || !ok || !run.DryRun || !run.Success || run.PropagationResult != "dry_run" || !run.RollbackReady || run.PreviousValue != "192.0.2.9" || run.DesiredValue != "192.0.2.10" {
		t.Fatalf("run=%+v ok=%v err=%v", run, ok, err)
	}
}

func TestFailoverSchemaUpgradesDNSRollbackMetadataColumns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "proxy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE dns_update_runs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		started_at INTEGER NOT NULL,
		completed_at INTEGER,
		provider_kind TEXT NOT NULL,
		record_name TEXT NOT NULL,
		record_type TEXT NOT NULL,
		from_value_redacted TEXT NOT NULL DEFAULT '',
		to_value_redacted TEXT NOT NULL DEFAULT '',
		dry_run INTEGER NOT NULL DEFAULT 1,
		provider_result TEXT NOT NULL DEFAULT '',
		propagation_result TEXT NOT NULL DEFAULT '',
		success INTEGER NOT NULL DEFAULT 0,
		error_summary TEXT NOT NULL DEFAULT ''
	)`)
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, column := range []string{"previous_value", "desired_value", "rollback_metadata_ready"} {
		var count int
		if err := store.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('dns_update_runs') WHERE name = ?`, column).Scan(&count); err != nil || count != 1 {
			t.Fatalf("column %s count=%d err=%v", column, count, err)
		}
	}
}
