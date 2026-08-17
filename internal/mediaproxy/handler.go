package mediaproxy

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
)

func NewExecutor(cfg Config) *Executor {
	transport := newTransport(cfg)
	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return &Executor{cfg: cfg, transport: transport, client: client, log: nil}
}

func (e *Executor) SetLogger(logger Logger) {
	if e != nil {
		e.log = logger
	}
}

func (e *Executor) ServeHTTP(w http.ResponseWriter, r *http.Request, target Target) {
	if e == nil {
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		return
	}
	e.serveHTTP(w, r, target, e.cfg.PublicPrefix)
}

// ServeHTTPWithPublicPrefix reuses the executor's transport and connection
// pool while allowing an adapter to select the externally visible route prefix.
func (e *Executor) ServeHTTPWithPublicPrefix(w http.ResponseWriter, r *http.Request, target Target, publicPrefix string) {
	if e == nil {
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		return
	}
	e.serveHTTP(w, r, target, publicPrefix)
}

func (e *Executor) serveHTTP(w http.ResponseWriter, r *http.Request, target Target, publicPrefix string) {
	if r == nil {
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		return
	}
	if err := ValidateTarget(target); err != nil {
		e.writeError(w, err)
		return
	}
	addresses, err := e.resolveTarget(r.Context(), target)
	if err != nil {
		e.logEvent("target_blocked", map[string]any{"error": RedactedError(err)})
		e.writeError(w, err)
		return
	}
	r = r.WithContext(withResolvedTarget(r.Context(), target, addresses))
	if isWebSocketRequest(r) {
		if err := e.serveWebSocket(w, r, target); err != nil {
			e.logEvent("websocket_error", map[string]any{"error": RedactedError(err)})
			e.writeError(w, err)
		}
		return
	}
	upstreamURL, err := target.URLForRequest(r.URL)
	if err != nil {
		e.writeError(w, err)
		return
	}
	e.logEvent("proxy_request", requestLogFields(r, target))
	request, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL.String(), r.Body)
	if err != nil {
		e.writeError(w, err)
		return
	}
	request.Header = outboundHeaders(r.Header, target, e.cfg.PreserveHost)
	if e.cfg.PreserveHost {
		request.Host = r.Host
	} else {
		request.Host = upstreamURL.Host
	}
	response, err := e.client.Do(request)
	if err != nil {
		e.logEvent("upstream_error", map[string]any{"error": RedactedError(err), "scheme": target.Scheme})
		e.writeError(w, err)
		return
	}
	defer response.Body.Close()
	if publicPrefix == "" {
		publicPrefix = "/"
	}
	for key, values := range rewriteResponseHeaders(response.Header, target, publicPrefix) {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(response.StatusCode)
	if r.Method != http.MethodHead {
		_, _ = io.Copy(w, response.Body)
	}
}

func isWebSocketRequest(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket") && strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade")
}

func (e *Executor) writeError(w http.ResponseWriter, err error) {
	status := http.StatusBadGateway
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		status = http.StatusGatewayTimeout
	}
	if errors.Is(err, ErrPrivateTarget) || errors.Is(err, ErrInvalidHost) || errors.Is(err, ErrInvalidPort) || errors.Is(err, ErrInvalidScheme) || errors.Is(err, ErrInvalidBasePath) || errors.Is(err, ErrInvalidRequestPath) {
		status = http.StatusBadRequest
	}
	http.Error(w, http.StatusText(status), status)
}

func RedactedError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "upstream_timeout"
	}
	if errors.Is(err, ErrPrivateTarget) {
		return "private_target"
	}
	if errors.Is(err, ErrInvalidHost) || errors.Is(err, ErrInvalidPort) || errors.Is(err, ErrInvalidScheme) || errors.Is(err, ErrInvalidBasePath) || errors.Is(err, ErrInvalidRequestPath) {
		return "invalid_target"
	}
	return "upstream_error"
}
