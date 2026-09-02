package proxyadapter

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"embyproxy/internal/mediaproxy"
	"embyproxy/internal/requestlog"
	"embyproxy/internal/storage"
)

func newProductionTestRouter(t *testing.T, store *storage.Store, fallback http.Handler) *Router {
	t.Helper()
	config := mediaproxy.Config{AllowPrivateTargets: true}
	return NewProductionRouter(NewStorageResolver(store, "admin"), mediaproxy.NewExecutor(config), config, fallback)
}

func newRouteStore(t *testing.T) *storage.Store {
	t.Helper()
	store, err := storage.New(filepath.Join(t.TempDir(), "proxy.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func seedManagedRoute(t *testing.T, store *storage.Store, slug, target string, enabled, public bool) {
	t.Helper()
	flag := func(value bool) int {
		if value {
			return 1
		}
		return 0
	}
	ctx := context.Background()
	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO managed_routes
			(slug, node_name, enabled, public, default_line, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'main', 1, 1)
	`, slug, slug+"-node", flag(enabled), flag(public)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO managed_route_lines
			(route_slug, line_slug, target, enabled, position)
		VALUES (?, 'main', ?, 1, 1)
	`, slug, target); err != nil {
		t.Fatal(err)
	}
}

func TestProductionSlugRouteUsesManagedTarget(t *testing.T) {
	var hits atomic.Int32
	var forbiddenHits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path != "/base/video" || r.URL.Query().Get("quality") != "original" {
			t.Errorf("unexpected upstream request path=%q", r.URL.Path)
		}
		if r.Header.Get("Range") != "bytes=0-2" || r.Header.Get("If-Range") != "etag-1" {
			t.Errorf("range headers were not preserved")
		}
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Range", "bytes 0-2/3")
		w.Header().Set("Location", "http://"+r.Host+"/base/redirect")
		w.Header().Set("Content-Location", "/base/content")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("abc"))
	}))
	defer upstream.Close()
	forbidden := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forbiddenHits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer forbidden.Close()
	store := newRouteStore(t)
	seedManagedRoute(t, store, "demo", upstream.URL+"/base", true, true)
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/s/demo/video?quality=original&upstream="+url.QueryEscape(forbidden.URL)+"&host=forbidden&scheme=https&port=1", nil)
	req.Header.Set("Range", "bytes=0-2")
	req.Header.Set("If-Range", "etag-1")
	newProductionTestRouter(t, store, http.NotFoundHandler()).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusPartialContent || recorder.Body.String() != "abc" || hits.Load() != 1 || forbiddenHits.Load() != 0 {
		t.Fatalf("status=%d body=%q hits=%d", recorder.Code, recorder.Body.String(), hits.Load())
	}
	if recorder.Header().Get("Location") != "/s/demo/redirect" || recorder.Header().Get("Content-Location") != "/s/demo/content" {
		t.Fatalf("rewritten headers=%v", recorder.Header())
	}
}

func TestProductionNodeRouteAndFallbackBoundaries(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/base/System/Ping" {
			t.Errorf("unexpected node path=%q", r.URL.Path)
		}
		w.Header().Set("Location", "/base/redirect")
		w.WriteHeader(http.StatusFound)
	}))
	defer upstream.Close()
	store := newRouteStore(t)
	if err := store.SaveNode(context.Background(), "admin", storage.Node{Name: "node", Secret: "shared", Target: upstream.URL + "/base"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveNode(context.Background(), "admin", storage.Node{Name: "multi", Target: upstream.URL + ";" + upstream.URL + "/backup"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveNode(context.Background(), "admin", storage.Node{Name: "legacy", Target: upstream.URL + "/base?legacy=1"}); err != nil {
		t.Fatal(err)
	}
	var fallbackHits atomic.Int32
	fallback := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fallbackHits.Add(1)
		w.WriteHeader(http.StatusTeapot)
	})
	router := newProductionTestRouter(t, store, fallback)
	valid := httptest.NewRecorder()
	router.ServeHTTP(valid, httptest.NewRequest(http.MethodGet, "/node/shared/System/Ping", nil))
	if valid.Code != http.StatusFound || valid.Header().Get("Location") != "/node/shared/redirect" {
		t.Fatalf("valid status=%d location=%q", valid.Code, valid.Header().Get("Location"))
	}
	wrong := httptest.NewRecorder()
	router.ServeHTTP(wrong, httptest.NewRequest(http.MethodGet, "/node/wrong/System/Ping", nil))
	if wrong.Code != http.StatusNotFound {
		t.Fatalf("wrong secret status=%d", wrong.Code)
	}
	for _, path := range []string{"/multi/System/Ping", "/legacy/System/Ping", "/missing/System/Ping", "/http/legacy", "/https/legacy", "/api/admin", "/admin/", "/health"} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusTeapot {
			t.Fatalf("fallback path=%q status=%d", path, recorder.Code)
		}
	}
	if fallbackHits.Load() != 8 {
		t.Fatalf("fallback hits=%d", fallbackHits.Load())
	}
}

