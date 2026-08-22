package publicationagent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"embyproxy/internal/proxyadapter"
	"embyproxy/internal/publicationprotocol"
)

// PlaybackCanaryEvidence is the redacted, runtime-only evidence accepted by
// the admin playback gate. It intentionally contains no URL query, token or
// header data. Redirect endpoints are reduced to scheme/host/port before they
// reach this type.
type PlaybackCanaryEvidence struct {
	ConnectivityStatus int                 `json:"connectivity_status"`
	PlaybackInfoStatus int                 `json:"playbackinfo_status"`
	VideoStreamStatus  int                 `json:"videostream_status"`
	Redirects          []PlaybackRedirect  `json:"redirects,omitempty"`
	Media              PlaybackMediaResult `json:"media"`
}

type PlaybackRedirect struct {
	Status   int    `json:"status"`
	Location string `json:"location"`
}

type PlaybackMediaResult struct {
	StatusCode        int   `json:"status_code"`
	BytesRead         int64 `json:"bytes_read"`
	GrowthBytes       int64 `json:"growth_bytes"`
	ContentRange      bool  `json:"content_range"`
	AcceptRanges      bool  `json:"accept_ranges"`
	RedirectsFollowed int   `json:"redirects_followed"`
}

type PlaybackCanaryResult struct {
	Status            string             `json:"status"`
	FailureClass      string             `json:"failure_class,omitempty"`
	RedirectEndpoints []RedirectEndpoint `json:"redirect_endpoints,omitempty"`
}

// ValidatePlaybackCanary separates API connectivity from actual media
// readiness. It is deliberately pure so publication and regression tests can
// exercise the same rules without contacting an upstream.
func ValidatePlaybackCanary(e PlaybackCanaryEvidence) (PlaybackCanaryResult, error) {
	if e.ConnectivityStatus < 200 || e.ConnectivityStatus >= 300 {
		return PlaybackCanaryResult{Status: "failed", FailureClass: classifyHTTP(e.ConnectivityStatus)}, errors.New("connectivity_failed")
	}
	if e.PlaybackInfoStatus < 200 || e.PlaybackInfoStatus >= 300 {
		return PlaybackCanaryResult{Status: "failed", FailureClass: classifyHTTP(e.PlaybackInfoStatus)}, errors.New("playbackinfo_failed")
	}
	streamDirect := e.VideoStreamStatus == 200 || e.VideoStreamStatus == 206
	if !streamDirect && (e.VideoStreamStatus < 300 || e.VideoStreamStatus > 399) {
		return PlaybackCanaryResult{Status: "failed", FailureClass: classifyHTTP(e.VideoStreamStatus)}, errors.New("videostream_redirect_missing")
	}
	if !streamDirect && (len(e.Redirects) == 0 || len(e.Redirects) > 8) {
		return PlaybackCanaryResult{Status: "failed", FailureClass: "redirect_missing"}, errors.New("redirect_chain_missing")
	}
	seen := map[string]bool{}
	endpoints := make([]RedirectEndpoint, 0, len(e.Redirects))
	for _, hop := range e.Redirects {
		if hop.Status < 300 || hop.Status > 399 {
			return PlaybackCanaryResult{Status: "failed", FailureClass: classifyHTTP(hop.Status)}, errors.New("redirect_chain_invalid")
		}
		u, err := url.Parse(strings.TrimSpace(hop.Location))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
			return PlaybackCanaryResult{Status: "failed", FailureClass: "unknown_media_host"}, errors.New("redirect_location_invalid")
		}
		host := strings.ToLower(u.Hostname())
		if !safeRedirectHost(host) {
			return PlaybackCanaryResult{Status: "failed", FailureClass: "unknown_media_host"}, errors.New("redirect_host_invalid")
		}
		port := 443
		if u.Port() != "" {
			port, err = strconv.Atoi(u.Port())
			if err != nil || port < 1 || port > 65535 {
				return PlaybackCanaryResult{Status: "failed", FailureClass: "unknown_media_host"}, errors.New("redirect_port_invalid")
			}
		} else if u.Scheme == "http" {
			port = 80
		}
		pathPrefix := redirectPathPrefix(u.EscapedPath())
		if strings.Trim(u.EscapedPath(), "/") != "" && pathPrefix == "" {
			return PlaybackCanaryResult{Status: "failed", FailureClass: "unknown_media_host"}, errors.New("redirect_path_invalid")
		}
		key := u.Scheme + "://" + host + ":" + strconv.Itoa(port) + "/" + pathPrefix
		if !seen[key] {
			seen[key] = true
			endpoints = append(endpoints, RedirectEndpoint{Scheme: u.Scheme, Host: host, Port: port, PathPrefix: pathPrefix})
		}
	}
	if e.Media.StatusCode != 200 && e.Media.StatusCode != 206 {
		return PlaybackCanaryResult{Status: "failed", FailureClass: classifyHTTP(e.Media.StatusCode), RedirectEndpoints: endpoints}, errors.New("media_status_invalid")
	}
	if e.Media.BytesRead <= 0 || e.Media.GrowthBytes <= 0 {
		return PlaybackCanaryResult{Status: "failed", FailureClass: "no_byte_growth", RedirectEndpoints: endpoints}, errors.New("media_byte_growth_missing")
	}
	if e.Media.StatusCode == 206 && (!e.Media.ContentRange || !e.Media.AcceptRanges) {
		return PlaybackCanaryResult{Status: "failed", FailureClass: "invalid_range", RedirectEndpoints: endpoints}, errors.New("range_headers_missing")
	}
	if e.Media.RedirectsFollowed > 8 {
		return PlaybackCanaryResult{Status: "failed", FailureClass: "redirect_loop", RedirectEndpoints: endpoints}, errors.New("redirect_loop")
	}
	return PlaybackCanaryResult{Status: "healthy", RedirectEndpoints: endpoints}, nil
}

