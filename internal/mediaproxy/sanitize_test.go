package mediaproxy

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func TestSanitizeURLRedactsQueryAndUUID(t *testing.T) {
	got := sanitizeURL("https://media.example/path?api_key=hidden")
	if got != "https://media.example/path?[redacted]" {
		t.Fatalf("sanitized=%q", got)
	}
	if sanitizeURL("https://media.example/123e4567-e89b-12d3-a456-426614174000") != "https://media.example/[uuid-redacted]" {
		t.Fatal("uuid was not redacted")
	}
}

func TestSanitizeHeaderValue(t *testing.T) {
	for _, name := range []string{"Authorization", "Cookie", "api_key", "X-Api-Key", "X-Session-Token", "X-Secret"} {
		if sanitizeHeaderValue(name, "sensitive") != "[redacted]" {
			t.Fatalf("header %s was not redacted", name)
		}
	}
}

func TestRequestLoggingOmitsQueryAndSensitiveHeaders(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()
	parsed, _ := url.Parse(upstream.URL)
	port, _ := strconv.Atoi(parsed.Port())
	var entries []string
	executor := NewExecutor(Config{AllowPrivateTargets: true})
	executor.SetLogger(func(event string, fields map[string]any) {
		entries = append(entries, fmt.Sprintf("%s:%v", event, fields))
	})
	request := httptest.NewRequest(http.MethodGet, "/secret-value/123e4567-e89b-12d3-a456-426614174000?token=sensitive-value&api_key=hidden&secret=hidden", nil)
	request.Header.Set("Authorization", "Bearer sensitive-value")
	request.Header.Set("Cookie", "session=sensitive-value")
	request.Header.Set("X-Secret", "hidden")
	executor.ServeHTTP(httptest.NewRecorder(), request, Target{Scheme: "http", Host: parsed.Hostname(), Port: port})
	joined := strings.Join(entries, "\n")
	for _, forbidden := range []string{"sensitive-value", "api_key", "secret=", "Authorization", "Cookie", "session=", "123e4567", "secret-value", parsed.Hostname()} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("log contains sensitive marker %q", forbidden)
		}
	}
}
