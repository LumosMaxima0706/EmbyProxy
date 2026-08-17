package publicationagent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"embyproxy/internal/publicationprotocol"
)

func testManifest() publicationprotocol.EdgeManifest {
	return publicationprotocol.EdgeManifest{
		Version: publicationprotocol.Version, Action: publicationprotocol.ActionPublish,
		OperationID: "test-operation", Slug: "testnode", UpstreamHost: "saved.example",
		UpstreamPort: 443,
	}
}

func TestRenderNginxFragmentIsExplicitAndUncached(t *testing.T) {
	fragment := string(renderNginxFragment(EdgeConfig{NodeName: "nosla"}, testManifest()))
	for _, marker := range []string{
		"location = /https/saved.example/443",
		"location ^~ /https/saved.example/443/",
		"proxy_buffering off;",
		"proxy_request_buffering off;",
		"proxy_cache off;",
		"proxy_set_header Range $http_range;",
		"proxy_set_header If-Range $http_if_range;",
	} {
		if !strings.Contains(fragment, marker) {
			t.Fatalf("fragment missing %q: %s", marker, fragment)
		}
	}
	if strings.Contains(fragment, "proxy_cache_path") || strings.Contains(fragment, "prefetch") {
		t.Fatalf("fragment contains forbidden pre-cache directive: %s", fragment)
	}
}

func TestRenderNginxFragmentUsesEdgeConnectionVariable(t *testing.T) {
	bwg := string(renderNginxFragment(EdgeConfig{NodeName: "bwg"}, testManifest()))
	nosla := string(renderNginxFragment(EdgeConfig{NodeName: "nosla"}, testManifest()))
	if !strings.Contains(bwg, "proxy_set_header Connection $stream_bwg_connection_upgrade;") || strings.Contains(bwg, "proxy_set_header Connection $stream_connection_upgrade;") {
		t.Fatalf("BWG fragment uses the wrong connection variable")
	}
	if !strings.Contains(nosla, "proxy_set_header Connection $stream_connection_upgrade;") {
		t.Fatalf("NOSLA fragment uses the wrong connection variable")
	}
}

func TestRenderNginxFragmentIncludesSavedBackupsAndControlledRedirectHosts(t *testing.T) {
	manifest := testManifest()
	manifest.Routes = []publicationprotocol.EdgeRoute{
		{LineID: "main", Scheme: "https", Host: "saved.example", Port: 443, Kind: "upstream", Position: 1},
		{LineID: "backup-2", Scheme: "https", Host: "backup.example", Port: 443, Kind: "upstream", Position: 2},
		{LineID: "redirect-1", Scheme: "https", Host: "media-cdn.example", Port: 443, Kind: "redirect", Position: 3},
		{LineID: "redirect-2", Scheme: "http", Host: "media-origin.example", Port: 9527, Kind: "redirect", Position: 4},
	}
	fragment := string(renderNginxFragment(EdgeConfig{NodeName: "nosla"}, manifest))
	for _, marker := range []string{
		"location ^~ /https/backup.example/443/",
		"location ^~ /https/media-cdn.example/443/",
		"error_page 403 404 416 429 500 502 503 504 = @embyproxy_testnode_backup_2;",
		"rewrite ^/https/saved\\.example/443(.*)$ /https/backup.example/443$1 break;",
	} {
		if !strings.Contains(fragment, marker) {
			t.Fatalf("multi-route fragment missing %q: %s", marker, fragment)
		}
	}
}

func TestRenderNginxFragmentAllowsOnlyManifestedHTTPRedirectPort(t *testing.T) {
	manifest := testManifest()
	manifest.Routes = []publicationprotocol.EdgeRoute{
		{LineID: "main", Scheme: "https", Host: "saved.example", Port: 443, Kind: "upstream", Position: 1},
		{LineID: "redirect-1", Scheme: "http", Host: "media-origin.example", Port: 9527, Kind: "redirect", Position: 2},
	}
	fragment := string(renderNginxFragment(EdgeConfig{NodeName: "nosla"}, manifest))
	if !strings.Contains(fragment, "location ^~ /http/media-origin.example/9527/") || strings.Contains(fragment, "/http/media-origin.example/443/") {
		t.Fatalf("non-standard redirect route was not rendered exactly: %s", fragment)
	}
}

func TestRedirectEndpointAllowsOnlyPublicIPAddress(t *testing.T) {
	if !safeRedirectEndpoint(RedirectEndpoint{Scheme: "http", Host: "198.51.100.7", Port: 9527}, "stream.example", "owner.example") {
		t.Fatal("public redirect IP was rejected")
	}
	for _, host := range []string{"127.0.0.1", "10.0.0.1", "169.254.1.1", "224.0.0.1", "0.0.0.0"} {
		if safeRedirectEndpoint(RedirectEndpoint{Scheme: "http", Host: host, Port: 9527}, "stream.example", "owner.example") {
			t.Fatalf("unsafe redirect IP %q was accepted", host)
		}
	}
}

func TestRenderNginxFragmentAllowsOnlyConfiguredCDNPattern(t *testing.T) {
	manifest := testManifest()
	manifest.Routes = []publicationprotocol.EdgeRoute{
		{LineID: "main", Scheme: "https", Host: "saved.example", Port: 443, Kind: "upstream", Position: 1},
		{LineID: "redirect-pattern-1", Scheme: "https", HostSuffix: "cdn.saved.example", LabelLength: 4, Port: 443, Kind: "redirect_pattern", Position: 2},
	}
	fragment := string(renderNginxFragment(EdgeConfig{NodeName: "nosla"}, manifest))
	want := "location ~ ^/https/[a-z0-9][a-z0-9][a-z0-9][a-z0-9]\\.cdn\\.saved\\.example/443(?:/|$)"
	if !strings.Contains(fragment, want) || strings.Contains(fragment, "location ~ ^/https/.+") {
		t.Fatalf("redirect pattern was not narrowly rendered: %s", fragment)
	}
}

