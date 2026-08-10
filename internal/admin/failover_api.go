package admin

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"

	"embyproxy/internal/failover"
)

func (h *Handler) handleFailoverAPI(w http.ResponseWriter, r *http.Request, path string) {
	w.Header().Set("Cache-Control", "no-store")
	if h.failover == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "FAILOVER_UNAVAILABLE"})
		return
	}
	switch {
	case r.Method == http.MethodGet && path == "/api/admin/failover/status":
		state, nodes, eligibility := h.failover.StatusWithEligibility()
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
				"failover_eligible":            eligibility[node.ID],
				"consecutive_health_failures":  node.ConsecutiveFailures,
				"consecutive_health_successes": node.ConsecutiveSuccesses,
				"traffic":                      failoverTrafficView(node.Traffic),
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "state": state, "nodes": views})
	case r.Method == http.MethodPost && path == "/api/admin/failover/check-now":
		decision, err := h.failover.Evaluate()
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "FAILOVER_STATE_PERSIST_FAILED"})
			return
		}
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
		if confirmed, ok := body["confirm"].(bool); !ok || !confirmed {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "CONFIRMATION_REQUIRED"})
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
			traffic = append(traffic, map[string]any{"node_id": node.ID, "name": node.Name, "sample": failoverTrafficView(node.Traffic)})
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
		if nodeID == "" || invalidTrafficNumber(inbound) || invalidTrafficNumber(outbound) || invalidTrafficNumber(quota) || math.IsNaN(threshold) || math.IsInf(threshold, 0) || threshold < 0 || threshold > 100 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "INVALID_TRAFFIC_SAMPLE"})
			return
		}
		sample := failover.TrafficSample{NodeID: nodeID, CycleKey: asString(body["cycle_key"]), InboundBytes: int64(inbound), OutboundBytes: int64(outbound), TotalBytes: int64(inbound + outbound), QuotaBytes: int64(quota), ThresholdPct: threshold, Quality: failover.TrafficKnown}
		if err := h.failover.SetTraffic(sample); err != nil {
			if err == failover.ErrInvalidTraffic {
				writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "INVALID_TRAFFIC_SAMPLE"})
				return
			}
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "UNKNOWN_NODE"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	case r.Method == http.MethodGet && path == "/api/admin/dns/status":
		state, _ := h.failover.Status()
		response := map[string]any{"ok": true, "provider": h.failover.DNSProviderMode(), "state": map[string]any{"observed_node_id": state.ObservedDNSNodeID, "reconciliation_required": state.ReconciliationRequired}}
		if h.dnsStatusReader != nil {
			response["last_run"] = h.dnsStatusReader()
		}
		writeJSON(w, http.StatusOK, response)
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
			nodeID := strings.TrimSpace(asString(body["node_id"]))
			plan, err := h.failover.PrepareDNSApply(r.Context(), change, nodeID)
			if err != nil {
				writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": "DNS_DRY_RUN_REJECTED"})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"ok": true, "dry_run": true, "dry_run_id": plan.ID,
				"target_node_id": plan.TargetNodeID, "provider_mode": plan.ProviderMode,
				"generated_at": plan.GeneratedAt, "expires_at": plan.ExpiresAt,
				"change": map[string]any{"name": plan.Change.Name, "type": plan.Change.Type, "ttl": plan.Change.TTL},
				"note":   plan.Note,
			})
			return
		}
		if confirmed, ok := body["confirm"].(bool); !ok || !confirmed {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "CONFIRMATION_REQUIRED"})
			return
		}
		nodeID := asString(body["node_id"])
		dryRunID := strings.TrimSpace(asString(body["dry_run_id"]))
		if dryRunID == "" {
			writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": "DNS_DRY_RUN_REQUIRED"})
			return
		}
		if err := h.failover.ApplyDNSAndCommit(r.Context(), change, nodeID, dryRunID); err != nil {
			writeDNSApplyError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "verified": true})
	default:
		http.NotFound(w, r)
	}
}

func writeDNSApplyError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, failover.ErrDNSInvalidRecordName),
		errors.Is(err, failover.ErrDNSInvalidRecordValue),
		errors.Is(err, failover.ErrDNSInvalidTTL),
		errors.Is(err, failover.ErrDNSUnsupportedRecordType):
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "INVALID_DNS_CHANGE"})
	case errors.Is(err, failover.ErrDNSDryRunRequired),
		errors.Is(err, failover.ErrDNSDryRunExpired),
		errors.Is(err, failover.ErrDNSDryRunMismatch),
		errors.Is(err, failover.ErrDNSRecordNotAllowed),
		errors.Is(err, failover.ErrDNSProviderModeDenied),
		errors.Is(err, failover.ErrDNSRollbackMetadataRequired),
		errors.Is(err, failover.ErrUnknownNode),
		errors.Is(err, failover.ErrNodeNotEligible):
		writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": "DNS_APPLY_REJECTED"})
	default:
		writeJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": "DNS_APPLY_NOT_COMMITTED"})
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

func invalidTrafficNumber(value float64) bool {
	return math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > float64(1<<63-1)
}

func failoverTrafficView(sample failover.TrafficSample) map[string]any {
	quality := sample.Quality
	if quality == "" {
		quality = failover.TrafficUnknown
	}
	view := map[string]any{
		"node_id": sample.NodeID,
		"quality": quality,
	}
	if quality != failover.TrafficKnown {
		return view
	}
	view["cycle_key"] = sample.CycleKey
	view["inbound_bytes"] = sample.InboundBytes
	view["outbound_bytes"] = sample.OutboundBytes
	view["total_bytes"] = sample.TotalBytes
	view["quota_bytes"] = sample.QuotaBytes
	view["threshold_percent"] = sample.ThresholdPct
	view["sampled_at"] = sample.SampledAt
	return view
}
