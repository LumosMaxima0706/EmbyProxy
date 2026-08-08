package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"embyproxy/internal/failover"
)

func (h *Handler) handleFailoverAPI(w http.ResponseWriter, r *http.Request, path string) {
	if h.failover == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "FAILOVER_UNAVAILABLE"})
		return
	}
	switch {
	case r.Method == http.MethodGet && path == "/api/admin/failover/status":
		state, nodes := h.failover.Status()
		views := make([]map[string]any, 0, len(nodes))
		for _, node := range nodes {
			views = append(views, map[string]any{
				"node_id":                      node.ID,
				"name":                         node.Name,
				"role":                         node.Role,
				"public_host":                  node.PublicHost,
				"enabled":                      node.Enabled,
				"maintenance_mode":             node.Maintenance,
				"health_status":                node.HealthStatus,
				"consecutive_health_failures":  node.ConsecutiveFailures,
				"consecutive_health_successes": node.ConsecutiveSuccesses,
				"traffic":                      node.Traffic,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "state": state, "nodes": views})
	case r.Method == http.MethodPost && path == "/api/admin/failover/check-now":
		decision := h.failover.Evaluate()
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "dry_run": true, "decision": decision})
	case r.Method == http.MethodPost && path == "/api/admin/failover/mode":
		body, ok := decodeFailoverJSON(w, r)
		if !ok {
			return
		}
		if err := h.failover.SetMode(failover.Mode(strings.TrimSpace(asString(body["mode"])))); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "INVALID_MODE"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	case r.Method == http.MethodPost && path == "/api/admin/failover/switch":
		body, ok := decodeFailoverJSON(w, r)
		if !ok {
			return
		}
		reason := failover.RedactReason(asString(body["reason"]))
		decision := failover.Decision{NodeID: asString(body["node_id"]), Change: true, Reason: reason}
		if err := h.failover.ApplyDecision(decision, reason); err != nil {
			writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": "SWITCH_NOT_APPLIED"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	case r.Method == http.MethodGet && path == "/api/admin/failover/events":
		limit := 50
		if value, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && value > 0 && value < 200 {
			limit = value
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "events": h.failover.Events(limit)})
	case r.Method == http.MethodGet && path == "/api/admin/traffic/status":
		_, nodes := h.failover.Status()
		traffic := make([]map[string]any, 0, len(nodes))
		for _, node := range nodes {
			traffic = append(traffic, map[string]any{"node_id": node.ID, "name": node.Name, "sample": node.Traffic})
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "nodes": traffic})
	case r.Method == http.MethodPost && path == "/api/admin/traffic/manual-sample":
		body, ok := decodeFailoverJSON(w, r)
		if !ok {
			return
		}
		nodeID := asString(body["node_id"])
		inbound, _ := body["inbound_bytes"].(float64)
		outbound, _ := body["outbound_bytes"].(float64)
		quota, _ := body["quota_bytes"].(float64)
		threshold, _ := body["threshold_percent"].(float64)
		sample := failover.TrafficSample{NodeID: nodeID, CycleKey: asString(body["cycle_key"]), InboundBytes: int64(inbound), OutboundBytes: int64(outbound), TotalBytes: int64(inbound + outbound), QuotaBytes: int64(quota), ThresholdPct: threshold, Quality: failover.TrafficKnown}
		if err := h.failover.SetTraffic(sample); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "UNKNOWN_NODE"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	case r.Method == http.MethodGet && path == "/api/admin/dns/status":
		state, _ := h.failover.Status()
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "provider": "mock", "state": map[string]any{"observed_node_id": state.ObservedDNSNodeID, "reconciliation_required": state.ReconciliationRequired}})
	case r.Method == http.MethodPost && (path == "/api/admin/dns/dry-run" || path == "/api/admin/dns/apply"):
		body, ok := decodeFailoverJSON(w, r)
		if !ok {
			return
		}
		change := failover.DNSChange{Name: asString(body["name"]), Type: asString(body["type"]), Value: asString(body["value"]), TTL: intFromJSON(body["ttl"]), DryRun: path == "/api/admin/dns/dry-run"}
		if change.TTL == 0 {
			change.TTL = 60
		}
		if change.DryRun {
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "dry_run": true, "change": map[string]any{"name": change.Name, "type": change.Type, "ttl": change.TTL}})
			return
		}
		nodeID := asString(body["node_id"])
		if err := h.failover.ApplyDNSAndCommit(context.Background(), change, nodeID); err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": "DNS_APPLY_NOT_COMMITTED"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "verified": true})
	default:
		http.NotFound(w, r)
	}
}

func decodeFailoverJSON(w http.ResponseWriter, r *http.Request) (map[string]any, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "INVALID_JSON"})
		return nil, false
	}
	return body, true
}

func intFromJSON(value any) int {
	n, _ := value.(float64)
	return int(n)
}
