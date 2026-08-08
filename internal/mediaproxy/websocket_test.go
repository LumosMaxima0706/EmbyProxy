package mediaproxy

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestServeHTTPWebSocketUpgradeAndTunnel(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Error("upstream does not support hijacking")
			return
		}
		conn, reader, err := hijacker.Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		_, _ = fmt.Fprintf(conn, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
		if reader.Reader.Buffered() > 0 {
			_, _ = reader.Reader.Discard(reader.Reader.Buffered())
		}
		buf := make([]byte, 4)
		if _, err := io.ReadFull(conn, buf); err == nil {
			_, _ = conn.Write(buf)
		}
	}))
	defer upstream.Close()
	parsed, _ := url.Parse(upstream.URL)
	port, _ := strconv.Atoi(parsed.Port())
	executor := NewExecutor(Config{AllowPrivateTargets: true})
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		executor.ServeHTTP(w, r, Target{Scheme: "http", Host: parsed.Hostname(), Port: port})
	}))
	defer proxy.Close()
	proxyURL, _ := url.Parse(proxy.URL)
	conn, err := net.DialTimeout("tcp", proxyURL.Host, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, _ = fmt.Fprintf(conn, "GET /socket?x=1 HTTP/1.1\r\nHost: %s\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: dGVzdA==\r\n\r\n", proxyURL.Host)
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, nil)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status=%d", response.StatusCode)
	}
	_, _ = conn.Write([]byte("ping"))
	buf := make([]byte, 4)
	if _, err := io.ReadFull(reader, buf); err != nil || string(buf) != "ping" {
		t.Fatalf("tunnel response=%q err=%v", buf, err)
	}
	if !strings.EqualFold(response.Header.Get("Upgrade"), "websocket") {
		t.Fatalf("headers=%v", response.Header)
	}
}

func TestWebSocketTrustProxyEnvControlsHTTPProxy(t *testing.T) {
	var proxyHits atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyHits.Add(1)
		hijacker, _ := w.(http.Hijacker)
		conn, _, err := hijacker.Hijack()
		if err != nil {
			return
		}
		_, _ = fmt.Fprint(conn, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
		_ = conn.Close()
	}))
	defer proxy.Close()
	t.Setenv("HTTP_PROXY", proxy.URL)
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("ALL_PROXY", "")
	t.Setenv("NO_PROXY", "")

	route := func(executor *Executor) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			executor.ServeHTTP(w, r, Target{Scheme: "http", Host: "127.0.0.1", Port: 1})
		}))
	}
	proxyRoute := route(NewExecutor(Config{AllowPrivateTargets: true, TrustProxyEnv: true}))
	defer proxyRoute.Close()
	conn, response := rawWebSocketRequest(t, proxyRoute.URL)
	_ = conn.Close()
	if response.StatusCode != http.StatusSwitchingProtocols || proxyHits.Load() != 1 {
		t.Fatalf("proxy mode status=%d hits=%d", response.StatusCode, proxyHits.Load())
	}

	proxyHits.Store(0)
	directUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hijacker, _ := w.(http.Hijacker)
		conn, _, err := hijacker.Hijack()
		if err != nil {
			return
		}
		_, _ = fmt.Fprint(conn, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
		_ = conn.Close()
	}))
	defer directUpstream.Close()
	parsed, _ := url.Parse(directUpstream.URL)
	port, _ := strconv.Atoi(parsed.Port())
	directRoute := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		NewExecutor(Config{AllowPrivateTargets: true, TrustProxyEnv: false}).ServeHTTP(w, r, Target{Scheme: "http", Host: parsed.Hostname(), Port: port})
	}))
	defer directRoute.Close()
	conn, response = rawWebSocketRequest(t, directRoute.URL)
	_ = conn.Close()
	if response.StatusCode != http.StatusSwitchingProtocols || proxyHits.Load() != 0 {
		t.Fatalf("direct mode status=%d hits=%d", response.StatusCode, proxyHits.Load())
	}
}

func TestWebSocketPrivateTargetCannotBypassBlockThroughProxy(t *testing.T) {
	var proxyHits atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyHits.Add(1)
	}))
	defer proxy.Close()
	t.Setenv("HTTP_PROXY", proxy.URL)
	t.Setenv("NO_PROXY", "")
	executor := NewExecutor(Config{TrustProxyEnv: true})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/socket", nil)
	request.Header.Set("Connection", "Upgrade")
	request.Header.Set("Upgrade", "websocket")
	executor.ServeHTTP(recorder, request, Target{Scheme: "http", Host: "127.0.0.1", Port: 1})
	if recorder.Code != http.StatusBadRequest || proxyHits.Load() != 0 {
		t.Fatalf("status=%d proxy_hits=%d", recorder.Code, proxyHits.Load())
	}
}

func TestWebSocketHTTPSProxyAndNoProxySelection(t *testing.T) {
	t.Setenv("HTTP_PROXY", "")
	t.Setenv("HTTPS_PROXY", "http://secure-proxy.example:8443")
	t.Setenv("ALL_PROXY", "")
	t.Setenv("NO_PROXY", "")
	executor := NewExecutor(Config{TrustProxyEnv: true})
	got, err := executor.proxyURL(Target{Scheme: "https", Host: "media.example", Port: 443})
	if err != nil || got == nil || got.Host != "secure-proxy.example:8443" {
		t.Fatalf("HTTPS proxy selection failed")
	}
	t.Setenv("NO_PROXY", "media.example")
	got, err = executor.proxyURL(Target{Scheme: "https", Host: "media.example", Port: 443})
	if err != nil || got != nil {
		t.Fatalf("NO_PROXY bypass failed")
	}
}

func rawWebSocketRequest(t *testing.T, rawURL string) (net.Conn, *http.Response) {
	t.Helper()
	parsed, _ := url.Parse(rawURL)
	conn, err := net.DialTimeout("tcp", parsed.Host, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fmt.Fprintf(conn, "GET /socket HTTP/1.1\r\nHost: %s\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: dGVzdA==\r\n\r\n", parsed.Host)
	response, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		_ = conn.Close()
		t.Fatal(err)
	}
	return conn, response
}
