package proxyadapter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"embyproxy/internal/mediaproxy"
)

func TestLegacyEdgeRestrictsHostsAndPreservesRange(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/media" || r.Header.Get("Range") != "bytes=0-2" {
			t.Errorf("path=%q range=%q", r.URL.Path, r.Header.Get("Range"))
		}
		w.Header().Set("Content-Range", "bytes 0-2/3")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("abc"))
	}))
	defer upstream.Close()
	store := newRouteStore(t)
	seedManagedRoute(t, store, "demo", upstream.URL, true, true)
	config := mediaproxy.Config{AllowPrivateTargets: true, TLSConfig: upstream.Client().Transport.(*http.Transport).TLSClientConfig}
	router := NewEdgeRouter(NewStorageResolver(store, "admin"), mediaproxy.NewExecutor(config), config, http.NotFoundHandler())
	var observed int64
	router.SetEdgeUsageSink(func(bytes int64) { observed += bytes })
	parsed, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/https/"+parsed.Hostname()+"/"+parsed.Port()+"/media", nil)
	request.Header.Set("Range", "bytes=0-2")
	result := httptest.NewRecorder()
	router.ServeHTTP(result, request)
	if result.Code != http.StatusPartialContent || result.Body.String() != "abc" {
		t.Fatalf("response=%d %s", result.Code, result.Body.String())
	}
	if observed != 3 {
		t.Fatalf("Range response bytes counted=%d want=3", observed)
	}
	for _, path := range []string{"/https/unknown.example/443/media", "/http/unknown.example/80/media", "/https/127.0.0.1/80/media", "/https/unknown.example/443/%2e%2e/media", "/https/unknown.example%2ftrusted.example/443/media"} {
		result = httptest.NewRecorder()
		router.ServeHTTP(result, httptest.NewRequest(http.MethodGet, path, nil))
		if result.Code != http.StatusNotFound {
			t.Fatalf("%s returned %d", path, result.Code)
		}
	}
}
func TestLegacyEdgeRejectsNonPublicAndDisabledRoutes(t *testing.T) {
	store := newRouteStore(t)
	seedManagedRoute(t, store, "hidden", "https://hidden.example", true, false)
	seedManagedRoute(t, store, "off", "https://off.example", false, true)
	config := mediaproxy.Config{}
	router := NewEdgeRouter(NewStorageResolver(store, "admin"), mediaproxy.NewExecutor(config), config, http.NotFoundHandler())
	for _, path := range []string{"/https/hidden.example/443/System/Info/Public", "/https/off.example/443/System/Info/Public"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s returned %d", path, rec.Code)
		}
	}
}

func TestLegacyEdgeBasePathAndRedirect(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/base/video" {
			t.Errorf("upstream path %q", r.URL.Path)
		}
		w.Header().Set("Location", "/base/next")
		w.WriteHeader(http.StatusFound)
	}))
	defer upstream.Close()
	parsed, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	store := newRouteStore(t)
	seedManagedRoute(t, store, "base", upstream.URL+"/base", true, true)
	config := mediaproxy.Config{AllowPrivateTargets: true, TLSConfig: upstream.Client().Transport.(*http.Transport).TLSClientConfig}
	router := NewEdgeRouter(NewStorageResolver(store, "admin"), mediaproxy.NewExecutor(config), config, http.NotFoundHandler())
	path := "/https/" + parsed.Hostname() + "/" + parsed.Port() + "/base/video"
	result := httptest.NewRecorder()
	router.ServeHTTP(result, httptest.NewRequest(http.MethodGet, path, nil))
	if result.Code != http.StatusFound || result.Header().Get("Location") != "/https/"+parsed.Hostname()+"/"+parsed.Port()+"/next" {
		t.Fatalf("status=%d location=%q", result.Code, result.Header().Get("Location"))
	}
}

func TestConfiguredLegacyTargetIsScoped(t *testing.T) {
	for _, tc := range []struct {
		path, host string
		port       int
		allowed    bool
	}{
		{"/https/media.example/443", "media.example", 443, true},
		{"/https/media.example/443/extra", "media.example", 443, false},
		{"/https/127.0.0.1/443", "127.0.0.1", 443, false},
		{"/https/media.example/443", "other.example", 443, false},
	} {
		if got := configuredLegacyTarget(map[string]string{"uhd": tc.path}, tc.host, tc.port); got != tc.allowed {
			t.Fatalf("path=%q host=%q got=%v", tc.path, tc.host, got)
		}
	}
}

func TestLegacyEdgeSnapshotMappingRevocation(t *testing.T) {
	store := newRouteStore(t)
	ctx := context.Background()
	if err := store.KV().Put(ctx, "edge:legacy-public-paths", map[string]string{"uhd": "/https/media.example/443"}); err != nil {
		t.Fatal(err)
	}
	config := mediaproxy.Config{}
	router := NewEdgeRouter(NewStorageResolver(store, "admin"), mediaproxy.NewExecutor(config), config, http.NotFoundHandler())
	path := "/https/media.example/443/System/Info/Public"
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code == http.StatusNotFound {
		t.Fatal("configured path was not routed")
	}
	if err := store.KV().Put(ctx, "edge:legacy-public-paths", map[string]string{}); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("removed mapping status %d", rec.Code)
	}
}
