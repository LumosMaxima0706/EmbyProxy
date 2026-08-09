package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"embyproxy/internal/failover"
	"embyproxy/internal/storage"
)

func restoreControllerFromSQLite(t *testing.T, path string, nodes []failover.Node, now time.Time) *failover.Controller {
	t.Helper()
	store, err := storage.New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	controller := failover.NewController(nodes, failover.DefaultPolicyConfig(), nil)
	controller.SetNow(func() time.Time { return now })
	ctx := context.Background()
	if saved, ok, err := store.LoadFailoverState(ctx); err != nil {
		t.Fatal(err)
	} else if ok {
		controller.RestoreState(failover.State{
			ActiveNodeID: saved.ActiveNodeID, DesiredNodeID: saved.DesiredNodeID,
			ObservedDNSNodeID: saved.ObservedDNSNodeID, Mode: failover.Mode(saved.Mode),
			CooldownUntil: unixTimeOrZero(saved.CooldownUntil), LastTransitionAt: unixTimeOrZero(saved.LastTransitionAt),
			LastEvaluationAt: unixTimeOrZero(saved.LastEvaluationAt), CurrentCycleKey: saved.CurrentCycleKey,
			ReconciliationRequired: saved.ReconciliationRequired,
		})
	}
	runtimes, err := store.LoadFailoverNodeRuntime(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for nodeID, runtime := range runtimes {
		controller.RestoreNodeRuntime(nodeID, runtime.ConsecutiveFailures, runtime.ConsecutiveSuccesses, trafficSampleFromRecord(runtime.Traffic))
	}
	return controller
}

func sqliteRestoreNodes() []failover.Node {
	return []failover.Node{
		{ID: "primary", Role: failover.RolePrimary, Enabled: true, HealthStatus: failover.HealthHealthy, Priority: 1, ResetDay: 21, ResetTimezone: "UTC"},
		{ID: "fallback", Role: failover.RoleFallback, Enabled: true, HealthStatus: failover.HealthHealthy, Priority: 2},
	}
}

func writeRestoreFixture(t *testing.T, path string, write func(*storage.Store)) {
	t.Helper()
	store, err := storage.New(path)
	if err != nil {
		t.Fatal(err)
	}
	write(store)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestSQLiteRestoreOneFailureIsNotFailoverEligible(t *testing.T) {
	path := filepath.Join(t.TempDir(), "proxy.db")
	writeRestoreFixture(t, path, func(store *storage.Store) {
		if err := store.RecordFailoverHealthCheck(context.Background(), "primary", 1, "mock", false, 503, 1, 1, 0, "unavailable"); err != nil {
			t.Fatal(err)
		}
	})
	controller := restoreControllerFromSQLite(t, path, sqliteRestoreNodes(), time.Unix(10, 0))
	decision, err := controller.Evaluate()
	if err != nil || decision.NodeID != "primary" || controller.FailoverEligible("primary") {
		t.Fatalf("decision=%+v eligible=%v err=%v", decision, controller.FailoverEligible("primary"), err)
	}
	_, nodes := controller.Status()
	if nodes[0].HealthStatus != failover.HealthDegraded || nodes[0].ConsecutiveFailures != 1 {
		t.Fatalf("primary=%+v", nodes[0])
	}
}

func TestSQLiteRestoreTwoFailuresNeedsOneMoreToFailOver(t *testing.T) {
	path := filepath.Join(t.TempDir(), "proxy.db")
	writeRestoreFixture(t, path, func(store *storage.Store) {
		if err := store.RecordFailoverHealthCheck(context.Background(), "primary", 2, "mock", false, 503, 1, 2, 0, "unavailable"); err != nil {
			t.Fatal(err)
		}
	})
	controller := restoreControllerFromSQLite(t, path, sqliteRestoreNodes(), time.Unix(10, 0))
	if decision, err := controller.Evaluate(); err != nil || decision.NodeID != "primary" {
		t.Fatalf("pre-third decision=%+v err=%v", decision, err)
	}
	if err := controller.SetHealth(failover.HealthResultAt("primary", "mock", false, 503, 0, "unavailable")); err != nil {
		t.Fatal(err)
	}
	decision, err := controller.Evaluate()
	if err != nil || decision.NodeID != "fallback" || !decision.Change || !controller.FailoverEligible("primary") {
		t.Fatalf("decision=%+v eligible=%v err=%v", decision, controller.FailoverEligible("primary"), err)
	}
}

func TestSQLiteRestoreSuccessResetsFailureCount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "proxy.db")
	writeRestoreFixture(t, path, func(store *storage.Store) {
		ctx := context.Background()
		if err := store.RecordFailoverHealthCheck(ctx, "primary", 1, "mock", false, 503, 1, 1, 0, "unavailable"); err != nil {
			t.Fatal(err)
		}
		if err := store.RecordFailoverHealthCheck(ctx, "primary", 2, "mock", true, 200, 1, 0, 1, ""); err != nil {
			t.Fatal(err)
		}
	})
	controller := restoreControllerFromSQLite(t, path, sqliteRestoreNodes(), time.Unix(10, 0))
	_, nodes := controller.Status()
	if nodes[0].HealthStatus != failover.HealthHealthy || nodes[0].ConsecutiveFailures != 0 || nodes[0].ConsecutiveSuccesses != 1 {
		t.Fatalf("primary=%+v", nodes[0])
	}
}

