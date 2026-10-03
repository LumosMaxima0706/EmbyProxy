package admin

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"embyproxy/internal/config"
)

func TestIngressObservationDoesNotReusePreviousNodeConnection(t *testing.T) {
	newNode := func(id string) *httptest.Server {
		s := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-EmbyProxy-Node-ID", id)
			_, _ = w.Write([]byte("ok"))
		}))
		s.EnableHTTP2 = true
		s.StartTLS()
		return s
	}
	old := newNode("old-node")
	defer old.Close()
	target := newNode("target-node")
	defer target.Close()
	var address atomic.Value
	address.Store(old.Listener.Addr().String())
	client := old.Client()
	transport := client.Transport.(*http.Transport).Clone()
	transport.ForceAttemptHTTP2 = true
	transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, address.Load().(string))
	}
	client.Transport = transport
	defer transport.CloseIdleConnections()
	o := ingressReadback{record: "example.com", client: client, lookup: func(context.Context, string) ([]string, error) { return []string{"1.1.1.1"}, nil }}
	if got := o.read(context.Background()); got.PublicNodeID != "old-node" {
		t.Fatalf("warmup: %+v", got)
	}
	address.Store(target.Listener.Addr().String())
	if got := o.read(context.Background()); got.PublicNodeID != "target-node" {
		t.Fatalf("observation reused previous node connection: %+v", got)
	}
}

func TestVerifyPublicRequestDoesNotInheritWarmHTTP2Pool(t *testing.T) {
	newNode := func(id string) *httptest.Server {
		s := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-EmbyProxy-Node-ID", id)
			_, _ = w.Write([]byte("ok"))
		}))
		s.EnableHTTP2 = true
		s.StartTLS()
		return s
	}
	old := newNode("old-node")
	defer old.Close()
	target := newNode("target-node")
	defer target.Close()
	var address atomic.Value
	address.Store(old.Listener.Addr().String())
	client := old.Client()
	transport := client.Transport.(*http.Transport).Clone()
	transport.ForceAttemptHTTP2 = true
	transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, address.Load().(string))
	}
	client.Transport = transport
	defer transport.CloseIdleConnections()
	resp, err := client.Get("https://example.com/health")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.ProtoMajor != 2 {
		t.Fatalf("warmup did not use HTTP/2: %s", resp.Proto)
	}
	address.Store(target.Listener.Addr().String())
	s := &publicIngressSwitcher{record: "example.com", httpClient: client}
	observed, err := s.verifyPublicRequest(context.Background(), "target-node")
	if err != nil || observed != "target-node" {
		t.Fatalf("fresh verification reused old HTTP/2 pool: observed=%q err=%v", observed, err)
	}
}

func TestFreshPublicRequestClientUsesConstructorAfterDefaultHTTP2Warmup(t *testing.T) {
	original := http.DefaultTransport
	defer func() { http.DefaultTransport = original }()
	warm := &http.Transport{ForceAttemptHTTP2: true}
	_ = warm.Clone()
	http.DefaultTransport = warm
	s := newPublicIngressSwitcher(&Handler{cfg: config.Config{PublicIngressHost: "stream.example.com"}})
	s.lookupHost = func(context.Context, string) ([]string, error) { return []string{"1.1.1.1"}, nil }
	client, closeIdle, err := s.freshPublicRequestClient(context.Background())
	defer closeIdle()
	if err != nil {
		t.Fatal(err)
	}
	transport := client.Transport.(*http.Transport)
	if transport.TLSNextProto["h2"] != nil {
		t.Fatal("verification inherited default HTTP/2 connection pool")
	}
}

func TestWaitPublicRequestRetriesTransientIdentityMismatch(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := "target-node"
		if calls.Add(1) == 1 {
			id = "old-node"
		}
		w.Header().Set("X-EmbyProxy-Node-ID", id)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()
	client := server.Client()
	transport := client.Transport.(*http.Transport).Clone()
	transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	client.Transport = transport
	s := &publicIngressSwitcher{record: "example.com", httpClient: client}
	got, err := s.waitPublicRequest(context.Background(), "target-node", 5*time.Second)
	if err != nil || got != "target-node" || calls.Load() != 2 {
		t.Fatalf("got=%q err=%v calls=%d", got, err, calls.Load())
	}
}

func TestWaitPublicRequestDoesNotAcceptWrongNodeAtDeadline(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-EmbyProxy-Node-ID", "old-node")
	}))
	defer server.Close()
	client := server.Client()
	transport := client.Transport.(*http.Transport).Clone()
	transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	client.Transport = transport
	s := &publicIngressSwitcher{record: "example.com", httpClient: client}
	got, err := s.waitPublicRequest(context.Background(), "target-node", 50*time.Millisecond)
	if got != "old-node" || err == nil || !strings.Contains(err.Error(), "identity_mismatch") {
		t.Fatalf("wrong node accepted: got=%q err=%v", got, err)
	}
}