func TestProductionSlugRequiresEnabledPublicRoute(t *testing.T) {
	store := newRouteStore(t)
	seedManagedRoute(t, store, "disabled", "https://media.example", false, true)
	seedManagedRoute(t, store, "private", "https://media.example", true, false)
	var fallbackHits atomic.Int32
	router := newProductionTestRouter(t, store, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fallbackHits.Add(1)
		w.WriteHeader(http.StatusTeapot)
	}))
	for _, path := range []string{"/s/disabled/item", "/s/private/item"} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("path=%q status=%d", path, recorder.Code)
		}
	}
	unknown := httptest.NewRecorder()
	router.ServeHTTP(unknown, httptest.NewRequest(http.MethodGet, "/s/missing/item", nil))
	if unknown.Code != http.StatusTeapot || fallbackHits.Load() != 1 {
		t.Fatalf("unknown slug status=%d fallbackHits=%d", unknown.Code, fallbackHits.Load())
	}
}

func TestProductionSlugSelectsEligibleProxyNodeAndFailsOver(t *testing.T) {
	var edgeAHits, edgeBHits, originHits atomic.Int32
	newEdge := func(counter *atomic.Int32, marker string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			counter.Add(1)
			if r.URL.Path != "/edge/demo/video" {
				t.Errorf("%s path=%q", marker, r.URL.Path)
			}
			w.Header().Set("Content-Range", "bytes 0-2/3")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write([]byte(marker))
		}))
	}
	edgeA := newEdge(&edgeAHits, "a")
	defer edgeA.Close()
	edgeB := newEdge(&edgeBHits, "b")
	defer edgeB.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		originHits.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("origin"))
	}))
	defer origin.Close()
	store := newRouteStore(t)
	seedManagedRoute(t, store, "demo", origin.URL, true, true)
	now := time.Now().Unix()
	for _, node := range []struct {
		id, name, address string
		priority          int
	}{
		{"node-a", "edge-a", edgeA.URL + "/edge/demo", 1},
		{"node-b", "edge-b", edgeB.URL + "/edge/demo", 2},
	} {
		if _, err := store.DB().Exec(`INSERT INTO proxy_nodes
			(id,name,public_address,enabled,state,priority,quota_bytes,used_bytes,reset_day,reset_timezone,next_reset_at,last_heartbeat_at,playback_healthy,config_synced,agent_version,agent_commit,credential_hash,last_error,created_at,updated_at)
			VALUES (?,?,?,1,'healthy',?,?,0,1,'UTC',0,?,1,1,'v1','test','hash','',?,?)`,
			node.id, node.name, node.address, node.priority, 0, now, now, now); err != nil {
			t.Fatal(err)
		}
	}
	router := NewProductionRouter(NewStorageResolver(store, "admin"), mediaproxy.NewExecutor(mediaproxy.Config{AllowPrivateTargets: true}), mediaproxy.Config{}, http.NotFoundHandler())
	first := httptest.NewRecorder()
	router.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/s/demo/video", nil))
	if first.Code != http.StatusPartialContent || first.Body.String() != "a" || edgeAHits.Load() != 1 || edgeBHits.Load() != 0 || originHits.Load() != 0 {
		t.Fatalf("first status=%d body=%q edgeA=%d edgeB=%d origin=%d", first.Code, first.Body.String(), edgeAHits.Load(), edgeBHits.Load(), originHits.Load())
	}
	if _, err := store.DB().Exec(`UPDATE proxy_nodes SET playback_healthy=0 WHERE id='node-a'`); err != nil {
		t.Fatal(err)
	}
	second := httptest.NewRecorder()
	router.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/s/demo/video", nil))
	if second.Code != http.StatusPartialContent || second.Body.String() != "b" || edgeBHits.Load() != 1 || originHits.Load() != 0 {
		t.Fatalf("second status=%d body=%q edgeA=%d edgeB=%d origin=%d", second.Code, second.Body.String(), edgeAHits.Load(), edgeBHits.Load(), originHits.Load())
	}
}

