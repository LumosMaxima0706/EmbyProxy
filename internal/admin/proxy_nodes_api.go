package admin

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"time"

	"embyproxy/internal/buildinfo"
	"embyproxy/internal/capture"
	"embyproxy/internal/config"
	"embyproxy/internal/edgecontrol"
	"embyproxy/internal/requestlog"
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
			Name                    string `json:"name"`
			DomainPrefix            string `json:"domain_prefix"`
			PublicIPv4              string `json:"public_ipv4"`
			PublicAddress           string `json:"public_address"`
			QuotaBytes              int64  `json:"quota_bytes"`
			ResetDay                int    `json:"reset_day"`
			ResetTimezone           string `json:"reset_timezone"`
			Priority                int    `json:"priority"`
			DNSRecordID             string `json:"dns_record_id"`
			DNSRecordType           string `json:"dns_record_type"`
			DNSProvider             string `json:"dns_provider"`
			DNSAccount              string `json:"dns_account"`
			DNSZone                 string `json:"dns_zone"`
			DNSOwned                bool   `json:"dns_owned"`
			CaddyInstalledByProject bool   `json:"caddy_installed_by_project"`
			CaddyConfigOwned        bool   `json:"caddy_config_owned"`
			TLSStateOwned           bool   `json:"tls_state_owned"`
			EdgeUnitOwned           bool   `json:"edge_unit_owned"`
		}
		if !decodeAuthJSON(w, r, &body) {
			return
		}
		// Automatic onboarding derives a stable HTTPS origin from a managed
		// domain and public IPv4; no network/TLS probe is performed here.
		if strings.TrimSpace(body.DomainPrefix) != "" || strings.TrimSpace(body.PublicIPv4) != "" {
			prefix := strings.ToLower(strings.TrimSpace(body.DomainPrefix))
			addr, parseErr := netip.ParseAddr(strings.TrimSpace(body.PublicIPv4))
			if prefix == "" || strings.ContainsAny(prefix, "._/\\?#") || parseErr != nil || !addr.Is4() {
				writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "INVALID_DNS_PREFIX_OR_IP"})
				return
			}
			if h.dnsAutomation == nil || h.dnsAutomation.ManagedDomain == "" {
				writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "DNS_AUTOMATION_NOT_CONFIGURED"})
				return
			}
			record, dnsErr := h.dnsAutomation.EnsureA(ctx, prefix, addr, h.cfg.SpaceshipDefaultTTL)
			if dnsErr != nil {
				code := "DNS_CREATE_FAILED"
				if strings.Contains(dnsErr.Error(), "conflict") {
					code = "DNS_RECORD_CONFLICT"
				}
				writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": code})
				return
			}
			body.PublicAddress = "https://" + prefix + "." + h.dnsAutomation.ManagedDomain
			body.DNSRecordID, body.DNSRecordType = record.ID, "A"
			accountSum := sha256.Sum256([]byte(h.dnsAutomation.APIKey))
			body.DNSProvider, body.DNSAccount, body.DNSZone, body.DNSOwned = "spaceship", fmt.Sprintf("sha256:%x", accountSum[:8]), h.dnsAutomation.ManagedDomain, true
		} else if strings.TrimSpace(body.PublicAddress) != "" {
			if normalized, normalizeErr := config.NormalizeEdgePublicOrigin(body.PublicAddress); normalizeErr != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "INVALID_EDGE_PUBLIC_ORIGIN"})
				return
			} else {
				body.PublicAddress = normalized
			}
		}
		if strings.TrimSpace(body.PublicAddress) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "EDGE_PUBLIC_HTTPS_ORIGIN_REQUIRED"})
			return
		}
		controllerURL, err := config.NormalizeEnrollmentControllerURL(h.cfg.EnrollmentControllerURL, false)
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "CONTROLLER_PUBLIC_URL_NOT_CONFIGURED"})
			return
		}
		enrollment, token, err := h.store.CreateProxyNode(ctx, storage.ProxyNode{Name: body.Name, PublicAddress: body.PublicAddress, QuotaBytes: body.QuotaBytes, ResetDay: body.ResetDay, ResetTimezone: body.ResetTimezone, Priority: body.Priority}, 15*time.Minute)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "INVALID_NODE"})
			return
		}
		if body.DNSRecordType == "" {
			body.DNSRecordType = "A"
		}
		if err := h.store.SetProxyNodeOwnershipBound(ctx, enrollment.NodeID, body.DNSProvider, body.DNSAccount, body.DNSZone, body.DNSRecordID, body.DNSRecordType, body.DNSOwned, body.CaddyInstalledByProject, body.CaddyConfigOwned, body.TLSStateOwned, body.EdgeUnitOwned); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "NODE_OWNERSHIP_FAILED"})
			return
		}
		if body.DomainPrefix != "" && h.dnsAutomation != nil {
			fqdn := strings.ToLower(strings.TrimSpace(body.DomainPrefix)) + "." + h.dnsAutomation.ManagedDomain
			_ = h.store.SetProxyNodeDNSMetadata(ctx, enrollment.NodeID, fqdn, strings.TrimSpace(body.PublicIPv4), "pending", "")
			_ = h.store.SetProxyNodePending(ctx, enrollment.NodeID)
		}
		writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "enrollment": enrollment, "install_command": buildEnrollmentCommand(controllerURL, enrollment.ID, token)})
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
			writeJSON(w, 200, map[string]any{"ok": true, "state": map[bool]string{true: "revoked", false: "draining"}[force], "force": force})
			return
		}
		if r.Method == http.MethodPost && len(parts) == 2 && parts[1] == "drain" {
			if err := h.store.BeginProxyNodeDrain(ctx, id); err != nil {
				writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": "DRAIN_FAILED"})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "state": "draining"})
			return
		}
		if r.Method == http.MethodPost && len(parts) == 2 && parts[1] == "decommission" {
			var body struct {
				Force       bool   `json:"force"`
				ConfirmName string `json:"confirm_name"`
			}
			if !decodeAuthJSON(w, r, &body) {
				return
			}
			node, err := h.store.GetProxyNode(ctx, id)
			if err != nil || node == nil {
				writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "NODE_NOT_FOUND"})
				return
			}
			if strings.TrimSpace(body.ConfirmName) != node.Name {
				writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "NODE_NAME_CONFIRMATION_REQUIRED"})
				return
			}
			if node.LastHeartbeatAt > 0 && time.Since(time.Unix(node.LastHeartbeatAt, 0)) <= 5*time.Minute && !node.DecommissionCapable {
				writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": "EDGE_DECOMMISSION_UNSUPPORTED", "capability": false})
				return
			}
			if node.State == "revoked" && !body.Force {
				// Revoked nodes can be safely archived without a remote identity.
			}
			active := h.proxyNodeIsActive(node)
			if active && !body.Force {
				items, _ := h.store.ListProxyNodes(ctx)
				fallback := false
				for _, candidate := range items {
					if candidate.ID != id && candidate.Enabled && candidate.State == "healthy" && candidate.PlaybackHealthy && candidate.IngressHealthy && candidate.ConfigSynced {
						fallback = true
						break
					}
				}
				if !fallback {
					writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": "ACTIVE_NODE_REQUIRES_HEALTHY_FALLBACK", "force_required": true})
					return
				}
				if err := h.switchProxyNodeForDecommission(ctx, id, items); err != nil {
					writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": "ACTIVE_SWITCH_FAILED"})
					return
				}
			}
			job, err := h.store.DecommissionProxyNode(ctx, id, body.Force)
			if err != nil {
				writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": "DECOMMISSION_FAILED"})
				return
			}
			// Offline nodes have no remote completion to wait for, so their
			// owned DNS is removed immediately. Online nodes remove DNS only
			// after the signed cleanup completion callback.
			if h.lifecycleDNS != nil && job.RemoteCleanupPending {
				if dnsErr := h.store.DeleteOwnedProxyNodeDNS(ctx, id); dnsErr != nil {
					_ = h.store.MarkProxyNodeDecommissionStep(ctx, job.ID, "removing_dns", dnsErr)
					writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "job": job, "dns_cleanup_pending": true})
					return
				}
				_ = h.store.MarkProxyNodeDecommissionStep(ctx, job.ID, "removing_dns", nil)
			}
			if !job.RemoteCleanupPending && job.SignedJob == nil && len(h.decommissionKey) == ed25519.PrivateKeySize {
				job, _ = h.store.IssueProxyNodeDecommissionJob(ctx, job.ID, h.decommissionKey, 15*time.Minute)
			}
			writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "job": job})
			return
		}
		if r.Method == http.MethodGet && len(parts) == 3 && parts[1] == "decommission" && parts[2] == "preview" {
			node, err := h.store.GetProxyNode(ctx, id)
			if err != nil || node == nil {
				writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "NODE_NOT_FOUND"})
				return
			}
			items, _ := h.store.ListProxyNodes(ctx)
			hasFallback := false
			for _, candidate := range items {
				if candidate.ID != id && candidate.Enabled && candidate.State == "healthy" && candidate.PlaybackHealthy && candidate.IngressHealthy && candidate.ConfigSynced {
					hasFallback = true
					break
				}
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "node": node, "preview": map[string]any{"scheduler_exclude": true, "revoke_identity": true, "remove_controller_state": true, "remove_dns": node.DNSOwned, "cleanup_remote": node.LastHeartbeatAt > 0, "remote_capability": node.DecommissionCapable, "cleanup_caddy": node.CaddyConfigOwned || node.CaddyInstalledByProject, "cleanup_tls": node.TLSStateOwned, "force_required": h.proxyNodeIsActive(node) && !hasFallback}})
			return
		}
		if r.Method == http.MethodPost && len(parts) == 3 && parts[1] == "decommission" && parts[2] == "retry" {
			job, err := h.store.LatestProxyNodeDecommissionJob(ctx, id)
			if err != nil || job == nil {
				writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "JOB_NOT_FOUND"})
				return
			}
			job, err = h.store.RetryProxyNodeDecommission(ctx, job.ID)
			if err != nil {
				writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": "RETRY_FAILED"})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "job": job})
			return
		}
		if r.Method == http.MethodPost && len(parts) == 2 && parts[1] == "bootstrap" {
			controllerURL, err := config.NormalizeEnrollmentControllerURL(h.cfg.EnrollmentControllerURL, false)
			if err != nil {
				writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "CONTROLLER_PUBLIC_URL_NOT_CONFIGURED"})
				return
			}
			enrollment, token, err := h.store.RegenerateProxyNodeEnrollment(ctx, id, 15*time.Minute)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "BOOTSTRAP_REGENERATION_FAILED"})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "enrollment": enrollment, "install_command": buildEnrollmentCommand(controllerURL, enrollment.ID, token)})
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
				if err := h.store.SetProxyNodeScheduling(ctx, id, v); err != nil {
					writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": "REVOKED_NODE_REQUIRES_REENROLLMENT"})
					return
				}
				n, _ = h.store.GetProxyNode(ctx, id)
			}
			if v, ok := body["public_address"].(string); ok {
				if strings.TrimSpace(v) == "" {
					writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "EDGE_PUBLIC_HTTPS_ORIGIN_REQUIRED"})
					return
				}
				normalized, normalizeErr := config.NormalizeEdgePublicOrigin(v)
				if normalizeErr != nil {
					writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "INVALID_EDGE_PUBLIC_ORIGIN"})
					return
				}
				n.PublicAddress = normalized
				n.IngressHealthy = false
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
			if v, ok := body["used_bytes"].(float64); ok && v >= 0 {
				n.UsedBytes = int64(v)
			}
			if v, ok := body["next_reset_at"].(float64); ok && v >= 0 {
				n.NextResetAt = int64(v)
			}
			if err := h.store.UpdateProxyNode(ctx, *n); err != nil {
				writeJSON(w, 400, map[string]any{"ok": false, "error": "INVALID_NODE"})
				return
			}
			writeJSON(w, 200, map[string]any{"ok": true, "node": n})
			return
		}
		if r.Method == http.MethodPost && len(parts) == 2 && parts[1] == "usage" {
			var body struct {
				UsedBytes int64 `json:"used_bytes"`
			}
			if !decodeAuthJSON(w, r, &body) {
				return
			}
			if err := h.store.RecordProxyNodeUsage(ctx, id, body.UsedBytes, time.Now()); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "USAGE_UPDATE_FAILED"})
				return
			}
			n, _ := h.store.GetProxyNode(ctx, id)
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "node": n})
			return
		}
	}
	http.NotFound(w, r)
}

