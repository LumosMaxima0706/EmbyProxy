package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"embyproxy/internal/config"
	"embyproxy/internal/storage"
)

func TestCertificateToolsAuthorizationAndScript(t *testing.T) {
	h := newAuthTestHandler(t, config.Config{PublicIngressHost: "stream.example.net"})
	e, token, err := h.store.CreateProxyNode(context.Background(), storage.ProxyNode{Name: "cert", PublicAddress: "https://edge.example.net", ResetDay: 1}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	node, credential, err := h.store.CompleteEnrollment(context.Background(), e.ID, token, "test", "test")
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/edge/certificate-tools/" + node.ID
	for _, secret := range []string{"wrong", credential} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("X-EmbyProxy-Node-Credential", secret)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if secret == "wrong" {
			if rec.Code != 404 {
				t.Fatal(rec.Code)
			}
			continue
		}
		if rec.Code != 200 {
			t.Fatal(rec.Code, rec.Body.String())
		}
		script := rec.Body.String()
		for _, marker := range []string{"stream.example.net", "certificate.timer", "manual-auth-hook", "EMBYPROXY_CERTIFICATE_SETUP_ONLY", "ACME_LEASE_CONFLICT", "fullchain.pem"} {
			if !strings.Contains(script, marker) {
				t.Fatal("missing", marker)
			}
		}
		if strings.Contains(script, credential) {
			t.Fatal("credential embedded in certificate tools")
		}
		p := filepath.Join(t.TempDir(), "setup.sh")
		os.WriteFile(p, []byte(script), 0600)
		if out, err := exec.Command("sh", "-n", p).CombinedOutput(); err != nil {
			t.Fatal(err, string(out))
		}
		if strings.Index(script, "EMBYPROXY_CERTIFICATE_SETUP_ONLY") > strings.Index(script, "systemctl enable --now") {
			t.Fatal("setup-only must not start timer")
		}
	}
}

func TestReadinessUnknownStaleAndDisabledRouting(t *testing.T) {
	h := newAuthTestHandler(t, config.Config{})
	e, _, err := h.store.CreateProxyNode(context.Background(), storage.ProxyNode{Name: "disabled", PublicAddress: "https://edge.example.net", ResetDay: 1}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	node, _ := h.store.GetProxyNode(context.Background(), e.NodeID)
	if len(h.proxyNodeReadinessViews(context.Background(), []storage.ProxyNode{*node})) != 0 {
		t.Fatal("unverified became ready")
	}
	v := proxyNodeReadiness{CheckedAt: time.Now().Add(-16 * time.Minute).Unix(), BusinessTLS: "ready", Routes: "ready", Playback: "ready", Origin: node.PublicAddress, AgentCommit: node.AgentCommit}
	if err := h.store.KV().Put(context.Background(), "node-readiness:"+node.ID, v); err != nil {
		t.Fatal(err)
	}
	got := h.proxyNodeReadinessViews(context.Background(), []storage.ProxyNode{*node})[node.ID]
	if got.Playback != "stale" || got.BusinessTLS != "stale" {
		t.Fatal(got)
	}
	rec := httptest.NewRecorder()
	h.handleProxyNodesAPI(rec, httptest.NewRequest(http.MethodPost, "/", nil), "/api/admin/proxy-nodes/"+node.ID+"/verify-readiness")
	if rec.Code != 503 {
		t.Fatal("verification route unreachable", rec.Code, rec.Body.String())
	}
	if after, _ := h.store.GetProxyNode(context.Background(), node.ID); after.Enabled || after.Priority != node.Priority {
		t.Fatal("probe changed settings")
	}
	for _, marker := range []string{"!n.enabled ||", "if (filter === 'disabled') return proxyNodeDisabled(n)", "业务 TLS / 路由", "业务播放", "stale:'已过期'"} {
		if !strings.Contains(indexHTML, marker) {
			t.Fatal("UI contract", marker)
		}
	}
	if strings.Index(indexHTML, "window.proxyReadiness = r.readiness") > strings.Index(indexHTML, "  renderProxyNodeTable();") {
		t.Fatal("readiness assigned after render")
	}
}

func TestMediaProbeRangeAndRedirectSafety(t *testing.T) {
	for _, mode := range []string{"range", "api-only", "redirect"} {
		t.Run(mode, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "PlaybackInfo") {
					json.NewEncoder(w).Encode(map[string]any{"MediaSources": []map[string]string{{"Id": "source"}}})
					return
				}
				if mode == "redirect" {
					w.Header().Set("Location", "https://untrusted.example/media")
					w.WriteHeader(302)
					return
				}
				if mode == "range" {
					w.Header().Set("Content-Range", "bytes 0-1023/4096")
					w.WriteHeader(206)
				}
				w.Write([]byte(strings.Repeat("x", 1024)))
			}))
			defer s.Close()
			err := probeNodeMedia(context.Background(), s.Client(), s.URL+"/s/test", "item", "token")
			if (err == nil) != (mode == "range") {
				t.Fatal(mode, err)
			}
		})
	}
}

