package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"embyproxy/internal/config"
	"embyproxy/internal/failover"
)

func failoverTestConfig() config.Config { return config.Config{AdminToken: "strong-admin-token"} }

func TestFailoverAPIRequiresAuthentication(t *testing.T) {
	handler := newAuthTestHandler(t, failoverTestConfig())
	req := httptest.NewRequest(http.MethodGet, "https://proxy.example/api/admin/failover/status", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAllFailoverAPIsRequireAuthentication(t *testing.T) {
	handler := newAuthTestHandler(t, failoverTestConfig())
	for _, endpoint := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/admin/failover/status"},
		{http.MethodPost, "/api/admin/failover/check-now"},
		{http.MethodPost, "/api/admin/failover/mode"},
		{http.MethodPost, "/api/admin/failover/switch"},
		{http.MethodGet, "/api/admin/failover/events"},
		{http.MethodGet, "/api/admin/traffic/status"},
		{http.MethodPost, "/api/admin/traffic/manual-sample"},
		{http.MethodGet, "/api/admin/dns/status"},
		{http.MethodPost, "/api/admin/dns/dry-run"},
		{http.MethodPost, "/api/admin/dns/apply"},
	} {
		req := httptest.NewRequest(endpoint.method, "https://proxy.example"+endpoint.path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s status = %d", endpoint.method, endpoint.path, rec.Code)
		}
	}
}

func TestFailoverAPIStatusAndMode(t *testing.T) {
	handler := newAuthTestHandler(t, failoverTestConfig())
	handler.SetFailoverController(failover.NewController([]failover.Node{
		{ID: "nosla", Name: "NOSLA", Role: failover.RolePrimary, Enabled: true, HealthStatus: failover.HealthHealthy},
		{ID: "bwg", Name: "BWG", Role: failover.RoleFallback, Enabled: true, HealthStatus: failover.HealthHealthy},
	}, failover.DefaultPolicyConfig(), failover.NewMockDNSProvider()))
	login := serveAdminJSON(t, handler, http.MethodPost, "/admin/auth/login", map[string]any{"token": "strong-admin-token"}, nil)
	if login.Code != http.StatusOK {
		t.Fatalf("login status = %d", login.Code)
	}
	cookie := login.Result().Cookies()[0]
	status := serveAdminJSON(t, handler, http.MethodGet, "/api/admin/failover/status", nil, cookie)
	if status.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", status.Code, status.Body.String())
	}
	mode := serveAdminJSON(t, handler, http.MethodPost, "/api/admin/failover/mode", map[string]any{"mode": "force_bwg"}, cookie)
	if mode.Code != http.StatusOK {
		t.Fatalf("mode = %d body=%s", mode.Code, mode.Body.String())
	}
}

func TestFailoverAPIDNSDryRunUsesMockProvider(t *testing.T) {
	handler := newAuthTestHandler(t, failoverTestConfig())
	controller := failover.NewController([]failover.Node{
		{ID: "nosla", Name: "NOSLA", Role: failover.RolePrimary, Enabled: true, HealthStatus: failover.HealthHealthy},
		{ID: "bwg", Name: "BWG", Role: failover.RoleFallback, Enabled: true, HealthStatus: failover.HealthHealthy},
	}, failover.DefaultPolicyConfig(), failover.NewMockDNSProvider())
	controller.ConfigureDNSGuard(failover.DNSGuardConfig{
		ProviderMode: failover.DNSProviderModeMock,
		Allowlist:    []failover.DNSRecordRule{{Name: "stream.example", Type: "A"}},
	})
	controller.RestoreState(failover.State{Mode: failover.ModeAuto, ActiveNodeID: "bwg", CurrentCycleKey: "old-cycle"})
	var writes int
	controller.SetDNSRunWriter(func(failover.DNSChange, bool) error { writes++; return nil })
	handler.SetFailoverController(controller)
	login := serveAdminJSON(t, handler, http.MethodPost, "/admin/auth/login", map[string]any{"token": "strong-admin-token"}, nil)
	cookie := login.Result().Cookies()[0]
	response := serveAdminJSON(t, handler, http.MethodPost, "/api/admin/dns/dry-run", map[string]any{
		"node_id": "bwg", "name": "stream.example", "type": "A", "value": "192.0.2.10", "ttl": 60,
	}, cookie)
	if response.Code != http.StatusOK || writes != 1 {
		t.Fatalf("status=%d writes=%d body=%s", response.Code, writes, response.Body.String())
	}
	state, _ := controller.Status()
	if state.ActiveNodeID != "bwg" || state.CurrentCycleKey != "old-cycle" || !state.LastEvaluationAt.IsZero() {
		t.Fatalf("dry-run changed state: %+v", state)
	}
}