func (h *Handler) proxyNodeIsActive(node *storage.ProxyNode) bool {
	if node == nil {
		return false
	}
	state := h.readExternalFailoverState()
	for _, key := range []string{"active_node_id", "active_target"} {
		if value, ok := state[key].(string); ok && (value == node.ID || strings.EqualFold(value, node.Name)) {
			return true
		}
	}
	return node.Priority == 1
}

func (h *Handler) switchProxyNodeForDecommission(ctx context.Context, id string, items []storage.ProxyNode) error {
	ordered := make([]string, 0, len(items))
	for _, n := range items {
		if n.ID != id && n.Enabled && n.State == "healthy" && n.PlaybackHealthy && n.IngressHealthy && n.ConfigSynced {
			ordered = append(ordered, n.ID)
		}
	}
	for _, n := range items {
		if n.ID == id {
			continue
		}
		found := false
		for _, existing := range ordered {
			if existing == n.ID {
				found = true
				break
			}
		}
		if !found {
			ordered = append(ordered, n.ID)
		}
	}
	ordered = append(ordered, id)
	return h.store.ReorderProxyNodes(ctx, ordered)
}

func (h *Handler) handleProxyNodeJobsAPI(w http.ResponseWriter, r *http.Request, path string) {
	parts := strings.Split(strings.TrimPrefix(path, "/api/admin/proxy-node-jobs/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	job, err := h.store.GetProxyNodeDecommissionJob(r.Context(), parts[0])
	if err != nil || job == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "JOB_NOT_FOUND"})
		return
	}
	if r.Method == http.MethodPost && len(parts) == 2 && parts[1] == "retry" {
		job, err = h.store.RetryProxyNodeDecommission(r.Context(), parts[0])
		if err != nil {
			writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": "RETRY_FAILED"})
			return
		}
		if job.SignedJob == nil && job.CurrentStep == "cleaning_remote" && len(h.decommissionKey) == ed25519.PrivateKeySize {
			job, err = h.store.IssueProxyNodeDecommissionJob(r.Context(), job.ID, h.decommissionKey, 15*time.Minute)
			if err != nil {
				writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": "RETRY_DISPATCH_FAILED"})
				return
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "job": job})
}

