package admin

import (
	"net/http"
	"regexp"
	"strings"

	"embyproxy/internal/spaceship"
	"embyproxy/internal/storage"
)

type dnsAutomationStored struct {
	Provider        string `json:"provider"`
	Domain          string `json:"managed_domain"`
	TTL             int    `json:"ttl"`
	APIKeyCipher    string `json:"api_key_cipher"`
	APISecretCipher string `json:"api_secret_cipher"`
}

var managedDomainRE = regexp.MustCompile(`^(?i)([a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)

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
		Provider      string `json:"provider"`
		ManagedDomain string `json:"managed_domain"`
		APIKey        string `json:"api_key"`
		APISecret     string `json:"api_secret"`
		TTL           int    `json:"ttl"`
	}
	if !decodeAuthJSON(w, r, &body) {
		return
	}
	body.Provider = strings.ToLower(strings.TrimSpace(body.Provider))
	if body.Provider == "" {
		body.Provider = "spaceship"
	}
	body.ManagedDomain = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(body.ManagedDomain)), ".")
	if body.Provider != "spaceship" {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "UNSUPPORTED_DNS_PROVIDER"})
		return
	}
	if !managedDomainRE.MatchString(body.ManagedDomain) {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "INVALID_MANAGED_DOMAIN"})
		return
	}
	if body.TTL == 0 {
		body.TTL = 300
	}
	if body.TTL < 30 || body.TTL > 86400 {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "INVALID_DNS_TTL"})
		return
	}
	for _, v := range []string{body.APIKey, body.APISecret} {
		if len(v) > 512 || strings.ContainsAny(v, "\x00\r\n") {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "INVALID_DNS_CREDENTIAL"})
			return
		}
	}
	var existing dnsAutomationStored
	existingOK, _ := h.store.KV().GetJSON(ctx, "dns:automation", &existing)
	key, secret := body.APIKey, body.APISecret
	if existingOK {
		if key == "" {
			key, _ = storage.DecryptSecret(existing.APIKeyCipher)
		}
		if secret == "" {
			secret, _ = storage.DecryptSecret(existing.APISecretCipher)
		}
	}
	if r.URL.Query().Get("test") == "true" {
		status, detail, err := (&spaceship.Client{BaseURL: h.cfg.SpaceshipAPIBaseURL, APIKey: key, APISecret: secret, ManagedDomain: body.ManagedDomain}).TestStatus(ctx)
		if err != nil {
			writeJSON(w, status, map[string]any{"ok": false, "error": spaceshipErrorCode(status), "provider": "spaceship", "operation": "dns_records_get", "http_status": status, "detail": detail})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "configured": true, "permissions": map[string]any{"dnsrecords_read": true}})
		return
	}
	if key == "" || secret == "" {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "DNS_CREDENTIAL_REQUIRED"})
		return
	}
	keyCipher, err := storage.EncryptSecret(key)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": "DNS_SECRET_ENCRYPTION_FAILED"})
		return
	}
	secretCipher, err := storage.EncryptSecret(secret)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": "DNS_SECRET_ENCRYPTION_FAILED"})
		return
	}
	if err := h.store.KV().Put(ctx, "dns:automation", dnsAutomationStored{Provider: body.Provider, Domain: body.ManagedDomain, TTL: body.TTL, APIKeyCipher: keyCipher, APISecretCipher: secretCipher}); err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": "DNS_CONFIGURATION_SAVE_FAILED"})
		return
	}
	h.dnsAutomation = &spaceship.Client{BaseURL: h.cfg.SpaceshipAPIBaseURL, APIKey: key, APISecret: secret, ManagedDomain: body.ManagedDomain}
	writeJSON(w, 200, map[string]any{"ok": true, "configured": true, "provider": "spaceship", "managed_domain": body.ManagedDomain, "ttl": body.TTL})
}
func spaceshipErrorCode(status int) string {
	switch status {
	case 400:
		return "SPACESHIP_BAD_REQUEST"
	case 401:
		return "SPACESHIP_AUTH_FAILED"
	case 403:
		return "SPACESHIP_PERMISSION_DENIED"
	case 404:
		return "SPACESHIP_DOMAIN_NOT_FOUND"
	case 422:
		return "SPACESHIP_UNPROCESSABLE"
	case 429:
		return "SPACESHIP_RATE_LIMITED"
	case 500, 503, 504:
		return "SPACESHIP_UPSTREAM_ERROR"
	case 502:
		return "SPACESHIP_NETWORK_ERROR"
	case 0:
		return "SPACESHIP_NETWORK_ERROR"
	default:
		return "SPACESHIP_NETWORK_ERROR"
	}
}
