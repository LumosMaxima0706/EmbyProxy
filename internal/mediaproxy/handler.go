package mediaproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
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
	e.serveHTTP(w, r, target, e.cfg.PublicPrefix, nil)
}

// ServeHTTPWithPublicPrefix reuses the executor's transport and connection
// pool while allowing an adapter to select the externally visible route prefix.
func (e *Executor) ServeHTTPWithPublicPrefix(w http.ResponseWriter, r *http.Request, target Target, publicPrefix string) {
	e.ServeHTTPWithPublicPrefixAndRoutes(w, r, target, publicPrefix, nil)
}

func (e *Executor) ServeHTTPWithPublicPrefixAndRoutes(w http.ResponseWriter, r *http.Request, target Target, publicPrefix string, routes []RedirectRoute) {
	if e == nil {
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		return
	}
	e.serveHTTP(w, r, target, publicPrefix, routes)
}

func (e *Executor) serveHTTP(w http.ResponseWriter, r *http.Request, target Target, publicPrefix string, routes []RedirectRoute) {
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
	if rewritten, ok := rewritePlaybackInfoBody(response, r, publicPrefix); ok {
		response = rewritten
		defer response.Body.Close()
	}
	if publicPrefix == "" {
		publicPrefix = "/"
	}
	for key, values := range rewriteResponseHeadersWithRoutes(response.Header, target, publicPrefix, routes) {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(response.StatusCode)
	if r.Method != http.MethodHead {
		_, _ = io.Copy(w, response.Body)
	}
}

// rewritePlaybackInfoBody prevents an Emby-compatible upstream from leaking a
// private MediaSources.Path (for example http://127.0.0.1:5244/...). Clients
// must receive a route-local playback URL so the next request follows the
// same controller/edge selection path as the initial PlaybackInfo request.
func rewritePlaybackInfoBody(response *http.Response, request *http.Request, publicPrefix string) (*http.Response, bool) {
	if response == nil || request == nil || response.StatusCode != http.StatusOK ||
		!strings.Contains(strings.ToLower(response.Header.Get("Content-Type")), "application/json") ||
		!strings.Contains(strings.ToLower(request.URL.Path), "/playbackinfo") {
		return response, false
	}
	itemID := playbackItemID(request.URL.Path)
	if itemID == "" {
		return response, false
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return response, false
	}
	// Reading an upstream PlaybackInfo body is necessary to decide whether a
	// private path must be replaced. Put it back before every no-rewrite exit;
	// otherwise a valid 200 response reaches clients with an empty JSON body.
	response.Body = io.NopCloser(bytes.NewReader(raw))
	var payload map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return response, false
	}
	sources, ok := payload["MediaSources"].([]any)
	if !ok {
		return response, false
	}
	playSessionID, _ := payload["PlaySessionId"].(string)
	userID := strings.TrimSpace(request.URL.Query().Get("UserId"))
	if userID == "" {
		userID = strings.TrimSpace(request.Header.Get("X-Emby-User-Id"))
	}
	changed := false
	for _, value := range sources {
		source, ok := value.(map[string]any)
		if !ok {
			continue
		}
		if pathValue, ok := source["Path"].(string); ok && isPrivateMediaPath(pathValue) {
			// MediaSources.Path is commonly a server filesystem path. For remote
			// HTTP media, Emby exposes the client-compatible original route; the
			// media source and play session bind the signed redirect response.
			stream := strings.TrimRight(publicPrefix, "/") + "/emby/videos/" + itemID + "/original.mkv?Static=true"
			if mediaID, ok := source["Id"].(string); ok && strings.TrimSpace(mediaID) != "" {
				stream += "&MediaSourceId=" + urlQueryEscape(mediaID)
			}
			if strings.TrimSpace(playSessionID) != "" {
				stream += "&PlaySessionId=" + urlQueryEscape(playSessionID)
			}
			if userID != "" {
				stream += "&UserId=" + urlQueryEscape(userID)
			}
			stream += "&DeviceId=embyproxy-canary"
			source["Path"] = stream
			changed = true
		}
	}
	if !changed {
		return response, false
	}
	out, err := json.Marshal(payload)
	if err != nil {
		return response, false
	}
	headers := response.Header.Clone()
	headers.Del("Content-Length")
	return &http.Response{StatusCode: response.StatusCode, Status: response.Status, Header: headers, Body: io.NopCloser(bytes.NewReader(out)), Request: response.Request}, true
}

func playbackItemID(rawPath string) string {
	parts := strings.Split(strings.Trim(rawPath, "/"), "/")
	for index := 0; index+2 < len(parts); index++ {
		if strings.EqualFold(parts[index], "items") && strings.EqualFold(parts[index+2], "playbackinfo") && parts[index+1] != "" {
			return parts[index+1]
		}
	}
	return ""
}

func isPrivateMediaPath(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Hostname() == "" {
		return false
	}
	ip := net.ParseIP(parsed.Hostname())
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast())
}

func urlQueryEscape(value string) string {
	return url.QueryEscape(value)
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
