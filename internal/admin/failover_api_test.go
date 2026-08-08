package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
	var writes int
	controller.SetDNSRunWriter(func(failover.DNSChange, bool) error { writes++; return nil })
	handler.SetFailoverController(controller)
	login := serveAdminJSON(t, handler, http.MethodPost, "/admin/auth/login", map[string]any{"token": "strong-admin-token"}, nil)
	cookie := login.Result().Cookies()[0]
	response := serveAdminJSON(t, handler, http.MethodPost, "/api/admin/dns/dry-run", map[string]any{
		"name": "stream.example", "type": "A", "value": "192.0.2.10", "ttl": 60,
	}, cookie)
	if response.Code != http.StatusOK || writes != 1 {
		t.Fatalf("status=%d writes=%d body=%s", response.Code, writes, response.Body.String())
	}
	state, _ := controller.Status()
	if state.ActiveNodeID != "nosla" {
		t.Fatalf("state = %+v", state)
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