type playbackInfoDocument struct {
	MediaSources []struct {
		ID                   string `json:"Id"`
		Path                 string `json:"Path"`
		Protocol             string `json:"Protocol"`
		IsRemote             bool   `json:"IsRemote"`
		SupportsDirectStream bool   `json:"SupportsDirectStream"`
		SupportsDirectPlay   bool   `json:"SupportsDirectPlay"`
		DirectStreamURL      string `json:"DirectStreamUrl"`
	} `json:"MediaSources"`
	PlaySessionID string `json:"PlaySessionId"`
}

func playbackStreamPath(item string, source struct {
	ID                   string `json:"Id"`
	Path                 string `json:"Path"`
	Protocol             string `json:"Protocol"`
	IsRemote             bool   `json:"IsRemote"`
	SupportsDirectStream bool   `json:"SupportsDirectStream"`
	SupportsDirectPlay   bool   `json:"SupportsDirectPlay"`
	DirectStreamURL      string `json:"DirectStreamUrl"`
}, playSessionID string) string {
	if strings.TrimSpace(source.DirectStreamURL) != "" {
		return strings.TrimSpace(source.DirectStreamURL)
	}
	if strings.HasPrefix(strings.TrimSpace(source.Path), "/") {
		return strings.TrimSpace(source.Path)
	}
	if source.IsRemote && strings.TrimSpace(source.Path) != "" {
		return "/emby/videos/" + item + "/original.mkv"
	}
	streamURL := "/emby/Videos/" + item + "/stream?Static=true"
	if id := strings.TrimSpace(source.ID); id != "" {
		streamURL += "&MediaSourceId=" + url.QueryEscape(id)
	}
	if strings.TrimSpace(playSessionID) != "" {
		streamURL += "&PlaySessionId=" + url.QueryEscape(playSessionID)
	}
	return streamURL
}

