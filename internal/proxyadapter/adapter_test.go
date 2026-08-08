package proxyadapter

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"embyproxy/internal/mediaproxy"
	"embyproxy/internal/storage"
)

func newRegistry(t *testing.T, targetURL string) *Registry {
	t.Helper()
	registry, err := NewRegistry(
		[]SlugConfig{{Slug: "demo", RawTarget: targetURL + "/base", Enabled: true, PublicPath: "/s/demo/"}},
		[]storage.Node{{Name: "node", Secret: "shared", Target: targetURL + "/nodebase"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func newRouter(t *testing.T, registry *Registry) *Router {
	t.Helper()
	config := mediaproxy.Config{AllowPrivateTargets: true}
	return NewRouter(DefaultMockPrefix, registry, mediaproxy.NewExecutor(config), config)
}

func TestSlugForwardsBasePathQueryRangeAndRewritesLocations(t *testing.T) {
	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path != "/base/Videos/1" || r.URL.RawQuery != "quality=original&token=dummy" {
			t.Errorf("upstream path=%q query=%q", r.URL.Path, r.URL.RawQuery)
		}
		if r.Header.Get("Range") != "bytes=0-2" || r.Header.Get("If-Range") != "etag-1" {
			t.Errorf("range headers=%v", r.Header)
		}
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Range", "bytes 0-2/6")
		w.Header().Set("Location", "http://"+r.Host+"/base/redirect")
		w.Header().Set("Content-Location", "/base/content")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("abc"))
	}))
	defer upstream.Close()
	router := newRouter(t, newRegistry(t, upstream.URL))
	req := httptest.NewRequest(http.MethodGet, DefaultMockPrefix+"/s/demo/Videos/1?quality=original&token=dummy", nil)
	req.Header.Set("Range", "bytes=0-2")
	req.Header.Set("If-Range", "etag-1")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusPartialContent || recorder.Body.String() != "abc" || hits.Load() != 1 {
		t.Fatalf("status=%d body=%q hits=%d", recorder.Code, recorder.Body.String(), hits.Load())
	}
	if recorder.Header().Get("Accept-Ranges") != "bytes" || recorder.Header().Get("Content-Range") != "bytes 0-2/6" {
		t.Fatalf("range response headers=%v", recorder.Header())
	}
	if recorder.Header().Get("Location") != "/s/demo/redirect" || recorder.Header().Get("Content-Location") != "/s/demo/content" {
		t.Fatalf("rewritten headers=%v", recorder.Header())
	}
}

func TestSlugUnknownAndQueryCannotSelectUpstream(t *testing.T) {
	var allowedHits atomic.Int32
	var forbiddenHits atomic.Int32
	allowed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		allowedHits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer allowed.Close()
	forbidden := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forbiddenHits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer forbidden.Close()
	router := newRouter(t, newRegistry(t, allowed.URL))
	unknown := httptest.NewRecorder()
	router.ServeHTTP(unknown, httptest.NewRequest(http.MethodGet, DefaultMockPrefix+"/s/missing/health", nil))
	if unknown.Code != http.StatusNotFound {
		t.Fatalf("unknown status=%d", unknown.Code)
	}
	query := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, DefaultMockPrefix+"/s/demo/health?upstream="+url.QueryEscape(forbidden.URL), nil)
	router.ServeHTTP(query, req)
	if query.Code != http.StatusNoContent || allowedHits.Load() != 1 || forbiddenHits.Load() != 0 {
		t.Fatalf("query override status=%d allowed=%d forbidden=%d", query.Code, allowedHits.Load(), forbiddenHits.Load())
	}
}