func TestFailoverAPIDNSApplyRequiresConfirmationAndBoundDryRun(t *testing.T) {
	handler := newAuthTestHandler(t, failoverTestConfig())
	provider := failover.NewMockDNSProvider()
	controller := failover.NewController([]failover.Node{
		{ID: "nosla", Role: failover.RolePrimary, Enabled: true, HealthStatus: failover.HealthHealthy},
		{ID: "bwg", Role: failover.RoleFallback, Enabled: true, HealthStatus: failover.HealthHealthy},
	}, failover.DefaultPolicyConfig(), provider)
	controller.ConfigureDNSGuard(failover.DNSGuardConfig{
		ProviderMode: failover.DNSProviderModeMock,
		Allowlist:    []failover.DNSRecordRule{{Name: "stream.example", Type: "A"}},
	})
	handler.SetFailoverController(controller)
	login := serveAdminJSON(t, handler, http.MethodPost, "/admin/auth/login", map[string]any{"token": "strong-admin-token"}, nil)
	cookie := login.Result().Cookies()[0]
	apply := map[string]any{"node_id": "bwg", "name": "stream.example", "type": "A", "value": "192.0.2.10", "ttl": 60}

	missingConfirm := serveAdminJSON(t, handler, http.MethodPost, "/api/admin/dns/apply", apply, cookie)
	if missingConfirm.Code != http.StatusBadRequest || !strings.Contains(missingConfirm.Body.String(), "CONFIRMATION_REQUIRED") {
		t.Fatalf("missing confirm status=%d body=%s", missingConfirm.Code, missingConfirm.Body.String())
	}
	apply["confirm"] = false
	falseConfirm := serveAdminJSON(t, handler, http.MethodPost, "/api/admin/dns/apply", apply, cookie)
	if falseConfirm.Code != http.StatusBadRequest || provider.ApplyCount != 0 {
		t.Fatalf("false confirm status=%d provider calls=%d", falseConfirm.Code, provider.ApplyCount)
	}
	apply["confirm"] = true
	missingDryRun := serveAdminJSON(t, handler, http.MethodPost, "/api/admin/dns/apply", apply, cookie)
	if missingDryRun.Code != http.StatusConflict || !strings.Contains(missingDryRun.Body.String(), "DNS_DRY_RUN_REQUIRED") {
		t.Fatalf("missing dry-run status=%d body=%s", missingDryRun.Code, missingDryRun.Body.String())
	}

	dryRun := serveAdminJSON(t, handler, http.MethodPost, "/api/admin/dns/dry-run", map[string]any{
		"node_id": "bwg", "name": "stream.example", "type": "A", "value": "192.0.2.10", "ttl": 60,
	}, cookie)
	if dryRun.Code != http.StatusOK {
		t.Fatalf("dry-run status=%d body=%s", dryRun.Code, dryRun.Body.String())
	}
	if dryRun.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("dry-run Cache-Control = %q", dryRun.Header().Get("Cache-Control"))
	}
	var dryRunBody struct {
		ID string `json:"dry_run_id"`
	}
	if err := json.Unmarshal(dryRun.Body.Bytes(), &dryRunBody); err != nil || dryRunBody.ID == "" {
		t.Fatalf("dry-run response invalid: err=%v", err)
	}
	apply["dry_run_id"] = dryRunBody.ID
	accepted := serveAdminJSON(t, handler, http.MethodPost, "/api/admin/dns/apply", apply, cookie)
	if accepted.Code != http.StatusOK || provider.ApplyCount != 1 {
		t.Fatalf("apply status=%d provider calls=%d body=%s", accepted.Code, provider.ApplyCount, accepted.Body.String())
	}
	reused := serveAdminJSON(t, handler, http.MethodPost, "/api/admin/dns/apply", apply, cookie)
	if reused.Code != http.StatusBadGateway || provider.ApplyCount != 1 {
		t.Fatalf("reused status=%d provider calls=%d", reused.Code, provider.ApplyCount)
	}
}

