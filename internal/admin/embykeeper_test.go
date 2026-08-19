package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"embyproxy/internal/config"
)

func TestEmbykeeperAPIRequiresAdminAuthentication(t *testing.T) {
	handler := newAuthTestHandler(t, config.Config{AdminToken: "test-admin-token"})
	req := httptest.NewRequest(http.MethodGet, "https://proxy.example/api/admin/embykeeper", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestEmbykeeperIntegrationStates(t *testing.T) {
	tests := []struct {
		name       string
		cfg        config.Config
		prepare    func(t *testing.T, cfg *config.Config)
		wantState  string
		wantReason string
		wantStatus bool
	}{
		{name: "disabled", cfg: config.Config{}, wantState: "disabled"},
		{name: "enabled no status path", cfg: config.Config{EmbykeeperIntegrationEnabled: true}, wantState: "unavailable", wantReason: "status_file_not_configured"},
		{name: "missing status", cfg: config.Config{EmbykeeperIntegrationEnabled: true}, prepare: func(t *testing.T, cfg *config.Config) {
			cfg.EmbykeeperStatusFile = filepath.Join(t.TempDir(), "status.json")
		}, wantState: "unavailable", wantReason: "status_file_missing"},
		{name: "malformed status", cfg: config.Config{EmbykeeperIntegrationEnabled: true}, prepare: func(t *testing.T, cfg *config.Config) {
			cfg.EmbykeeperStatusFile = writeTestKeeperStatus(t, `{not-json`)
		}, wantState: "unavailable", wantReason: "status_file_malformed"},
		{name: "unknown field", cfg: config.Config{EmbykeeperIntegrationEnabled: true}, prepare: func(t *testing.T, cfg *config.Config) {
			cfg.EmbykeeperStatusFile = writeTestKeeperStatus(t, `{"last_success":"","next_run":"","last_error":"","enabled_profiles_count":1,"failed_profiles_count":0,"secret":"must-not-pass"}`)
		}, wantState: "unavailable", wantReason: "status_file_malformed"},
		{name: "valid status", cfg: config.Config{EmbykeeperIntegrationEnabled: true, EmbykeeperExternalURL: "https://keeper.example.invalid", EmbykeeperDisplayName: "Keeper"}, prepare: func(t *testing.T, cfg *config.Config) {
			cfg.EmbykeeperStatusFile = writeTestKeeperStatus(t, `{"last_success":"2026-08-18T12:00:00Z","next_run":"2026-08-19T12:00:00Z","last_error":"","enabled_profiles_count":2,"failed_profiles_count":0}`)
		}, wantState: "available", wantStatus: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.prepare != nil {
				tt.prepare(t, &tt.cfg)
			}
			handler := newAuthTestHandler(t, tt.cfg)
			view := handler.embykeeperIntegration()
			if view.StatusState != tt.wantState || view.StatusReason != tt.wantReason || (view.Status != nil) != tt.wantStatus {
				t.Fatalf("view=%+v", view)
			}
			if tt.name == "valid status" && (view.Status.EnabledProfilesCount != 2 || view.ExternalURL != "https://keeper.example.invalid") {
				t.Fatalf("valid view=%+v", view)
			}
		})
	}
}

func TestEmbykeeperStatusEndpointReturnsOnlyAllowlistedFields(t *testing.T) {
	cfg := config.Config{
		AdminToken:                   "test-admin-token",
		EmbykeeperIntegrationEnabled: true,
		EmbykeeperExternalURL:        "https://keeper.example.invalid/console",
		EmbykeeperDisplayName:        "Keeper",
		EmbykeeperStatusFile:         writeTestKeeperStatus(t, `{"last_success":"2026-08-18T12:00:00Z","next_run":"2026-08-19T12:00:00Z","last_error":"UPSTREAM_TIMEOUT","enabled_profiles_count":2,"failed_profiles_count":1}`),
	}
	handler := newAuthTestHandler(t, cfg)
	handler.cfg.OwnerAdminAuthMode = "basic_only"
	handler.cfg.OwnerAdminHost = "owner.example.invalid"
	req := httptest.NewRequest(http.MethodGet, "https://owner.example.invalid/api/admin/embykeeper", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Host = "owner.example.invalid"
	req.Header.Set(ownerAdminAuthenticatedHeader, "1")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{`"enabled":true`, `"status_state":"available"`, `"last_error":"UPSTREAM_TIMEOUT"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q: %s", want, body)
		}
	}
	for _, forbidden := range []string{"password", "cache.json", "config.toml", "Authorization", "console.example.invalid"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("body leaked %q: %s", forbidden, body)
		}
	}
}

func TestEmbykeeperStatusRejectsUnsafeValues(t *testing.T) {
	for _, raw := range []string{
		`{"last_success":"not-a-time","next_run":"","last_error":"","enabled_profiles_count":1,"failed_profiles_count":0}`,
		`{"last_success":"","next_run":"","last_error":"free form error text","enabled_profiles_count":1,"failed_profiles_count":1}`,
		`{"last_success":"","next_run":"","last_error":"","enabled_profiles_count":0,"failed_profiles_count":1}`,
	} {
		path := writeTestKeeperStatus(t, raw)
		if _, reason := readEmbykeeperStatus(path); reason != "status_file_invalid" {
			t.Fatalf("reason=%q for %s", reason, raw)
		}
	}
}

func TestEmbykeeperPlaceholderTemplateContainsNoRuntimeData(t *testing.T) {
	cfg := config.Config{
		EmbykeeperIntegrationEnabled: true,
		EmbykeeperExternalURL:        "https://console.example.invalid",
		EmbykeeperDisplayName:        "Private display label",
	}
	handler := newAuthTestHandler(t, cfg)
	req := httptest.NewRequest(http.MethodGet, "https://proxy.example/api/admin/embykeeper/template", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Host = "owner.example.invalid"
	req.Header.Set(ownerAdminAuthenticatedHeader, "1")
	handler.cfg.OwnerAdminAuthMode = "basic_only"
	handler.cfg.OwnerAdminHost = "owner.example.invalid"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "emby.example.invalid") || strings.Contains(body, "console.example.invalid") || strings.Contains(body, "Private display label") {
		t.Fatalf("template included runtime data: %s", body)
	}
}

func TestEmbykeeperAdminUIIsReadOnlyAndExternal(t *testing.T) {
	for _, want := range []string{
		`switchTab('embykeeper',this)`,
		`id="tabEmbykeeper"`,
		`const EMBYKEEPER_ENDPOINT = '/api/admin/embykeeper';`,
		`id="embykeeperOpenLink"`,
		`target="_blank" rel="noopener noreferrer"`,
		`href="/api/admin/embykeeper/template"`,
	} {
		if !strings.Contains(indexHTML, want) {
			t.Fatalf("Admin UI missing %q", want)
		}
	}
	blockStart := strings.Index(indexHTML, `id="tabEmbykeeper"`)
	if blockStart < 0 {
		t.Fatal("Embykeeper UI section not found")
	}
	blockEnd := strings.Index(indexHTML[blockStart:], `id="tabLogs"`)
	if blockEnd < 0 {
		t.Fatal("Embykeeper UI section not found")
	}
	block := indexHTML[blockStart : blockStart+blockEnd]
	for _, forbidden := range []string{"<iframe", "startEmbykeeper", "stopEmbykeeper", "restartEmbykeeper", "saveEmbykeeper"} {
		if strings.Contains(block, forbidden) {
			t.Fatalf("Embykeeper UI contains control surface %q", forbidden)
		}
	}
}

func TestStatusExampleSchema(t *testing.T) {
	path := filepath.Join("..", "..", "deploy", "embykeeper", "status.example.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("standalone example not created yet: %v", err)
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	if _, reason := readEmbykeeperStatus(path); reason != "" {
		t.Fatalf("status example rejected: %s", reason)
	}
}

func writeTestKeeperStatus(t *testing.T, raw string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "status.json")
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
