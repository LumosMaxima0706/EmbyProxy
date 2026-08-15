package admin

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"embyproxy/internal/proxyadapter"
	"embyproxy/internal/storage"
	"embyproxy/internal/validators"
)

// PublicationPlan contains only safe, operator-facing metadata. TargetURL is
// deliberately excluded from JSON because it is used internally by a syncer.
type PublicationPlan struct {
	NodeName          string   `json:"node_name"`
	RouteSlug         string   `json:"route_slug"`
	PublicPathShape   string   `json:"public_path"`
	PublicURLShape    string   `json:"public_url"`
	UpstreamScheme    string   `json:"upstream_scheme"`
	UpstreamHostShape string   `json:"upstream_host"`
	UpstreamPort      int      `json:"upstream_port"`
	HasBasePath       bool     `json:"has_base_path"`
	Changes           []string `json:"changes"`
	PublicPath        string   `json:"-"`
	PublicURL         string   `json:"-"`
	UpstreamHost      string   `json:"-"`
	BasePath          string   `json:"-"`
	TargetURL         string   `json:"-"`
}

type PublicationEdgeResult struct {
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

type PublicationSyncResult struct {
	NOSLA      PublicationEdgeResult
	BWG        PublicationEdgeResult
	FailedStep string
	Reason     string
}

// PublicationSyncer is the boundary to the privileged BWG/NOSLA publish
// bridge. The sidecar intentionally has no default implementation: without a
// configured bridge, publish fails closed instead of claiming a route exists.
type PublicationSyncer interface {
	Publish(context.Context, PublicationPlan) (PublicationSyncResult, error)
	Unpublish(context.Context, PublicationPlan) (PublicationSyncResult, error)
}

type publicationStatusView struct {
	Status      string           `json:"status"`
	Reason      string           `json:"reason,omitempty"`
	FailedStep  string           `json:"failed_step,omitempty"`
	PublicURL   string           `json:"public_url,omitempty"`
	RouteSlug   string           `json:"route_slug,omitempty"`
	NOSLAStatus string           `json:"nosla_status"`
	BWGStatus   string           `json:"bwg_status"`
	Managed     bool             `json:"managed"`
	Plan        *PublicationPlan `json:"plan,omitempty"`
}

func (h *Handler) SetPublicationSyncer(syncer PublicationSyncer) {
	h.publicationSyncer = syncer
}

func (h *Handler) buildPublicationPlan(ctx context.Context, uid, name string) (PublicationPlan, error) {
	name = validators.NormalizeName(name)
	if !validators.NameRE.MatchString(name) {
		return PublicationPlan{}, errors.New("INVALID_EMBY_SERVER_SLUG")
	}
	node, err := h.store.GetNode(ctx, uid, name)
	if err != nil {
		return PublicationPlan{}, err
	}
	if node == nil {
		return PublicationPlan{}, errors.New("EMBY_SERVER_NOT_FOUND")
	}
	targets := storage.SplitTargets(node.Target)
	if len(targets) != 1 {
		return PublicationPlan{}, errors.New("PUBLISH_REQUIRES_ONE_SAVED_UPSTREAM")
	}
	target := strings.TrimSpace(targets[0])
	u, err := url.Parse(target)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return PublicationPlan{}, errors.New("PUBLISH_TARGET_MUST_BE_A_SINGLE_HTTPS_URL_WITHOUT_CREDENTIALS")
	}
	if strings.EqualFold(u.Hostname(), h.cfg.OwnerAdminHost) || strings.EqualFold(u.Hostname(), "owner-admin.149077530.xyz") {
		return PublicationPlan{}, errors.New("PUBLISH_TARGET_CANNOT_BE_OWNER_ADMIN")
	}
	if strings.EqualFold(u.Hostname(), "localhost") {
		return PublicationPlan{}, errors.New("PUBLISH_TARGET_MUST_BE_PUBLIC")
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsMulticast()) {
		return PublicationPlan{}, errors.New("PUBLISH_TARGET_MUST_BE_PUBLIC")
	}
	if err := proxyadapter.ValidateManagedRouteSlug(name); err != nil {
		return PublicationPlan{}, errors.New("INVALID_ROUTE_SLUG")
	}
	port := 443
	if u.Port() != "" {
		parsedPort, err := strconv.Atoi(u.Port())
		if err != nil || parsedPort < 1 || parsedPort > 65535 {
			return PublicationPlan{}, errors.New("INVALID_UPSTREAM_PORT")
		}
		port = parsedPort
	}
	baseURL := strings.TrimSuffix(h.cfg.PublicMediaBaseURL, "/")
	if baseURL == "" {
		return PublicationPlan{}, errors.New("PUBLIC_MEDIA_BASE_URL_NOT_CONFIGURED")
	}
	publicBase, _ := url.Parse(baseURL)
	if strings.EqualFold(u.Hostname(), publicBase.Hostname()) {
		return PublicationPlan{}, errors.New("PUBLISH_TARGET_CANNOT_BE_PUBLIC_ENTRY")
	}
	upstreamHost := strings.ToLower(u.Hostname())
	basePath := strings.Trim(path.Clean(u.EscapedPath()), "/.")
	publicPath := "/https/" + upstreamHost + "/" + formatPort(port)
	publicPathShape := "/https/<saved-host>/" + formatPort(port)
	if basePath != "" {
		publicPath += "/" + basePath
		publicPathShape += "/<saved-base-path>"
	}
	return PublicationPlan{
		NodeName: name, RouteSlug: name,
		PublicPathShape: publicPathShape,
		PublicURLShape:  baseURL + publicPathShape,
		UpstreamScheme:  "https", UpstreamHostShape: "<saved-host>", UpstreamPort: port,
		HasBasePath: basePath != "", PublicPath: publicPath, PublicURL: baseURL + publicPath,
		UpstreamHost: upstreamHost, BasePath: basePath, Changes: []string{
			"managed_route", "managed_route_line", "public_media_mapping",
			"bwg_edge_route", "nosla_edge_route", "redirect_host_allowlist",
		}, TargetURL: target,
	}, nil
}

