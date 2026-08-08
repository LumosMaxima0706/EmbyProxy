package proxyadapter

import (
	"net/http"
	"strings"

	"embyproxy/internal/mediaproxy"
)

func (r *Router) serveSlug(w http.ResponseWriter, req *http.Request, rawPath string, parts []string) bool {
	if len(parts) < 2 || parts[0] != "s" {
		return false
	}
	if err := validateSlug(parts[1]); err != nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		return true
	}
	target, enabled, publicPath, err := r.registry.slug(parts[1])
	if err != nil || !enabled {
		http.Error(w, "Not Found", http.StatusNotFound)
		return true
	}
	pathStart := 2
	forward := "/"
	if len(parts) > pathStart {
		forward = "/" + strings.Join(parts[pathStart:], "/")
	}
	if strings.HasSuffix(rawPath, "/") && !strings.HasSuffix(forward, "/") {
		forward += "/"
	}
	r.forward(w, req, forward, target, publicPath)
	return true
}

func (r *Router) forward(w http.ResponseWriter, req *http.Request, path string, target mediaproxy.Target, publicPath string) {
	clone := req.Clone(req.Context())
	clone.URL.Path = path
	clone.URL.RawPath = ""
	if publicPath == "" {
		publicPath = r.executorConfig.PublicPrefix
	}
	if publicPath != "" {
		// The executor remains the single proxy implementation; this optional
		// prefix only controls response Location rewriting for the mock route.
		config := r.executorConfig
		config.PublicPrefix = publicPath
		mediaproxy.NewExecutor(config).ServeHTTP(w, clone, target)
		return
	}
	r.executor.ServeHTTP(w, clone, target)
}
