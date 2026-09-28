package proxyadapter

import (
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