func formatPort(port int) string {
	if port <= 0 {
		return "443"
	}
	return strconv.Itoa(port)
}

func (h *Handler) publicationStatus(ctx context.Context, uid, name string) (publicationStatusView, error) {
	name = validators.NormalizeName(name)
	p, err := h.store.GetPublication(ctx, uid, name)
	if err != nil {
		return publicationStatusView{}, err
	}
	if p != nil {
		return publicationStatusView{
			Status: p.Status, Reason: p.Reason, FailedStep: p.FailedStep,
			PublicURL: p.PublicURL, RouteSlug: p.RouteSlug,
			NOSLAStatus: p.NOSLAStatus, BWGStatus: p.BWGStatus, Managed: true,
		}, nil
	}
	legacy := h.publicNodeURLs()[name]
	if legacy != "" {
		return publicationStatusView{
			Status: storage.PublicationPublished, Reason: "public_entry_configured",
			PublicURL: legacy, NOSLAStatus: "synced", BWGStatus: "synced",
		}, nil
	}
	return publicationStatusView{
		Status: storage.PublicationSavedUnpublished, Reason: "no_edge_route_configured",
		NOSLAStatus: "not_configured", BWGStatus: "not_configured",
	}, nil
}

func publicationRecord(uid string, plan PublicationPlan, status, reason, step string, sync PublicationSyncResult) storage.Publication {
	if sync.NOSLA.Status == "" {
		sync.NOSLA.Status = "unknown"
	}
	if sync.BWG.Status == "" {
		sync.BWG.Status = "unknown"
	}
	return storage.Publication{
		UID: uid, NodeName: plan.NodeName, RouteSlug: plan.RouteSlug, PublicURL: "",
		Status: status, Reason: reason, FailedStep: step,
		NOSLAStatus: sync.NOSLA.Status, BWGStatus: sync.BWG.Status, UpdatedAt: time.Now().Unix(),
	}
}

func publicationEdgesRemoved(sync PublicationSyncResult) bool {
	clean := func(status string) bool {
		return status == "removed" || status == "not_configured"
	}
	return clean(sync.NOSLA.Status) && clean(sync.BWG.Status)
}

func (h *Handler) managedRouteAvailable(ctx context.Context, plan PublicationPlan) (bool, error) {
	routes, err := h.store.ListManagedRoutes(ctx)
	if err != nil {
		return false, err
	}
	for _, route := range routes {
		if route.Slug == plan.RouteSlug || route.NodeName == plan.NodeName {
			return false, nil
		}
	}
	return true, nil
}