func TestProductionSlugWebSocketUsesMediaProxy(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		_, _ = fmt.Fprint(conn, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
		_ = conn.Close()
	}))
	defer upstream.Close()
	store := newRouteStore(t)
	seedManagedRoute(t, store, "socket", upstream.URL, true, true)
	server := httptest.NewServer(newProductionTestRouter(t, store, http.NotFoundHandler()))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)
	conn, err := net.Dial("tcp", parsed.Host)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, _ = fmt.Fprintf(conn, "GET /s/socket/connect HTTP/1.1\r\nHost: %s\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: dGVzdA==\r\n\r\n", parsed.Host)
	response, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status=%d", response.StatusCode)
	}
}

func TestStorageResolverRejectsUnsafeManagedTarget(t *testing.T) {
	store := newRouteStore(t)
	seedManagedRoute(t, store, "unsafe", "https://media.example/base%252Fescape", true, true)
	_, _, _, err := NewStorageResolver(store, "admin").slug(context.Background(), "unsafe")
	if err != ErrInvalidTarget {
		t.Fatalf("err=%v", err)
	}
}

func TestProductionLogsDoNotContainRequestSecrets(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()
	store := newRouteStore(t)
	seedManagedRoute(t, store, "demo", upstream.URL, true, true)
	config := mediaproxy.Config{AllowPrivateTargets: true}
	executor := mediaproxy.NewExecutor(config)
	var logged []string
	executor.SetLogger(func(event string, fields map[string]any) {
		logged = append(logged, fmt.Sprintf("%s:%v", event, fields))
	})
	router := NewProductionRouter(NewStorageResolver(store, "admin"), executor, config, http.NotFoundHandler())
	ctx := requestlog.WithAccessLogState(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/s/demo/private-segment?credential=hidden", nil).WithContext(ctx)
	req.Header.Set("Authorization", "redacted")
	req.Header.Set("Cookie", "redacted")
	router.ServeHTTP(httptest.NewRecorder(), req)
	joined := strings.Join(logged, "\n")
	if len(logged) == 0 {
		t.Fatal("expected the shared executor to emit a request log")
	}
	for _, blocked := range []string{"private-segment", "credential", "Authorization", "Cookie", "hidden"} {
		if strings.Contains(joined, blocked) {
			t.Fatalf("log contains blocked value %q", blocked)
		}
	}
	if uri, ok := requestlog.RequestURI(ctx); !ok || uri != "/s/demo/<path>" {
		t.Fatalf("safe request URI=%q ok=%v", uri, ok)
	}
}

func TestProductionNodeAccessLogRedactsSecretAndTail(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()
	store := newRouteStore(t)
	if err := store.SaveNode(context.Background(), "admin", storage.Node{Name: "node", Secret: "shared", Target: upstream.URL}); err != nil {
		t.Fatal(err)
	}
	ctx := requestlog.WithAccessLogState(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/node/shared/private-segment", nil).WithContext(ctx)
	newProductionTestRouter(t, store, http.NotFoundHandler()).ServeHTTP(httptest.NewRecorder(), req)
	if uri, ok := requestlog.RequestURI(ctx); !ok || uri != "/node/<secret>/<path>" {
		t.Fatalf("safe request URI=%q ok=%v", uri, ok)
	}
}
