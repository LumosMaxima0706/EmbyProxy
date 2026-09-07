package proxyadapter

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"

	"embyproxy/internal/mediaproxy"
	"embyproxy/internal/requestlog"
)

func (r *Router) serveSlug(w http.ResponseWriter, req *http.Request, rawPath string, parts []string) bool {
	if len(parts) < 2 || parts[0] != "s" {
		return false
	}
	if err := validateSlug(parts[1]); err != nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		return true
	}
	meta := &selectionMeta{}
	resolverCtx := context.WithValue(req.Context(), selectionMetaContextKey{}, meta)
	target, enabled, publicPath, err := r.resolver.slug(resolverCtx, parts[1])
	if errors.Is(err, ErrNotFound) {
		return false
	}
	if err != nil || !enabled {
		status := http.StatusNotFound
		if errors.Is(err, ErrResolver) {
			status = http.StatusBadGateway
		}
		http.Error(w, http.StatusText(status), status)
		return true
	}
	pathStart := 2
	var redirectRoutes []mediaproxy.RedirectRoute
	if resolver, ok := r.resolver.(*StorageResolver); ok {
		if endpoints, ok := resolver.store.(ProxyRedirectEndpointStore); ok {
			for _, endpoint := range mustRedirectEndpoints(resolverCtx, endpoints, parts[1]) {
				redirectRoutes = append(redirectRoutes, mediaproxy.RedirectRoute{Scheme: endpoint.Scheme, Host: endpoint.Host, Port: endpoint.Port, BasePath: endpoint.PathPrefix})
			}
		}
	}
	// A canary-validated upstream redirect is represented as
	// /s/<slug>/<scheme>/<host>/<port>/<path>. Only endpoints persisted by the
	// control plane are accepted; arbitrary client-supplied hosts never become
	// proxy targets.
	if resolver, ok := r.resolver.(*StorageResolver); ok && len(parts) >= 5 {
		_, selectedEdgeRequest := resolverCtx.Value(selectedNodeContextKey{}).(bool)
		if selectedEdgeRequest {
			if redirected, consumed, matched, _ := resolver.redirectTarget(resolverCtx, parts[1], parts[2:]); matched {
				target = redirected
				pathStart += consumed
			}
		}
	}
	requestlog.SetRequestURI(req.Context(), "/s/"+parts[1]+"/<path>")
	forward := "/"
	if len(parts) > pathStart {
		forward = "/" + strings.Join(parts[pathStart:], "/")
	}
	if strings.HasSuffix(rawPath, "/") && !strings.HasSuffix(forward, "/") {
		forward += "/"
	}
	if meta.selected {
		if connections, ok := r.resolver.(ProxyNodeConnectionStore); ok {
			if err := connections.BeginProxyNodeConnection(req.Context(), meta.nodeID); err != nil {
				http.Error(w, "Proxy node unavailable", http.StatusServiceUnavailable)
				return true
			}
			defer func() { _ = connections.EndProxyNodeConnection(context.Background(), meta.nodeID) }()
		}
		req = req.WithContext(MarkSelectedNodeRequest(req.Context()))
		req.Header.Set(selectedNodeHeader, "1")
	}
	counted := &countingResponseWriter{ResponseWriter: w}
	if req.Header.Get("Upgrade") != "" {
		r.forward(w, req, forward, target, publicPath, redirectRoutes)
	} else {
		r.forward(counted, req, forward, target, publicPath, redirectRoutes)
	}
	if meta.selected {
		if usage, ok := r.resolver.(interface {
			RecordProxyNodeBytes(context.Context, string, int64)
		}); ok {
			usage.RecordProxyNodeBytes(context.Background(), meta.nodeID, counted.bytes)
		}
	}
	return true
}

type countingResponseWriter struct {
	http.ResponseWriter
	bytes int64
}

func (w *countingResponseWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	w.bytes += int64(n)
	return n, err
}

func (w *countingResponseWriter) ReadFrom(r io.Reader) (int64, error) {
	n, err := io.Copy(struct{ io.Writer }{w.ResponseWriter}, r)
	w.bytes += n
	return n, err
}

func (w *countingResponseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *countingResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("hijacking unsupported")
	}
	return h.Hijack()
}

func (r *Router) forward(w http.ResponseWriter, req *http.Request, path string, target mediaproxy.Target, publicPath string, redirectRoutes []mediaproxy.RedirectRoute) {
	clone := req.Clone(req.Context())
	clone.URL.Path = path
	clone.URL.RawPath = ""
	if publicPath == "" {
		publicPath = r.executorConfig.PublicPrefix
	}
	if publicPath != "" {
		// The executor remains the single proxy implementation; this route prefix
		// only controls response Location rewriting.
		r.executor.ServeHTTPWithPublicPrefixAndRoutes(w, clone, target, publicPath, redirectRoutes)
		return
	}
	r.executor.ServeHTTP(w, clone, target)
}
