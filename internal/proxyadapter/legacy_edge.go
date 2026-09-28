package proxyadapter

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"strings"

	"embyproxy/internal/mediaproxy"
	"embyproxy/internal/storage"
)

type legacyEdgeRouteStore interface {
	ListManagedRoutes(context.Context) ([]storage.ManagedRoute, error)
	ListManagedRouteLines(context.Context, string) ([]storage.ManagedRouteLine, error)
}

// serveLegacyEdge accepts only configured public routes, never an arbitrary
// host supplied by a client in the legacy /https/<host>/<port>/ path.
func (r *Router) serveLegacyEdge(w http.ResponseWriter, req *http.Request, parts []string) bool {
	if !r.edgeLocal || len(parts) < 3 || parts[0] != "https" {
		return false
	}
	provider, ok := r.resolver.(*StorageResolver)
	if !ok {
		return false
	}
	store, ok := provider.store.(legacyEdgeRouteStore)
	if !ok {
		return false
	}
	port, err := strconv.Atoi(parts[2])
	if err != nil || port < 1 || port > 65535 || parts[1] == "" {
		return false
	}
	if paths, ok := provider.store.(interface{ KV() *storage.KV }); ok {
		var configured map[string]string
		found, readErr := paths.KV().GetJSON(req.Context(), "edge:legacy-public-paths", &configured)
		if readErr != nil {
			http.Error(w, "route unavailable", http.StatusServiceUnavailable)
			return true
		}
		if found && configuredLegacyTarget(configured, parts[1], port) {
			target, err := mediaproxy.ParseTarget("https", parts[1], port, "")
			if err != nil {
				return false
			}
			r.forwardLegacy(w, req, legacyTail(req, parts), target, "/https/"+parts[1]+"/"+strconv.Itoa(port)+"/")
			return true
		}
	}
	routes, err := store.ListManagedRoutes(req.Context())
	if err != nil {
		http.Error(w, "route unavailable", http.StatusServiceUnavailable)
		return true
	}
	for _, route := range routes {
		if !route.Enabled || !route.Public {
			continue
		}
		lines, err := store.ListManagedRouteLines(req.Context(), route.Slug)
		if err != nil {
			http.Error(w, "route unavailable", http.StatusServiceUnavailable)
			return true
		}
		line, found := selectManagedLine(route.DefaultLine, lines)
		if !found {
			continue
		}
		target, err := parseServerTarget(line.Target)
		if err != nil || target.Scheme != "https" || target.Port != port || !strings.EqualFold(target.Host, parts[1]) {
			continue
		}
		path := "/"
		if len(parts) > 3 {
			path += strings.Join(parts[3:], "/")
		}
		if strings.HasSuffix(req.URL.Path, "/") && !strings.HasSuffix(path, "/") {
			path += "/"
		}
		if target.BasePath != "" && strings.HasPrefix(path, target.BasePath+"/") {
			path = strings.TrimPrefix(path, target.BasePath)
		}
		r.forwardLegacy(w, req, path, target, "/https/"+target.Host+"/"+strconv.Itoa(port)+"/")
		return true
	}
	return false
}

func (r *Router) forwardLegacy(w http.ResponseWriter, req *http.Request, path string, target mediaproxy.Target, publicPath string) {
	if r.edgeUsageSink == nil || req.Header.Get("Upgrade") != "" {
		r.forward(w, req, path, target, publicPath, nil)
		return
	}
	counted := &countingResponseWriter{ResponseWriter: w}
	r.forward(counted, req, path, target, publicPath, nil)
	if counted.bytes > 0 {
		r.edgeUsageSink(counted.bytes)
	}
}

func configuredLegacyTarget(paths map[string]string, host string, port int) bool {
	if net.ParseIP(host) != nil || len(host) > 253 || !strings.Contains(host, ".") || strings.ContainsAny(host, ":@\\ ") {
		return false
	}
	for _, segment := range strings.Split(host, ".") {
		if segment == "" || len(segment) > 63 || segment[0] == '-' || segment[len(segment)-1] == '-' {
			return false
		}
		for _, c := range segment {
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-') {
				return false
			}
		}
	}
	for _, path := range paths {
		parts := strings.Split(strings.Trim(path, "/"), "/")
		if len(parts) == 3 && parts[0] == "https" && strings.EqualFold(parts[1], host) && parts[2] == strconv.Itoa(port) {
			return true
		}
	}
	return false
}

func legacyTail(req *http.Request, parts []string) string {
	if len(parts) == 3 {
		return "/"
	}
	tail := "/" + strings.Join(parts[3:], "/")
	if strings.HasSuffix(req.URL.Path, "/") && !strings.HasSuffix(tail, "/") {
		tail += "/"
	}
	return tail
}
