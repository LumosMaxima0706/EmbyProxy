package admin

import (
	"net/http"
	"strings"

	"embyproxy/internal/spaceship"
	"embyproxy/internal/storage"
)

type dnsAutomationStored struct {
	Provider, Domain              string
	TTL                           int
	APIKeyCipher, APISecretCipher string
}

func (h *Handler) handleDNSAutomationAPI(w http.ResponseWriter, r *http.Request, path string) {
	if path != "/api/admin/dns/automation" {
		http.NotFound(w, r)
		return
	}
	ctx := r.Context()
	if r.Method == http.MethodGet {
		var cfg dnsAutomationStored
		ok, err := h.store.KV().GetJSON(ctx, "dns:automation", &cfg)
		if err != nil {
			writeJSON(w, 500, map[string]any{"ok": false, "error": "DNS_CONFIG_READ_FAILED"})
			return
		}
		configured := ok && cfg.Domain != "" && cfg.APIKeyCipher != "" && cfg.APISecretCipher != ""
		writeJSON(w, 200, map[string]any{"ok": true, "provider": cfg.Provider, "managed_domain": cfg.Domain, "ttl": cfg.TTL, "configured": configured, "permissions": map[string]any{"dnsrecords_read": configured, "dnsrecords_write": configured}})
		return
	}
	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	var body struct {
		Provider, ManagedDomain, APIKey, APISecret string
		TTL                                        int `json:"ttl"`
	}
	if !decodeAuthJSON(w, r, &body) {
		return
	}
	body.Provider = strings.ToLower(strings.TrimSpace(body.Provider))
	body.ManagedDomain = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(body.ManagedDomain)), ".")
	if body.Provider != "spaceship" || body.ManagedDomain == "" || body.APIKey == "" || body.APISecret == "" {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "INVALID_DNS_CONFIGURATION"})
		return
	}
	if body.TTL == 0 {
		body.TTL = 300
	}
	if body.TTL < 30 || body.TTL > 86400 {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "INVALID_DNS_TTL"})
		return
	}
	client := &spaceship.Client{BaseURL: h.cfg.SpaceshipAPIBaseURL, APIKey: body.APIKey, APISecret: body.APISecret, ManagedDomain: body.ManagedDomain}
	if path == "/api/admin/dns/automation" && r.URL.Query().Get("test") == "true" {
		if err := client.Test(ctx); err != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "DNS_CREDENTIALS_INVALID"})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "configured": true})
		return
	}
	keyCipher, err := storage.EncryptSecret(body.APIKey)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": "DNS_SECRET_STORAGE_UNAVAILABLE"})
		return
	}
	secCipher, err := storage.EncryptSecret(body.APISecret)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": "DNS_SECRET_STORAGE_UNAVAILABLE"})
		return
	}
	if err := h.store.KV().Put(ctx, "dns:automation", dnsAutomationStored{Provider: body.Provider, Domain: body.ManagedDomain, TTL: body.TTL, APIKeyCipher: keyCipher, APISecretCipher: secCipher}); err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": "DNS_CONFIG_SAVE_FAILED"})
		return
	}
	h.dnsAutomation = client
	writeJSON(w, 200, map[string]any{"ok": true, "configured": true, "provider": "spaceship", "managed_domain": body.ManagedDomain, "ttl": body.TTL})
}
