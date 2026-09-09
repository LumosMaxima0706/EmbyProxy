package admin

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"embyproxy/internal/config"
	"embyproxy/internal/storage"
)

func TestDNSAutomationSavePayloadAndCredentialCharacters(t *testing.T) {
	t.Setenv("SPACESHIP_SECRET_ENCRYPTION_KEY", "test-master-key-for-dns-automation")
	h := newAuthTestHandler(t, config.Config{AdminToken: "strong-admin-token", EnrollmentControllerURL: "https://controller.example.net"})
	login := serveAdminJSON(t, h, http.MethodPost, "/admin/auth/login", map[string]any{"token": "strong-admin-token"}, nil)
	cookie := login.Result().Cookies()[0]
	resp := serveAdminJSON(t, h, http.MethodPost, "/api/admin/dns/automation", map[string]any{"managed_domain": "149077530.xyz", "api_key": "key_with_underscore", "api_secret": "secret-with-dash", "ttl": 300}, cookie)
	if resp.Code != http.StatusOK || strings.Contains(resp.Body.String(), "secret-with-dash") {
		t.Fatalf("save=%d %s", resp.Code, resp.Body.String())
	}
	var stored dnsAutomationStored
	ok, err := h.store.KV().GetJSON(t.Context(), "dns:automation", &stored)
	if err != nil || !ok {
		t.Fatalf("stored ok=%v err=%v", ok, err)
	}
	if stored.Provider != "spaceship" || stored.Domain != "149077530.xyz" {
		t.Fatalf("stored=%+v", stored)
	}
	secret, err := storage.DecryptSecret(stored.APISecretCipher)
	if err != nil || secret != "secret-with-dash" {
		t.Fatalf("decrypt=%q err=%v", secret, err)
	}
	resp = serveAdminJSON(t, h, http.MethodPost, "/api/admin/dns/automation", map[string]any{"managed_domain": "149077530.xyz", "api_key": "", "api_secret": "", "ttl": 300}, cookie)
	if resp.Code != http.StatusOK {
		t.Fatalf("preserve save=%d %s", resp.Code, resp.Body.String())
	}
}

func TestDNSAutomationValidationErrors(t *testing.T) {
	t.Setenv("SPACESHIP_SECRET_ENCRYPTION_KEY", "test-master-key-for-dns-automation")
	h := newAuthTestHandler(t, config.Config{AdminToken: "strong-admin-token"})
	login := serveAdminJSON(t, h, http.MethodPost, "/admin/auth/login", map[string]any{"token": "strong-admin-token"}, nil)
	cookie := login.Result().Cookies()[0]
	for _, tc := range []struct {
		name, domain string
		ttl          int
		want         string
	}{{"bad domain", "not a domain", 300, "INVALID_MANAGED_DOMAIN"}, {"bad ttl", "example.com", 1, "INVALID_DNS_TTL"}} {
		resp := serveAdminJSON(t, h, http.MethodPost, "/api/admin/dns/automation", map[string]any{"managed_domain": tc.domain, "api_key": "k", "api_secret": "s", "ttl": tc.ttl}, cookie)
		var body map[string]any
		_ = json.Unmarshal(resp.Body.Bytes(), &body)
		if body["error"] != tc.want {
			t.Errorf("%s error=%v body=%s", tc.name, body["error"], resp.Body.String())
		}
	}
}

func TestDNSAutomationTestMapsNetworkFailureToValidHTTPStatus(t *testing.T) {
	t.Setenv("SPACESHIP_SECRET_ENCRYPTION_KEY", "test-master-key-for-dns-automation")
	h := newAuthTestHandler(t, config.Config{AdminToken: "strong-admin-token"})
	login := serveAdminJSON(t, h, http.MethodPost, "/admin/auth/login", map[string]any{"token": "strong-admin-token"}, nil)
	if login.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", login.Code, login.Body.String())
	}
	cookie := login.Result().Cookies()[0]
	// Saving is deliberately local-only; the test request below exercises
	// the persisted credential read path against an unavailable provider.
	save := serveAdminJSON(t, h, http.MethodPost, "/api/admin/dns/automation", map[string]any{
		"managed_domain": "example.com",
		"api_key":        "opaque_key",
		"api_secret":     "opaque_secret",
		"ttl":            300,
	}, cookie)
	if save.Code != http.StatusOK {
		t.Fatalf("save status=%d body=%s", save.Code, save.Body.String())
	}
	h.cfg.SpaceshipAPIBaseURL = "http://127.0.0.1:1"
	probe := serveAdminJSON(t, h, http.MethodPost, "/api/admin/dns/automation?test=true", map[string]any{
		"managed_domain": "example.com",
		"api_key":        "",
		"api_secret":     "",
		"ttl":            300,
	}, cookie)
	if probe.Code != http.StatusServiceUnavailable {
		t.Fatalf("network probe status=%d body=%s", probe.Code, probe.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(probe.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["error"] != "SPACESHIP_NETWORK_ERROR" {
		t.Fatalf("error=%v body=%s", body["error"], probe.Body.String())
	}
	if got, ok := body["http_status"].(float64); !ok || got != 0 {
		t.Fatalf("http_status=%v body=%s", body["http_status"], probe.Body.String())
	}
	if strings.Contains(probe.Body.String(), "opaque_key") || strings.Contains(probe.Body.String(), "opaque_secret") {
		t.Fatalf("response leaked credential: %s", probe.Body.String())
	}
}
