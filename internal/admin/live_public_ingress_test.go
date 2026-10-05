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

func TestLivePublicIngressMedia(t *testing.T) {
	if os.Getenv("EMBYPROXY_LIVE_PUBLIC_CHECK") != "1" {
		t.Skip("explicit public ingress check only")
	}
	expectedIP := os.Getenv("EMBYPROXY_LIVE_EXPECTED_IP"); if expectedIP == "" { expectedIP = "104.233.158.9" }
	expectedID := os.Getenv("EMBYPROXY_LIVE_EXPECTED_NODE"); if expectedID == "" { expectedID = "5800c9d8fbd5cc739ae25cf4" }
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		conn, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, address)
		if err == nil {
			t.Log("public DNS connection", conn.RemoteAddr().String())
			if conn.RemoteAddr().String() != net.JoinHostPort(expectedIP,"443") {
				conn.Close()
				return nil, net.InvalidAddrError("public connection did not reach requested node")
			}
		}
		return conn, err
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: traceReadinessTransport{transport, t}, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Get("https://stream.149077530.xyz/health")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 || response.Header.Get("X-EmbyProxy-Node-ID") != expectedID {
		t.Fatal("public health did not verify requested node")
	}
	for _, slug := range []string{"1111", "younoyes"} {
		t.Run(slug, func(t *testing.T) {
			raw, err := os.ReadFile("/var/lib/embyproxy-gsy-sidecar/playback-credentials/" + slug + ".token")
			if err != nil {
				t.Fatal(err)
			}
			token := strings.TrimSpace(string(raw))
			root := "https://stream.149077530.xyz/s/" + slug
			items, err := discoverEmbyPlaybackItems(context.Background(), root, token, client)
			if err != nil {
				t.Fatal(err)
			}
			if len(items) < 2 {
				t.Fatal("two playable samples required")
			}
			for _, item := range items[:2] {
				if err := probeNodeMedia(context.Background(), client, root, item, token); err != nil {
					t.Error(err)
				}
			}
		})
	}
}

func TestLiveAuthoritativePublicIngress(t *testing.T) {
	if os.Getenv("EMBYPROXY_LIVE_PUBLIC_CHECK") != "1" {
		t.Skip("explicit authoritative DNS check only")
	}
	expectedIP := os.Getenv("EMBYPROXY_LIVE_EXPECTED_IP"); if expectedIP == "" { expectedIP = "104.233.158.9" }
	for _, ns := range []string{"launch1.spaceship.net", "launch2.spaceship.net"} {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		addresses, err := net.DefaultResolver.LookupHost(ctx, ns)
		if err != nil || len(addresses) == 0 {
			cancel()
			t.Fatalf("NS lookup failed: %v", err)
		}
		endpoint := net.JoinHostPort(addresses[0], "53")
		resolver := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, endpoint)
		}}
		answers, err := resolver.LookupHost(ctx, "stream.149077530.xyz")
		cancel()
		if err != nil || !recursiveAnswersMatch(answers, expectedIP) {
			t.Fatalf("NS %s answers=%v err=%v", ns, answers, err)
		}
		t.Log("authoritative", ns, endpoint, answers)
	}
}
