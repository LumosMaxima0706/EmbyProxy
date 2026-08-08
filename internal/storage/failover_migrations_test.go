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