// runPlaybackCanary validates a bounded, runtime-only sample set. A publication
// cannot be labelled healthy from one lucky title when another title resolves
// through a different media origin. Each sample remains independently bounded;
// the request carries no URLs, cookies, or persisted credentials.
func (a *Agent) runPlaybackCanary(ctx context.Context, request publicationprotocol.Request, manifest publicationprotocol.EdgeManifest) publicationprotocol.Response {
	items, err := normalizedCanaryItems(*request.PlaybackCanary)
	if err != nil {
		return publicationprotocol.Response{OK: false, ErrorCode: "playback_canary_failed", FailedStep: "canary_input_invalid",
			NOSLA: publicationprotocol.EdgeResult{Status: "synced"}, BWG: publicationprotocol.EdgeResult{Status: "synced"},
			Playback: &publicationprotocol.PlaybackCanaryResponse{Status: "failed", FailureClass: "canary_input_invalid"}}
	}
	var last *publicationprotocol.PlaybackCanaryResponse
	for index, item := range items {
		sampleRequest := request
		sampleRequest.PlaybackCanary = &publicationprotocol.PlaybackCanaryRequest{ItemID: item, AccessToken: request.PlaybackCanary.AccessToken, UserID: request.PlaybackCanary.UserID}
		if index > 0 {
			refreshed, refreshErr := a.manifestFromDatabase(ctx, sampleRequest)
			if refreshErr != nil {
				return publicationprotocol.Response{OK: false, ErrorCode: "playback_canary_failed", FailedStep: "manifest_refresh",
					NOSLA: publicationprotocol.EdgeResult{Status: "synced"}, BWG: publicationprotocol.EdgeResult{Status: "synced"},
					Playback: &publicationprotocol.PlaybackCanaryResponse{Status: "failed", FailureClass: "manifest_refresh_failed", Samples: len(items)}}
			}
			manifest = refreshed
		}
		response := a.runPlaybackCanaryOne(ctx, sampleRequest, manifest)
		if response.Playback == nil {
			response.Playback = &publicationprotocol.PlaybackCanaryResponse{Status: "failed", FailureClass: "transport_failure"}
		}
		response.Playback.Samples = len(items)
		response.Playback.SamplesPassed = index
		if !response.OK || response.Playback.Status != "healthy" {
			return response
		}
		response.Playback.SamplesPassed = index + 1
		last = response.Playback
	}
	last.Samples = len(items)
	last.SamplesPassed = len(items)
	return publicationprotocol.Response{OK: true, NOSLA: publicationprotocol.EdgeResult{Status: "synced"}, BWG: publicationprotocol.EdgeResult{Status: "synced"}, Playback: last}
}

func normalizedCanaryItems(in publicationprotocol.PlaybackCanaryRequest) ([]string, error) {
	values := append([]string(nil), in.ItemIDs...)
	if strings.TrimSpace(in.ItemID) != "" {
		values = append([]string{in.ItemID}, values...)
	}
	if len(values) == 0 || len(values) > 8 {
		return nil, errors.New("canary_input_invalid")
	}
	seen := make(map[string]bool, len(values))
	items := make([]string, 0, len(values))
	for _, value := range values {
		item := strings.TrimSpace(value)
		if item == "" || len(item) > 128 || strings.ContainsAny(item, "/?#\\\r\n") || seen[item] {
			return nil, errors.New("canary_input_invalid")
		}
		seen[item] = true
		items = append(items, item)
	}
	if strings.TrimSpace(in.AccessToken) == "" || len(in.AccessToken) > 4096 || strings.ContainsAny(in.AccessToken, "\r\n") {
		return nil, errors.New("canary_input_invalid")
	}
	return items, nil
}

