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

func TestCandidateFragmentPassesNginxSyntax(t *testing.T) {
	if _, err := os.Stat("/usr/sbin/nginx"); err != nil {
		t.Skip("nginx is not installed in the test environment")
	}
	directory := t.TempDir()
	fragment := filepath.Join(directory, "candidate.conf")
	if err := os.WriteFile(fragment, renderNginxFragment(EdgeConfig{NodeName: "bwg"}, testManifest()), 0600); err != nil {
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
