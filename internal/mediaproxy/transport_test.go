package mediaproxy

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestProxyEnvironmentSelectionAndNoProxy(t *testing.T) {
	proxyHits := 0
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyHits++
		w.WriteHeader(http.StatusTeapot)
	}))
	defer proxy.Close()
	proxyURL, _ := url.Parse(proxy.URL)
	t.Setenv("HTTP_PROXY", proxyURL.String())
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("ALL_PROXY", "")
	t.Setenv("NO_PROXY", "")

	transport := newTransport(Config{TrustProxyEnv: true})
	request, _ := http.NewRequest(http.MethodGet, "http://public.example/health", nil)
	proxyRequest, err := transport.Proxy(request)
	if err != nil || proxyRequest == nil || proxyRequest.Host != proxyURL.Host {
		t.Fatalf("proxy=%v err=%v", proxyRequest, err)
	}
	if !noProxyMatch("public.example", "public.example") || noProxyMatch("other.example", "public.example") {
		t.Fatal("NO_PROXY host matching incorrect")
	}
	if !noProxyMatch("anything.example", "*") {
		t.Fatal("NO_PROXY wildcard not honored")
	}

	request, _ = http.NewRequest(http.MethodGet, "http://public.example/health", nil)
	t.Setenv("NO_PROXY", "public.example")
	proxyRequest, err = transport.Proxy(request)
	if err != nil || proxyRequest != nil {
		t.Fatalf("NO_PROXY should bypass proxy: proxy=%v err=%v", proxyRequest, err)
	}
	_ = proxyHits
}

func TestProxyEnvironmentSchemeAndFallbackSelection(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://http-proxy.example:8080")
	t.Setenv("HTTPS_PROXY", "http://https-proxy.example:8443")
	t.Setenv("ALL_PROXY", "http://all-proxy.example:1080")
	t.Setenv("NO_PROXY", "bypass.example")
	transport := newTransport(Config{TrustProxyEnv: true})
	for _, tc := range []struct {
		requestURL string
		wantHost   string
	}{
		{"http://media.example/item", "http-proxy.example:8080"},
		{"https://media.example/item", "https-proxy.example:8443"},
		{"http://bypass.example/item", ""},
	} {
		request, _ := http.NewRequest(http.MethodGet, tc.requestURL, nil)
		got, err := transport.Proxy(request)
		if err != nil {
			t.Fatalf("url=%s err=%v", tc.requestURL, err)
		}
		if tc.wantHost == "" && got != nil {
			t.Fatalf("url=%s proxy=%v", tc.requestURL, got)
		}
		if tc.wantHost != "" && (got == nil || got.Host != tc.wantHost) {
			t.Fatalf("url=%s proxy=%v want=%s", tc.requestURL, got, tc.wantHost)
		}
	}
	t.Setenv("HTTP_PROXY", "")
	t.Setenv("HTTPS_PROXY", "")
	request, _ := http.NewRequest(http.MethodGet, "https://media.example/item", nil)
	got, err := transport.Proxy(request)
	if err != nil || got == nil || got.Host != "all-proxy.example:1080" {
		t.Fatalf("ALL_PROXY fallback=%v err=%v", got, err)
	}
	if newTransport(Config{TrustProxyEnv: false}).Proxy != nil {
		t.Fatal("proxy environment must be disabled by default")
	}
}

func TestProxyEnvironmentHTTPRequestUsesConfiguredProxy(t *testing.T) {
	proxyHits := make(chan struct{}, 1)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyHits <- struct{}{}
		w.WriteHeader(http.StatusTeapot)
	}))
	defer proxy.Close()
	t.Setenv("HTTP_PROXY", proxy.URL)
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("ALL_PROXY", "")
	t.Setenv("NO_PROXY", "")
	client := &http.Client{Transport: newTransport(Config{TrustProxyEnv: true})}
	request, _ := http.NewRequest(http.MethodGet, "http://public.example/health", nil)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusTeapot {
		t.Fatalf("status=%d", response.StatusCode)
	}
	select {
	case <-proxyHits:
	case <-time.After(time.Second):
		t.Fatal("proxy was not used")
	}
}