func (a *Agent) runPlaybackCanaryOne(ctx context.Context, request publicationprotocol.Request, manifest publicationprotocol.EdgeManifest) publicationprotocol.Response {
	result := &publicationprotocol.PlaybackCanaryResponse{Status: "failed"}
	fail := func(class string) publicationprotocol.Response {
		result.FailureClass = class
		return publicationprotocol.Response{OK: false, ErrorCode: "playback_canary_failed", FailedStep: class,
			NOSLA: publicationprotocol.EdgeResult{Status: "synced"}, BWG: publicationprotocol.EdgeResult{Status: "synced"}, Playback: result}
	}
	if request.PlaybackCanary == nil || !validCanaryCredential(*request.PlaybackCanary) {
		return fail("canary_input_invalid")
	}
	publicRoot := "https://" + a.config.PublicMediaHost + routePublicPath(manifest.Routes[0])
	client := canaryHTTPClient(false)
	connectivity, err := canaryRequest(ctx, client, http.MethodGet, publicRoot+"/emby/System/Info/Public", request.PlaybackCanary.AccessToken, false)
	if err != nil {
		return fail(classifyTransport(err))
	}
	result.ConnectivityStatus = connectivity.StatusCode
	_ = connectivity.Body.Close()
	if connectivity.StatusCode < 200 || connectivity.StatusCode >= 300 {
		return fail(classifyHTTP(connectivity.StatusCode))
	}

	item := url.PathEscape(request.PlaybackCanary.ItemID)
	playbackInfoURL := publicRoot + "/emby/Items/" + item + "/PlaybackInfo"
	if userID := strings.TrimSpace(request.PlaybackCanary.UserID); userID != "" {
		playbackInfoURL += "?UserId=" + url.QueryEscape(userID)
	}
	playbackInfo, err := canaryRequest(ctx, client, http.MethodPost, playbackInfoURL, request.PlaybackCanary.AccessToken, false)
	if err != nil {
		return fail(classifyTransport(err))
	}
	result.PlaybackInfoStatus = playbackInfo.StatusCode
	if playbackInfo.StatusCode < 200 || playbackInfo.StatusCode >= 300 {
		_ = playbackInfo.Body.Close()
		return fail(classifyHTTP(playbackInfo.StatusCode))
	}
	var document playbackInfoDocument
	decodeErr := json.NewDecoder(io.LimitReader(playbackInfo.Body, 2<<20)).Decode(&document)
	_ = playbackInfo.Body.Close()
	if decodeErr != nil || len(document.MediaSources) == 0 {
		return fail("playbackinfo_invalid")
	}
	streamURL := playbackStreamPath(item, document.MediaSources[0], document.PlaySessionID)
	streamURL = addPlaybackQuery(streamURL, document.MediaSources[0].ID, document.PlaySessionID, request.PlaybackCanary.UserID)
	streamURL, err = canaryPublicURL(publicRoot, a.config.PublicMediaHost, streamURL)
	if err != nil {
		return fail("playbackinfo_media_url_invalid")
	}
	first, err := canaryRequest(ctx, client, http.MethodGet, streamURL, request.PlaybackCanary.AccessToken, true)
	if err != nil {
		return fail(classifyTransport(err))
	}
	result.VideoStreamStatus = first.StatusCode
	location := strings.TrimSpace(first.Header.Get("Location"))
	_ = first.Body.Close()

	oldEndpoints, err := a.loadDiscoveredEndpoints()
	if err != nil {
		return fail("redirect_store_invalid")
	}
	candidateEndpoints := cloneEndpointMap(oldEndpoints)
	oldManifest := manifest
	changedRoutes := false
	var redirects []PlaybackRedirect
	installEndpoint := func(endpoint RedirectEndpoint) *publicationprotocol.Response {
		if manifestEndpointListed(manifest.Routes, endpoint) || endpointListed(candidateEndpoints[request.RouteSlug], endpoint) {
			return nil
		}
		if len(candidateEndpoints[request.RouteSlug])+len(a.config.RedirectEndpoints[request.RouteSlug])+len(a.config.RedirectHosts[request.RouteSlug])+len(a.config.RedirectPatterns[request.RouteSlug]) >= 16 {
			response := fail("redirect_limit_exceeded")
			return &response
		}
		candidateEndpoints[request.RouteSlug] = append(candidateEndpoints[request.RouteSlug], endpoint)
		newManifest := appendManifestEndpoint(manifest, endpoint)
		if sync := a.publishUpdate(ctx, manifest, newManifest); !sync.OK {
			result.FailureClass = "edge_sync_failed"
			sync.Playback = result
			return &sync
		}
		if err := a.saveDiscoveredEndpoints(candidateEndpoints); err != nil {
			_ = a.publishUpdate(ctx, newManifest, manifest)
			response := fail("redirect_store_write_failed")
			return &response
		}
		manifest = newManifest
		changedRoutes = true
		result.EndpointsDiscovered++
		return nil
	}
	if first.StatusCode >= 300 && first.StatusCode <= 399 {
		endpoint, endpointErr := redirectEndpointFromLocation(location, a.config.PublicMediaHost)
		if endpointErr != nil || !a.endpointResolvesPublic(ctx, endpoint) {
			return fail("unknown_media_host")
		}
		redirects = append(redirects, PlaybackRedirect{Status: first.StatusCode, Location: endpointURL(endpoint)})
		if response := installEndpoint(endpoint); response != nil {
			return *response
		}
	}

	var media *http.Response
	var mediaErr error
	redirectCount := 0
	for discoveryRound := 0; discoveryRound <= 8; discoveryRound++ {
		rejectedLocation := ""
		followClient := canaryHTTPClient(true)
		followClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			redirectCount = len(via)
			if len(via) > 8 {
				return errors.New("redirect_loop")
			}
			if !canaryRedirectAllowed(req.URL, a.config.PublicMediaHost, manifest.Routes) {
				rejectedLocation = req.URL.String()
				return errors.New("redirect_not_allowlisted")
			}
			return nil
		}
		media, mediaErr = canaryRequest(ctx, followClient, http.MethodGet, streamURL, request.PlaybackCanary.AccessToken, true)
		if mediaErr == nil || rejectedLocation == "" {
			break
		}
		endpoint, endpointErr := redirectEndpointFromLocation(rejectedLocation, a.config.PublicMediaHost)
		if endpointErr != nil || !a.endpointResolvesPublic(ctx, endpoint) || manifestEndpointListed(manifest.Routes, endpoint) {
			break
		}
		redirects = append(redirects, PlaybackRedirect{Status: http.StatusFound, Location: endpointURL(endpoint)})
		if response := installEndpoint(endpoint); response != nil {
			if changedRoutes {
				_ = a.saveDiscoveredEndpoints(oldEndpoints)
				_ = a.publishUpdate(ctx, manifest, oldManifest)
			}
			return *response
		}
	}
	evidence := PlaybackCanaryEvidence{ConnectivityStatus: result.ConnectivityStatus, PlaybackInfoStatus: result.PlaybackInfoStatus,
		VideoStreamStatus: result.VideoStreamStatus, Redirects: redirects}
	if mediaErr == nil {
		result.MediaStatus = media.StatusCode
		result.RedirectsFollowed = redirectCount
		result.ContentRange = strings.TrimSpace(media.Header.Get("Content-Range")) != ""
		result.AcceptRanges = strings.Contains(strings.ToLower(media.Header.Get("Accept-Ranges")), "bytes")
		read, _ := io.CopyN(io.Discard, media.Body, 64<<10)
		_ = media.Body.Close()
		result.BytesRead = read
		result.ByteGrowth = read >= 16<<10
		evidence.Media = PlaybackMediaResult{StatusCode: result.MediaStatus, BytesRead: read, GrowthBytes: read,
			ContentRange: result.ContentRange, AcceptRanges: result.AcceptRanges, RedirectsFollowed: redirectCount}
	} else {
		evidence.Media = PlaybackMediaResult{}
	}
	validated, validationErr := ValidatePlaybackCanary(evidence)
	if mediaErr != nil || validationErr != nil {
		// Keep endpoints that were observed and safely synchronized even when a
		// sample ultimately fails upstream (for example media host Y returns 403).
		// The publication remains playback failed/unverified, but a later canary
		// can reuse the exact scoped route without widening it manually.
		if mediaErr != nil {
			return fail(classifyTransport(mediaErr))
		}
		return fail(validated.FailureClass)
	}
	result.Status = "healthy"
	result.FailureClass = ""
	return publicationprotocol.Response{OK: true, NOSLA: publicationprotocol.EdgeResult{Status: "synced"}, BWG: publicationprotocol.EdgeResult{Status: "synced"}, Playback: result}
}

