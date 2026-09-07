package mediaproxy

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type RedirectRoute struct {
	Scheme   string
	Host     string
	Port     int
	BasePath string
}

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
	return rewriteResponseHeadersWithRoutes(headers, target, publicPrefix, nil)
}

func rewriteResponseHeadersWithRoutes(headers map[string][]string, target Target, publicPrefix string, routes []RedirectRoute) map[string][]string {
	result := make(map[string][]string, len(headers))
	for key, values := range headers {
		copied := append([]string(nil), values...)
		if strings.EqualFold(key, "Location") || strings.EqualFold(key, "Content-Location") {
			for idx, value := range copied {
				copied[idx] = rewriteLocationWithRoutes(value, target, publicPrefix, routes)
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

func rewriteLocationWithRoutes(value string, target Target, publicPrefix string, routes []RedirectRoute) string {
	u, err := url.Parse(value)
	if err != nil {
		return rewriteLocation(value, target, publicPrefix)
	}
	// A response from a redirect endpoint may use a relative Location. Resolve
	// it against that endpoint before the generic primary-route rewriter sees
	// it, otherwise /stream becomes /s/<slug>/ and loses the scoped alias.
	if !u.IsAbs() {
		for _, route := range routes {
			if sameRedirectTarget(target, route) {
				return rewriteRedirectRouteLocation(u, route, publicPrefix)
			}
		}
		return rewriteLocation(value, target, publicPrefix)
	}
	for _, route := range routes {
		if !sameRedirectURL(u, route) {
			continue
		}
		return rewriteRedirectRouteLocation(u, route, publicPrefix)
	}
	return rewriteLocation(value, target, publicPrefix)
}

func sameRedirectURL(u *url.URL, route RedirectRoute) bool {
	if u == nil || !strings.EqualFold(route.Scheme, u.Scheme) || !strings.EqualFold(route.Host, u.Hostname()) {
		return false
	}
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	return fmt.Sprint(route.Port) == port
}

func sameRedirectTarget(target Target, route RedirectRoute) bool {
	return strings.EqualFold(target.Scheme, route.Scheme) && strings.EqualFold(target.Host, route.Host) &&
		target.Port == route.Port && strings.Trim(target.BasePath, "/") == strings.Trim(route.BasePath, "/")
}

func rewriteRedirectRouteLocation(u *url.URL, route RedirectRoute, publicPrefix string) string {
	prefix := strings.Trim(route.BasePath, "/")
	pathValue := strings.TrimLeft(u.EscapedPath(), "/")
	if prefix != "" && (pathValue == prefix || strings.HasPrefix(pathValue, prefix+"/")) {
		pathValue = strings.TrimPrefix(pathValue, prefix)
		pathValue = strings.TrimLeft(pathValue, "/")
	}
	result := strings.TrimRight(publicPrefix, "/") + "/" + route.Scheme + "/" + route.Host + "/" + fmt.Sprint(route.Port)
	if prefix != "" {
		result += "/" + prefix
	}
	if pathValue != "" {
		result += "/" + pathValue
	}
	if u.RawQuery != "" {
		result += "?" + u.RawQuery
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
