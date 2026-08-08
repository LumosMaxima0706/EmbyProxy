package proxyadapter

import (
	"net/http"
	"strings"

	"embyproxy/internal/mediaproxy"
)

const DefaultMockPrefix = "/embyproxy-mediaproxy-test"

type Router struct {
	prefix         string
	registry       *Registry
	executor       *mediaproxy.Executor
	executorConfig mediaproxy.Config
}

func NewRouter(prefix string, registry *Registry, executor *mediaproxy.Executor, configs ...mediaproxy.Config) *Router {
	if strings.TrimSpace(prefix) == "" {
		prefix = DefaultMockPrefix
	}
	var executorConfig mediaproxy.Config
	if len(configs) > 0 {
		executorConfig = configs[0]
	}
	return &Router{prefix: strings.TrimRight(prefix, "/"), registry: registry, executor: executor, executorConfig: executorConfig}
}

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if r == nil || r.registry == nil || r.executor == nil || req == nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}
	rawPath, err := requestPath(req)
	if err != nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}
	if !strings.HasPrefix(rawPath, r.prefix+"/") && rawPath != r.prefix {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}
	relative := strings.TrimPrefix(rawPath, r.prefix)
	parts, err := splitRoutePath(relative)
	if err != nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}
	if parts[0] == "admin" || parts[0] == "api" || parts[0] == "health" || parts[0] == "http" || parts[0] == "https" {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}
	if r.serveSlug(w, req, rawPath, parts) || r.serveNode(w, req, rawPath, parts) {
		return
	}
	http.Error(w, "Not Found", http.StatusNotFound)
}
