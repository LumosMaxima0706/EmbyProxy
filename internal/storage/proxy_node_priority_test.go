package storage

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

func priorityTestStore(t *testing.T, values []int) (*Store, []string, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "priority.db")
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	var ids []string
	for i, value := range values {
		e, _, err := s.CreateProxyNode(context.Background(), ProxyNode{Name: fmt.Sprintf("priority-%d", i), Priority: value, ResetDay: 1}, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, e.NodeID)
	}
	return s, ids, path
}

func assertNodePriorities(t *testing.T, s *Store, ids []string, want []int) {
	t.Helper()
	var got []int
	for _, id := range ids {
		n, err := s.GetProxyNode(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, n.Priority)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("priorities=%v want=%v", got, want)
	}
}

func TestProxyNodePriorityInsertion(t *testing.T) {
	for _, tc := range []struct {
		name          string
		values        []int
		index, target int
		want          []int
	}{
		{"161-before-bwg", []int{0, 1, 2}, 2, 1, []int{0, 2, 1}},
		{"backwards", []int{0, 1, 2}, 0, 2, []int{2, 0, 1}},
		{"first", []int{0, 1, 2}, 2, 0, []int{1, 2, 0}},
		{"unchanged", []int{0, 1, 2}, 1, 1, []int{0, 1, 2}},
		{"legacy-duplicate", []int{0, 1, 1}, 2, 1, []int{0, 2, 1}},
		{"gaps", []int{0, 10, 20}, 2, 10, []int{0, 11, 10}},
		{"maximum", []int{0, 1, 2}, 2, 10000, []int{0, 1, 10000}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, ids, path := priorityTestStore(t, tc.values)
			if err := s.SetProxyNodePriority(context.Background(), ids[tc.index], tc.target); err != nil {
				t.Fatal(err)
			}
			assertNodePriorities(t, s, ids, tc.want)
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := New(path)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			assertNodePriorities(t, reopened, ids, tc.want)
		})
	}
}

func TestProxyNodePriorityRollbackAndScope(t *testing.T) {
	s, ids, _ := priorityTestStore(t, []int{0, 1, 2, 1, 1})
	ctx := context.Background()
	if _, err := s.db.Exec(`UPDATE proxy_nodes SET state='removed' WHERE id=?`, ids[3]); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE proxy_nodes SET state='revoked' WHERE id=?`, ids[4]); err != nil {
		t.Fatal(err)
	}
	for _, value := range []int{-1, 10001} {
		if err := s.SetProxyNodePriority(ctx, ids[2], value); err == nil {
			t.Fatal("invalid priority accepted")
		}
	}
	for _, id := range []string{"missing", ids[3], ids[4]} {
		if err := s.SetProxyNodePriority(ctx, id, 0); err == nil {
			t.Fatal("unavailable node accepted")
		}
	}
	// Fail the target write after its peer was shifted, and require full rollback.
	if _, err := s.db.Exec(fmt.Sprintf(`CREATE TRIGGER fail_priority BEFORE UPDATE OF priority ON proxy_nodes WHEN NEW.id='%s' BEGIN SELECT RAISE(ABORT,'injected'); END`, ids[2])); err != nil {
		t.Fatal(err)
	}
	if err := s.SetProxyNodePriority(ctx, ids[2], 1); err == nil {
		t.Fatal("injected failure ignored")
	}
	assertNodePriorities(t, s, ids, []int{0, 1, 2, 1, 1})
	if _, err := s.db.Exec(`DROP TRIGGER fail_priority`); err != nil {
		t.Fatal(err)
	}
	if err := s.SetProxyNodePriority(ctx, ids[2], 1); err != nil {
		t.Fatal(err)
	}
	assertNodePriorities(t, s, ids, []int{0, 2, 1, 1, 1})
}

func TestProxyNodePriorityConcurrentUpdates(t *testing.T) {
	s, ids, _ := priorityTestStore(t, []int{0, 1, 2, 3})
	var wg sync.WaitGroup
	errs := make(chan error, len(ids))
	for _, id := range ids {
		wg.Add(1)
		go func(id string) { defer wg.Done(); errs <- s.SetProxyNodePriority(context.Background(), id, 0) }(id)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	nodes, err := s.ListProxyNodes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for i, n := range nodes {
		if n.Priority != i {
			t.Fatalf("non-serial order: %+v", nodes)
		}
	}
}

func TestProxyNodePriorityOverflowRollsBack(t *testing.T) {
	s, ids, _ := priorityTestStore(t, []int{10000, 10000})
	if err := s.SetProxyNodePriority(context.Background(), ids[0], 10000); err == nil {
		t.Fatal("overflow accepted")
	}
	assertNodePriorities(t, s, ids, []int{10000, 10000})
}

func TestProxyNodeUnrelatedPatchPreservesPriority(t *testing.T) {
	s, ids, _ := priorityTestStore(t, []int{0, 1, 2})
	n, err := s.GetProxyNode(context.Background(), ids[1])
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetProxyNodePriority(context.Background(), ids[2], 1); err != nil {
		t.Fatal(err)
	}
	n.QuotaBytes = 100
	if err := s.UpdateProxyNodeWithPriority(context.Background(), *n, nil); err != nil {
		t.Fatal(err)
	}
	assertNodePriorities(t, s, ids, []int{0, 2, 1})
}

func TestProxyNodeMixedPriorityUpdateRollsBack(t *testing.T) {
	s, ids, _ := priorityTestStore(t, []int{0, 1, 2})
	n, err := s.GetProxyNode(context.Background(), ids[2])
	if err != nil {
		t.Fatal(err)
	}
	n.QuotaBytes = 100
	if _, err := s.db.Exec(`CREATE TRIGGER fail_quota BEFORE UPDATE OF quota_bytes ON proxy_nodes BEGIN SELECT RAISE(ABORT,'injected'); END`); err != nil {
		t.Fatal(err)
	}
	priority := 1
	if err := s.UpdateProxyNodeWithPriority(context.Background(), *n, &priority); err == nil {
		t.Fatal("quota failure ignored")
	}
	assertNodePriorities(t, s, ids, []int{0, 1, 2})
	n, err = s.GetProxyNode(context.Background(), ids[2])
	if err != nil || n.QuotaBytes != 0 {
		t.Fatalf("quota changed after failure: %+v %v", n, err)
	}
}
