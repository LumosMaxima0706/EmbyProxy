package admin

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRecursiveAnswersRequireOnlyTargetIPv4(t *testing.T) {
	for _, tc := range []struct {
		name string
		ips  []string
		want bool
	}{
		{"one", []string{"1.1.1.1"}, true},
		{"duplicates", []string{"1.1.1.1", "1.1.1.1"}, true},
		{"mixed", []string{"1.1.1.1", "2.2.2.2"}, false},
		{"ipv6", []string{"1.1.1.1", "2001:db8::1"}, false},
		{"invalid", []string{"1.1.1.1", "invalid"}, false},
		{"empty", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := recursiveAnswersMatch(tc.ips, "1.1.1.1"); got != tc.want {
				t.Fatalf("answers=%v matched=%t want=%t", tc.ips, got, tc.want)
			}
		})
	}
}

func TestWaitRecursiveRejectsMixedAnswers(t *testing.T) {
	s := newPublicIngressSwitcher(&Handler{})
	s.record = "stream.example.com"
	s.now = func() time.Time { return time.Now() }
	s.lookupHost = func(context.Context, string) ([]string, error) {
		return []string{"1.1.1.1", "2.2.2.2"}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.waitRecursive(ctx, "1.1.1.1", time.Minute); !errors.Is(err, context.Canceled) {
		t.Fatalf("mixed results were accepted: %v", err)
	}
	s.lookupHost = func(context.Context, string) ([]string, error) { return []string{"1.1.1.1"}, nil }
	if got, err := s.waitRecursive(context.Background(), "1.1.1.1", time.Second); err != nil || got != "1.1.1.1" {
		t.Fatalf("matching results rejected: %s %v", got, err)
	}
}

func TestVerifyPublicRequestAlwaysUsesFreshConnection(t *testing.T) {
	var connections atomic.Int64
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !r.Close {
			t.Error("public verification request did not disable connection reuse")
		}
		w.Header().Set("X-EmbyProxy-Node-ID", "node-a")
		_, _ = w.Write([]byte("ok"))
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}
	server.StartTLS()
	defer server.Close()
	client := server.Client()
	transport := client.Transport.(*http.Transport).Clone()
	address := server.Listener.Addr().String()
	transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}
	client.Transport = transport
	switcher := &publicIngressSwitcher{record: "stream.example.com", httpClient: client}
	for i := 0; i < 2; i++ {
		observed, err := switcher.verifyPublicRequest(context.Background(), "node-a")
		if err != nil || observed != "node-a" {
			t.Fatalf("verification %d observed=%q err=%v", i, observed, err)
		}
	}
	if got := connections.Load(); got < 2 {
		t.Fatalf("connections=%d, want at least 2", got)
	}
}

func TestLookupPublicARequiresResolverConsensus(t *testing.T) {
	answer := "1.1.1.1"
	resolver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"Status":0,"Answer":[{"type":1,"data":"` + answer + `"}]}`))
	}))
	defer resolver.Close()
	got, err := lookupPublicAEndpoints(context.Background(), []string{resolver.URL + "/one", resolver.URL + "/two"})
	if err != nil || len(got) != 1 || got[0] != "1.1.1.1" {
		t.Fatalf("answers=%v err=%v", got, err)
	}
	calls := 0
	resolver.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		value := "1.1.1.1"
		if calls == 2 {
			value = "2.2.2.2"
		}
		_, _ = w.Write([]byte(`{"Status":0,"Answer":[{"type":1,"data":"` + value + `"}]}`))
	})
	if _, err := lookupPublicAEndpoints(context.Background(), []string{resolver.URL + "/one", resolver.URL + "/two"}); err == nil || !strings.Contains(err.Error(), "resolvers_disagree") {
		t.Fatalf("resolver disagreement accepted: %v", err)
	}
}

func TestFreshPublicRequestClientBypassesProxyForObservedIP(t *testing.T) {
	switcher := &publicIngressSwitcher{
		record: "stream.example.com",
		lookupHost: func(context.Context, string) ([]string, error) {
			return []string{"1.1.1.1"}, nil
		},
		httpClient: &http.Client{Timeout: time.Second},
	}
	client, closeIdle, err := switcher.freshPublicRequestClient(context.Background())
	defer closeIdle()
	transport, ok := client.Transport.(*http.Transport)
	if err != nil || !ok || transport.Proxy != nil || !transport.DisableKeepAlives || transport.DialContext == nil {
		t.Fatalf("client=%+v transport=%+v err=%v", client, transport, err)
	}
}