func addPlaybackQuery(raw, mediaSourceID, playSessionID, userID string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	if strings.TrimSpace(mediaSourceID) != "" {
		q.Set("MediaSourceId", mediaSourceID)
	}
	if strings.TrimSpace(playSessionID) != "" {
		q.Set("PlaySessionId", playSessionID)
	}
	if strings.TrimSpace(userID) != "" {
		q.Set("UserId", userID)
	}
	// This upstream requires a stable client device identifier to create its
	// remote-media redirect. It is a non-secret constant, not a user device ID.
	q.Set("DeviceId", "embyproxy-canary")
	u.RawQuery = q.Encode()
	return u.String()
}

func validCanaryCredential(in publicationprotocol.PlaybackCanaryRequest) bool {
	item, token := strings.TrimSpace(in.ItemID), strings.TrimSpace(in.AccessToken)
	if item == "" || len(item) > 128 || token == "" || len(token) > 4096 || strings.ContainsAny(item, "/?#\\\r\n") || strings.ContainsAny(token, "\r\n") || len(in.UserID) > 128 || strings.ContainsAny(in.UserID, "\r\n") {
		return false
	}
	return true
}

func canaryHTTPClient(follow bool) *http.Client {
	client := &http.Client{Timeout: 25 * time.Second}
	if !follow {
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	}
	return client
}

