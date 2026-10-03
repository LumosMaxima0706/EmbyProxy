package admin

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

type traceReadinessTransport struct {
	base http.RoundTripper
	t    *testing.T
}

func (x traceReadinessTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	res, err := x.base.RoundTrip(r)
	if res != nil {
		x.t.Log("response", r.Method, r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:], res.StatusCode, res.Header.Get("Content-Range"), "redirect", strings.Contains(res.Header.Get("Location"), "/s/1111/http/"))
	} else {
		x.t.Log("request failed")
	}
	return res, err
}

func TestLiveNodeReadiness(t *testing.T) {
	if os.Getenv("EMBYPROXY_LIVE_NODE_CHECK") != "1" {
		t.Skip("explicit live check only")
	}
	slug := os.Getenv("EMBYPROXY_LIVE_ROUTE")
	if slug != "1111" && slug != "younoyes" {
		slug = "1111"
	}
	token, err := os.ReadFile("/var/lib/embyproxy-gsy-sidecar/playback-credentials/" + slug + ".token")
	if err != nil {
		t.Fatal(err)
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{ServerName: "stream.149077530.xyz", MinVersion: tls.VersionTLS12}, DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, "104.233.158.9:443")
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: traceReadinessTransport{transport, t}, Timeout: 15 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
	root := "https://stream.149077530.xyz/s/" + slug
	items, err := discoverEmbyPlaybackItems(context.Background(), root, strings.TrimSpace(string(token)), client)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("samples", len(items))
	for _, item := range items[:2] {
		if err := probeNodeMedia(context.Background(), client, root, item, strings.TrimSpace(string(token))); err != nil {
			t.Error(err)
		} else {
			t.Log("sample media passed")
		}
	}
}
