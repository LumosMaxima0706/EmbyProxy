package admin

import (
	"embed"
	"fmt"
	"net/http"
	"strings"
)

//go:embed edgecert/*.py
var edgeCertificateFiles embed.FS

func (h *Handler) edgeCertificateSetup() string {
	domain := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(h.cfg.PublicIngressHost)), ".")
	if domain == "" || strings.ContainsAny(domain, "\r\n\"' /\\") {
		return "echo 'BUSINESS TLS: NOT CONFIGURED'\n"
	}
	var out strings.Builder
	out.WriteString("#!/bin/sh\nset -eu\numask 077\n")
	out.WriteString("test -f /var/lib/embyproxy-edge/caddy-managed && grep -Fx 'managed_by=embyproxy-edge' /var/lib/embyproxy-edge/caddy-managed >/dev/null || { echo 'unowned Caddy; certificate setup refused' >&2; exit 1; }\n")
	out.WriteString("if ! command -v certbot >/dev/null 2>&1 || ! command -v python3 >/dev/null 2>&1 || ! command -v openssl >/dev/null 2>&1; then apt-get update; DEBIAN_FRONTEND=noninteractive apt-get install -y certbot python3 openssl; fi\n")
	out.WriteString("install -d -m 0755 /usr/local/lib/embyproxy-edge\n")
	for _, name := range []string{"acme_hook.py", "certificate_manager.py"} {
		data, _ := edgeCertificateFiles.ReadFile("edgecert/" + name)
		fmt.Fprintf(&out, "tmp=$(mktemp /usr/local/lib/embyproxy-edge/tool.XXXXXX)\ncat > \"$tmp\" <<'EMBYPROXY_TOOL'\n%s\nEMBYPROXY_TOOL\nchmod 0700 \"$tmp\"\nmv -f \"$tmp\" /usr/local/lib/embyproxy-edge/%s\n", data, name)
	}
	fmt.Fprintf(&out, "printf '%%s\\n' %s > /etc/embyproxy-edge/business-domain\nchmod 0600 /etc/embyproxy-edge/business-domain\n", shellQuote(domain))
	out.WriteString(`cat > /etc/systemd/system/embyproxy-edge-certificate.service <<'UNIT'
[Unit]
Description=EmbyProxy business-domain certificate preparation and renewal
Wants=network-online.target
After=network-online.target
[Service]
Type=oneshot
ExecStart=/usr/bin/python3 /usr/local/lib/embyproxy-edge/certificate_manager.py
TimeoutStartSec=4500
UMask=0077
UNIT
cat > /etc/systemd/system/embyproxy-edge-certificate.timer <<'UNIT'
[Unit]
Description=Retry and renew EmbyProxy business-domain certificate
[Timer]
OnBootSec=5min
OnUnitActiveSec=12h
RandomizedDelaySec=30min
Persistent=true
[Install]
WantedBy=timers.target
UNIT
systemctl daemon-reload
if [ "${EMBYPROXY_CERTIFICATE_SETUP_ONLY:-0}" = 1 ]; then echo 'BUSINESS TLS: SETUP COMPLETE'; exit 0; fi
systemctl enable --now embyproxy-edge-certificate.timer
systemctl start --no-block embyproxy-edge-certificate.service
echo 'BUSINESS TLS: PREPARING (asynchronous DNS-01; certificate timer retries; public DNS and scheduling unchanged)'
`)
	return out.String()
}

func (h *Handler) handleEdgeCertificateTools(w http.ResponseWriter, r *http.Request, path string) bool {
	const prefix = "/api/edge/certificate-tools/"
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	id := strings.TrimPrefix(path, prefix)
	credential := r.Header.Get("X-EmbyProxy-Node-Credential")
	if r.Method != http.MethodGet || strings.Contains(id, "/") || !h.store.ValidateProxyNodeCredential(r.Context(), id, credential) {
		http.NotFound(w, r)
		return true
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(h.edgeCertificateSetup()))
	return true
}