func canaryRequest(ctx context.Context, client *http.Client, method, target, token string, ranged bool) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "embyproxy-playback-canary/1.0")
	req.Header.Set("X-Emby-Token", token)
	// Give the upstream a stable, non-user device identity. Some Emby forks
	// require this to bind a token to a playback session before producing a
	// remote-media redirect.
	req.Header.Set("X-Emby-Authorization", `Emby Client="EmbyProxy", Device="EmbyProxy", DeviceId="embyproxy-canary", Version="1.0"`)
	if ranged {
		req.Header.Set("Range", "bytes=0-65535")
	}
	return client.Do(req)
}

func canaryPublicURL(publicRoot, publicHost, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if !u.IsAbs() {
		// PlaybackInfo normally returns an upstream-relative path. Some proxy
		// versions return an already encoded public path instead; do not prefix
		// the slug a second time in that case.
		if strings.HasPrefix(raw, "/") {
			base, baseErr := url.Parse(publicRoot)
			if baseErr != nil {
				return "", baseErr
			}
			if strings.HasPrefix(u.EscapedPath(), base.EscapedPath()+"/") || u.EscapedPath() == base.EscapedPath() {
				u.Scheme, u.Host = base.Scheme, base.Host
			} else {
				u, err = url.Parse(strings.TrimRight(publicRoot, "/") + raw)
			}
		} else {
			u, err = url.Parse(strings.TrimRight(publicRoot, "/") + "/" + raw)
		}
		if err != nil {
			return "", err
		}
	} else if !strings.EqualFold(u.Hostname(), publicHost) {
		base, _ := url.Parse(publicRoot)
		u.Scheme, u.Host = base.Scheme, base.Host
		u.Path = strings.TrimRight(base.Path, "/") + "/" + strings.TrimLeft(u.Path, "/")
	}
	if u.Scheme != "https" || !strings.EqualFold(u.Hostname(), publicHost) || u.User != nil {
		return "", errors.New("invalid public media URL")
	}
	// The proxy may return a runtime-only signed media URL. Preserve its query
	// for the request itself; this value is never logged, persisted, or returned
	// from the canary API.
	return u.String(), nil
}

