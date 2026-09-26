package edgecleanup

import (
	"strings"
	"testing"
)

func TestScriptOwnershipGuards(t *testing.T) {
	script, err := Script("node", "job", "https://controller.example", "token", Ownership{EdgeUnitOwned: true, CaddyConfigOwned: true, TLSStateOwned: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"systemctl stop embyproxy-edge.service", "rm -f /etc/systemd/system/embyproxy-edge.service", "systemctl stop caddy.service", "systemctl disable caddy.service", "managed_by=embyproxy-edge", "rmdir \"$config_dir\"", "X-EmbyProxy-Cleanup-Token"} {
		if !strings.Contains(script, marker) {
			t.Fatalf("missing %q", marker)
		}
	}
	if strings.Index(script, "systemctl stop caddy.service") > strings.Index(script, "rm -f /etc/caddy/Caddyfile") {
		t.Fatal("Caddy must stop before its managed configuration is removed")
	}
	shared, err := Script("node", "job", "https://controller.example", "token", Ownership{})
	if err != nil || strings.Contains(shared, "rm -f /etc/caddy/Caddyfile") || strings.Contains(shared, "rm -f /usr/local/bin/embyproxy-edge-agent") {
		t.Fatalf("shared cleanup unsafe: %v", err)
	}
	if strings.Contains(shared, "systemctl stop caddy.service") || strings.Contains(shared, "systemctl disable caddy.service") {
		t.Fatal("shared Caddy must not be stopped or disabled")
	}
}
