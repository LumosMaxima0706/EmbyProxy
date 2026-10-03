package admin

import (
	"context"
	"crypto/tls"
	"embyproxy/internal/storage"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type proxyNodeReadiness struct {
	RouteResults map[string]string `json:"route_results,omitempty"`
	CheckedAt    int64             `json:"checked_at"`
	BusinessTLS  string            `json:"business_tls"`
	Routes       string            `json:"routes"`
	Playback     string            `json:"playback"`
	Error        string            `json:"error,omitempty"`
	Origin       string            `json:"origin"`
	AgentCommit  string            `json:"agent_commit"`
}

func (h *Handler) proxyNodeReadinessViews(ctx context.Context, nodes []storage.ProxyNode) map[string]proxyNodeReadiness {
	views := map[string]proxyNodeReadiness{}
	for _, node := range nodes {
		var view proxyNodeReadiness
		found, err := h.store.KV().GetJSON(ctx, "node-readiness:"+node.ID, &view)
		if err != nil || !found {
			continue
		}
		if view.CheckedAt < time.Now().Add(-15*time.Minute).Unix() || view.Origin != node.PublicAddress || view.AgentCommit != node.AgentCommit {
			view.BusinessTLS, view.Routes, view.Playback = "stale", "stale", "stale"
		}
		views[node.ID] = view
	}
	return views
}

// Direct business-SNI probes never change scheduling, DNS or publication state.
func (h *Handler) verifyProxyNodeReadiness(w http.ResponseWriter, r *http.Request, id string) {
	node, err := h.store.GetProxyNode(r.Context(), id)
	if err != nil || node == nil || node.State == "removed" || node.State == "revoked" {
		http.NotFound(w, r)
		return
	}
	if h.publicIngress == nil {
		writeJSON(w, 503, map[string]any{"ok": false, "error": "PUBLIC_INGRESS_UNAVAILABLE"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	view := proxyNodeReadiness{CheckedAt: time.Now().Unix(), BusinessTLS: "unverified", Routes: "unverified", Playback: "unverified", Origin: node.PublicAddress, AgentCommit: node.AgentCommit}
	defer func() {
		if err := h.store.KV().Put(context.WithoutCancel(r.Context()), "node-readiness:"+id, view); err != nil {
			writeJSON(w, 500, map[string]any{"ok": false, "error": "READINESS_SAVE_FAILED"})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "readiness": view})
	}()
	ip, err := h.publicIngress.proxyNodeIPv4(ctx, *node)
	if err != nil {
		view.Error = "target_address_invalid"
		return
	}
	host := h.publicIngress.record
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := &http.Transport{TLSClientConfig: &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}, DialContext: func(c context.Context, network, _ string) (net.Conn, error) {
		return dialer.DialContext(c, network, net.JoinHostPort(ip.String(), "443"))
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 15 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	root := "https://" + host
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, root+"/health", nil)
	resp, err := client.Do(req)
	if err != nil {
		view.BusinessTLS = "failed"
		view.Error = "business_tls_or_transport_failed"
		return
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("X-EmbyProxy-Node-Id") != id {
		view.BusinessTLS = "failed"
		view.Error = "node_identity_or_health_failed"
		return
	}
	view.BusinessTLS = "ready"
	routes, err := h.store.ListManagedRoutes(ctx)
	if err != nil {
		view.Error = "route_list_failed"
		return
	}
	tested := 0
	allPlayed := true
	allRoutes := true
	view.RouteResults = map[string]string{}
	for _, route := range routes {
		if !route.Enabled || !route.Public {
			continue
		}
		tested++
		view.RouteResults[route.Slug] = "unverified"
		target := root + "/s/" + url.PathEscape(route.Slug)
		info, _ := http.NewRequestWithContext(ctx, http.MethodGet, target+"/emby/System/Info/Public", nil)
		res, e := client.Do(info)
		if e != nil {
			view.Routes = "failed"
			view.Error = "public_route_unreachable"
			view.RouteResults[route.Slug] = view.Error
			allPlayed, allRoutes = false, false
			continue
		}
		io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
		res.Body.Close()
		if res.StatusCode != 200 {
			view.Routes = "failed"
			view.Error = "public_route_failed"
			view.RouteResults[route.Slug] = view.Error
			allPlayed, allRoutes = false, false
			continue
		}
		if h.playbackCredentials == nil || !h.playbackCredentials.PlaybackCredentialConfigured(ctx, route.Slug) {
			allPlayed = false
			view.Error = "playback_credential_missing"
			continue
		}
		token, e := h.playbackCredentials.ReadPlaybackCredential(ctx, route.Slug)
		if e != nil {
			allPlayed = false
			view.Error = "playback_credential_missing"
			continue
		}
		items, e := discoverEmbyPlaybackItems(ctx, target, token, client)
		if e != nil {
			allPlayed = false
			view.Playback = "failed"
			view.Error = e.Error()
			view.RouteResults[route.Slug] = view.Error
			continue
		}
		view.RouteResults[route.Slug] = "ready"
		for _, item := range items[:2] {
			if e := probeNodeMedia(ctx, client, target, item, token); e != nil {
				allPlayed = false
				view.Playback = "failed"
				view.Error = e.Error()
				view.RouteResults[route.Slug] = view.Error
				break
			}
		}
	}
	if tested == 0 {
		view.Error = "public_routes_unconfigured"
		return
	}
	if allRoutes {
		view.Routes = "ready"
	}
	if allPlayed {
		view.Playback = "ready"
	}
}

func probeNodeMedia(ctx context.Context, client *http.Client, root, item, token string) error {
	userID, _ := resolveEmbyPlaybackUserID(ctx, root, token, client)
	infoURL := root + "/emby/Items/" + url.PathEscape(item) + "/PlaybackInfo"
	if userID != "" {
		infoURL += "?UserId=" + url.QueryEscape(userID)
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, infoURL, strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Emby-Token", token)
	req.Header.Set("X-Emby-Authorization", `Emby Client="EmbyProxy", Device="EmbyProxy", DeviceId="embyproxy-canary", Version="1.0"`)
	res, err := client.Do(req)
	if err != nil {
		return errors.New("playbackinfo_unreachable")
	}
	var info struct {
		PlaySessionID string
		MediaSources  []struct {
			ID              string
			DirectStreamURL string
			IsRemote        bool
			Protocol        string
		}
	}
	decode := json.NewDecoder(io.LimitReader(res.Body, 2<<20)).Decode(&info)
	res.Body.Close()
	if res.StatusCode != 200 || decode != nil || len(info.MediaSources) == 0 {
		return errors.New("playbackinfo_failed")
	}
	source := info.MediaSources[0]
	query := url.Values{"Static": {"true"}, "MediaSourceId": {source.ID}, "PlaySessionId": {info.PlaySessionID}}
	if userID != "" {
		query.Set("UserId", userID)
	}
	candidates := []string{source.DirectStreamURL, "/emby/Videos/" + url.PathEscape(item) + "/stream?" + query.Encode(), "/emby/Videos/" + url.PathEscape(item) + "/stream.mkv?" + query.Encode()}
	if source.IsRemote && (strings.EqualFold(source.Protocol, "http") || strings.EqualFold(source.Protocol, "https")) {
		candidates = append(candidates, "/emby/videos/"+url.PathEscape(item)+"/original.mkv?"+query.Encode())
	}
	base, _ := url.Parse(root)
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		path, e := url.Parse(candidate)
		if e != nil || path.IsAbs() || path.Host != "" {
			continue
		}
		full := *base
		if strings.HasPrefix(path.Path, "/s/") {
			full.Path = path.Path
		} else {
			full.Path = strings.TrimRight(base.Path, "/") + "/" + strings.TrimLeft(path.Path, "/")
		}
		full.RawQuery = path.RawQuery
		for hop := 0; hop < 5; hop++ {
			media, _ := http.NewRequestWithContext(ctx, http.MethodGet, full.String(), nil)
			media.Header.Set("X-Emby-Token", token)
			media.Header.Set("Range", "bytes=0-1023")
			response, e := client.Do(media)
			if e != nil {
				break
			}
			if response.StatusCode >= 300 && response.StatusCode < 400 {
				next, e := full.Parse(response.Header.Get("Location"))
				response.Body.Close()
				if e != nil || next.Scheme != "https" || next.Host != base.Host || !strings.HasPrefix(next.Path, strings.TrimRight(base.Path, "/")+"/") {
					break
				}
				full = *next
				continue
			}
			data, e := io.ReadAll(io.LimitReader(response.Body, 1025))
			response.Body.Close()
			if e == nil && response.StatusCode == 206 && strings.HasPrefix(response.Header.Get("Content-Range"), "bytes 0-") && len(data) == 1024 {
				return nil
			}
			break
		}
	}
	return errors.New("media_range_failed")
}
