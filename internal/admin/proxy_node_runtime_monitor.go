package admin

import (
	"context"
	"embyproxy/internal/storage"
	"sync"
	"time"
)

type nodeProbeMonitor struct {
	LastAttemptAt int64  `json:"last_attempt_at"`
	LastSuccessAt int64  `json:"last_success_at,omitempty"`
	NextRunAt     int64  `json:"next_run_at,omitempty"`
	Failures      int    `json:"consecutive_failures"`
	Error         string `json:"error,omitempty"`
}

func (s *publicIngressSwitcher) runtimeIngressHealthy(ctx context.Context, node storage.ProxyNode) bool {
	if s.h.playbackCredentials == nil {
		return true
	}
	var monitor nodeProbeMonitor
	found, err := s.h.store.KV().GetJSON(ctx, "node-probe-monitor:"+node.ID, &monitor)
	if err != nil || !found {
		return true
	} // initial probe starts with controller
	return monitor.LastAttemptAt > s.now().Add(-5*time.Minute).Unix() && monitor.Failures < 2
}

func (h *Handler) monitorNodeBusiness(ctx context.Context, node storage.ProxyNode) {
	var monitor nodeProbeMonitor
	_, _ = h.store.KV().GetJSON(ctx, "node-probe-monitor:"+node.ID, &monitor)
	monitor.LastAttemptAt = time.Now().Unix()
	monitor.NextRunAt = 0
	_ = h.store.KV().Put(ctx, "node-probe-monitor:"+node.ID, monitor)
	var view proxyNodeReadiness
	if node.LastHeartbeatAt <= time.Now().Add(-5*time.Minute).Unix() {
		view = proxyNodeReadiness{CheckedAt: time.Now().Unix(), BusinessTLS: "failed", Routes: "unverified", Playback: "unverified", Error: "heartbeat_expired", Origin: node.PublicAddress, AgentCommit: node.AgentCommit}
	} else {
		view = h.checkProxyNodeReadiness(ctx, node)
	}
	if err := h.store.KV().Put(context.WithoutCancel(ctx), "node-readiness:"+node.ID, view); err != nil {
		return
	}
	failed := view.BusinessTLS != "ready" || view.Routes == "failed" || view.Playback == "failed"
	if failed {
		monitor.Failures++
		monitor.Error = view.Error
		if monitor.Error == "" {
			monitor.Error = "business_or_media_failed"
		}
	} else {
		monitor.Failures = 0
		monitor.Error = ""
		monitor.LastSuccessAt = time.Now().Unix()
	}
	monitor.NextRunAt = time.Now().Add(2 * time.Minute).Unix()
	_ = h.store.KV().Put(context.WithoutCancel(ctx), "node-probe-monitor:"+node.ID, monitor)
}

// Separate from the DNS scheduler: an unreachable node cannot stop its peers'
// probes or block the ingress switch mutex. Each probe has a 90s deadline.
func (h *Handler) StartNodeBusinessMonitor(ctx context.Context) {
	if h == nil || h.publicIngress == nil || h.playbackCredentials == nil {
		return
	}
	go func() {
		run := func() {
			nodes, err := h.store.ListProxyNodes(ctx)
			if err != nil {
				return
			}
			sem := make(chan struct{}, 3)
			var wg sync.WaitGroup
			for _, node := range nodes {
				if !node.Enabled || node.State == "revoked" || node.State == "removed" {
					continue
				}
				wg.Add(1)
				go func(node storage.ProxyNode) {
					defer wg.Done()
					select {
					case sem <- struct{}{}:
					case <-ctx.Done():
						return
					}
					defer func() { <-sem }()
					h.monitorNodeBusiness(ctx, node)
				}(node)
			}
			wg.Wait()
		}
		run()
		ticker := time.NewTicker(2 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				run()
			}
		}
	}()
}