func buildEnrollmentCommand(controllerURL, id, token string) string {
	return "curl --fail --silent --show-error --proto '=https' --tlsv1.2 " + shellQuote(controllerURL+"/api/edge/bootstrap/"+url.PathEscape(id)+"/"+url.PathEscape(token)) + " | sudo sh"
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

// handleBootstrap returns a guarded, self-contained installer. It intentionally
// contains no administrator credential; the one-time enrollment token is
// exchanged by the installer over TLS and is never persisted by the server.
func (h *Handler) handleBootstrap(w http.ResponseWriter, r *http.Request, enrollmentID, token string) {
	if r.Method != http.MethodGet || enrollmentID == "" || token == "" {
		http.NotFound(w, r)
		return
	}
	if err := h.store.ValidateEnrollment(r.Context(), enrollmentID, token); err != nil {
		// Treat expired, consumed and unknown enrollment records alike.
		http.NotFound(w, r)
		return
	}
	node, err := h.store.GetProxyNodeForEnrollment(r.Context(), enrollmentID)
	if err != nil || node == nil {
		http.Error(w, "edge public HTTPS ingress is not configured", http.StatusServiceUnavailable)
		return
	}
	persistedEdgePublic, normalizeErr := config.NormalizeEdgePublicOrigin(node.PublicAddress)
	if normalizeErr != nil {
		http.Error(w, "edge HTTPS origin is required before bootstrap; configure DNS and ingress first (no DNS provider is configured)", http.StatusServiceUnavailable)
		return
	}
	// Do not disclose enrollment state in the generated script. The script
	// performs environment checks and posts the token only to the edge API.
	controller, err := config.NormalizeEnrollmentControllerURL(h.cfg.EnrollmentControllerURL, h.cfg.AllowInsecureLoopbackEnrollment)
	if err != nil {
		http.Error(w, "controller public URL is not configured", http.StatusServiceUnavailable)
		return
	}
	curlProtocol := "=https"
	if strings.HasPrefix(controller, "http://") && h.cfg.AllowInsecureLoopbackEnrollment {
		curlProtocol = "=http,https"
	}
	decommissionPublicKey := ""
	if len(h.decommissionKey) == ed25519.PrivateKeySize {
		decommissionPublicKey = edgecontrol.EncodePublicKey(h.decommissionKey.Public().(ed25519.PublicKey))
	}
	script := fmt.Sprintf(`#!/bin/sh
set -eu
command -v curl >/dev/null 2>&1 || { echo 'curl is required' >&2; exit 1; }
command -v sha256sum >/dev/null 2>&1 || { echo 'sha256sum is required' >&2; exit 1; }
install_root=${EMBYPROXY_INSTALL_ROOT:-}
if [ -n "$install_root" ]; then
  case "$install_root" in /*) ;; *) echo 'EMBYPROXY_INSTALL_ROOT must be absolute' >&2; exit 1;; esac
  case "$install_root" in *..*|*' '*|*'	'*) echo 'unsafe install root' >&2; exit 1;; esac
else
  command -v systemctl >/dev/null 2>&1 || { echo 'systemd is required' >&2; exit 1; }
fi
umask 077
CONTROLLER='%s'
PERSISTED_EDGE_PUBLIC='%s'
cfg_dir="${install_root}/etc/embyproxy-edge"
state_dir="${install_root}/var/lib/embyproxy-edge"
lib_dir="${install_root}/usr/local/lib"
bin_dir="${install_root}/usr/local/bin"
unit_dir="${install_root}/etc/systemd/system"
# Refuse an unowned ingress before enrollment or writing any edge artifact.
# A failed bootstrap must leave a clean host clean, so a pre-existing Caddy
# installation cannot result in a consumed enrollment plus a half-installed
# edge unit/configuration.
if [ -z "$install_root" ] && [ "${EMBYPROXY_EDGE_INGRESS_MODE:-auto}" = auto ]; then
  caddy_root=/etc/caddy
  caddy_marker="$state_dir/caddy-managed"
  caddy_before_installed=false
  command -v caddy >/dev/null 2>&1 && caddy_before_installed=true
  caddy_unit_before=false
  # Vendor unit files can remain after a package purge; only a locally
  # managed/custom unit is an ownership conflict when the package is absent.
  [ -e /etc/systemd/system/caddy.service ] && caddy_unit_before=true
  caddy_config_before=false
  [ -e "$caddy_root/Caddyfile" ] && caddy_config_before=true
  caddy_managed=false
  if [ -f "$caddy_marker" ] && grep -Fx 'managed_by=embyproxy-edge' "$caddy_marker" >/dev/null 2>&1; then
    caddy_managed=true
  fi
  if [ "$caddy_managed" != true ] && { [ "$caddy_before_installed" = true ] || [ "$caddy_unit_before" = true ] || [ "$caddy_config_before" = true ]; }; then
    echo 'unmanaged Caddy/configuration already exists; refusing to overwrite it' >&2
    echo 'choose EMBYPROXY_EDGE_INGRESS_MODE=external with an existing HTTPS ingress' >&2
    exit 1
  fi
  if [ "$caddy_managed" != true ] && command -v ss >/dev/null 2>&1 && ss -lnt '( sport = :80 or sport = :443 )' | tail -n +2 | grep -q .; then
    echo 'ports 80/443 are already occupied; refusing to replace an existing ingress' >&2
    echo 'choose EMBYPROXY_EDGE_INGRESS_MODE=external with an existing HTTPS ingress' >&2
    exit 1
  fi
fi
install -d -m 0700 "$cfg_dir" "$state_dir" "$lib_dir" "$bin_dir" "$unit_dir"
# The edge agent has no TLS server. Keep the bind and local probe on loopback;
# the default mode installs an isolated Caddy HTTPS ingress on clean hosts.
edge_listen=${EMBYPROXY_EDGE_LISTEN_ADDR:-127.0.0.1:18080}
edge_probe=${EMBYPROXY_EDGE_PROBE_ADDR:-127.0.0.1:18080}
edge_canary=${EMBYPROXY_EDGE_CANARY_PATH-}
edge_allow_private=${EMBYPROXY_EDGE_ALLOW_PRIVATE_TARGETS:-false}
edge_isolated_media=${EMBYPROXY_ISOLATED_TEST_MEDIA:-true}
case "$edge_isolated_media" in
  true|false) ;;
  *) echo 'EMBYPROXY_ISOLATED_TEST_MEDIA must be true or false' >&2; exit 1;;
esac
if [ "$edge_isolated_media" = true ] && [ -z "$edge_canary" ]; then
  edge_canary='/__isolated-media/canary'
fi
if [ "$edge_isolated_media" = false ] && [ -z "$edge_canary" ]; then
  echo 'invalid playback health configuration: no playback canary configured' >&2
  exit 1
fi
case "$edge_canary" in
  /*) ;;
  *) echo 'EMBYPROXY_EDGE_CANARY_PATH must be an absolute URI path' >&2; exit 1;;
esac
case "$edge_canary" in
  *"'"*|*'"'*|*'\\'*|*" "*|*'	'*|*'?'*|*'#'*) echo 'EMBYPROXY_EDGE_CANARY_PATH contains unsafe characters' >&2; exit 1;;
esac
ingress_mode=${EMBYPROXY_EDGE_INGRESS_MODE:-auto}
case "$ingress_mode" in auto|external) ;; *) echo 'EMBYPROXY_EDGE_INGRESS_MODE must be auto or external' >&2; exit 1;; esac
if [ "$ingress_mode" = external ]; then
  edge_public=${EMBYPROXY_EDGE_EXTERNAL_INGRESS:-${PERSISTED_EDGE_PUBLIC}}
  [ -n "$edge_public" ] || { echo 'external ingress URL is required' >&2; exit 1; }
else
  edge_domain=${EMBYPROXY_EDGE_DOMAIN:-}
  if [ -z "$edge_domain" ]; then
    edge_domain=$(printf '%%s' "$PERSISTED_EDGE_PUBLIC" | sed -n 's#^https://##p' | sed 's/:.*$//')
  fi
  case "$edge_domain" in
    ''|*[!A-Za-z0-9.-]*) echo 'EMBYPROXY_EDGE_DOMAIN is required and must be a DNS name' >&2; exit 1;;
  esac
  edge_public="https://$edge_domain"
  command -v getent >/dev/null 2>&1 || { echo 'getent is required for DNS preflight' >&2; exit 1; }
  resolved_ips=$(getent ahosts "$edge_domain" | awk '{print $1}' | sort -u)
  [ -n "$resolved_ips" ] || { echo "DNS for $edge_domain does not resolve to this host" >&2; exit 1; }
  local_ip=$(curl --fail --silent --show-error --connect-timeout 10 --max-time 15 https://api.ipify.org || true)
  [ -n "$local_ip" ] && printf '%%s\n' "$resolved_ips" | grep -Fx "$local_ip" >/dev/null || { echo "DNS for $edge_domain does not point to this VPS" >&2; exit 1; }
fi
case "$edge_public" in https://?*) ;; *) echo 'edge ingress must be an HTTPS origin' >&2; exit 1;; esac
case "$edge_public" in *' '*|*'?'*|*'#'*|*'@'*) echo 'edge ingress URL contains unsafe characters' >&2; exit 1;; esac
enroll_payload=$(printf '{"version":"%%s","commit":"%%s","public_address":"%%s"}' '%s' '%s' "$edge_public")
response=$(curl --fail --silent --show-error --proto '%s' --tlsv1.2 -H 'Content-Type: application/json' --data "$enroll_payload" "$CONTROLLER/api/edge/enroll/%s/%s")
credential=$(printf '%%s' "$response" | sed -n 's/.*"credential":"\([^"]*\)".*/\1/p')
node_id=$(printf '%%s' "$response" | sed -n 's/.*"node_id":"\([^"]*\)".*/\1/p')
[ -n "$credential" ] && [ -n "$node_id" ] || { echo 'invalid enrollment response' >&2; exit 1; }
printf 'NODE_ID=%%s\nCREDENTIAL=%%s\nCONTROLLER=%%s\n' "$node_id" "$credential" '%s' > "$cfg_dir/identity.env"
chmod 0600 "$cfg_dir/identity.env"
artifact_tmp=$(mktemp "$bin_dir/embyproxy-edge-agent.XXXXXX")
artifact_headers=$(mktemp "$state_dir/edge-agent.headers.XXXXXX")
if ! curl --fail --silent --show-error --proto '%s' --tlsv1.2 -D "$artifact_headers" -H "X-EmbyProxy-Node-Credential: $credential" "$CONTROLLER/api/edge/artifact/$node_id/edge-agent" -o "$artifact_tmp"; then
  echo 'failed to download edge agent artifact' >&2
  rm -f "$artifact_tmp" "$artifact_headers"
  exit 1
fi
artifact_sha=$(sed -n 's/^[Xx]-[Ee]mby[Pp]roxy-[Aa]rtifact-[Ss][Hh][Aa]256: *\([0-9a-fA-F]\{64\}\).*$/\1/p' "$artifact_headers" | tail -n 1)
actual_sha=$(sha256sum "$artifact_tmp" | awk '{print $1}')
[ -n "$artifact_sha" ] && [ "$artifact_sha" = "$actual_sha" ] || { echo 'edge agent checksum verification failed' >&2; rm -f "$artifact_tmp" "$artifact_headers"; exit 1; }
rm -f "$artifact_headers"
chmod 0700 "$artifact_tmp"
# Atomic replacement keeps reruns safe while the currently running executable
# still has its old inode open (direct writes would fail with curl 23/ETXTBSY).
mv -f "$artifact_tmp" "$bin_dir/embyproxy-edge-agent"
printf '{"listen_addr":"%%s","probe_addr":"%%s","db_path":"%%s/edge.db","controller":"%%s","node_id":"%%s","credential":"%%s","version":"bootstrap","commit":"%s","decommission_public_key":"%s","caddy_installed_by_project":%s,"caddy_config_owned":%s,"tls_state_owned":%s,"edge_unit_owned":%s,"canary_path":"%%s","allow_private_targets":%%s,"isolated_test_media":%%s}\n' "$edge_listen" "$edge_probe" "$state_dir" "$CONTROLLER" "$node_id" "$credential" "$edge_canary" "$edge_allow_private" "$edge_isolated_media" > "$cfg_dir/edge-agent.json"
chmod 0600 "$cfg_dir/edge-agent.json"
cat > "$unit_dir/embyproxy-edge.service" <<UNIT
[Unit]
Description=EmbyProxy enrolled edge agent
Wants=network-online.target
After=network-online.target
[Service]
Type=simple
ExecStart=$bin_dir/embyproxy-edge-agent --config $cfg_dir/edge-agent.json
Restart=on-failure
RestartSec=3s
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=$cfg_dir $state_dir
[Install]
WantedBy=multi-user.target
UNIT
if [ -z "$install_root" ]; then
  if [ "$ingress_mode" = auto ]; then
    caddy_root=/etc/caddy
    caddy_marker="$state_dir/caddy-managed"
    caddy_before_installed=false
    command -v caddy >/dev/null 2>&1 && caddy_before_installed=true
    caddy_postinst_active=false
    caddy_unit_before=false
    [ -e /etc/systemd/system/caddy.service ] && caddy_unit_before=true
    caddy_config_before=false
    [ -e "$caddy_root/Caddyfile" ] && caddy_config_before=true
    caddy_managed=false
    if [ -f "$caddy_marker" ] && grep -Fx 'managed_by=embyproxy-edge' "$caddy_marker" >/dev/null 2>&1; then
      caddy_managed=true
    fi
    # Existing Caddy is reusable only when this installer previously claimed it.
    # This prevents overwriting an unrelated package, service, or Caddyfile.
    if [ "$caddy_managed" != true ] && { [ "$caddy_before_installed" = true ] || [ "$caddy_unit_before" = true ] || [ "$caddy_config_before" = true ]; }; then
      echo 'unmanaged Caddy/configuration already exists; refusing to overwrite it' >&2
      echo 'choose EMBYPROXY_EDGE_INGRESS_MODE=external with an existing HTTPS ingress' >&2
      exit 1
    fi
    if [ "$caddy_managed" != true ] && command -v ss >/dev/null 2>&1 && ss -lnt '( sport = :80 or sport = :443 )' | tail -n +2 | grep -q .; then
      echo 'ports 80/443 are already occupied; refusing to replace an existing ingress' >&2
      echo 'choose EMBYPROXY_EDGE_INGRESS_MODE=external with an existing HTTPS ingress' >&2
      exit 1
    fi
    if [ "$caddy_before_installed" != true ]; then
      command -v apt-get >/dev/null 2>&1 || { echo 'caddy is not installed and apt-get is unavailable' >&2; exit 1; }
      apt-get update
      DEBIAN_FRONTEND=noninteractive apt-get install -y ca-certificates curl gnupg debian-keyring debian-archive-keyring apt-transport-https
      caddy_keyring=/usr/share/keyrings/caddy-stable-archive-keyring.gpg
      caddy_repo=/etc/apt/sources.list.d/caddy-stable.list
      caddy_repo_line="deb [signed-by=$caddy_keyring] https://dl.cloudsmith.io/public/caddy/stable/deb/debian any-version main"
      if [ -e "$caddy_repo" ] && ! grep -Fx "$caddy_repo_line" "$caddy_repo" >/dev/null 2>&1; then
        echo 'an unexpected Caddy APT source already exists; refusing to overwrite it' >&2
        echo 'choose EMBYPROXY_EDGE_INGRESS_MODE=external or remove the source after review' >&2
        exit 1
      fi
      if [ ! -s "$caddy_keyring" ]; then
        caddy_key_tmp=$(mktemp)
        curl --fail --silent --show-error --proto '=https' --tlsv1.2 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' -o "$caddy_key_tmp"
        install -d -m 0755 /usr/share/keyrings
        gpg --dearmor --yes -o "$caddy_keyring" "$caddy_key_tmp"
        rm -f "$caddy_key_tmp"
        chmod 0644 "$caddy_keyring"
      fi
      printf '%%s\n' "$caddy_repo_line" > "$caddy_repo"
      chmod 0644 "$caddy_repo"
      apt-get update
      if [ -n "${EMBYPROXY_CADDY_VERSION:-}" ]; then
        DEBIAN_FRONTEND=noninteractive apt-get install -y "caddy=${EMBYPROXY_CADDY_VERSION}"
      else
        DEBIAN_FRONTEND=noninteractive apt-get install -y caddy
      fi
      if systemctl is-active --quiet caddy.service; then
        caddy_postinst_active=true
      fi
    fi
    caddy_bin=$(command -v caddy)
    id caddy >/dev/null 2>&1 || { echo 'trusted Caddy package did not create the caddy service user' >&2; exit 1; }
    systemctl cat caddy.service >/dev/null 2>&1 || { echo 'trusted Caddy package did not provide caddy.service' >&2; exit 1; }
    install -d -m 0750 "$caddy_root"
    chown root:caddy "$caddy_root"
    chmod 0750 "$caddy_root"
    caddy_tmp=$(mktemp "$caddy_root/Caddyfile.embyproxy.XXXXXX")
    caddy_backup="$caddy_marker.previous"
    caddy_had_working=false
    caddy_restore() {
      if [ "$caddy_had_working" = true ]; then
        cp -p "$caddy_backup" "$caddy_root/Caddyfile" || true
        systemctl restart caddy.service || true
      else
        rm -f "$caddy_root/Caddyfile"
        systemctl stop caddy.service || true
      fi
    }
    cat > "$caddy_tmp" <<CADDY
$edge_domain {
  encode gzip
  @isolated path /__isolated-media/*
  respond @isolated 404
  reverse_proxy 127.0.0.1:18080
}
CADDY
    chown root:caddy "$caddy_tmp"
    chmod 0640 "$caddy_tmp"
    "$caddy_bin" validate --config "$caddy_tmp" --adapter caddyfile || {
      echo 'Caddy configuration validation failed' >&2
      rm -f "$caddy_tmp"
      systemctl status caddy.service --no-pager || true
      journalctl -u caddy.service -n 80 --no-pager || true
      exit 1
    }
    if [ "$caddy_postinst_active" = true ] || systemctl is-active --quiet caddy.service; then
      systemctl stop caddy.service || {
        echo 'failed to stop caddy.service before configuration replacement' >&2
        rm -f "$caddy_tmp"
        systemctl status caddy.service --no-pager || true
        journalctl -u caddy.service -n 80 --no-pager || true
        exit 1
      }
    fi
    if [ "$caddy_managed" = true ] && [ -f "$caddy_root/Caddyfile" ]; then
      cp -p "$caddy_root/Caddyfile" "$caddy_backup" || {
        echo 'failed to back up the managed Caddyfile' >&2
        rm -f "$caddy_tmp"
        systemctl restart caddy.service || true
        systemctl status caddy.service --no-pager || true
        journalctl -u caddy.service -n 80 --no-pager || true
        exit 1
      }
      caddy_had_working=true
    fi
    mv -f "$caddy_tmp" "$caddy_root/Caddyfile" || {
      echo 'failed to atomically install the Caddyfile' >&2
      caddy_restore
      systemctl status caddy.service --no-pager || true
      journalctl -u caddy.service -n 80 --no-pager || true
      exit 1
    }
    systemctl enable caddy.service || {
      echo 'failed to enable caddy.service' >&2
      caddy_restore
      systemctl status caddy.service --no-pager || true
      journalctl -u caddy.service -n 80 --no-pager || true
      exit 1
    }
    systemctl restart caddy.service || {
      echo 'failed to restart caddy.service' >&2
      caddy_restore
      systemctl status caddy.service --no-pager || true
      journalctl -u caddy.service -n 80 --no-pager || true
      exit 1
    }
    systemctl is-active --quiet caddy.service || {
      echo 'caddy.service is not active after restart' >&2
      caddy_restore
      systemctl status caddy.service --no-pager || true
      journalctl -u caddy.service -n 80 --no-pager || true
      exit 1
    }
    caddy_marker_tmp=$(mktemp "$state_dir/caddy-managed.XXXXXX") || {
      echo 'failed to create managed Caddy ownership marker' >&2
      caddy_restore
      systemctl status caddy.service --no-pager || true
      journalctl -u caddy.service -n 80 --no-pager || true
      exit 1
    }
    if ! printf 'managed_by=embyproxy-edge\ndomain=%%s\n' "$edge_domain" > "$caddy_marker_tmp" || ! chmod 0600 "$caddy_marker_tmp" || ! mv -f "$caddy_marker_tmp" "$caddy_marker"; then
      echo 'failed to record managed Caddy ownership' >&2
      rm -f "$caddy_marker_tmp"
      caddy_restore
      systemctl status caddy.service --no-pager || true
      journalctl -u caddy.service -n 80 --no-pager || true
      exit 1
    fi
  fi
  systemctl daemon-reload
  # Older installers emitted a synthetic heartbeat timer that reported false
  # health and could overwrite the agent's real heartbeat. Remove only those
  # project-owned legacy units during upgrades.
  if [ -z "$install_root" ]; then
    systemctl disable --now embyproxy-edge-heartbeat.timer >/dev/null 2>&1 || true
    rm -f "$unit_dir/embyproxy-edge-heartbeat.timer" "$unit_dir/embyproxy-edge-heartbeat.service" "$lib_dir/embyproxy-edge-heartbeat"
    systemctl daemon-reload
  fi
  # Always restart after replacing the enrolled config. enable --now is a
  # no-op for an already active unit and would leave a rerun using stale
  # credentials and settings.
  systemctl enable embyproxy-edge.service
  systemctl restart embyproxy-edge.service || {
    echo 'failed to restart embyproxy-edge.service' >&2
    systemctl status embyproxy-edge.service --no-pager || true
    journalctl -u embyproxy-edge.service -n 80 --no-pager || true
    exit 1
  }
  wait_http_200() {
    wait_label="$1"
    wait_url="$2"
    wait_attempts="$3"
    wait_connect_timeout="$4"
    wait_max_time="$5"
    wait_delay="$6"
    wait_protocol="$7"
    wait_status=000
    wait_attempt=1
    while [ "$wait_attempt" -le "$wait_attempts" ]; do
      if [ "$wait_protocol" = https ]; then
        wait_status=$(curl --silent --show-error --proto '=https' --tlsv1.2 --connect-timeout "$wait_connect_timeout" --max-time "$wait_max_time" -o /dev/null -w '%%{http_code}' "$wait_url" || true)
      else
        wait_status=$(curl --silent --show-error --connect-timeout "$wait_connect_timeout" --max-time "$wait_max_time" -o /dev/null -w '%%{http_code}' "$wait_url" || true)
      fi
      [ "$wait_status" = 200 ] && return 0
      if [ "$wait_attempt" -lt "$wait_attempts" ]; then sleep "$wait_delay"; fi
      wait_attempt=$((wait_attempt + 1))
    done
    echo "$wait_label health check failed after $wait_attempts attempts (last HTTP status: $wait_status)" >&2
    return 1
  }
  wait_http_200 'edge agent local' "http://$edge_probe/health" 5 2 3 4 http || exit 1
  if [ "$ingress_mode" = external ]; then
    wait_http_200 'external HTTPS ingress' "$edge_public/health" 18 5 5 5 https || exit 1
  else
    wait_http_200 'HTTPS edge ingress' "$edge_public/health" 18 5 5 5 https || exit 1
  fi
fi
echo 'Edge identity enrolled. This host remains unadmitted until its data-plane configuration reports a passing playback canary.'
	`, controller, persistedEdgePublic, buildinfo.Current().Version, buildinfo.Current().Commit, decommissionPublicKey, map[bool]string{true: "true", false: "false"}[node.CaddyInstalledByProject], map[bool]string{true: "true", false: "false"}[node.CaddyConfigOwned], map[bool]string{true: "true", false: "false"}[node.TLSStateOwned], map[bool]string{true: "true", false: "false"}[node.EdgeUnitOwned], curlProtocol, url.PathEscape(enrollmentID), url.PathEscape(token), controller, curlProtocol, buildinfo.Current().Commit)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(script))
}

