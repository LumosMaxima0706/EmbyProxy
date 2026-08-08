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

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if r == nil || r.resolver == nil || r.executor == nil || req == nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
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
