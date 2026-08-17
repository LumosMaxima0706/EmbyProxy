package mediaproxy

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strconv"
	"testing"
)

type sequenceResolver struct {
	calls     int
	responses [][]netip.Addr
}

func (r *sequenceResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	r.calls++
	index := r.calls - 1
	if index >= len(r.responses) {
		index = len(r.responses) - 1
	}
	return r.responses[index], nil
}

func TestPrivateTargetBlockedByDefault(t *testing.T) {
	target := Target{Scheme: "http", Host: "127.0.0.1", Port: 8080}
	if err := target.CheckPrivate(context.Background(), false); err != ErrPrivateTarget {
		t.Fatalf("err=%v", err)
	}
}

func TestPrivateTargetAllowedOnlyExplicitly(t *testing.T) {
	target := Target{Scheme: "http", Host: "127.0.0.1", Port: 8080}
	if err := target.CheckPrivate(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(nil)
	defer server.Close()
}

func TestPrivateNetworkRangesBlocked(t *testing.T) {
	for _, host := range []string{
		"10.0.0.1", "172.16.0.1", "192.168.1.1", "169.254.169.254",
		"100.100.100.100", "::1", "fc00::1", "fe80::1",
	} {
		target := Target{Scheme: "https", Host: host, Port: 443}
		if err := target.CheckPrivate(context.Background(), false); err != ErrPrivateTarget {
			t.Fatalf("host %s err=%v", host, err)
		}
	}
}

func TestClientQueryCannotSelectTarget(t *testing.T) {
	target := Target{Scheme: "https", Host: "media.example", Port: 443}
	requestURL, _ := url.Parse("/items?upstream=http://private.invalid:80")
	got, err := target.URLForRequest(requestURL)
	if err != nil || got.Host != "media.example:443" {
		t.Fatalf("got=%v err=%v", got, err)
	}
}

func TestRequestPathCannotEscapeBasePath(t *testing.T) {
	target := Target{Scheme: "https", Host: "media.example", Port: 443, BasePath: "/emby"}
	for _, raw := range []string{"/../admin", "/%2e%2e/admin"} {
		requestURL, _ := url.Parse(raw)
		if _, err := target.URLForRequest(requestURL); err != ErrInvalidRequestPath {
			t.Fatalf("path=%q err=%v", raw, err)
		}
	}
}

func TestDNSResolutionIsBoundToDialAddress(t *testing.T) {
	resolver := &sequenceResolver{responses: [][]netip.Addr{
		{netip.MustParseAddr("198.51.100.10")},
		{netip.MustParseAddr("127.0.0.1")},
	}}
	var dialed string
	executor := NewExecutor(Config{
		Resolver: resolver,
		DialContext: func(_ context.Context, _, address string) (net.Conn, error) {
			dialed = address
			return nil, errors.New("mock dial failure")
		},
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("GET", "/health", nil)
	executor.ServeHTTP(recorder, request, Target{Scheme: "http", Host: "media.example", Port: 80})
	if resolver.calls != 1 {
		t.Fatalf("resolver calls=%d want=1", resolver.calls)
	}
	if dialed != "198.51.100.10:80" {
		t.Fatalf("dialed=%q", dialed)
	}
	if recorder.Code != 502 {
		t.Fatalf("status=%d", recorder.Code)
	}
}

func TestUpstreamRedirectIsNotFollowed(t *testing.T) {
	redirectTargetHit := false
	redirectTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirectTargetHit = true
		w.WriteHeader(http.StatusNoContent)
	}))
	defer redirectTarget.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", redirectTarget.URL)
		w.WriteHeader(http.StatusFound)
	}))
	defer upstream.Close()
	parsed, _ := url.Parse(upstream.URL)
	port, _ := strconv.Atoi(parsed.Port())
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/redirect", nil)
	NewExecutor(Config{AllowPrivateTargets: true}).ServeHTTP(recorder, request, Target{Scheme: "http", Host: parsed.Hostname(), Port: port})
	if recorder.Code != http.StatusFound || redirectTargetHit {
		t.Fatalf("status=%d redirect_target_hit=%v", recorder.Code, redirectTargetHit)
	}
}
