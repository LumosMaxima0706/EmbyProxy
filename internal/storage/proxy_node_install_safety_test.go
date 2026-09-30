package storage

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestProxyNodeAppendDisabledAndPreserveReinstallSettings(t *testing.T) {
	ctx := context.Background()
	s, err := New(filepath.Join(t.TempDir(), "install.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for i, p := range []int{0, 20, 99} {
		e, _, err := s.CreateProxyNode(ctx, ProxyNode{Name: fmt.Sprintf("existing-%d", i), Priority: p, ResetDay: 1}, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if i == 2 {
			if _, err := s.DB().Exec(`UPDATE proxy_nodes SET state='removed' WHERE id=?`, e.NodeID); err != nil {
				t.Fatal(err)
			}
		}
	}
	e, token, err := s.CreateProxyNodeAtLowestPriority(ctx, ProxyNode{Name: "new-test", ResetDay: 1, Enabled: true}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	n, err := s.GetProxyNode(ctx, e.NodeID)
	if err != nil || n.Enabled || n.Priority != 21 {
		t.Fatalf("new node=%+v err=%v", n, err)
	}
	node, cred, err := s.CompleteEnrollment(ctx, e.ID, token, "v", "test")
	if err != nil || node.Enabled || node.Priority != 21 {
		t.Fatalf("enrolled=%+v err=%v", node, err)
	}
	if err := s.HeartbeatProxyNode(ctx, node.ID, cred, "v", "test", "healthy", true, true, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.SetProxyNodeIngressHealth(ctx, node.ID, true, ""); err != nil {
		t.Fatal(err)
	}
	n, err = s.GetProxyNode(ctx, node.ID)
	if err != nil || n.Enabled || n.State != "healthy" {
		t.Fatalf("heartbeat auto-enabled: %+v err=%v", n, err)
	}
	for _, enabled := range []bool{false, true} {
		n.Enabled, n.Priority = enabled, 3
		if _, err := s.DB().Exec(`UPDATE proxy_nodes SET enabled=?,priority=3 WHERE id=?`, enabled, n.ID); err != nil {
			t.Fatal(err)
		}
		e, token, err = s.RegenerateProxyNodeEnrollment(ctx, n.ID, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		node, _, err = s.CompleteEnrollment(ctx, e.ID, token, "v2", "test2")
		if err != nil || node.Enabled != enabled || node.Priority != 3 {
			t.Fatalf("reinstall changed settings: %+v err=%v", node, err)
		}
	}
	peers, err := s.ListProxyNodes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, peer := range peers {
		if peer.Name == "existing-0" && peer.Priority != 0 || peer.Name == "existing-1" && peer.Priority != 20 {
			t.Fatalf("peer changed: %+v", peer)
		}
	}
}

func TestProxyNodeAppendConcurrentAndExhaustion(t *testing.T) {
	ctx := context.Background()
	s, err := New(filepath.Join(t.TempDir(), "append.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, _, err := s.CreateProxyNodeAtLowestPriority(ctx, ProxyNode{Name: fmt.Sprintf("append-%d", i), ResetDay: 1}, time.Minute); err != nil {
				t.Errorf("append: %v", err)
			}
		}(i)
	}
	wg.Wait()
	nodes, err := s.ListProxyNodes(ctx)
	if err != nil || len(nodes) != 8 {
		t.Fatalf("nodes=%v err=%v", nodes, err)
	}
	for i, n := range nodes {
		if n.Enabled || n.Priority != i {
			t.Fatalf("node[%d]=%+v", i, n)
		}
	}
	if _, _, err := s.CreateProxyNode(ctx, ProxyNode{Name: "last-slot", ResetDay: 1, Priority: 10000}, time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CreateProxyNodeAtLowestPriority(ctx, ProxyNode{Name: "overflow", ResetDay: 1}, time.Minute); err == nil || err.Error() != "priority_range_exhausted" {
		t.Fatalf("overflow=%v", err)
	}
	var count int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM proxy_nodes WHERE name='overflow'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial insert=%d err=%v", count, err)
	}
}
