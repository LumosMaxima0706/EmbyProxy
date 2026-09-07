package proxyadapter

import (
	"net/http"
	"strings"

	"embyproxy/internal/mediaproxy"
)

const DefaultMockPrefix = "/embyproxy-mediaproxy-test"

type Router struct {
	prefix         string
	resolver       Resolver
	executor       *mediaproxy.Executor
	executorConfig mediaproxy.Config
	fallback       http.Handler
	// edgeLocal marks a router running on an edge agent. Such a router must
	// resolve persisted redirect endpoints locally on client follow-up
	// requests, while the controller router keeps those aliases blocked.
	edgeLocal bool
}

func NewRouter(prefix string, registry *Registry, executor *mediaproxy.Executor, configs ...mediaproxy.Config) *Router {
	if strings.TrimSpace(prefix) == "" {
		prefix = DefaultMockPrefix
	}
	var executorConfig mediaproxy.Config
	if len(configs) > 0 {
		executorConfig = configs[0]
	}
	return &Router{prefix: strings.TrimRight(prefix, "/"), resolver: registry, executor: executor, executorConfig: executorConfig}
}

func NewProductionRouter(resolver *StorageResolver, executor *mediaproxy.Executor, config mediaproxy.Config, fallback http.Handler) *Router {
	return &Router{resolver: resolver, executor: executor, executorConfig: config, fallback: fallback}
}

// NewEdgeRouter creates the production router used by an edge agent. Requests
// already arrived at this edge, so they must not trigger another scheduler
// selection. This also enables the allowlisted, persisted redirect endpoints
// emitted in rewritten PlaybackInfo responses.
func NewEdgeRouter(resolver *StorageResolver, executor *mediaproxy.Executor, config mediaproxy.Config, fallback http.Handler) *Router {
	return &Router{resolver: resolver, executor: executor, executorConfig: config, fallback: fallback, edgeLocal: true}
}

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if r == nil || r.resolver == nil || r.executor == nil || req == nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}
	if req.Header.Get(selectedNodeHeader) == "1" {
		req = req.WithContext(MarkSelectedNodeRequest(req.Context()))
		req.Header.Set(selectedNodeHeader, "2")
	}
	if r.edgeLocal {
		req = req.WithContext(MarkSelectedNodeRequest(req.Context()))
	}
	rawPath, err := requestPath(req)
	if err != nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}
	relative := rawPath
	if r.prefix != "" {
		if !strings.HasPrefix(rawPath, r.prefix+"/") && rawPath != r.prefix {
			r.serveFallback(w, req)
			return
		}
		relative = strings.TrimPrefix(rawPath, r.prefix)
	}
	parts, err := splitRoutePath(relative)
	if err != nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}
	if parts[0] == "admin" || parts[0] == "api" || parts[0] == "health" || parts[0] == "http" || parts[0] == "https" {
		r.serveFallback(w, req)
		return
	}
	if r.serveSlug(w, req, rawPath, parts) || r.serveNode(w, req, rawPath, parts) {
		return
	}
	r.serveFallback(w, req)
}

func (r *Router) serveFallback(w http.ResponseWriter, req *http.Request) {
	if r != nil && r.fallback != nil {
		r.fallback.ServeHTTP(w, req)
		return
	}
	http.Error(w, "Not Found", http.StatusNotFound)
}