func (h *Handler) rollbackFailedPublish(ctx context.Context, uid string, plan PublicationPlan, publishResult PublicationSyncResult) storage.Publication {
	reason := publishResult.Reason
	if reason == "" {
		reason = "edge_sync_failed"
	}
	failedStep := publishResult.FailedStep
	if failedStep == "" {
		failedStep = "edge_sync"
	}
	cleanup, cleanupErr := h.publicationSyncer.Unpublish(ctx, plan)
	routeErr := h.store.DeleteManagedRoute(ctx, plan.RouteSlug)
	if cleanupErr == nil && routeErr == nil && publicationEdgesRemoved(cleanup) {
		return publicationRecord(uid, plan, storage.PublicationFailed, reason, failedStep, cleanup)
	}
	if cleanup.Reason == "" {
		cleanup.Reason = "publication_rollback_failed"
	}
	if cleanup.FailedStep == "" {
		cleanup.FailedStep = "edge_rollback"
	}
	if routeErr != nil {
		cleanup.Reason = "managed_route_rollback_failed"
		cleanup.FailedStep = "db_rollback"
	}
	return publicationRecord(uid, plan, storage.PublicationNeedsSync, cleanup.Reason, cleanup.FailedStep, cleanup)
}

func (h *Handler) handlePublicationAPI(w http.ResponseWriter, r *http.Request, path string) {
	const prefix = "/api/admin/emby-servers/"
	suffix := strings.TrimPrefix(path, prefix)
	parts := strings.Split(strings.Trim(suffix, "/"), "/")
	if len(parts) < 2 || len(parts) > 3 || !validators.NameRE.MatchString(parts[0]) {
		http.NotFound(w, r)
		return
	}
	name, action := parts[0], parts[1]
	if len(parts) == 3 && action == "publish" && parts[2] == "dry-run" {
		action = "publish/dry-run"
	} else if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	uid := "admin"
	ctx := r.Context()
	plan, planErr := h.buildPublicationPlan(ctx, uid, name)
	if action == "publish-status" {
		status, err := h.publicationStatus(ctx, uid, name)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "PUBLICATION_STATUS_FAILED"})
			return
		}
		if planErr == nil {
			status.Plan = &plan
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "publication": status})
		return
	}
	if r.Method != http.MethodPost || (action != "publish" && action != "unpublish" && action != "publish/dry-run" && action != "verify-proxy") {
		w.Header().Set("Allow", "GET, POST")
		http.NotFound(w, r)
		return
	}
	if action == "publish" || action == "unpublish" {
		h.publicationMu.Lock()
		defer h.publicationMu.Unlock()
	}
	if action == "publish" && r.Method == http.MethodPost {
		if planErr != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "status": storage.PublicationFailed, "reason": planErr.Error(), "failed_step": "plan"})
			return
		}
		current, err := h.publicationStatus(ctx, uid, name)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "status": storage.PublicationFailed, "reason": "PUBLICATION_STATUS_FAILED", "failed_step": "state_read"})
			return
		}
		if current.Status == storage.PublicationPublished {
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": current.Status, "publication": current})
			return
		}
		if current.Status == storage.PublicationPublishing || current.Status == storage.PublicationUnpublishing || current.Status == storage.PublicationNeedsSync {
			writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "status": current.Status, "reason": "PUBLICATION_REQUIRES_RECONCILIATION", "failed_step": "state_guard"})
			return
		}
		available, err := h.managedRouteAvailable(ctx, plan)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "status": storage.PublicationFailed, "reason": "MANAGED_ROUTE_LOOKUP_FAILED", "failed_step": "db_read"})
			return
		}
		if !available {
			writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "status": storage.PublicationNeedsSync, "reason": "MANAGED_ROUTE_REQUIRES_RECONCILIATION", "failed_step": "managed_route"})
			return
		}
		pending := publicationRecord(uid, plan, storage.PublicationPublishing, "sync_in_progress", "", PublicationSyncResult{NOSLA: PublicationEdgeResult{Status: "pending"}, BWG: PublicationEdgeResult{Status: "pending"}})
		if err := h.store.SavePublication(ctx, pending); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "status": storage.PublicationFailed, "reason": "PUBLICATION_STATE_WRITE_FAILED", "failed_step": "db_write"})
			return
		}
		if h.publicationSyncer == nil {
			failed := publicationRecord(uid, plan, storage.PublicationFailed, "edge_sync_unavailable", "edge_sync", PublicationSyncResult{NOSLA: PublicationEdgeResult{Status: "not_configured"}, BWG: PublicationEdgeResult{Status: "not_configured"}})
			_ = h.store.SavePublication(ctx, failed)
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "status": failed.Status, "reason": failed.Reason, "failed_step": failed.FailedStep, "publication": failed})
			return
		}
		stagedRoute := storage.ManagedRoute{Slug: plan.RouteSlug, NodeName: plan.NodeName, Enabled: false, Public: false, DefaultLine: "main"}
		stagedLines := []storage.ManagedRouteLine{{RouteSlug: plan.RouteSlug, LineSlug: "main", Target: plan.TargetURL, Enabled: true, Position: 1}}
		if err := h.store.SaveManagedRoute(ctx, stagedRoute, stagedLines); err != nil {
			failed := publicationRecord(uid, plan, storage.PublicationFailed, "managed_route_write_failed", "db_write", PublicationSyncResult{NOSLA: PublicationEdgeResult{Status: "pending"}, BWG: PublicationEdgeResult{Status: "pending"}})
			_ = h.store.SavePublication(ctx, failed)
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "status": failed.Status, "reason": failed.Reason, "failed_step": failed.FailedStep, "publication": failed})
			return
		}
		result, err := h.publicationSyncer.Publish(ctx, plan)
		if err != nil || result.NOSLA.Status != "synced" || result.BWG.Status != "synced" {
			failed := h.rollbackFailedPublish(ctx, uid, plan, result)
			_ = h.store.SavePublication(ctx, failed)
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "status": failed.Status, "reason": failed.Reason, "failed_step": failed.FailedStep, "publication": failed})
			return
		}
		publishedRoute := storage.ManagedRoute{Slug: plan.RouteSlug, NodeName: plan.NodeName, Enabled: true, Public: true, DefaultLine: "main"}
		if err := h.store.SaveManagedRoute(ctx, publishedRoute, stagedLines); err != nil {
			result.Reason = "managed_route_commit_failed"
			result.FailedStep = "db_write"
			failed := h.rollbackFailedPublish(ctx, uid, plan, result)
			_ = h.store.SavePublication(ctx, failed)
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "status": failed.Status, "reason": failed.Reason, "failed_step": failed.FailedStep, "publication": failed})
			return
		}
		published := publicationRecord(uid, plan, storage.PublicationPublished, "public_entry_configured", "", result)
		published.PublicURL = plan.PublicURL
		if err := h.store.SavePublication(ctx, published); err != nil {
			result.Reason = "PUBLICATION_STATE_WRITE_FAILED"
			result.FailedStep = "db_write"
			failed := h.rollbackFailedPublish(ctx, uid, plan, result)
			_ = h.store.SavePublication(ctx, failed)
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "status": failed.Status, "reason": failed.Reason, "failed_step": failed.FailedStep, "publication": failed})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": published.Status, "publication": published})
		return
	}
	if action == "publish/dry-run" {
		if planErr != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "status": storage.PublicationFailed, "reason": planErr.Error(), "failed_step": "plan"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "dry_run": true, "plan": plan})
		return
	}
	if action == "verify-proxy" {
		status, err := h.publicationStatus(ctx, uid, name)
		if err != nil || status.Status != storage.PublicationPublished || status.PublicURL == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "PUBLIC_ENTRY_NOT_PUBLISHED"})
			return
		}
		publicBase, _ := url.Parse(h.cfg.PublicMediaBaseURL)
		checkURL, err := url.Parse(strings.TrimSuffix(status.PublicURL, "/") + "/System/Info/Public")
		if err != nil || checkURL.Scheme != "https" || !strings.EqualFold(checkURL.Host, publicBase.Host) || checkURL.RawQuery != "" || checkURL.Fragment != "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "INVALID_PUBLIC_ENTRY"})
			return
		}
		client := &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
		request, _ := http.NewRequestWithContext(ctx, http.MethodGet, checkURL.String(), nil)
		request.Header.Set("User-Agent", "emby-proxy-publish-check/1.0")
		started := time.Now()
		response, err := client.Do(request)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "status_code": 0, "latency_ms": time.Since(started).Milliseconds(), "error": "PUBLIC_ENTRY_CHECK_FAILED"})
			return
		}
		_ = response.Body.Close()
		ok := response.StatusCode >= 200 && response.StatusCode < 400
		writeJSON(w, http.StatusOK, map[string]any{"ok": ok, "status_code": response.StatusCode, "latency_ms": time.Since(started).Milliseconds()})
		return
	}
	if action == "unpublish" && r.Method == http.MethodPost {
		p, err := h.store.GetPublication(ctx, uid, name)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "PUBLICATION_STATUS_FAILED"})
			return
		}
		if p == nil {
			if h.publicNodeURLs()[name] != "" {
				writeJSON(w, http.StatusOK, map[string]any{"ok": false, "status": storage.PublicationFailed, "reason": "legacy_publication_requires_migration", "failed_step": "public_mapping"})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": storage.PublicationSavedUnpublished, "reason": "unpublished"})
			return
		}
		if p.Status == storage.PublicationSavedUnpublished {
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": storage.PublicationSavedUnpublished, "publication": p})
			return
		}
		if planErr != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "status": storage.PublicationFailed, "reason": planErr.Error(), "failed_step": "plan"})
			return
		}
		route, routeErr := h.store.GetManagedRoute(ctx, plan.RouteSlug)
		if routeErr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "status": storage.PublicationNeedsSync, "reason": "MANAGED_ROUTE_LOOKUP_FAILED", "failed_step": "db_read"})
			return
		}
		storedSync := PublicationSyncResult{NOSLA: PublicationEdgeResult{Status: p.NOSLAStatus}, BWG: PublicationEdgeResult{Status: p.BWGStatus}}
		if p.Status == storage.PublicationFailed && route == nil && publicationEdgesRemoved(storedSync) {
			unpublished := publicationRecord(uid, plan, storage.PublicationSavedUnpublished, "unpublished", "", storedSync)
			if err := h.store.SavePublication(ctx, unpublished); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "status": storage.PublicationFailed, "reason": "PUBLICATION_STATE_WRITE_FAILED", "failed_step": "db_write"})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": unpublished.Status, "publication": unpublished})
			return
		}
		if h.publicationSyncer == nil {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "status": storage.PublicationFailed, "reason": "edge_sync_unavailable", "failed_step": "edge_sync"})
			return
		}
		unpublishing := *p
		unpublishing.Status = storage.PublicationUnpublishing
		unpublishing.Reason = "sync_in_progress"
		unpublishing.FailedStep = ""
		unpublishing.NOSLAStatus = "pending"
		unpublishing.BWGStatus = "pending"
		unpublishing.UpdatedAt = time.Now().Unix()
		if err := h.store.SavePublication(ctx, unpublishing); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "status": storage.PublicationFailed, "reason": "PUBLICATION_STATE_WRITE_FAILED", "failed_step": "db_write"})
			return
		}
		result, syncErr := h.publicationSyncer.Unpublish(ctx, plan)
		if syncErr != nil || result.NOSLA.Status != "removed" || result.BWG.Status != "removed" {
			reason := result.Reason
			if reason == "" {
				reason = "edge_unpublish_failed"
			}
			failed := publicationRecord(uid, plan, storage.PublicationNeedsSync, reason, result.FailedStep, result)
			_ = h.store.SavePublication(ctx, failed)
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "status": failed.Status, "reason": failed.Reason, "failed_step": failed.FailedStep, "publication": failed})
			return
		}
		if err := h.store.DeleteManagedRoute(ctx, plan.RouteSlug); err != nil {
			failed := publicationRecord(uid, plan, storage.PublicationFailed, "managed_route_delete_failed", "db_write", result)
			_ = h.store.SavePublication(ctx, failed)
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "status": failed.Status, "reason": failed.Reason, "failed_step": failed.FailedStep, "publication": failed})
			return
		}
		unpublished := publicationRecord(uid, plan, storage.PublicationSavedUnpublished, "unpublished", "", result)
		if err := h.store.SavePublication(ctx, unpublished); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "status": storage.PublicationFailed, "reason": "PUBLICATION_STATE_WRITE_FAILED", "failed_step": "db_write"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": unpublished.Status, "publication": unpublished})
	}
}