func TestCertificatePythonFailureCleanupAndRollback(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"acme_hook.py", "certificate_manager.py"} {
		data, _ := edgeCertificateFiles.ReadFile("edgecert/" + name)
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	code := `
import importlib.util,pathlib,tempfile,unittest.mock as m,urllib.error,io,subprocess,os
def load(name):
 s=importlib.util.spec_from_file_location(name,ROOT/name);x=importlib.util.module_from_spec(s);s.loader.exec_module(x);return x
h=load('acme_hook.py'); manager=load('certificate_manager.py')
with tempfile.TemporaryDirectory() as d:
 h.ROOT=pathlib.Path(d);h.PENDING=h.ROOT/'pending.json'
 h.PENDING.write_text('{"domain":"stream.example.net","validation":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}')
 calls=[]
 with m.patch.object(h,'call',side_effect=lambda *args:calls.append(args)):
  h.cleanup()
 assert len(calls)==1 and not h.PENDING.exists()
 h.PENDING.write_text('{"domain":"stream.example.net","validation":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}')
 with m.patch.object(h,'call',side_effect=RuntimeError('failure')),m.patch.object(h.time,'sleep'):
  try:h.cleanup();raise AssertionError('cleanup failure hidden')
  except RuntimeError:pass
 assert h.PENDING.exists()
 with m.patch.object(manager,'output',return_value=b'Hostname stream.example.net does NOT match certificate'):
  try:manager.deploy('stream.example.net','edge.example.net',pathlib.Path(d));raise AssertionError('wrong SAN accepted')
  except RuntimeError:pass
 live=pathlib.Path(d)/'live';live.mkdir()
 for n in ('fullchain.pem','privkey.pem','cert.pem','chain.pem'):(live/n).write_text('test')
 conf=pathlib.Path(d)/'Caddyfile';conf.write_text('old config')
 original=pathlib.Path
 def mapped(p):return conf if str(p)=='/etc/caddy/Caddyfile' else original(p)
 manager.ROOT=pathlib.Path(d)/'state';manager.ROOT.mkdir()
 commands=[]
 def command(args,**kwargs):
  commands.append(args)
  if args[0]=='curl':raise RuntimeError('TLS verification failed')
  return subprocess.CompletedProcess(args,0)
 def out(args):return b'Hostname stream.example.net does match certificate' if '-checkhost' in args else b'pubkey'
 with m.patch.object(manager.pathlib,'Path',side_effect=mapped),m.patch.object(manager,'run',side_effect=command),m.patch.object(manager,'output',side_effect=out),m.patch.object(manager.grp,'getgrnam',return_value=type('Group',(),{'gr_gid':os.getgid()})()),m.patch.object(manager.os,'chown'):
  try:manager.deploy('stream.example.net','edge.example.net',live);raise AssertionError('reload failure hidden')
  except RuntimeError:pass
 assert conf.read_text()=='old config'
 assert len([c for c in commands if c[:2]==['systemctl','reload']])==2
print('python cleanup, SAN rejection and rollback: PASS')
`
	cmd := exec.Command("python3", "-c", "import pathlib; ROOT=pathlib.Path("+fmtQuote(dir)+")\n"+code)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	} else {
		t.Log(string(out))
	}
}

func fmtQuote(s string) string { b, _ := json.Marshal(s); return string(b) }