func TestNodeSecretAndQueryIsolation(t *testing.T) {
	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path != "/nodebase/emby/System/Ping" {
			t.Errorf("node path=%q", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()
	router := newRouter(t, newRegistry(t, upstream.URL))
	missing := httptest.NewRecorder()
	router.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, DefaultMockPrefix+"/node/emby/System/Ping", nil))
	wrong := httptest.NewRecorder()
	router.ServeHTTP(wrong, httptest.NewRequest(http.MethodGet, DefaultMockPrefix+"/node/wrong/emby/System/Ping", nil))
	if missing.Code != http.StatusNotFound || wrong.Code != http.StatusNotFound || hits.Load() != 0 {
		t.Fatalf("secret checks missing=%d wrong=%d hits=%d", missing.Code, wrong.Code, hits.Load())
	}
	valid := httptest.NewRecorder()
	router.ServeHTTP(valid, httptest.NewRequest(http.MethodGet, DefaultMockPrefix+"/node/shared/emby/System/Ping?upstream=other", nil))
	if valid.Code != http.StatusNoContent || hits.Load() != 1 {
		t.Fatalf("valid status=%d hits=%d", valid.Code, hits.Load())
	}
}

func TestRegistryRejectsUnsafeSlugAndTarget(t *testing.T) {
	for _, slug := range []string{"admin", "api", "health", "http", "https", "s", "BadSlug", "bad_slug", "bad/slug"} {
		if _, err := NewRegistry([]SlugConfig{{Slug: slug, RawTarget: "http://media.example", Enabled: true}}, nil); err == nil {
			t.Fatalf("slug %q accepted", slug)
		}
	}
	for _, target := range []string{
		"http://user:pass@media.example",
		"http://media.example/path?token=dummy",
		"http://media.example/path#fragment",
		"ftp://media.example",
	} {
		if _, err := NewRegistry([]SlugConfig{{Slug: "demo", RawTarget: target, Enabled: true}}, nil); err != ErrInvalidTarget {
			t.Fatalf("target %q err=%v", target, err)
		}
	}
}

func TestAdapterDoesNotHandleAdminOrUnmatchedRoutes(t *testing.T) {
	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()
	router := newRouter(t, newRegistry(t, upstream.URL))
	for _, path := range []string{DefaultMockPrefix + "/admin/", DefaultMockPrefix + "/api/admin", "/admin/", "/api/admin/", "/other/path"} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("path=%q status=%d", path, recorder.Code)
		}
	}
	if hits.Load() != 0 {
		t.Fatalf("unmatched routes reached upstream: %d", hits.Load())
	}
}

func TestAdapterWebSocketDelegatesToMediaproxy(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			return
		}
		conn, _, err := hijacker.Hijack()
		if err != nil {
			return
		}
		_, _ = fmt.Fprint(conn, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
		_ = conn.Close()
	}))
	defer upstream.Close()
	server := httptest.NewServer(newRouter(t, newRegistry(t, upstream.URL)))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)
	conn, err := net.Dial("tcp", parsed.Host)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, _ = fmt.Fprintf(conn, "GET %s/s/demo/socket HTTP/1.1\r\nHost: %s\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: dGVzdA==\r\n\r\n", DefaultMockPrefix, parsed.Host)
	response, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status=%d", response.StatusCode)
	}
}

func TestAdapterLoggingDoesNotExposeRequestSecrets(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()
	executor := mediaproxy.NewExecutor(mediaproxy.Config{AllowPrivateTargets: true})
	var logged []string
	executor.SetLogger(func(event string, fields map[string]any) {
		logged = append(logged, fmt.Sprintf("%s:%v", event, fields))
	})
	router := NewRouter(DefaultMockPrefix, newRegistry(t, upstream.URL), executor, mediaproxy.Config{AllowPrivateTargets: true})
	req := httptest.NewRequest(http.MethodGet, DefaultMockPrefix+"/s/demo/token-value/123e4567-e89b-12d3-a456-426614174000?token=secret&api_key=hidden", nil)
	req.Header.Set("Authorization", "Bearer hidden")
	req.Header.Set("Cookie", "session=hidden")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	joined := strings.Join(logged, "\n")
	for _, value := range []string{"token-value", "123e4567", "token=", "api_key", "Authorization", "Cookie", "session="} {
		if strings.Contains(joined, value) {
			t.Fatalf("log contains %q", value)
		}
	}
}

func TestAdapterUsesServerTargetOnly(t *testing.T) {
	parsed, _ := url.Parse("https://media.example:443/emby")
	if parsed.Hostname() == "" {
		t.Fatal("test target unavailable")
	}
	if _, err := parseServerTarget(parsed.String()); err != nil {
		t.Fatal(err)
	}
}
