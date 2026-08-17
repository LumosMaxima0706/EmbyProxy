package mediaproxy

import "testing"

func TestRewriteLocationAndContentLocation(t *testing.T) {
	target := Target{Scheme: "https", Host: "media.example", Port: 443}
	headers := map[string][]string{
		"Location":         {"https://media.example:443/emby/login"},
		"Content-Location": {"/emby/items"},
	}
	got := rewriteResponseHeaders(headers, target, "/s/demo/")
	if got["Location"][0] != "/s/demo/emby/login" || got["Content-Location"][0] != "/s/demo/emby/items" {
		t.Fatalf("headers=%v", got)
	}
}

func TestRewriteLocationStripsTargetBasePath(t *testing.T) {
	target := Target{Scheme: "https", Host: "media.example", Port: 443, BasePath: "/emby"}
	got := rewriteLocation("https://media.example:443/emby/Videos/1?quality=original", target, "/s/demo/")
	if got != "/s/demo/Videos/1?quality=original" {
		t.Fatalf("location=%q", got)
	}
}
