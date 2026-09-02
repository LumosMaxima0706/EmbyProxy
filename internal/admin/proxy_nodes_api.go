package admin

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"embyproxy/internal/storage"
)

func (h *Handler) handleProxyNodesAPI(w http.ResponseWriter, r *http.Request, path string) {
	ctx := r.Context()
	if r.Method == http.MethodGet && path == "/api/admin/proxy-nodes" {
		items, err := h.store.ListProxyNodes(ctx)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "NODE_LIST_FAILED"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "nodes": items})
		return
	}
	if r.Method == http.MethodPost && path == "/api/admin/proxy-nodes" {
		var body struct {
			Name          string `json:"name"`
			PublicAddress string `json:"public_address"`
			QuotaBytes    int64  `json:"quota_bytes"`
			ResetDay      int    `json:"reset_day"`
			ResetTimezone string `json:"reset_timezone"`
			Priority      int    `json:"priority"`
		}
		if !decodeAuthJSON(w, r, &body) {
			return
		}
		enrollment, token, err := h.store.CreateProxyNode(ctx, storage.ProxyNode{Name: body.Name, PublicAddress: body.PublicAddress, QuotaBytes: body.QuotaBytes, ResetDay: body.ResetDay, ResetTimezone: body.ResetTimezone, Priority: body.Priority}, 15*time.Minute)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "INVALID_NODE"})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "enrollment": enrollment, "install_command": buildEnrollmentCommand(enrollment.ID, token)})
		return
	}
	if r.Method == http.MethodPost && path == "/api/admin/proxy-nodes/reorder" {
		var body struct {
			IDs []string `json:"ids"`
		}
		if !decodeAuthJSON(w, r, &body) {
			return
		}
		if len(body.IDs) == 0 || len(body.IDs) > 100 {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "INVALID_ORDER"})
			return
		}
		if err := h.store.ReorderProxyNodes(ctx, body.IDs); err != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "ORDER_FAILED"})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true})
		return
	}
	prefix := "/api/admin/proxy-nodes/"
	if strings.HasPrefix(path, prefix) {
		parts := strings.Split(strings.TrimPrefix(path, prefix), "/")
		id := parts[0]
		if id == "" {
			http.NotFound(w, r)
			return
		}
		if r.Method == http.MethodPost && len(parts) == 2 && parts[1] == "revoke" {
			force := r.URL.Query().Get("force") == "true"
			if err := h.store.RevokeProxyNode(ctx, id, force); err != nil {
				writeJSON(w, 404, map[string]any{"ok": false, "error": "NODE_NOT_FOUND"})
				return
			}
			writeJSON(w, 200, map[string]any{"ok": true, "state": map[bool]string{true: "revoked", false: "draining"}[force]})
			return
		}
		if r.Method == http.MethodPatch && len(parts) == 1 {
			n, err := h.store.GetProxyNode(ctx, id)
			if err != nil || n == nil {
				writeJSON(w, 404, map[string]any{"ok": false, "error": "NODE_NOT_FOUND"})
				return
			}
			var body map[string]any
			if !decodeAuthJSON(w, r, &body) {
				return
			}
			if v, ok := body["enabled"].(bool); ok {
				n.Enabled = v
			}
			if v, ok := body["priority"].(float64); ok {
				n.Priority = int(v)
			}
			if v, ok := body["quota_bytes"].(float64); ok {
				n.QuotaBytes = int64(v)
			}
			if v, ok := body["reset_day"].(float64); ok {
				n.ResetDay = int(v)
			}
			if v, ok := body["reset_timezone"].(string); ok {
				n.ResetTimezone = v
			}
			if err := h.store.UpdateProxyNode(ctx, *n); err != nil {
				writeJSON(w, 400, map[string]any{"ok": false, "error": "INVALID_NODE"})
				return
			}
			writeJSON(w, 200, map[string]any{"ok": true, "node": n})
			return
		}
	}
	http.NotFound(w, r)
}

func buildEnrollmentCommand(id, token string) string {
	return "curl --fail --silent --show-error --proto '=https' --tlsv1.2 https://OWNER_CONTROLLER/enroll/" + url.PathEscape(id) + "/" + url.PathEscape(token) + " | sudo sh"
}

func (h *Handler) handleEdgeEnrollment(w http.ResponseWriter, r *http.Request, path string) {
	if r.Method == http.MethodPost && strings.HasPrefix(path, "/api/edge/enroll/") {
		parts := strings.Split(strings.TrimPrefix(path, "/api/edge/enroll/"), "/")
		if len(parts) != 2 {
			http.NotFound(w, r)
			return
		}
		var body struct {
			Version string `json:"version"`
			Commit  string `json:"commit"`
		}
		if !decodeAuthJSON(w, r, &body) {
			return
		}
		node, credential, err := h.store.CompleteEnrollment(r.Context(), parts[0], parts[1], body.Version, body.Commit)
		if err != nil {
			writeJSON(w, 403, map[string]any{"ok": false, "error": "ENROLLMENT_DENIED"})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "node_id": node.ID, "credential": credential})
		return
	}
	if r.Method == http.MethodPost && strings.HasPrefix(path, "/api/edge/heartbeat/") {
		id := strings.TrimPrefix(path, "/api/edge/heartbeat/")
		var body struct {
			Credential, Version, Commit, State, LastError string
			PlaybackHealthy, ConfigSynced                 bool
		}
		if !decodeAuthJSON(w, r, &body) {
			return
		}
		if err := h.store.HeartbeatProxyNode(r.Context(), id, body.Credential, body.Version, body.Commit, body.State, body.PlaybackHealthy, body.ConfigSynced, body.LastError); err != nil {
			writeJSON(w, 403, map[string]any{"ok": false, "error": "HEARTBEAT_DENIED"})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true})
		return
	}
	http.NotFound(w, r)
}
