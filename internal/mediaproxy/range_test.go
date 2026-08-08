package mediaproxy

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
)

func TestServeHTTPPreservesRangeResponse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "bytes=0-2" || r.Header.Get("If-Range") != "etag-1" {
			t.Errorf("range headers missing: %v", r.Header)
		}
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Range", "bytes 0-2/6")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("abc"))
	}))
	defer upstream.Close()
	urlValue := upstream.URL
	parsed, _ := url.Parse(urlValue)
	port, _ := strconv.Atoi(parsed.Port())
	target := Target{Scheme: "http", Host: parsed.Hostname(), Port: port}
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/video.mkv", nil)
	req.Header.Set("Range", "bytes=0-2")
	req.Header.Set("If-Range", "etag-1")
	NewExecutor(Config{AllowPrivateTargets: true}).ServeHTTP(recorder, req, target)
	if recorder.Code != http.StatusPartialContent || recorder.Header().Get("Accept-Ranges") != "bytes" || recorder.Header().Get("Content-Range") != "bytes 0-2/6" || recorder.Body.String() != "abc" {
		t.Fatalf("response=%d headers=%v body=%q", recorder.Code, recorder.Header(), recorder.Body.String())
	}
}

func TestServeHTTPForwardsUpstreamErrorStatuses(t *testing.T) {
	for _, status := range []int{http.StatusBadGateway, http.StatusGatewayTimeout} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
			}))
			defer upstream.Close()
			parsed, _ := url.Parse(upstream.URL)
			port, _ := strconv.Atoi(parsed.Port())
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/health", nil)
			NewExecutor(Config{AllowPrivateTargets: true}).ServeHTTP(recorder, req, Target{Scheme: "http", Host: parsed.Hostname(), Port: port})
			if recorder.Code != status {
				t.Fatalf("status=%d want=%d", recorder.Code, status)
			}
		})
	}
}
