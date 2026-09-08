package edgecleanup

import (
	"fmt"
	"strings"
)

// Ownership is copied from the controller's node ownership metadata during
// enrollment. Cleanup never infers ownership from package presence alone.
type Ownership struct {
	CaddyInstalledByProject bool
	CaddyConfigOwned        bool
	TLSStateOwned           bool
	EdgeUnitOwned           bool
}

// Script returns a fixed, auditable self-uninstall script. No job supplied
// value is interpreted as a command or path.
func Script(nodeID, jobID, controllerURL, completionToken string, ownership Ownership) (string, error) {
	if strings.TrimSpace(nodeID) == "" || strings.TrimSpace(jobID) == "" || strings.TrimSpace(controllerURL) == "" || strings.TrimSpace(completionToken) == "" {
		return "", fmt.Errorf("invalid cleanup input")
	}
	return fmt.Sprintf(`#!/bin/sh
set -eu
node_id=%q
job_id=%q
controller=%q
completion_token=%q
config_path=/etc/embyproxy-edge/edge-agent.json
config_dir=/etc/embyproxy-edge
state_dir=/var/lib/embyproxy-edge

# Fixed project-owned paths only. Missing resources are successful.
systemctl stop embyproxy-edge.service 2>/dev/null || true
systemctl disable embyproxy-edge.service 2>/dev/null || true
%s
systemctl daemon-reload 2>/dev/null || true
rm -f "$config_path" "$config_dir/identity.env" "$config_dir/bootstrap.sh"
%s
find "$state_dir" -xdev -depth -type f -delete 2>/dev/null || true
find "$state_dir" -xdev -depth -type d -empty -delete 2>/dev/null || true
curl --fail --silent --show-error --proto '=https' --tlsv1.2 -X POST \
  -H 'Content-Type: application/json' \
  -H "X-EmbyProxy-Cleanup-Token: $completion_token" \
  --data "{\"job_id\":\"$job_id\",\"completion_token\":\"$completion_token\"}" \
  "$controller/api/edge/decommission/$node_id/complete" || true
rm -f -- "$0"
`, nodeID, jobID, controllerURL, completionToken,
		unitCleanup(ownership.EdgeUnitOwned), caddyCleanup(ownership)), nil
}

func unitCleanup(owned bool) string {
	if !owned {
		return "# Edge unit/binary ownership was not asserted; preserve edge service and binary"
	}
	return "rm -f /etc/systemd/system/embyproxy-edge.service /usr/local/bin/embyproxy-edge-agent"
}

func caddyCleanup(o Ownership) string {
	if !o.CaddyConfigOwned && !o.TLSStateOwned {
		return "# Caddy/TLS ownership was not asserted; preserve shared Caddy"
	}
	line := "if [ -f /var/lib/embyproxy-edge/caddy-managed ] && grep -Fx 'managed_by=embyproxy-edge' /var/lib/embyproxy-edge/caddy-managed >/dev/null 2>&1; then"
	if o.CaddyConfigOwned {
		line += " rm -f /etc/caddy/Caddyfile;"
	}
	if o.TLSStateOwned {
		line += " rm -rf /var/lib/embyproxy-edge/tls;"
	}
	line += " rm -f /var/lib/embyproxy-edge/caddy-managed; fi"
	if o.CaddyInstalledByProject {
		line += " # Caddy purge intentionally remains gated by an external dependency check."
	}
	return line
}