func redirectEndpointFromLocation(raw, publicHost string) (RedirectEndpoint, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !u.IsAbs() || u.User != nil {
		return RedirectEndpoint{}, errors.New("redirect_location_invalid")
	}
	if strings.EqualFold(u.Hostname(), publicHost) {
		parts := strings.Split(strings.Trim(u.EscapedPath(), "/"), "/")
		if len(parts) < 3 || (parts[0] != "http" && parts[0] != "https") {
			return RedirectEndpoint{}, errors.New("redirect_location_invalid")
		}
		port, parseErr := strconv.Atoi(parts[2])
		pathPrefix := ""
		if len(parts) > 3 {
			pathPrefix = redirectPathPrefix("/" + parts[3])
			if pathPrefix == "" {
				return RedirectEndpoint{}, errors.New("redirect_location_invalid")
			}
		}
		endpoint := RedirectEndpoint{Scheme: parts[0], Host: strings.ToLower(parts[1]), Port: port, PathPrefix: pathPrefix}
		if parseErr != nil || !safeRedirectEndpoint(endpoint, publicHost, "invalid.local") {
			return RedirectEndpoint{}, errors.New("redirect_location_invalid")
		}
		return endpoint, nil
	}
	port := 443
	if u.Scheme == "http" {
		port = 80
	}
	if u.Port() != "" {
		port, err = strconv.Atoi(u.Port())
	}
	pathPrefix := redirectPathPrefix(u.EscapedPath())
	if strings.Trim(u.EscapedPath(), "/") != "" && pathPrefix == "" {
		return RedirectEndpoint{}, errors.New("redirect_location_invalid")
	}
	endpoint := RedirectEndpoint{Scheme: strings.ToLower(u.Scheme), Host: strings.ToLower(u.Hostname()), Port: port, PathPrefix: pathPrefix}
	if err != nil || !safeRedirectEndpoint(endpoint, publicHost, "invalid.local") {
		return RedirectEndpoint{}, errors.New("redirect_location_invalid")
	}
	return endpoint, nil
}