func TestSQLiteRestorePreservesQuotaCycleAndNewCycleRecovery(t *testing.T) {
	for _, test := range []struct {
		name         string
		stateCycle   string
		sampleCycle  string
		now          time.Time
		wantNode     string
		wantStateKey string
	}{
		{name: "same cycle remains fallback", stateCycle: "2026-08-21", sampleCycle: "2026-08-21", now: time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC), wantNode: "fallback", wantStateKey: "2026-08-21"},
		{name: "new cycle recovers primary", stateCycle: "2026-08-21", sampleCycle: "2026-09-21", now: time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC), wantNode: "primary", wantStateKey: "2026-09-21"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "proxy.db")
			writeRestoreFixture(t, path, func(store *storage.Store) {
				ctx := context.Background()
				if err := store.SaveFailoverState(ctx, "fallback", "fallback", "", "auto", test.stateCycle, 0, 0, 0, false); err != nil {
					t.Fatal(err)
				}
				if err := store.RecordFailoverHealthCheck(ctx, "primary", 1, "mock", true, 200, 1, 0, 3, ""); err != nil {
					t.Fatal(err)
				}
				total, quota := int64(990), int64(1000)
				if test.sampleCycle != test.stateCycle {
					total = 1
				}
				if err := store.RecordFailoverTrafficSample(ctx, storage.TrafficSampleRecord{NodeID: "primary", SampledAt: 2, CycleKey: test.sampleCycle, SourceType: "mock", TotalBytes: &total, QuotaBytes: &quota, Quality: "known"}); err != nil {
					t.Fatal(err)
				}
			})
			controller := restoreControllerFromSQLite(t, path, sqliteRestoreNodes(), test.now)
			decision, err := controller.Evaluate()
			state, _ := controller.Status()
			if err != nil || decision.NodeID != test.wantNode || state.CurrentCycleKey != test.stateCycle {
				t.Fatalf("decision=%+v state=%+v err=%v", decision, state, err)
			}
			if decision.Change {
				if err := controller.ApplyDecision(decision, "cycle_recovery"); err != nil {
					t.Fatal(err)
				}
			}
			state, _ = controller.Status()
			if state.CurrentCycleKey != test.wantStateKey || state.ActiveNodeID != test.wantNode {
				t.Fatalf("applied state=%+v decision=%+v", state, decision)
			}
		})
	}
}
