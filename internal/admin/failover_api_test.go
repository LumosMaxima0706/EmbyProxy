package admin

import (
	"net/http"
	"net/http/httptest"
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
