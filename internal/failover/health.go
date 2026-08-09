package failover

import (
	"context"
	"sort"
	"sync"
	"time"
)

// HealthProbe is deliberately provider-neutral. Production probes are out of
// scope for the local phase; callers may supply deterministic mock results.
type HealthProbe interface {
	Check(context.Context, Node) HealthResult
}

type MockHealthProbe struct {
	mu      sync.RWMutex
	results map[string]HealthResult
}

func NewMockHealthProbe() *MockHealthProbe {
	return &MockHealthProbe{results: make(map[string]HealthResult)}
}

func (m *MockHealthProbe) Set(result HealthResult) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.results[result.NodeID] = result
}

func (m *MockHealthProbe) Check(_ context.Context, node Node) HealthResult {
	if m == nil {
		return HealthResultAt(node.ID, "mock", false, 0, 0, "probe_unavailable")
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if result, ok := m.results[node.ID]; ok {
		return result
	}
	return HealthResultAt(node.ID, "mock", false, 0, 0, "unknown")
}

type HealthTracker struct {
	mu    sync.RWMutex
	Nodes map[string]Node
}

func NewHealthTracker(nodes []Node) *HealthTracker {
	tracker := &HealthTracker{Nodes: make(map[string]Node, len(nodes))}
	for _, node := range nodes {
		tracker.Nodes[node.ID] = node
	}
	return tracker
}

// Record updates local counters from an already collected result.
func (h *HealthTracker) Record(result HealthResult) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	node, ok := h.Nodes[result.NodeID]
	if !ok {
		return
	}
	if result.Success {
		node.ConsecutiveSuccesses++
		node.ConsecutiveFailures = 0
	} else {
		node.ConsecutiveFailures++
		node.ConsecutiveSuccesses = 0
	}
	node.HealthStatus = healthStatusForCounters(node.ConsecutiveFailures, node.ConsecutiveSuccesses, DefaultPolicyConfig().FailureThreshold)
	h.Nodes[result.NodeID] = node
}

func healthStatusForCounters(failures, successes, threshold int) HealthStatus {
	if threshold <= 0 {
		threshold = DefaultPolicyConfig().FailureThreshold
	}
	if failures >= threshold {
		return HealthFailed
	}
	if failures > 0 {
		return HealthDegraded
	}
	if successes > 0 {
		return HealthHealthy
	}
	return HealthUnknown
}

func (h *HealthTracker) Snapshot() []Node {
	if h == nil {
		return nil
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	result := make([]Node, 0, len(h.Nodes))
	for _, node := range h.Nodes {
		result = append(result, node)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func (h *HealthTracker) Check(ctx context.Context, probe HealthProbe) []HealthResult {
	if h == nil || probe == nil {
		return nil
	}
	nodes := h.Snapshot()
	results := make([]HealthResult, 0, len(nodes))
	for _, node := range nodes {
		result := probe.Check(ctx, node)
		h.Record(result)
		results = append(results, result)
	}
	return results
}

func HealthResultAt(nodeID, kind string, success bool, code int, latency time.Duration, errorCode string) HealthResult {
	return HealthResult{NodeID: nodeID, Kind: kind, Success: success, StatusCode: code, Latency: latency, ErrorCode: errorCode}
}
