package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	projectconfig "embyproxy/internal/config"
)

func TestNormalizePlaybackConfigDefaultsIsolatedCanary(t *testing.T) {
	cfg := config{IsolatedTestMedia: true}
	if err := normalizePlaybackConfig(&cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.CanaryPath != projectconfig.DefaultEdgePlaybackCanaryPath {
		t.Fatalf("canary path = %q, want %q", cfg.CanaryPath, projectconfig.DefaultEdgePlaybackCanaryPath)
	}
}

func TestPlaybackHealthIsolatedMediaReturnsRangeHealthy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(isolatedTestMedia))
	defer server.Close()
	cfg := config{ListenAddr: "0.0.0.0:18080", ProbeAddr: strings.TrimPrefix(server.URL, "http://"), CanaryPath: projectconfig.DefaultEdgePlaybackCanaryPath, IsolatedTestMedia: true}
	if !playbackHealth(context.Background(), server.Client(), cfg) {
		t.Fatal("isolated media canary was not healthy")
	}
}

func TestPlaybackHealthExternalCanaryMatrix(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		contentRng string
		want       bool
	}{
		{name: "200 is unhealthy", status: http.StatusOK, want: false},
		{name: "206 without content range is unhealthy", status: http.StatusPartialContent, want: false},
		{name: "206 with content range is healthy", status: http.StatusPartialContent, contentRng: "bytes 0-1/2", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.Header.Get("Range") != "bytes=0-1023" {
					t.Errorf("request method=%s range=%q", r.Method, r.Header.Get("Range"))
				}
				if tt.contentRng != "" {
					w.Header().Set("Content-Range", tt.contentRng)
				}
				w.WriteHeader(tt.status)
			}))
			defer server.Close()
			cfg := config{ListenAddr: "0.0.0.0:18080", ProbeAddr: strings.TrimPrefix(server.URL, "http://"), CanaryPath: "/canary"}
			if got := playbackHealth(context.Background(), server.Client(), cfg); got != tt.want {
				t.Fatalf("playback health = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPlaybackHealthUsesProbeAddressNotWildcardBind(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(isolatedTestMedia))
	defer server.Close()
	cfg := config{ListenAddr: "0.0.0.0:18080", ProbeAddr: strings.TrimPrefix(server.URL, "http://"), CanaryPath: projectconfig.DefaultEdgePlaybackCanaryPath, IsolatedTestMedia: true}
	if !playbackHealth(context.Background(), server.Client(), cfg) {
		t.Fatal("playback health did not use the configured local probe address")
	}
}

func TestNormalizePlaybackConfigRejectsMissingExternalCanary(t *testing.T) {
	if err := normalizePlaybackConfig(&config{}); err == nil || !strings.Contains(err.Error(), "no playback canary configured") {
		t.Fatalf("error = %v, want missing canary validation error", err)
	}
}

func TestNormalizePlaybackConfigAcceptsExternalCanary(t *testing.T) {
	cfg := config{CanaryPath: "/health/range"}
	if err := normalizePlaybackConfig(&cfg); err != nil {
		t.Fatal(err)
	}
}
