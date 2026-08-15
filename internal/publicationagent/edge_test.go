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
	fragment := string(renderNginxFragment(testManifest()))
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

func TestCandidateFragmentPassesNginxSyntax(t *testing.T) {
	if _, err := os.Stat("/usr/sbin/nginx"); err != nil {
		t.Skip("nginx is not installed in the test environment")
	}
	directory := t.TempDir()
	fragment := filepath.Join(directory, "candidate.conf")
	if err := os.WriteFile(fragment, renderNginxFragment(testManifest()), 0600); err != nil {
		t.Fatal(err)
	}
	if err := testCandidate(context.Background(), fragment, directory); err != nil {
		t.Fatalf("candidate nginx syntax failed: %v", err)
	}
}
