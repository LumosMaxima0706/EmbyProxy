package proxyadapter

import (
	"net/http"
	"strings"
)

func (r *Router) serveNode(w http.ResponseWriter, req *http.Request, rawPath string, parts []string) bool {
	if len(parts) < 1 || parts[0] == "s" {
		return false
	}
	if err := validateNodeName(parts[0]); err != nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		return true
	}
	node, target, err := r.registry.node(parts[0])
	if err != nil {
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
	forward := "/"
	if len(parts) > strip {
		forward = "/" + strings.Join(parts[strip:], "/")
	}
	if strings.HasSuffix(rawPath, "/") && !strings.HasSuffix(forward, "/") {
		forward += "/"
	}
	r.forward(w, req, forward, target, "")
	return true
}
