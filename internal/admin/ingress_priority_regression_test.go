package admin

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"embyproxy/internal/config"
	"embyproxy/internal/storage"
)

func TestLegacyManualFixedEnablesHealthFallbackWithoutDNSWrite(t *testing.T) {
	h := newAuthTestHandler(t, config.Config{PublicIngressHost: "stream.example.com"})
	s := newPublicIngressSwitcher(h)
	ctx := context.Background()
	state := publicIngressState{OperationID: "legacy", Phase: "verified", Trigger: "admin_manual", Mode: "fixed", ActiveNodeID: "missing", RequestVerified: true, CompletedAt: time.Now().Unix()}
	if err := s.save(ctx, state); err != nil {
		t.Fatal(err)
	}
	if err := s.reconcile(ctx); err == nil || !strings.Contains(err.Error(), "no_eligible_public_ingress_candidate") {
		t.Fatalf("fallback warning: %v", err)
	}
	updated := newPublicIngressSwitcher(h).status(ctx)
	if updated.Mode != "preferred" || updated.ActiveNodeID != state.ActiveNodeID || updated.Phase != "verified" {
		t.Fatalf("legacy operation lost: %+v", updated)
	}
	if status := s.schedulerStatus(ctx); status.Trigger != "automatic_health" {
		t.Fatalf("health check not enabled: %+v", status)
	}

	state.OperationID, state.Trigger = "explicit", "admin_fixed"
	if err := s.save(ctx, state); err != nil {
		t.Fatal(err)
	}
	if err := s.reconcile(ctx); err != nil {
		t.Fatalf("explicit pin changed: %v", err)
	}
	if updated := s.status(ctx); updated.Mode != "fixed" {
		t.Fatalf("explicit pin lost: %+v", updated)
	}
}

func TestIngressPriorityPatchPersistsZeroAndRejectsInvalidValues(t *testing.T) {
	h := newAuthTestHandler(t, config.Config{AdminToken: "strong-admin-token"})
	ctx := context.Background()
	a, _, err := h.store.CreateProxyNode(ctx, storage.ProxyNode{Name: "node-a", Priority: 1, ResetDay: 1}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := h.store.CreateProxyNode(ctx, storage.ProxyNode{Name: "node-b", Priority: 1, ResetDay: 1}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	cookie := serveAdminJSON(t, h, http.MethodPost, "/admin/auth/login", map[string]any{"token": "strong-admin-token"}, nil).Result().Cookies()[0]
	path := "/api/admin/proxy-nodes/" + b.NodeID
	for _, value := range []any{-1, 10001, 1.5, "0", nil} {
		resp := serveAdminJSON(t, h, http.MethodPatch, path, map[string]any{"priority": value}, cookie)
		if resp.Code != http.StatusBadRequest || !strings.Contains(resp.Body.String(), "INVALID_PRIORITY") {
			t.Fatalf("invalid %v accepted: %d %s", value, resp.Code, resp.Body.String())
		}
	}
	resp := serveAdminJSON(t, h, http.MethodPatch, path, map[string]any{"priority": 0}, cookie)
	if resp.Code != http.StatusOK {
		t.Fatalf("priority update: %d %s", resp.Code, resp.Body.String())
	}
	ordered, err := h.store.ListProxyNodes(ctx)
	if err != nil || len(ordered) != 2 || ordered[0].ID != b.NodeID || ordered[0].Priority != 0 || ordered[1].ID != a.NodeID {
		t.Fatalf("order after save: %+v err=%v", ordered, err)
	}
	resp = serveAdminJSON(t, h, http.MethodPatch, "/api/admin/proxy-nodes/"+a.NodeID, map[string]any{"priority": 0}, cookie)
	if resp.Code != http.StatusOK {
		t.Fatalf("tie update: %d %s", resp.Code, resp.Body.String())
	}
	ordered, err = h.store.ListProxyNodes(ctx)
	if err != nil || ordered[0].ID != a.NodeID || ordered[0].Priority != 0 || ordered[1].ID != b.NodeID || ordered[1].Priority != 1 {
		t.Fatalf("insertion did not shift peer: %+v err=%v", ordered, err)
	}
	resp = serveAdminJSON(t, h, http.MethodGet, "/api/admin/proxy-nodes", nil, cookie)
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"priority":0`) {
		t.Fatalf("priority readback: %d %s", resp.Code, resp.Body.String())
	}
}

func TestIngressPriorityPatchShiftsBWG(t *testing.T) {
	h := newAuthTestHandler(t, config.Config{AdminToken: "strong-admin-token"})
	ctx := context.Background()
	var ids []string
	for i, name := range []string{"nosla", "bwg", "161"} {
		e, _, err := h.store.CreateProxyNode(ctx, storage.ProxyNode{Name: name, Priority: i, ResetDay: 1}, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, e.NodeID)
	}
	cookie := serveAdminJSON(t, h, http.MethodPost, "/admin/auth/login", map[string]any{"token": "strong-admin-token"}, nil).Result().Cookies()[0]
	for _, body := range []map[string]any{{"priority": 1}, {"priority": 1, "quota_bytes": 100}} {
		resp := serveAdminJSON(t, h, http.MethodPatch, "/api/admin/proxy-nodes/"+ids[2], body, cookie)
		if resp.Code != http.StatusOK {
			t.Fatalf("patch failed: %d %s", resp.Code, resp.Body.String())
		}
		nodes, err := h.store.ListProxyNodes(ctx)
		if err != nil || nodes[0].ID != ids[0] || nodes[1].ID != ids[2] || nodes[2].ID != ids[1] || nodes[2].Priority != 2 {
			t.Fatalf("161 insertion failed: %+v err=%v", nodes, err)
		}
	}
	resp := serveAdminJSON(t, h, http.MethodGet, "/api/admin/proxy-nodes", nil, cookie)
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"priority":2`) {
		t.Fatalf("list refresh failed: %d %s", resp.Code, resp.Body.String())
	}
}

func TestIngressPriorityUIContract(t *testing.T) {
	for _, marker := range []string{"id=\"proxyPriorityModal\"", "function openProxyPriorityModal(id)", "function saveProxyPriority(event)", "<td>${Number(n.priority)}</td>", "mode:'preferred'", "故障时仍按优先级回退"} {
		if !strings.Contains(indexHTML, marker) {
			t.Fatalf("missing UI priority/fallback marker %q", marker)
		}
	}
	if strings.Contains(indexHTML, "priority || i + 1") || strings.Contains(indexHTML, "mode:fixed?'fixed':'preferred'") {
		t.Fatal("old priority or fixed-by-default UI remains")
	}
}