func TestFailoverAPISwitchRequiresConfirmationAndNeverCallsDNS(t *testing.T) {
	handler := newAuthTestHandler(t, failoverTestConfig())
	provider := failover.NewMockDNSProvider()
	controller := failover.NewController([]failover.Node{
		{ID: "nosla", Role: failover.RolePrimary, Enabled: true, HealthStatus: failover.HealthHealthy},
		{ID: "bwg", Role: failover.RoleFallback, Enabled: true, HealthStatus: failover.HealthHealthy},
		{ID: "disabled", Role: failover.RoleFallback, Enabled: false, HealthStatus: failover.HealthHealthy},
		{ID: "maintenance", Role: failover.RoleFallback, Enabled: true, Maintenance: true, HealthStatus: failover.HealthHealthy},
	}, failover.DefaultPolicyConfig(), provider)
	handler.SetFailoverController(controller)
	login := serveAdminJSON(t, handler, http.MethodPost, "/admin/auth/login", map[string]any{"token": "strong-admin-token"}, nil)
	cookie := login.Result().Cookies()[0]

	missingConfirm := serveAdminJSON(t, handler, http.MethodPost, "/api/admin/failover/switch", map[string]any{"node_id": "bwg"}, cookie)
	if missingConfirm.Code != http.StatusBadRequest {
		t.Fatalf("missing confirm status=%d", missingConfirm.Code)
	}
	falseConfirm := serveAdminJSON(t, handler, http.MethodPost, "/api/admin/failover/switch", map[string]any{"node_id": "bwg", "confirm": false}, cookie)
	if falseConfirm.Code != http.StatusBadRequest {
		t.Fatalf("false confirm status=%d", falseConfirm.Code)
	}
	unknown := serveAdminJSON(t, handler, http.MethodPost, "/api/admin/failover/switch", map[string]any{"node_id": "unknown", "confirm": true}, cookie)
	if unknown.Code != http.StatusConflict {
		t.Fatalf("unknown status=%d", unknown.Code)
	}
	for _, nodeID := range []string{"disabled", "maintenance"} {
		rejected := serveAdminJSON(t, handler, http.MethodPost, "/api/admin/failover/switch", map[string]any{"node_id": nodeID, "confirm": true}, cookie)
		if rejected.Code != http.StatusConflict {
			t.Fatalf("%s status=%d", nodeID, rejected.Code)
		}
	}
	accepted := serveAdminJSON(t, handler, http.MethodPost, "/api/admin/failover/switch", map[string]any{"node_id": "bwg", "confirm": true, "reason": "manual"}, cookie)
	if accepted.Code != http.StatusOK || provider.ApplyCount != 0 {
		t.Fatalf("switch status=%d provider calls=%d", accepted.Code, provider.ApplyCount)
	}
}

func TestFailoverUISkeletonIsEmbedded(t *testing.T) {
	for _, marker := range []string{"tabFailover", "failoverCards", "failoverEvents", "pauseFailoverAutomation", "dryRunFailoverDNS"} {
		if !strings.Contains(indexHTML, marker) {
			t.Fatalf("admin UI missing %q", marker)
		}
	}
}

func TestUnknownTrafficAPIDoesNotReportZeroUsage(t *testing.T) {
	view := failoverTrafficView(failover.UnknownTraffic("nosla"))
	if view["quality"] != failover.TrafficUnknown {
		t.Fatalf("view = %+v", view)
	}
	for _, field := range []string{"inbound_bytes", "outbound_bytes", "total_bytes", "quota_bytes"} {
		if _, ok := view[field]; ok {
			t.Fatalf("unknown traffic includes %s: %+v", field, view)
		}
	}
}

