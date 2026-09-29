package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"embyproxy/internal/config"
	"embyproxy/internal/spaceship"
)

func TestIngressReadbackWorksWithWriterDisabled(t *testing.T) {
	h := newAuthTestHandler(t, config.Config{AdminToken: "strong-admin-token", PublicIngressHost: "stream.example.com"})
	puts := 0
	includeIPv6 := false
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			puts++
			w.WriteHeader(http.StatusNoContent)
			return
		}
		items := []spaceship.Record{{Name: "stream", Type: "A", Address: "1.1.1.1", TTL: 60}}
		if includeIPv6 {
			items = append(items, spaceship.Record{Name: "stream", Type: "AAAA", Address: "2001:db8::1", TTL: 60})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"items": items, "total": len(items)})
	}))
	defer provider.Close()
	h.dnsAutomation = &spaceship.Client{BaseURL: provider.URL, APIKey: "k", APISecret: "s", ManagedDomain: "example.com"}
	h.ingressReadback = &ingressReadback{provider: h.dnsAutomation, record: "stream.example.com", lookup: func(_ context.Context, _ string) ([]string, error) { return []string{"1.1.1.1", "9.9.9.9"}, nil }}
	public := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-EmbyProxy-Node-ID", "edge-a")
		_, _ = w.Write([]byte("ok"))
	}))
	defer public.Close()
	endpoint, err := url.Parse(public.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := public.Client()
	transport := client.Transport.(*http.Transport).Clone()
	transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, endpoint.Host)
	}
	client.Transport = transport
	h.ingressReadback.client = client
	path := "/api/admin/public-ingress/observe"
	if rec := serveAdminJSON(t, h, http.MethodGet, path, nil, nil); rec.Code == http.StatusOK {
		t.Fatal("unauthenticated observation accepted")
	}
	login := serveAdminJSON(t, h, http.MethodPost, "/admin/auth/login", map[string]any{"token": "strong-admin-token"}, nil)
	if login.Code != http.StatusOK || len(login.Result().Cookies()) == 0 {
		t.Fatalf("login=%d", login.Code)
	}
	cookie := login.Result().Cookies()[0]
	response := serveAdminJSON(t, h, http.MethodGet, path, nil, cookie)
	if response.Code != http.StatusOK {
		t.Fatalf("observe=%d %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"address":"1.1.1.1"`) {
		t.Fatalf("provider missing: %s", response.Body.String())
	}
	for _, marker := range []string{`"ttl":60`, `"recursive":["1.1.1.1","9.9.9.9"]`, `"public_node_id":"edge-a"`, `"recursive_provider_mismatch"`} {
		if !strings.Contains(response.Body.String(), marker) {
			t.Fatalf("missing %s: %s", marker, response.Body.String())
		}
	}
	if puts != 0 {
		t.Fatalf("readback wrote provider DNS: %d", puts)
	}
	if h.publicIngress != nil {
		t.Fatal("writer unexpectedly enabled")
	}
	includeIPv6 = true
	v6 := serveAdminJSON(t, h, http.MethodGet, path, nil, cookie)
	for _, marker := range []string{`"ipv6":["2001:db8::1"]`, `"address":"1.1.1.1"`} {
		if !strings.Contains(v6.Body.String(), marker) {
			t.Fatalf("IPv6/A not separated: %s", v6.Body.String())
		}
	}
	if puts != 0 {
		t.Fatalf("IPv6 readback wrote provider: %d", puts)
	}
}

type failedObservationTransport struct{}

func (failedObservationTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("private diagnostic not for API clients")
}

func TestIngressReadbackReportsIndependentFailures(t *testing.T) {
	requests := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer provider.Close()
	o := ingressReadback{
		provider: &spaceship.Client{BaseURL: provider.URL, APIKey: "k", APISecret: "s", ManagedDomain: "example.com"},
		record:   "stream.example.com",
		lookup: func(context.Context, string) ([]string, error) {
			return []string{"1.1.1.1"}, nil
		},
		client: &http.Client{Transport: failedObservationTransport{}},
	}
	got := o.read(context.Background())
	if requests != 1 || len(got.Recursive) != 1 || got.Recursive[0] != "1.1.1.1" {
		t.Fatalf("provider failure prevented recursive observation: %+v requests=%d", got, requests)
	}
	for _, code := range []string{"provider_read_failed", "public_request_failed"} {
		if !strings.Contains(strings.Join(got.Errors, ","), code) {
			t.Fatalf("missing %s: %+v", code, got)
		}
	}
	if strings.Contains(strings.Join(got.Errors, ","), "private diagnostic") {
		t.Fatalf("raw diagnostic escaped: %+v", got)
	}
	if got.PublicStatus != 0 || got.PublicNodeID != "" || got.ObservedAt == 0 {
		t.Fatalf("failure looked verified: %+v", got)
	}
}
