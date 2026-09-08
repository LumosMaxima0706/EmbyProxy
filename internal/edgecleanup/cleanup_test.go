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
	for _, marker := range []string{"systemctl stop embyproxy-edge.service", "rm -f /etc/systemd/system/embyproxy-edge.service", "managed_by=embyproxy-edge", "X-EmbyProxy-Cleanup-Token"} {
		if !strings.Contains(script, marker) {
			t.Fatalf("missing %q", marker)
		}
	}
	shared, err := Script("node", "job", "https://controller.example", "token", Ownership{})
	if err != nil || strings.Contains(shared, "rm -f /etc/caddy/Caddyfile") || strings.Contains(shared, "rm -f /usr/local/bin/embyproxy-edge-agent") {
		t.Fatalf("shared cleanup unsafe: %v", err)
	}
}