func (h *Handler) handleEdgeEnrollment(w http.ResponseWriter, r *http.Request, path string) {
	// Enrollment and heartbeat payloads contain short-lived or long-lived node
	// credentials; keep them out of traffic capture and access logs.
	capture.Suppress(r)
	requestlog.SuppressAccessLog(r.Context())
	if r.Method == http.MethodGet && strings.HasPrefix(path, "/api/edge/bootstrap/") {
		parts := strings.Split(strings.TrimPrefix(path, "/api/edge/bootstrap/"), "/")
		if len(parts) != 2 {
			http.NotFound(w, r)
			return
		}
		h.handleBootstrap(w, r, parts[0], parts[1])
		return
	}
	if r.Method == http.MethodPost && strings.HasPrefix(path, "/api/edge/enroll/") {
		parts := strings.Split(strings.TrimPrefix(path, "/api/edge/enroll/"), "/")
		if len(parts) != 2 {
			http.NotFound(w, r)
			return
		}
		var body struct {
			Version       string `json:"version"`
			Commit        string `json:"commit"`
			PublicAddress string `json:"public_address"`
		}
		if !decodeAuthJSON(w, r, &body) {
			return
		}
		publicAddress := strings.TrimSpace(body.PublicAddress)
		if publicAddress != "" {
			normalized, normalizeErr := config.NormalizeEdgePublicOrigin(publicAddress)
			if normalizeErr != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "INVALID_EDGE_PUBLIC_ORIGIN"})
				return
			}
			publicAddress = normalized
		}
		node, credential, err := h.store.CompleteEnrollmentWithPublicAddress(r.Context(), parts[0], parts[1], body.Version, body.Commit, publicAddress)
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
			DecommissionCapable                           bool
		}
		if !decodeAuthJSON(w, r, &body) {
			return
		}
		if err := h.store.HeartbeatProxyNodeWithCapability(r.Context(), id, body.Credential, body.Version, body.Commit, body.State, body.PlaybackHealthy, body.ConfigSynced, body.LastError, body.DecommissionCapable); err != nil {
			writeJSON(w, 403, map[string]any{"ok": false, "error": "HEARTBEAT_DENIED"})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true})
		return
	}
	if strings.HasPrefix(path, "/api/edge/decommission/") {
		parts := strings.Split(strings.TrimPrefix(path, "/api/edge/decommission/"), "/")
		if len(parts) < 1 || parts[0] == "" {
			http.NotFound(w, r)
			return
		}
		nodeID := parts[0]
		if r.Method == http.MethodGet && len(parts) == 1 {
			credential := strings.TrimSpace(r.Header.Get("X-EmbyProxy-Node-Credential"))
			if !h.store.ValidateProxyNodeDecommissionCredential(r.Context(), nodeID, credential) {
				http.NotFound(w, r)
				return
			}
			job, err := h.store.LatestProxyNodeDecommissionJob(r.Context(), nodeID)
			if err != nil || job == nil || job.State == "complete" || job.SignedJob == nil && job.CurrentStep != "cleaning_remote" {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			if job.SignedJob == nil {
				job, err = h.store.IssueProxyNodeDecommissionJob(r.Context(), job.ID, h.decommissionKey, 15*time.Minute)
				if err != nil {
					http.Error(w, "decommission unavailable", http.StatusServiceUnavailable)
					return
				}
			}
			w.Header().Set("Cache-Control", "no-store")
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "job": job.SignedJob})
			return
		}
		if len(parts) == 2 && r.Method == http.MethodPost && (parts[1] == "accept" || parts[1] == "complete") {
			var body struct {
				JobID           string `json:"job_id"`
				CompletionToken string `json:"completion_token"`
			}
			_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body)
			token := strings.TrimSpace(r.Header.Get("X-EmbyProxy-Cleanup-Token"))
			if token == "" {
				token = strings.TrimSpace(body.CompletionToken)
			}
			if body.JobID == "" {
				body.JobID = parts[0]
			}
			jobRecord, jobErr := h.store.GetProxyNodeDecommissionJob(r.Context(), body.JobID)
			if jobErr != nil || jobRecord == nil || jobRecord.NodeID != nodeID {
				writeJSON(w, http.StatusForbidden, map[string]any{"ok": false, "error": "DECOMMISSION_JOB_NODE_MISMATCH"})
				return
			}
			if parts[1] == "accept" {
				if err := h.store.AcceptProxyNodeDecommissionJob(r.Context(), body.JobID, token); err != nil {
					writeJSON(w, http.StatusForbidden, map[string]any{"ok": false, "error": "DECOMMISSION_ACCEPT_DENIED"})
					return
				}
				writeJSON(w, http.StatusOK, map[string]any{"ok": true})
				return
			}
			job, err := h.store.CompleteProxyNodeDecommission(r.Context(), body.JobID, token)
			if err != nil {
				writeJSON(w, http.StatusForbidden, map[string]any{"ok": false, "error": "DECOMMISSION_COMPLETE_DENIED"})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "job": job})
			return
		}
		http.NotFound(w, r)
		return
	}
	if r.Method == http.MethodGet && strings.HasPrefix(path, "/api/edge/artifact/") {
		parts := strings.Split(strings.TrimPrefix(path, "/api/edge/artifact/"), "/")
		if len(parts) != 2 || h.cfg.EdgeAgentBinaryPath == "" {
			http.NotFound(w, r)
			return
		}
		credential := strings.TrimSpace(r.Header.Get("X-EmbyProxy-Node-Credential"))
		node, err := h.store.GetProxyNode(r.Context(), parts[0])
		if err != nil || node == nil || credential == "" || !h.store.ValidateProxyNodeCredential(r.Context(), parts[0], credential) {
			http.NotFound(w, r)
			return
		}
		file, err := os.Open(h.cfg.EdgeAgentBinaryPath)
		if err != nil {
			http.Error(w, "artifact unavailable", http.StatusServiceUnavailable)
			return
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 128<<20 {
			http.Error(w, "artifact unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", info.Size()))
		sum := sha256.New()
		if _, err := io.Copy(sum, file); err != nil {
			http.Error(w, "artifact unavailable", http.StatusServiceUnavailable)
			return
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			http.Error(w, "artifact unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("X-EmbyProxy-Artifact-SHA256", fmt.Sprintf("%x", sum.Sum(nil)))
		_, _ = io.Copy(w, io.LimitReader(file, 128<<20))
		return
	}
	if r.Method == http.MethodGet && strings.HasPrefix(path, "/api/edge/config/") {
		id := strings.TrimPrefix(path, "/api/edge/config/")
		credential := strings.TrimSpace(r.Header.Get("X-EmbyProxy-Node-Credential"))
		if id == "" || !h.store.ValidateProxyNodeCredential(r.Context(), id, credential) {
			http.NotFound(w, r)
			return
		}
		routes, err := h.store.ListManagedRoutes(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false})
			return
		}
		nodes, err := h.store.ListProxyNodes(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false})
			return
		}
		response := struct {
			NodeID string              `json:"node_id"`
			Nodes  []storage.ProxyNode `json:"nodes"`
			Routes []struct {
				Route storage.ManagedRoute       `json:"route"`
				Lines []storage.ManagedRouteLine `json:"lines"`
			} `json:"routes"`
			RedirectEndpoints map[string][]storage.ProxyRedirectEndpoint `json:"redirect_endpoints,omitempty"`
		}{NodeID: id, Nodes: nodes, Routes: make([]struct {
			Route storage.ManagedRoute       `json:"route"`
			Lines []storage.ManagedRouteLine `json:"lines"`
		}, 0, len(routes)), RedirectEndpoints: map[string][]storage.ProxyRedirectEndpoint{}}
		for _, route := range routes {
			if !route.Enabled || !route.Public {
				continue
			}
			lines, listErr := h.store.ListManagedRouteLines(r.Context(), route.Slug)
			if listErr != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false})
				return
			}
			response.Routes = append(response.Routes, struct {
				Route storage.ManagedRoute       `json:"route"`
				Lines []storage.ManagedRouteLine `json:"lines"`
			}{Route: route, Lines: lines})
			redirects, redirectErr := h.store.ListProxyRedirectEndpoints(r.Context(), route.Slug)
			if redirectErr != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false})
				return
			}
			response.RedirectEndpoints[route.Slug] = redirects
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, response)
		return
	}
	http.NotFound(w, r)
}