func TestRenderNginxFragmentRewritesOnlyManifestedRedirectLocations(t *testing.T) {
	manifest := testManifest()
	manifest.Routes = []publicationprotocol.EdgeRoute{
		{LineID: "main", Scheme: "https", Host: "saved.example", Port: 443, Kind: "upstream", Position: 1},
		{LineID: "redirect-1", Scheme: "http", Host: "media-origin.example", Port: 9527, Kind: "redirect", Position: 2},
		{LineID: "redirect-pattern-1", Scheme: "https", HostSuffix: "cdn.saved.example", LabelLength: 4, Port: 443, Kind: "redirect_pattern", Position: 3},
	}
	fragment := string(renderNginxFragment(EdgeConfig{NodeName: "nosla"}, manifest))
	for _, marker := range []string{
		"proxy_redirect ~^https://saved\\.example(?::443)?(.*)$ $scheme://$host/https/saved.example/443$1;",
		"proxy_redirect ~^http://media-origin\\.example:9527(.*)$ $scheme://$host/http/media-origin.example/9527$1;",
		"proxy_redirect ~^https://([a-z0-9][a-z0-9][a-z0-9][a-z0-9])\\.cdn\\.saved\\.example(?::443)?(.*)$ $scheme://$host/https/$1.cdn.saved.example/443$2;",
		"proxy_redirect ~^/(?!https/)(.*)$ $scheme://$host/https/saved.example/443/$1;",
	} {
		if !strings.Contains(fragment, marker) {
			t.Fatalf("redirect rewrite missing %q: %s", marker, fragment)
		}
	}
	if strings.Contains(fragment, "proxy_redirect ~^https?://") || strings.Contains(fragment, "location ~ ^/https/.+") {
		t.Fatalf("fragment contains an unrestricted redirect rule: %s", fragment)
	}
}

func TestCandidateFragmentPassesNginxSyntax(t *testing.T) {
	if _, err := os.Stat("/usr/sbin/nginx"); err != nil {
		t.Skip("nginx is not installed in the test environment")
	}
	directory := t.TempDir()
	fragment := filepath.Join(directory, "candidate.conf")
	manifest := testManifest()
	manifest.Routes = []publicationprotocol.EdgeRoute{
		{LineID: "main", Scheme: "https", Host: "saved.example", Port: 443, Kind: "upstream", Position: 1},
		{LineID: "backup-2", Scheme: "https", Host: "backup.example", Port: 443, Kind: "upstream", Position: 2},
		{LineID: "redirect-1", Scheme: "https", Host: "media-cdn.example", Port: 443, Kind: "redirect", Position: 3},
	}
	if err := os.WriteFile(fragment, renderNginxFragment(EdgeConfig{NodeName: "bwg"}, manifest), 0600); err != nil {
		t.Fatal(err)
	}
	if err := testCandidate(context.Background(), fragment, directory); err != nil {
		t.Fatalf("candidate nginx syntax failed: %v", err)
	}
}

func TestVerifyIncludeHookRequiresHTTPAndHTTPSBlocks(t *testing.T) {
	config := filepath.Join(t.TempDir(), "stream.conf")
	cfg := EdgeConfig{StreamConfig: config, IncludeDirective: "/etc/nginx/conf.d/embyproxy-publications/*.conf"}
	directive := "include /etc/nginx/conf.d/embyproxy-publications/*.conf;\n"
	if err := os.WriteFile(config, []byte(directive), 0600); err != nil {
		t.Fatal(err)
	}
	if err := verifyIncludeHook(cfg); err == nil || err.Error() != "edge_include_hook_missing" {
		t.Fatalf("single HTTP hook was accepted: %v", err)
	}
	if err := os.WriteFile(config, []byte(directive+directive), 0600); err != nil {
		t.Fatal(err)
	}
	if err := verifyIncludeHook(cfg); err != nil {
		t.Fatalf("HTTP and HTTPS hooks were rejected: %v", err)
	}
}

func TestCombineEdgesReportsBWGWhenNOSLAWasNotAttempted(t *testing.T) {
	response := combineEdges(publicationprotocol.ActionPublish,
		publicationprotocol.EdgeResult{Status: "not_attempted"},
		publicationprotocol.EdgeResult{Status: "failed", ErrorCode: "nginx_test_failed", FailedStep: "nginx_test"})
	if response.ErrorCode != "nginx_test_failed" || response.FailedStep != "nginx_test" {
		t.Fatalf("response=%+v", response)
	}
}

func TestOperationBackupDirectoryIsUniqueForRollback(t *testing.T) {
	cfg := EdgeConfig{NodeName: "bwg", BackupRoot: t.TempDir()}
	manifest := testManifest()
	first, err := createOperationBackupDir(cfg, manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Action = publicationprotocol.ActionUnpublish
	second, err := createOperationBackupDir(cfg, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("publish and rollback reused backup directory %q", first)
	}
	for _, directory := range []string{first, second} {
		info, err := os.Stat(directory)
		if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
			t.Fatalf("backup directory %q info=%v err=%v", directory, info, err)
		}
	}
}
