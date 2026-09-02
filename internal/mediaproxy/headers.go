package mediaproxy

import (
	"net/http"
	"strconv"
	"strings"
)

var hopByHopHeaders = map[string]bool{
	"Connection": true, "Keep-Alive": true, "Proxy-Authenticate": true,
	"Proxy-Authorization": true, "Proxy-Connection": true, "Te": true, "Trailer": true,
	"Transfer-Encoding": true, "Upgrade": true,
}

func cloneHeaders(in http.Header) http.Header {
	out := make(http.Header, len(in))
	for key, values := range in {
		out[key] = append([]string(nil), values...)
	}
	return out
}

func outboundHeaders(in http.Header, target Target, preserveHost bool) http.Header {
	out := cloneHeaders(in)
	// Internal routing markers are consumed by the edge router and must never
	// be forwarded to an upstream Emby server.
	if out.Get("X-EmbyProxy-Selected-Node") == "2" {
		out.Del("X-EmbyProxy-Selected-Node")
	}
	removeHopByHopHeaders(out, false)
	if !preserveHost {
		out.Set("Host", netHost(target))
	}
	return out
}

func netHost(target Target) string {
	if strings.Contains(target.Host, ":") {
		return "[" + target.Host + "]:" + strconv.Itoa(target.Port)
	}
	return target.Host + ":" + strconv.Itoa(target.Port)
}

func websocketHeaders(in http.Header, target Target, preserveHost bool) http.Header {
	out := cloneHeaders(in)
	removeHopByHopHeaders(out, true)
	for key := range out {
		if strings.EqualFold(key, "Host") || strings.EqualFold(key, "Content-Length") {
			delete(out, key)
		}
	}
	if !preserveHost {
		out.Set("Host", netHost(target))
	}
	out.Set("Connection", "Upgrade")
	out.Set("Upgrade", "websocket")
	return out
}

func removeHopByHopHeaders(headers http.Header, preserveUpgrade bool) {
	for _, token := range connectionHeaderTokens(headers) {
		if preserveUpgrade && (strings.EqualFold(token, "connection") || strings.EqualFold(token, "upgrade")) {
			continue
		}
		headers.Del(token)
	}
	for key := range headers {
		canonical := http.CanonicalHeaderKey(key)
		if hopByHopHeaders[canonical] && !(preserveUpgrade && (canonical == "Connection" || canonical == "Upgrade")) {
			delete(headers, key)
		}
	}
}

func connectionTokens(value string) []string {
	var tokens []string
	for _, token := range strings.Split(value, ",") {
		if token = strings.TrimSpace(token); token != "" {
			tokens = append(tokens, token)
		}
	}
	return tokens
}

func connectionHeaderTokens(headers map[string][]string) []string {
	var tokens []string
	for key, values := range headers {
		if !strings.EqualFold(key, "Connection") {
			continue
		}
		for _, value := range values {
			tokens = append(tokens, connectionTokens(value)...)
		}
	}
	return tokens
}
