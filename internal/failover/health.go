package failover

import "time"

type HealthTracker struct {
	Nodes map[string]Node
}

func NewHealthTracker(nodes []Node) *HealthTracker {
	tracker := &HealthTracker{Nodes: make(map[string]Node, len(nodes))}
	for _, node := range nodes {
		tracker.Nodes[node.ID] = node
	}
	return tracker
}

// Record updates only local counters. Network probing is intentionally absent
// from Phase 2A; a later adapter will produce HealthResult values.
func (h *HealthTracker) Record(result HealthResult) {
	if h == nil {
		return
	}
	node, ok := h.Nodes[result.NodeID]
	if !ok {
		return
	}
	if result.Success {
		node.HealthStatus = HealthHealthy
		node.ConsecutiveSuccesses++
		node.ConsecutiveFailures = 0
	} else {
		node.HealthStatus = HealthFailed
		node.ConsecutiveFailures++
		node.ConsecutiveSuccesses = 0
	}
	h.Nodes[result.NodeID] = node
}

func (h *HealthTracker) Snapshot() []Node {
	if h == nil {
		return nil
	}
	result := make([]Node, 0, len(h.Nodes))
	for _, node := range h.Nodes {
		result = append(result, node)
	}
	return result
}

func HealthResultAt(nodeID, kind string, success bool, code int, latency time.Duration, errorCode string) HealthResult {
	return HealthResult{NodeID: nodeID, Kind: kind, Success: success, StatusCode: code, Latency: latency, ErrorCode: errorCode}
}
