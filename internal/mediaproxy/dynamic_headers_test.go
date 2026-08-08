package mediaproxy

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
)

func TestDynamicHopByHopHeadersAreRemovedBothDirections(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Foo"); got != "" {
			t.Errorf("dynamic request header forwarded: %q", got)
		}
		w.Header().Set("Connection", "X-Bar")
		w.Header().Set("X-Bar", "must-not-leak")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()
	parsed, _ := url.Parse(upstream.URL)
	port, _ := strconv.Atoi(parsed.Port())
	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	request.Header.Set("Connection", "X-Foo, keep-alive")
	request.Header.Set("X-Foo", "must-not-forward")
	recorder := httptest.NewRecorder()
	NewExecutor(Config{AllowPrivateTargets: true}).ServeHTTP(recorder, request, Target{Scheme: "http", Host: parsed.Hostname(), Port: port})
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status=%d", recorder.Code)
	}
	if recorder.Header().Get("X-Bar") != "" || recorder.Header().Get("Connection") != "" {
		t.Fatalf("dynamic response headers leaked: %v", recorder.Header())
	}
}
