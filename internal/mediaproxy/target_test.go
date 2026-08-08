package mediaproxy

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
)

func TestParseHTTPAndHTTPS(t *testing.T) {
	for _, scheme := range []string{"http", "https"} {
		target, err := ParseTarget(scheme, "media.example", 8443, "/emby")
		if err != nil || target.Scheme != scheme || target.BasePath != "/emby" {
			t.Fatalf("target=%+v err=%v", target, err)
		}
	}
}

func TestTargetURLForRequestJoinsBasePathAndQuery(t *testing.T) {
	target, err := ParseTarget("https", "media.example", 443, "/base/")
	if err != nil {
		t.Fatal(err)
	}
	requestURL, _ := url.Parse("/Videos/1?Range=1&redacted=2")
	got, err := target.URLForRequest(requestURL)
	if err != nil || got.String() != "https://media.example:443/base/Videos/1?Range=1&redacted=2" {
		t.Fatalf("url=%q err=%v", got, err)
	}
}

func TestTargetURLForCommonStreamingPaths(t *testing.T) {
	target := Target{Scheme: "https", Host: "media.example", Port: 443, BasePath: "/emby"}
	for _, requestPath := range []string{
		"/Videos/1/stream.m3u8",
		"/Videos/1/manifest.mpd",
		"/Audio/1/stream.mp3",
	} {
		requestURL, _ := url.Parse(requestPath)
		got, err := target.URLForRequest(requestURL)
		if err != nil || got.Path != "/emby"+requestPath {
			t.Fatalf("path=%s got=%v err=%v", requestPath, got, err)
		}
	}
}

func TestServeHTTPSTargetWithBasePathAndQuery(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/emby/video" || r.URL.Query().Get("quality") != "original" {
			t.Fatalf("upstream request path=%q query=%q", r.URL.Path, r.URL.RawQuery)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()
	parsed, _ := url.Parse(upstream.URL)
	port, _ := strconv.Atoi(parsed.Port())
	executor := NewExecutor(Config{
		AllowPrivateTargets: true,
		TLSConfig:           &tls.Config{InsecureSkipVerify: true}, // Test-only certificate.
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/video?quality=original", nil)
	executor.ServeHTTP(recorder, request, Target{Scheme: "https", Host: parsed.Hostname(), Port: port, BasePath: "/emby"})
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%q", recorder.Code, recorder.Body.String())
	}
}

func TestTargetRejectsInvalidValues(t *testing.T) {
	for _, target := range []Target{
		{Scheme: "ftp", Host: "media.example", Port: 21},
		{Scheme: "https", Host: "", Port: 443},
		{Scheme: "https", Host: "media.example", Port: 0},
		{Scheme: "https", Host: "media.example", Port: 443, BasePath: "relative"},
	} {
		if ValidateTarget(target) == nil {
			t.Fatalf("target unexpectedly valid: %+v", target)
		}
	}
}