func (a *Agent) endpointResolvesPublic(ctx context.Context, endpoint RedirectEndpoint) bool {
	if ip := net.ParseIP(endpoint.Host); ip != nil {
		return safeRedirectHost(endpoint.Host)
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	addresses, err := net.DefaultResolver.LookupIPAddr(lookupCtx, endpoint.Host)
	if err != nil || len(addresses) == 0 {
		return false
	}
	for _, address := range addresses {
		ip := address.IP
		if ip.IsUnspecified() || ip.IsLoopback() || ip.IsPrivate() || ip.IsMulticast() || ip.IsLinkLocalUnicast() {
			return false
		}
	}
	return true
}

func routePublicPath(route publicationprotocol.EdgeRoute) string {
	value := "/" + route.Scheme + "/" + route.Host + "/" + strconv.Itoa(route.Port)
	if route.BasePath != "" {
		value += "/" + route.BasePath
	}
	return value
}

func endpointURL(endpoint RedirectEndpoint) string {
	value := endpoint.Scheme + "://" + endpoint.Host + ":" + strconv.Itoa(endpoint.Port) + "/"
	if endpoint.PathPrefix != "" {
		value += endpoint.PathPrefix
	}
	return value
}

func appendManifestEndpoint(manifest publicationprotocol.EdgeManifest, endpoint RedirectEndpoint) publicationprotocol.EdgeManifest {
	manifest.Action = publicationprotocol.ActionPublish
	manifest.Routes = append(append([]publicationprotocol.EdgeRoute(nil), manifest.Routes...), publicationprotocol.EdgeRoute{
		LineID: "redirect-canary-" + strconv.Itoa(len(manifest.Routes)+1), Scheme: endpoint.Scheme, Host: endpoint.Host,
		Port: endpoint.Port, BasePath: endpoint.PathPrefix, Kind: "redirect", Position: len(manifest.Routes) + 1,
	})
	return manifest
}

func (a *Agent) publishUpdate(ctx context.Context, oldManifest, nextManifest publicationprotocol.EdgeManifest) publicationprotocol.Response {
	nextManifest.Action = publicationprotocol.ActionPublish
	oldManifest.Action = publicationprotocol.ActionPublish
	bwg := a.invokeLocal(ctx, nextManifest)
	if bwg.Status != "synced" {
		return combineEdges(publicationprotocol.ActionPublish, publicationprotocol.EdgeResult{Status: "not_attempted"}, bwg)
	}
	nosla := a.invokeRemote(ctx, nextManifest)
	if nosla.Status == "synced" {
		return combineEdges(publicationprotocol.ActionPublish, nosla, bwg)
	}
	rollback := a.invokeLocal(ctx, oldManifest)
	if rollback.Status != "synced" {
		bwg = publicationprotocol.EdgeResult{Status: "failed", ErrorCode: "rollback_failed", FailedStep: "bwg_rollback", BackupPath: rollback.BackupPath}
	}
	return combineEdges(publicationprotocol.ActionPublish, nosla, bwg)
}

func canaryRedirectAllowed(u *url.URL, publicHost string, routes []publicationprotocol.EdgeRoute) bool {
	if !strings.EqualFold(u.Hostname(), publicHost) || u.Scheme != "https" {
		return false
	}
	path := u.EscapedPath()
	for _, route := range routes {
		prefix := routePublicPath(route)
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}

func (a *Agent) loadDiscoveredEndpoints() (map[string][]RedirectEndpoint, error) {
	result := map[string][]RedirectEndpoint{}
	raw, err := os.ReadFile(a.config.DiscoveredEndpointsPath)
	if os.IsNotExist(err) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	for slug, endpoints := range result {
		if len(endpoints) > 16 {
			return nil, errors.New("redirect_store_invalid")
		}
		for _, endpoint := range endpoints {
			if !safeRedirectEndpoint(endpoint, a.config.PublicMediaHost, a.config.OwnerAdminHost) {
				return nil, errors.New("redirect_store_invalid")
			}
		}
		if proxyadapter.ValidateManagedRouteSlug(slug) != nil {
			return nil, errors.New("redirect_store_invalid")
		}
	}
	return result, nil
}

func (a *Agent) saveDiscoveredEndpoints(endpoints map[string][]RedirectEndpoint) error {
	directory := filepath.Dir(a.config.DiscoveredEndpointsPath)
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(endpoints, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".redirect-endpoints-*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(raw); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, a.config.DiscoveredEndpointsPath)
}

func cloneEndpointMap(source map[string][]RedirectEndpoint) map[string][]RedirectEndpoint {
	result := make(map[string][]RedirectEndpoint, len(source))
	for slug, endpoints := range source {
		result[slug] = append([]RedirectEndpoint(nil), endpoints...)
	}
	return result
}

func endpointListed(endpoints []RedirectEndpoint, candidate RedirectEndpoint) bool {
	for _, endpoint := range endpoints {
		if endpoint.Scheme == candidate.Scheme && strings.EqualFold(endpoint.Host, candidate.Host) && endpoint.Port == candidate.Port && endpoint.PathPrefix == candidate.PathPrefix {
			return true
		}
	}
	return false
}

func manifestEndpointListed(routes []publicationprotocol.EdgeRoute, candidate RedirectEndpoint) bool {
	for _, route := range routes {
		if route.Kind == "redirect" && route.Scheme == candidate.Scheme && strings.EqualFold(route.Host, candidate.Host) && route.Port == candidate.Port && route.BasePath == candidate.PathPrefix {
			return true
		}
	}
	return false
}

func redirectPathPrefix(escapedPath string) string {
	parts := strings.Split(strings.Trim(escapedPath, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		return ""
	}
	value, err := url.PathUnescape(parts[0])
	if err != nil || !safeRedirectPathPrefix(value) {
		return ""
	}
	return value
}

func classifyTransport(err error) string {
	if errors.Is(err, context.DeadlineExceeded) || strings.Contains(strings.ToLower(err.Error()), "timeout") {
		return "timeout"
	}
	if strings.Contains(strings.ToLower(err.Error()), "redirect") {
		return "redirect_loop_or_unknown"
	}
	return "transport_failure"
}

func classifyHTTP(status int) string {
	switch status {
	case 401:
		return "upstream_401"
	case 403:
		return "upstream_403"
	case 404:
		return "upstream_404"
	case 429:
		return "upstream_429"
	case 0:
		return "transport_failure"
	default:
		if status >= 500 {
			return "timeout_or_upstream_5xx"
		}
		return "http_status_" + strconv.Itoa(status)
	}
}
