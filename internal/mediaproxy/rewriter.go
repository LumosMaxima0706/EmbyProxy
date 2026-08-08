package mediaproxy

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

func rewriteLocation(value string, target Target, publicPrefix string) string {
	if strings.TrimSpace(value) == "" {
		return value
	}
	u, err := url.Parse(value)
	if err != nil {
		return value
	}
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else if u.Scheme == "http" {
			port = "80"
		}
	}
	if u.IsAbs() && strings.EqualFold(u.Hostname(), target.Host) && port == fmt.Sprint(target.Port) {
		u.Scheme = ""
		u.Host = ""
	}
	if u.IsAbs() {
		return value
	}
	base := strings.TrimRight(publicPrefix, "/")
	locationPath := u.EscapedPath()
	basePath := strings.TrimRight(target.BasePath, "/")
	if basePath != "" && (locationPath == basePath || strings.HasPrefix(locationPath, basePath+"/")) {
		locationPath = strings.TrimPrefix(locationPath, basePath)
	}
	pathValue := strings.TrimLeft(locationPath, "/")
	result := base + "/" + pathValue
	if u.RawQuery != "" {
		result += "?" + u.RawQuery
	}
	return result
}

func rewriteResponseHeaders(headers map[string][]string, target Target, publicPrefix string) map[string][]string {
	result := make(map[string][]string, len(headers))
	for key, values := range headers {
		copied := append([]string(nil), values...)
		if strings.EqualFold(key, "Location") || strings.EqualFold(key, "Content-Location") {
			for idx, value := range copied {
				copied[idx] = rewriteLocation(value, target, publicPrefix)
			}
		}
		result[key] = copied
	}
	for key := range result {
		if hopByHopHeaders[http.CanonicalHeaderKey(key)] {
			delete(result, key)
		}
	}
	if tokens := connectionHeaderTokens(headers); len(tokens) > 0 {
		for _, token := range tokens {
			deleteHeader(result, token)
		}
	}
	return result
}

func deleteHeader(headers map[string][]string, name string) {
	for key := range headers {
		if strings.EqualFold(key, name) {
			delete(headers, key)
		}
	}
}
