package mediaproxy

import (
	"net/http"
	"testing"
)

func TestOutboundHeadersPreserveRangeAndIfRange(t *testing.T) {
	headers := outboundHeaders(http.Header{
		"Range": {"bytes=0-10"}, "If-Range": {"etag"}, "Connection": {"keep-alive"},
	}, Target{Host: "media.example", Port: 443}, false)
	if headers.Get("Range") != "bytes=0-10" || headers.Get("If-Range") != "etag" || headers.Get("Connection") != "" {
		t.Fatalf("headers=%v", headers)
	}
}

func TestWebSocketHeadersSetUpgrade(t *testing.T) {
	headers := websocketHeaders(http.Header{"Upgrade": {"websocket"}}, Target{Host: "media.example", Port: 443}, false)
	if headers.Get("Upgrade") != "websocket" || headers.Get("Connection") != "Upgrade" {
		t.Fatalf("headers=%v", headers)
	}
}
