package admin

import (
	"context"
	"embyproxy/internal/config"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCertificateBootstrapDoesNotBlockOnDNSCache(t *testing.T) {
	h := newAuthTestHandler(t, config.Config{PublicIngressHost: "stream.example.net"})
	script := h.edgeCertificateSetup()
	if !strings.Contains(script, "systemctl start --no-block") || !strings.Contains(script, "TimeoutStartSec=4500") {
		t.Fatal("DNS wait blocks bootstrap or exceeds service budget")
	}
}

func TestPlaybackDiscoveryFallsBackToSessionUser(t *testing.T) {
	userPath := false
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/Users/Me"):
			w.WriteHeader(500)
		case strings.HasSuffix(r.URL.Path, "/Sessions"):
			w.Write([]byte(`[{"UserId":"11111111111111111111111111111111","DeviceId":"embyproxy-canary"}]`))
		case strings.Contains(r.URL.Path, "/Users/11111111111111111111111111111111/Items"):
			userPath = true
			w.Write([]byte(`{"Items":[{"Id":"sample-a"},{"Id":"sample-b"}]}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer s.Close()
	items, err := discoverEmbyPlaybackItems(context.Background(), s.URL, "token", s.Client())
	if err != nil || len(items) != 2 || !userPath {
		t.Fatal(items, err, userPath)
	}
}
