package proxyadapter

import (
	"bufio"
	"crypto/tls"
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
	req := httptest.NewRequest(http.MethodGet, DefaultMockPrefix+"/s/demo/health?upstream="+url.QueryEscape(forbidden.URL)+"&url="+url.QueryEscape(forbidden.URL)+"&target="+url.QueryEscape(forbidden.URL)+"&host=forbidden&scheme=https&port=1", nil)
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
		"http://media.example:",
		"http://media.example/path%2Fsegment",
		"http://media.example/path%2fsegment",
		"http://media.example/path%5Csegment",
		"http://media.example/path%5csegment",
		"http://media.example/path%2Esegment",
		"http://media.example/path%2esegment",
		"http://media.example/path/../segment",
		"http://media.example/path/..%2Fsegment",
		"http://media.example/path/%2e%2e%2fsegment",
		"http://media.example/path%252Fsegment",
		"http://media.example/path%252Esegment",
		"http://media.example/path/%zz",
	} {
		if _, err := NewRegistry([]SlugConfig{{Slug: "demo", RawTarget: target, Enabled: true}}, nil); err != ErrInvalidTarget {
			t.Fatalf("target %q err=%v", target, err)
		}
		if _, err := NewRegistry(nil, []storage.Node{{Name: "node", Target: target}}); err != ErrInvalidTarget {
			t.Fatalf("node target %q err=%v", target, err)
		}
	}
	for _, publicPath := range []string{"/s/demo/%2F", "/s/demo/%2E", "/s/demo/..%2F"} {
		if _, err := NewRegistry([]SlugConfig{{Slug: "demo", RawTarget: "http://media.example", Enabled: true, PublicPath: publicPath}}, nil); err != ErrInvalidSlug {
			t.Fatalf("public path %q err=%v", publicPath, err)
		}
	}
}

func TestAdapterRejectsEncodedRouteSeparatorsAndTraversal(t *testing.T) {
	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()
	router := newRouter(t, newRegistry(t, upstream.URL))
	for _, escaped := range []string{
		"%2F", "%2f", "%5C", "%5c", "%2E", "%2e",
		"%2e%2e/", "..%2F", "%2e%2e%2f",
	} {
		for _, route := range []string{
			DefaultMockPrefix + "/s/demo/" + escaped + "tail",
			DefaultMockPrefix + "/node/shared/" + escaped + "tail",
		} {
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, route, nil))
			if recorder.Code != http.StatusNotFound {
				t.Fatalf("route=%q status=%d", route, recorder.Code)
			}
		}
	}
	for _, segment := range []string{".", ".."} {
		for _, route := range []string{
			DefaultMockPrefix + "/s/demo/" + segment + "/tail",
			DefaultMockPrefix + "/node/shared/" + segment + "/tail",
		} {
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, route, nil))
			if recorder.Code != http.StatusNotFound {
				t.Fatalf("dot traversal route=%q status=%d", route, recorder.Code)
			}
		}
	}
	for _, route := range []string{
		DefaultMockPrefix + "/s/de%2Fmo/path",
		DefaultMockPrefix + "/no%2Fde/shared/path",
	} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, route, nil))
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("encoded boundary route=%q status=%d", route, recorder.Code)
		}
	}
	if hits.Load() != 0 {
		t.Fatalf("unsafe routes reached upstream: %d", hits.Load())
	}
	for _, route := range []string{
		DefaultMockPrefix + "/s/demo/normal/path",
		DefaultMockPrefix + "/node/shared/normal/path",
	} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, route, nil))
		if recorder.Code != http.StatusNoContent {
			t.Fatalf("normal route=%q status=%d", route, recorder.Code)
		}
	}
}

func TestSlugHTTPSForwarding(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/secure/Ping" {
			t.Errorf("path=%q", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()
	parsed, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	config := mediaproxy.Config{
		AllowPrivateTargets: true,
		TLSConfig:           &tls.Config{InsecureSkipVerify: true}, // test-only local TLS server
	}
	registry, err := NewRegistry([]SlugConfig{{Slug: "secure", RawTarget: upstream.URL + "/secure", Enabled: true}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	NewRouter(DefaultMockPrefix, registry, mediaproxy.NewExecutor(config), config).ServeHTTP(
		recorder,
		httptest.NewRequest(http.MethodGet, DefaultMockPrefix+"/s/secure/Ping", nil),
	)
	if recorder.Code != http.StatusNoContent || parsed.Scheme != "https" {
		t.Fatalf("status=%d scheme=%q", recorder.Code, parsed.Scheme)
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
