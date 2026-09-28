package proxyadapter

import (
	"context"
	"net/http"
	"strconv"
	"strings"

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
		r.forward(w, req, path, target, "/https/"+target.Host+"/"+strconv.Itoa(port)+"/", nil)
		return true
	}
	return false
}