func TestDNSStatusIncludesPersistedLatestRun(t *testing.T) {
	handler := newAuthTestHandler(t, failoverTestConfig())
	handler.SetFailoverController(failover.NewController([]failover.Node{
		{ID: "nosla", Role: failover.RolePrimary, Enabled: true},
		{ID: "bwg", Role: failover.RoleFallback, Enabled: true},
	}, failover.DefaultPolicyConfig(), failover.NewMockDNSProvider()))
	handler.SetDNSStatusReader(func() map[string]any { return map[string]any{"available": true, "success": false} })
	login := serveAdminJSON(t, handler, http.MethodPost, "/admin/auth/login", map[string]any{"token": "strong-admin-token"}, nil)
	cookie := login.Result().Cookies()[0]
	response := serveAdminJSON(t, handler, http.MethodGet, "/api/admin/dns/status", nil, cookie)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"last_run"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestFailoverAPIEligibilityUsesControllerPolicy(t *testing.T) {
	handler := newAuthTestHandler(t, failoverTestConfig())
	controller := failover.NewController([]failover.Node{
		{ID: "primary", Role: failover.RolePrimary, Enabled: true, HealthStatus: failover.HealthHealthy, Priority: 1},
		{ID: "fallback", Role: failover.RoleFallback, Enabled: true, HealthStatus: failover.HealthHealthy, Priority: 2},
	}, failover.DefaultPolicyConfig(), nil)
	handler.SetFailoverController(controller)
	login := serveAdminJSON(t, handler, http.MethodPost, "/admin/auth/login", map[string]any{"token": "strong-admin-token"}, nil)
	cookie := login.Result().Cookies()[0]

	if err := controller.SetHealth(failover.HealthResultAt("primary", "mock", false, 503, 0, "unavailable")); err != nil {
		t.Fatal(err)
	}
	if eligible := failoverEligibleFromResponse(t, serveAdminJSON(t, handler, http.MethodGet, "/api/admin/failover/status", nil, cookie), "primary"); eligible {
		t.Fatal("single failure reported failover eligible")
	}
	for i := 0; i < 2; i++ {
		if err := controller.SetHealth(failover.HealthResultAt("primary", "mock", false, 503, 0, "unavailable")); err != nil {
			t.Fatal(err)
		}
	}
	if eligible := failoverEligibleFromResponse(t, serveAdminJSON(t, handler, http.MethodGet, "/api/admin/failover/status", nil, cookie), "primary"); !eligible {
		t.Fatal("threshold failure was not reported failover eligible")
	}
	if err := controller.SetMode(failover.ModeForceBWG); err != nil {
		t.Fatal(err)
	}
	if eligible := failoverEligibleFromResponse(t, serveAdminJSON(t, handler, http.MethodGet, "/api/admin/failover/status", nil, cookie), "primary"); eligible {
		t.Fatal("forced mode reported automatic failover eligible")
	}
}

func TestFailoverCheckNowDoesNotAdvanceCycle(t *testing.T) {
	handler := newAuthTestHandler(t, failoverTestConfig())
	controller := failover.NewController([]failover.Node{
		{ID: "primary", Role: failover.RolePrimary, Enabled: true, HealthStatus: failover.HealthHealthy, Priority: 1, ResetDay: 21, ResetTimezone: "UTC"},
		{ID: "fallback", Role: failover.RoleFallback, Enabled: true, HealthStatus: failover.HealthHealthy, Priority: 2},
	}, failover.DefaultPolicyConfig(), nil)
	controller.RestoreState(failover.State{Mode: failover.ModeAuto, ActiveNodeID: "fallback", CurrentCycleKey: "2026-08-21"})
	controller.SetNow(func() time.Time { return time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC) })
	for i := 0; i < 3; i++ {
		if err := controller.SetHealth(failover.HealthResultAt("primary", "mock", true, 200, 0, "")); err != nil {
			t.Fatal(err)
		}
	}
	if err := controller.SetTraffic(failover.TrafficSample{NodeID: "primary", CycleKey: "2026-09-21", TotalBytes: 1, QuotaBytes: 1000, Quality: failover.TrafficKnown}); err != nil {
		t.Fatal(err)
	}
	handler.SetFailoverController(controller)
	login := serveAdminJSON(t, handler, http.MethodPost, "/admin/auth/login", map[string]any{"token": "strong-admin-token"}, nil)
	cookie := login.Result().Cookies()[0]
	before, _ := controller.Status()
	response := serveAdminJSON(t, handler, http.MethodPost, "/api/admin/failover/check-now", nil, cookie)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"node_id":"primary"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	after, _ := controller.Status()
	if after.ActiveNodeID != before.ActiveNodeID || after.CurrentCycleKey != before.CurrentCycleKey || !after.LastEvaluationAt.Equal(before.LastEvaluationAt) {
		t.Fatalf("check-now changed state: before=%+v after=%+v", before, after)
	}
}

func failoverEligibleFromResponse(t *testing.T, response *httptest.ResponseRecorder, nodeID string) bool {
	t.Helper()
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Nodes []struct {
			NodeID   string `json:"node_id"`
			Eligible bool   `json:"failover_eligible"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, node := range body.Nodes {
		if node.NodeID == nodeID {
			return node.Eligible
		}
	}
	t.Fatalf("node %q missing from response", nodeID)
	return false
}
