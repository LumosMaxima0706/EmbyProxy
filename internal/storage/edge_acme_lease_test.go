package storage

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestEdgeACMELeaseSurvivesRestartAndRejectsTakeover(t *testing.T) {
	path := filepath.Join(t.TempDir(), "edge-acme.db")
	store, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	expires := time.Now().Add(5 * time.Minute)
	if err := store.AcquireEdgeACMELease(ctx, "node-a", "value-a", expires); err != nil {
		t.Fatal(err)
	}
	if err := store.AcquireEdgeACMELease(ctx, "node-a", "value-a", expires); err != nil {
		t.Fatal(err)
	}
	if err := store.AcquireEdgeACMELease(ctx, "node-b", "value-b", expires); err == nil {
		t.Fatal("takeover accepted")
	}
	if err := store.ReleaseEdgeACMELease(ctx, "node-b", "value-b"); err == nil {
		t.Fatal("wrong release accepted")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if lease, err := store.GetEdgeACMELease(ctx); err != nil || lease == nil || lease.NodeID != "node-a" || lease.Value != "value-a" {
		t.Fatalf("lost lease after restart: %+v err=%v", lease, err)
	}
	if err := store.AcquireEdgeACMELease(ctx, "node-b", "value-b", expires); err == nil {
		t.Fatal("takeover after restart accepted")
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE edge_acme_lease SET expires_at=? WHERE id=1`, time.Now().Add(-time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	if err := store.AcquireEdgeACMELease(ctx, "node-b", "value-b", expires); err == nil {
		t.Fatal("expired lease silently taken over")
	}
	if err := store.ReleaseEdgeACMELease(ctx, "node-a", "value-b"); err == nil {
		t.Fatal("wrong value released lease")
	}
	if err := store.ReleaseEdgeACMELease(ctx, "node-a", "value-a"); err != nil {
		t.Fatal(err)
	}
	if err := store.AcquireEdgeACMELease(ctx, "node-b", "value-b", expires); err != nil {
		t.Fatal(err)
	}
	if lease, err := store.GetEdgeACMELease(ctx); err != nil || lease == nil || lease.NodeID != "node-b" {
		t.Fatalf("new lease=%+v err=%v", lease, err)
	}
}
func TestEdgeACMELeaseConcurrentAcquireHasSingleWinner(t *testing.T) {
	store, err := New(filepath.Join(t.TempDir(), "edge-acme.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	var wg sync.WaitGroup
	winners := make(chan string, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			id := string(rune('a' + n))
			if store.AcquireEdgeACMELease(ctx, id, id, time.Now().Add(5*time.Minute)) == nil {
				winners <- id
			}
		}(i)
	}
	wg.Wait()
	close(winners)
	if len(winners) != 1 {
		t.Fatalf("concurrent winners=%d", len(winners))
	}
}
