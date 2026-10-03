package admin

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagedReinstallKeepsBusinessCertificate(t *testing.T) {
	script := installSafetyScript(t)
	start := strings.Index(script, "    # Preserve a working business certificate")
	end := strings.Index(script[start:], "    chown root:caddy") + start
	if start < 0 || end <= start {
		t.Fatal("preservation guard missing")
	}
	guard := script[start:end]
	for _, tc := range []struct {
		name                 string
		managed, match, cert bool
		want                 string
	}{
		{"working", true, true, true, "certificate"},
		{"domain-change", true, false, true, "initial"},
		{"unmanaged", false, true, true, "initial"},
		{"missing-certificate", true, true, false, "initial"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			cfg := filepath.Join(dir, "config")
			os.Mkdir(cfg, 0700)
			os.WriteFile(filepath.Join(dir, "Caddyfile"), []byte("tls /var/lib/embyproxy-edge/tls/business-certificate/fullchain.pem\n"), 0600)
			domain := "edge.example.net"
			if !tc.match {
				domain = "other.example.net"
			}
			os.WriteFile(filepath.Join(dir, "marker"), []byte("domain="+domain+"\n"), 0600)
			if tc.cert {
				os.WriteFile(filepath.Join(cfg, "business-domain"), []byte("stream.example.net"), 0600)
			}
			tmp := filepath.Join(dir, "pending")
			os.WriteFile(tmp, []byte("initial"), 0600)
			managed := "false"
			if tc.managed {
				managed = "true"
			}
			prefix := "set -eu\ncaddy_managed=" + managed + "\ncaddy_root='" + dir + "'\ncaddy_tmp='" + tmp + "'\ncaddy_marker='" + filepath.Join(dir, "marker") + "'\ncfg_dir='" + cfg + "'\nedge_domain=edge.example.net\n"
			if out, err := exec.Command("sh", "-c", prefix+guard).CombinedOutput(); err != nil {
				t.Fatal(err, string(out))
			}
			data, _ := os.ReadFile(tmp)
			if !strings.Contains(string(data), tc.want) {
				t.Fatal(string(data))
			}
		})
	}
}
