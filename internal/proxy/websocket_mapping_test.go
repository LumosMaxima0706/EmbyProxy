package proxy

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"embyproxy/internal/config"
	"embyproxy/internal/logging"
	"embyproxy/internal/storage"
)

func TestHandleWebSocketPassesThroughClientRejectionWithoutBanningOrFallback(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound} {
		status := status
		t.Run(http.StatusText(status), func(t *testing.T) {
			primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Connection", "keep-alive, X-Hop")
				w.Header().Set("Keep-Alive", "timeout=5")
				w.Header().Set("Upgrade", "websocket")
				w.Header().Set("X-Hop", "drop")
				w.Header().Set("WWW-Authenticate", `Bearer realm="staging"`)
				w.WriteHeader(status)
				_, _ = io.WriteString(w, "upstream rejection body")
			}))
			defer primary.Close()

			var fallbackHits atomic.Int32
			fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				fallbackHits.Add(1)
				w.WriteHeader(http.StatusSwitchingProtocols)
			}))
			defer fallback.Close()

			h := newWebSocketMappingTestHandler(t)
			node := storage.Node{Name: "node", Target: primary.URL + "\n" + fallback.URL}
			recorder := httptest.NewRecorder()
			h.handleWebSocket(recorder, newWebSocketMappingRequest(), node, parsedRoute{Name: "node", Path: "/embywebsocket"})

			if recorder.Code != status {
				t.Fatalf("status = %d, want %d", recorder.Code, status)
			}
			if fallbackHits.Load() != 0 {
				t.Fatalf("fallback hits = %d, want 0", fallbackHits.Load())
			}
			if _, banned := h.lineBan.Get("admin:node|" + primary.URL); banned {
				t.Fatal("client rejection line-banned primary target")
			}
			if recorder.Header().Get("Connection") != "" || recorder.Header().Get("Keep-Alive") != "" || recorder.Header().Get("Upgrade") != "" || recorder.Header().Get("X-Hop") != "" {
				t.Fatalf("hop-by-hop headers leaked: %v", recorder.Header())
			}
			if recorder.Header().Get("WWW-Authenticate") == "" {
				t.Fatal("safe rejection header was not forwarded")
			}
			if recorder.Body.Len() != 0 {
				t.Fatalf("rejection body length = %d, want 0", recorder.Body.Len())
			}
		})
	}
}

func TestWriteWebSocketClientRejectionFiltersUnsafeHeaders(t *testing.T) {
	unsafeHeaders := make(http.Header)
	for name, value := range map[string]string{
		"Location":            "https://upstream.example/private",
		"Connection":          "keep-alive, X-Hop",
		"Upgrade":             "websocket",
		"Keep-Alive":          "timeout=5",
		"Proxy-Authenticate":  "challenge",
		"Proxy-Authorization": "value",
		"TE":                  "trailers",
		"Trailer":             "X-Trailer",
		"Transfer-Encoding":   "chunked",
		"X-Hop":               "drop",
		"WWW-Authenticate":    `Bearer realm="staging"`,
	} {
		unsafeHeaders.Set(name, value)
	}
	response := &http.Response{
		StatusCode: http.StatusNotFound,
		Header:     unsafeHeaders,
		Body:       http.NoBody,
	}
	recorder := httptest.NewRecorder()

	writeWebSocketClientRejection(recorder, response)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
	for _, name := range []string{"Location", "Connection", "Upgrade", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "TE", "Trailer", "Transfer-Encoding", "X-Hop"} {
		if got := recorder.Header().Values(name); len(got) != 0 {
			t.Errorf("%s leaked: %v", name, got)
		}
	}
	if got := recorder.Header().Get("WWW-Authenticate"); got != `Bearer realm="staging"` {
		t.Errorf("WWW-Authenticate = %q, want safe challenge", got)
	}
}

func TestHandleWebSocketServerFailureStillBansAndReturnsBadGateway(t *testing.T) {
	var primaryHits atomic.Int32
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		primaryHits.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer primary.Close()
	var fallbackHits atomic.Int32
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fallbackHits.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer fallback.Close()

	h := newWebSocketMappingTestHandler(t)
	node := storage.Node{Name: "node", Target: primary.URL + "\n" + fallback.URL}
	recorder := httptest.NewRecorder()
	h.handleWebSocket(recorder, newWebSocketMappingRequest(), node, parsedRoute{Name: "node", Path: "/embywebsocket"})

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", recorder.Code)
	}
	if primaryHits.Load() != 1 || fallbackHits.Load() != 1 {
		t.Fatalf("upstream hits = primary:%d fallback:%d, want 1 each", primaryHits.Load(), fallbackHits.Load())
	}
	for _, target := range []string{primary.URL, fallback.URL} {
		if _, banned := h.lineBan.Get("admin:node|" + target); !banned {
			t.Fatalf("server failure did not line-ban %s", target)
		}
	}
}

func TestHandleWebSocketTransportFailureStillReturnsBadGateway(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	target := "http://" + listener.Addr().String()
	_ = listener.Close()

	h := newWebSocketMappingTestHandler(t)
	recorder := httptest.NewRecorder()
	h.handleWebSocket(recorder, newWebSocketMappingRequest(), storage.Node{Name: "node", Target: target}, parsedRoute{Name: "node", Path: "/embywebsocket"})

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", recorder.Code)
	}
	if _, banned := h.lineBan.Get("admin:node|" + target); !banned {
		t.Fatal("transport failure did not line-ban target")
	}
}

func TestHandleWebSocketSwitchingProtocolsStillTunnels(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hijacker := w.(http.Hijacker)
		conn, reader, err := hijacker.Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		_, _ = fmt.Fprint(conn, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
		if reader.Reader.Buffered() > 0 {
			_, _ = reader.Reader.Discard(reader.Reader.Buffered())
		}
		buf := make([]byte, 4)
		if _, err := io.ReadFull(conn, buf); err == nil {
			_, _ = conn.Write(buf)
		}
	}))
	defer upstream.Close()

	h := newWebSocketMappingTestHandler(t)
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.handleWebSocket(w, r, storage.Node{Name: "node", Target: upstream.URL}, parsedRoute{Name: "node", Path: r.URL.Path})
	}))
	defer proxyServer.Close()

	parsedProxy, err := url.Parse(proxyServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.DialTimeout("tcp", parsedProxy.Host, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, _ = fmt.Fprintf(conn, "GET /socket HTTP/1.1\r\nHost: %s\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: dGVzdA==\r\n\r\n", parsedProxy.Host)
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, nil)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status = %d, want 101", response.StatusCode)
	}
	_, _ = conn.Write([]byte("ping"))
	buf := make([]byte, 4)
	if _, err := io.ReadFull(reader, buf); err != nil || string(buf) != "ping" {
		t.Fatalf("tunnel response = %q, err = %v", buf, err)
	}
}

func newWebSocketMappingTestHandler(t *testing.T) *Handler {
	t.Helper()
	logger := logging.New("silent", false)
	t.Cleanup(func() { _ = logger.Close() })
	return New(config.Config{}, newProxyTestStore(t), nil, logger)
}

func newWebSocketMappingRequest() *http.Request {
	request := httptest.NewRequest(http.MethodGet, "https://proxy.example/node/embywebsocket", nil)
	request.Header.Set("Connection", "Upgrade")
	request.Header.Set("Upgrade", "websocket")
	return request
}
