package proxyadapter

import (
	"errors"
	"net/http"
	"strings"

	"embyproxy/internal/requestlog"
)

func (r *Router) serveNode(w http.ResponseWriter, req *http.Request, rawPath string, parts []string) bool {
	if len(parts) < 1 || parts[0] == "s" {
		return false
	}
	if err := validateNodeName(parts[0]); err != nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		return true
	}
	node, target, err := r.resolver.node(req.Context(), parts[0])
	if err != nil {
		if r.fallback != nil && (errors.Is(err, ErrNotFound) || errors.Is(err, ErrMultipleTarget)) {
			return false
		}
		if errors.Is(err, ErrResolver) {
			http.Error(w, http.StatusText(http.StatusBadGateway), http.StatusBadGateway)
			return true
		}
		http.Error(w, "Not Found", http.StatusNotFound)
		return true
	}
	strip := 1
	if node.Secret != "" {
		if len(parts) < 2 {
			http.Error(w, "Not Found", http.StatusNotFound)
			return true
		}
		if parts[1] != node.Secret {
			http.Error(w, "Not Found", http.StatusNotFound)
			return true
		}
		strip = 2
	}
	logPath := "/" + parts[0] + "/<path>"
	if node.Secret != "" {
		logPath = "/" + parts[0] + "/<secret>/<path>"
	}
	requestlog.SetRequestURI(req.Context(), logPath)
	forward := "/"
	if len(parts) > strip {
		forward = "/" + strings.Join(parts[strip:], "/")
	}
	if strings.HasSuffix(rawPath, "/") && !strings.HasSuffix(forward, "/") {
		forward += "/"
	}
	publicPath := "/" + strings.Join(parts[:strip], "/") + "/"
	r.forward(w, req, forward, target, publicPath)
	return true
}
