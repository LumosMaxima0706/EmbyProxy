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
	OperationID       string              `json:"operation_id"`
	NodeName          string              `json:"node_name"`
	RouteSlug         string              `json:"route_slug"`
	PublicPathShape   string              `json:"public_path"`
	PublicURLShape    string              `json:"public_url"`
	UpstreamScheme    string              `json:"upstream_scheme"`
	UpstreamHostShape string              `json:"upstream_host"`
	UpstreamPort      int                 `json:"upstream_port"`
	HasBasePath       bool                `json:"has_base_path"`
	LineCount         int                 `json:"line_count"`
	Changes           []string            `json:"changes"`
	PublicPath        string              `json:"-"`
	PublicURL         string              `json:"-"`
	UpstreamHost      string              `json:"-"`
	BasePath          string              `json:"-"`
	TargetURL         string              `json:"-"`
	TargetURLs        []string            `json:"-"`
	Targets           []PublicationTarget `json:"-"`
}

type PublicationTarget struct {
	LineID   string
	URL      string
	Host     string
	Port     int
	BasePath string
}

type PublicationEdgeResult struct {
	Status     string `json:"status"`
	Reason     string `json:"reason,omitempty"`
	FailedStep string `json:"failed_step,omitempty"`
	BackupPath string `json:"backup_path,omitempty"`
}

type PublicationSyncResult struct {
	NOSLA      PublicationEdgeResult `json:"nosla"`
	BWG        PublicationEdgeResult `json:"bwg"`
	FailedStep string                `json:"failed_step,omitempty"`
	Reason     string                `json:"reason,omitempty"`
}

// PublicationSyncer is the boundary to the privileged BWG/NOSLA publish
// bridge. The sidecar intentionally has no default implementation: without a
// configured bridge, publish fails closed instead of claiming a route exists.
type PublicationSyncer interface {
	Publish(context.Context, PublicationPlan) (PublicationSyncResult, error)
	Unpublish(context.Context, PublicationPlan) (PublicationSyncResult, error)
}

type publicationReadinessChecker interface {
	Readiness(context.Context, PublicationPlan) (PublicationSyncResult, error)
}

type publicationStatusView struct {
	Status             string           `json:"status"`
	WorkflowState      string           `json:"workflow_state"`
	Reason             string           `json:"reason,omitempty"`
	FailedStep         string           `json:"failed_step,omitempty"`
	PublicURL          string           `json:"public_url,omitempty"`
	RouteSlug          string           `json:"route_slug,omitempty"`
	NOSLAStatus        string           `json:"nosla_status"`
	BWGStatus          string           `json:"bwg_status"`
	Managed            bool             `json:"managed"`
	AdapterRegistered  bool             `json:"adapter_registered"`
	PlaybackStatus     string           `json:"playback_status"`
	PlaybackVerifiedAt int64            `json:"playback_verified_at,omitempty"`
	Plan               *PublicationPlan `json:"plan,omitempty"`
}

type publicationArtifacts struct {
	ManagedRoute      bool     `json:"managed_route"`
	ManagedRouteLines int      `json:"managed_route_lines"`
	PublicURL         bool     `json:"public_url"`
	NOSLA             string   `json:"nosla"`
	BWG               string   `json:"bwg"`
	Items             []string `json:"items,omitempty"`
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
	if len(targets) == 0 || len(targets) > 16 {
		return PublicationPlan{}, errors.New("PUBLISH_REQUIRES_SAVED_UPSTREAM")
	}
	if err := proxyadapter.ValidateManagedRouteSlug(name); err != nil {
		return PublicationPlan{}, errors.New("INVALID_ROUTE_SLUG")
	}
	baseURL := strings.TrimSuffix(h.cfg.PublicMediaBaseURL, "/")
	if baseURL == "" {
		return PublicationPlan{}, errors.New("PUBLIC_MEDIA_BASE_URL_NOT_CONFIGURED")
	}
	publicBase, _ := url.Parse(baseURL)
	parsedTargets := make([]PublicationTarget, 0, len(targets))
	for index, target := range targets {
		u, parseErr := url.Parse(strings.TrimSpace(target))
		if parseErr != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return PublicationPlan{}, errors.New("PUBLISH_TARGETS_MUST_BE_HTTPS_URLS_WITHOUT_CREDENTIALS")
		}
		if strings.EqualFold(u.Hostname(), h.cfg.OwnerAdminHost) {
			return PublicationPlan{}, errors.New("PUBLISH_TARGET_CANNOT_BE_OWNER_ADMIN")
		}
		if strings.EqualFold(u.Hostname(), publicBase.Hostname()) {
			return PublicationPlan{}, errors.New("PUBLISH_TARGET_CANNOT_BE_PUBLIC_ENTRY")
		}
		if strings.EqualFold(u.Hostname(), "localhost") {
			return PublicationPlan{}, errors.New("PUBLISH_TARGET_MUST_BE_PUBLIC")
		}
		if ip := net.ParseIP(u.Hostname()); ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsMulticast()) {
			return PublicationPlan{}, errors.New("PUBLISH_TARGET_MUST_BE_PUBLIC")
		}
		port := 443
		if u.Port() != "" {
			parsedPort, portErr := strconv.Atoi(u.Port())
			if portErr != nil || parsedPort < 1 || parsedPort > 65535 {
				return PublicationPlan{}, errors.New("INVALID_UPSTREAM_PORT")
			}
			port = parsedPort
		}
		parsedTargets = append(parsedTargets, PublicationTarget{
			LineID: publicationPlanLineID(index), URL: strings.TrimRight(strings.TrimSpace(target), "/"),
			Host: strings.ToLower(u.Hostname()), Port: port,
			BasePath: strings.Trim(path.Clean(u.EscapedPath()), "/."),
		})
	}
	primary := parsedTargets[0]
	publicPath := "/https/" + primary.Host + "/" + formatPort(primary.Port)
	publicPathShape := "/https/<saved-host>/" + formatPort(primary.Port)
	if primary.BasePath != "" {
		publicPath += "/" + primary.BasePath
		publicPathShape += "/<saved-base-path>"
	}
	return PublicationPlan{
		NodeName: name, RouteSlug: name,
		PublicPathShape: publicPathShape,
		PublicURLShape:  baseURL + publicPathShape,
		UpstreamScheme:  "https", UpstreamHostShape: "<saved-host>", UpstreamPort: primary.Port,
		HasBasePath: primary.BasePath != "", LineCount: len(parsedTargets), PublicPath: publicPath, PublicURL: baseURL + publicPath,
		UpstreamHost: primary.Host, BasePath: primary.BasePath, Changes: []string{
			"managed_route", "managed_route_line", "public_media_mapping",
			"bwg_edge_route", "nosla_edge_route", "redirect_host_allowlist",
		}, TargetURL: primary.URL, TargetURLs: append([]string(nil), targets...), Targets: parsedTargets,
	}, nil
}

func publicationPlanLineID(index int) string {
	if index == 0 {
		return "main"
	}
	return "backup-" + strconv.Itoa(index+1)
}

func publicationManagedLines(plan PublicationPlan) []storage.ManagedRouteLine {
	lines := make([]storage.ManagedRouteLine, 0, len(plan.Targets))
	for index, target := range plan.Targets {
		lines = append(lines, storage.ManagedRouteLine{
			RouteSlug: plan.RouteSlug, LineSlug: target.LineID, Target: target.URL,
			Enabled: true, Position: index + 1,
		})
	}
	return lines
}

func (h *Handler) syncPublishedNodeTargets(ctx context.Context, uid string, previous, next storage.Node) error {
	h.publicationMu.Lock()
	defer h.publicationMu.Unlock()
	if h.publicationSyncer == nil {
		return errors.New("edge_sync_unavailable")
	}
	previousPlan, err := h.buildPublicationPlan(ctx, uid, previous.Name)
	if err != nil {
		return err
	}
	operationID := "route-update-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	previousPlan.OperationID = operationID + "-rollback"
	publication, err := h.store.GetPublication(ctx, uid, previous.Name)
	if err != nil || publication == nil || publication.Status != storage.PublicationPublished {
		return errors.New("PUBLICATION_STATUS_INVALID")
	}
	previousRoute, err := h.store.GetManagedRoute(ctx, previousPlan.RouteSlug)
	if err != nil || previousRoute == nil {
		return errors.New("MANAGED_ROUTE_LOOKUP_FAILED")
	}
	previousLines, err := h.store.ListManagedRouteLines(ctx, previousPlan.RouteSlug)
	if err != nil {
		return errors.New("MANAGED_ROUTE_LOOKUP_FAILED")
	}
	if err := h.store.SaveNode(ctx, uid, next); err != nil {
		return err
	}
	nextPlan, err := h.buildPublicationPlan(ctx, uid, next.Name)
	if err != nil {
		_ = h.store.SaveNode(ctx, uid, previous)
		return err
	}
	nextPlan.OperationID = operationID
	pending := *publication
	pending.Status = storage.PublicationPublishing
	pending.Reason = "route_lines_sync_in_progress"
	pending.FailedStep = ""
	pending.NOSLAStatus, pending.BWGStatus = "pending", "pending"
	pending.UpdatedAt = time.Now().Unix()
	if err := h.store.SavePublication(ctx, pending); err != nil {
		_ = h.store.SaveNode(ctx, uid, previous)
		return errors.New("PUBLICATION_STATE_WRITE_FAILED")
	}
	if err := h.store.SaveManagedRoute(ctx, *previousRoute, publicationManagedLines(nextPlan)); err != nil {
		_ = h.store.SaveNode(ctx, uid, previous)
		_ = h.store.SavePublication(ctx, *publication)
		return errors.New("managed_route_write_failed")
	}
	result, syncErr := h.publicationSyncer.Publish(ctx, nextPlan)
	if syncErr == nil && result.NOSLA.Status == "synced" && result.BWG.Status == "synced" {
		completed := *publication
		completed.Status = storage.PublicationPublished
		completed.Reason = "public_entry_configured"
		completed.FailedStep = ""
		completed.NOSLAStatus, completed.BWGStatus = "synced", "synced"
		completed.UpdatedAt = time.Now().Unix()
		if err := h.store.SavePublication(ctx, completed); err == nil {
			return nil
		}
	}

	// Restore the old database view first. The restricted adapter rebuilds the
	// prior manifest from that view and atomically restores both slug fragments.
	_ = h.store.SaveNode(ctx, uid, previous)
	_ = h.store.SaveManagedRoute(ctx, *previousRoute, previousLines)
	rollbackPending := *publication
	rollbackPending.Status = storage.PublicationPublishing
	rollbackPending.Reason = "route_lines_rollback_in_progress"
	rollbackPending.FailedStep = ""
	rollbackPending.NOSLAStatus, rollbackPending.BWGStatus = "pending", "pending"
	rollbackPending.UpdatedAt = time.Now().Unix()
	_ = h.store.SavePublication(ctx, rollbackPending)
	rollback, rollbackErr := h.publicationSyncer.Publish(ctx, previousPlan)
	_ = h.store.SavePublication(ctx, *publication)
	if rollbackErr != nil || rollback.NOSLA.Status != "synced" || rollback.BWG.Status != "synced" {
		return errors.New("PUBLISHED_ROUTE_UPDATE_ROLLBACK_FAILED")
	}
	if result.Reason != "" {
		return errors.New(result.Reason)
	}
	return errors.New("PUBLISHED_ROUTE_UPDATE_FAILED")
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
		playbackStatus := strings.TrimSpace(p.PlaybackStatus)
		if playbackStatus == "" {
			playbackStatus = playbackPublicationStatus(p.Status)
		}
		return publicationStatusView{
			Status: p.Status, WorkflowState: publicationWorkflowState(p.Status, p.Reason), Reason: p.Reason, FailedStep: p.FailedStep,
			PublicURL: p.PublicURL, RouteSlug: p.RouteSlug,
			NOSLAStatus: p.NOSLAStatus, BWGStatus: p.BWGStatus, Managed: true,
			AdapterRegistered:  h.publicationSyncer != nil,
			PlaybackStatus:     playbackStatus,
			PlaybackVerifiedAt: p.PlaybackVerifiedAt,
		}, nil
	}
	legacy := h.publicNodeURLs()[name]
	if legacy != "" {
		return publicationStatusView{
			Status: storage.PublicationPublished, WorkflowState: storage.PublicationPublished, Reason: "public_entry_configured",
			PublicURL: legacy, NOSLAStatus: "synced", BWGStatus: "synced",
			AdapterRegistered: h.publicationSyncer != nil,
			PlaybackStatus:    "unverified",
		}, nil
	}
	return publicationStatusView{
		Status: storage.PublicationSavedUnpublished, WorkflowState: storage.PublicationSavedUnpublished, Reason: "no_edge_route_configured",
		NOSLAStatus: "not_configured", BWGStatus: "not_configured",
		AdapterRegistered: h.publicationSyncer != nil,
		PlaybackStatus:    "not_published",
	}, nil
}

func playbackPublicationStatus(status string) string {
	if status == storage.PublicationPublished {
		return "unverified"
	}
	return "not_published"
}

func publicationWorkflowState(status, reason string) string {
	switch status {
	case storage.PublicationPublishing:
		return "edge_sync_pending"
	case storage.PublicationFailed:
		if strings.Contains(reason, "unpublish") {
			return "unpublish_failed"
		}
		return "edge_sync_failed"
	case storage.PublicationNeedsSync:
		return "cleanup_required"
	default:
		return status
	}
}

func publicationRecord(uid string, plan PublicationPlan, status, reason, step string, sync PublicationSyncResult) storage.Publication {
	if sync.NOSLA.Status == "" {
		sync.NOSLA.Status = "unknown"
	}
	if sync.BWG.Status == "" {
		sync.BWG.Status = "unknown"
	}
	playbackStatus := "not_published"
	if status == storage.PublicationPublished {
		playbackStatus = "unverified"
	}
	return storage.Publication{
		UID: uid, NodeName: plan.NodeName, RouteSlug: plan.RouteSlug, PublicURL: "",
		Status: status, Reason: reason, FailedStep: step,
		NOSLAStatus: sync.NOSLA.Status, BWGStatus: sync.BWG.Status,
		PlaybackStatus: playbackStatus, UpdatedAt: time.Now().Unix(),
	}
}

func publicationEdgesRemoved(sync PublicationSyncResult) bool {
	clean := func(status string) bool {
		return status == "removed" || status == "not_configured"
	}
	return clean(sync.NOSLA.Status) && clean(sync.BWG.Status)
}

func edgeHasArtifact(status string) bool {
	return status != "" && status != "removed" && status != "not_configured"
}

func (h *Handler) publicationArtifacts(ctx context.Context, plan PublicationPlan, p *storage.Publication) (publicationArtifacts, error) {
	route, err := h.store.GetManagedRoute(ctx, plan.RouteSlug)
	if err != nil {
		return publicationArtifacts{}, err
	}
	lines, err := h.store.ListManagedRouteLines(ctx, plan.RouteSlug)
	if err != nil {
		return publicationArtifacts{}, err
	}
	artifacts := publicationArtifacts{ManagedRoute: route != nil, ManagedRouteLines: len(lines)}
	if p != nil {
		artifacts.PublicURL = strings.TrimSpace(p.PublicURL) != ""
		artifacts.NOSLA, artifacts.BWG = p.NOSLAStatus, p.BWGStatus
		if edgeHasArtifact(p.NOSLAStatus) {
			artifacts.Items = append(artifacts.Items, "nosla_edge")
		}
		if edgeHasArtifact(p.BWGStatus) {
			artifacts.Items = append(artifacts.Items, "bwg_edge")
		}
	}
	if artifacts.ManagedRoute {
		artifacts.Items = append(artifacts.Items, "managed_route")
	}
	if artifacts.ManagedRouteLines > 0 {
		artifacts.Items = append(artifacts.Items, "managed_route_lines")
	}
	if artifacts.PublicURL {
		artifacts.Items = append(artifacts.Items, "public_url")
	}
	return artifacts, nil
}

func (a publicationArtifacts) any() bool {
	return a.ManagedRoute || a.ManagedRouteLines > 0 || a.PublicURL || len(a.Items) > 0
}

func publicationNoArtifactState(status string) bool {
	return status == storage.PublicationFailed || status == storage.PublicationNeedsSync || status == storage.PublicationSavedUnpublished
}

func (h *Handler) normalizeStalePublication(ctx context.Context, uid string, plan PublicationPlan, p *storage.Publication, artifacts publicationArtifacts) (storage.Publication, error) {
	state := publicationRecord(uid, plan, storage.PublicationSavedUnpublished, "stale_failed_state_only", "", PublicationSyncResult{
		NOSLA: PublicationEdgeResult{Status: normalizedEdgeStatus(artifacts.NOSLA)},
		BWG:   PublicationEdgeResult{Status: normalizedEdgeStatus(artifacts.BWG)},
	})
	if p != nil && p.Status == storage.PublicationSavedUnpublished && p.Reason == "" {
		state.Reason = "no_publication_to_unpublish"
	}
	if err := h.store.SavePublication(ctx, state); err != nil {
		return storage.Publication{}, err
	}
	return state, nil
}

func normalizedEdgeStatus(status string) string {
	if status == "removed" || status == "not_configured" {
		return "not_configured"
	}
	return "not_configured"
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
	if len(parts) == 3 && action == "publish" && (parts[2] == "dry-run" || parts[2] == "reconcile" || parts[2] == "cleanup") {
		action = "publish/" + parts[2]
	} else if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	uid := "admin"
	ctx := r.Context()
	plan, planErr := h.buildPublicationPlan(ctx, uid, name)
	plan.OperationID = publicationOperationID(r)
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
	if r.Method != http.MethodPost || (action != "publish" && action != "unpublish" && action != "publish/dry-run" && action != "publish/reconcile" && action != "publish/cleanup" && action != "verify-proxy" && action != "playback-verify") {
		w.Header().Set("Allow", "GET, POST")
		http.NotFound(w, r)
		return
	}
	if action == "publish" || action == "unpublish" || action == "publish/reconcile" || action == "publish/cleanup" || action == "playback-verify" {
		h.publicationMu.Lock()
		defer h.publicationMu.Unlock()
	}
	if action == "playback-verify" {
		verifiedAt := time.Now().Unix()
		if err := h.store.SetPublicationPlaybackVerified(ctx, uid, name, verifiedAt); err != nil {
			writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "reason": "PLAYBACK_VERIFICATION_REQUIRES_SYNCED_PUBLICATION", "failed_step": "state_guard"})
			return
		}
		status, err := h.publicationStatus(ctx, uid, name)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "reason": "PUBLICATION_STATUS_FAILED", "failed_step": "state_read"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": status.Status, "playback_status": status.PlaybackStatus, "playback_verified_at": status.PlaybackVerifiedAt})
		return
	}
	if action == "publish/reconcile" || action == "publish/cleanup" {
		if planErr != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "reason": planErr.Error(), "failed_step": "plan"})
			return
		}
		current, err := h.store.GetPublication(ctx, uid, name)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "reason": "PUBLICATION_STATUS_FAILED", "failed_step": "state_read"})
			return
		}
		artifacts, err := h.publicationArtifacts(ctx, plan, current)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "reason": "PUBLICATION_ARTIFACT_LOOKUP_FAILED", "failed_step": "db_read"})
			return
		}
		if !artifacts.any() {
			if current == nil {
				writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": storage.PublicationSavedUnpublished, "reason": "no_publication_to_reconcile", "operation_id": plan.OperationID, "artifacts": artifacts})
				return
			}
			state, saveErr := h.normalizeStalePublication(ctx, uid, plan, current, artifacts)
			if saveErr != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "reason": "PUBLICATION_STATE_WRITE_FAILED", "failed_step": "db_write"})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": state.Status, "reason": "stale_failed_state_only", "operation_id": plan.OperationID, "publication": state, "artifacts": artifacts})
			return
		}
		if action == "publish/reconcile" {
			writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "status": storage.PublicationNeedsSync, "reason": "partial_artifacts_exist", "failed_step": "reconciliation", "operation_id": plan.OperationID, "artifacts": artifacts})
			return
		}
		// Cleanup is deliberately scoped to this route slug. Edge unpublish is
		// only attempted when an edge artifact is actually recorded.
		result := PublicationSyncResult{
			NOSLA: PublicationEdgeResult{Status: normalizedEdgeStatus(artifacts.NOSLA)},
			BWG:   PublicationEdgeResult{Status: normalizedEdgeStatus(artifacts.BWG)},
		}
		if edgeHasArtifact(artifacts.NOSLA) || edgeHasArtifact(artifacts.BWG) {
			if h.publicationSyncer == nil {
				writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "status": storage.PublicationNeedsSync, "reason": "edge_sync_unavailable", "failed_step": "edge_cleanup", "artifacts": artifacts})
				return
			}
			var syncErr error
			result, syncErr = h.publicationSyncer.Unpublish(ctx, plan)
			if syncErr != nil || !publicationEdgesRemoved(result) {
				reason := result.Reason
				if reason == "" {
					reason = "helper_failed"
				}
				step := result.FailedStep
				if step == "" {
					step = "edge_cleanup"
				}
				writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "status": storage.PublicationNeedsSync, "reason": reason, "failed_step": step, "artifacts": artifacts})
				return
			}
		}
		if err := h.store.DeleteManagedRoute(ctx, plan.RouteSlug); err != nil {
			writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "status": storage.PublicationNeedsSync, "reason": "db_route_delete_failed", "failed_step": "db_cleanup", "artifacts": artifacts})
			return
		}
		state := publicationRecord(uid, plan, storage.PublicationSavedUnpublished, "publication_cleanup_complete", "", result)
		if err := h.store.SavePublication(ctx, state); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "reason": "PUBLICATION_STATE_WRITE_FAILED", "failed_step": "db_write"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": state.Status, "reason": "publication_cleanup_complete", "operation_id": plan.OperationID, "publication": state, "artifacts": publicationArtifacts{NOSLA: state.NOSLAStatus, BWG: state.BWGStatus}})
		return
	}
	if action == "publish" && r.Method == http.MethodPost {
		h.logPublication("publicationRequested", plan, "publishing", "")
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
			stored, readErr := h.store.GetPublication(ctx, uid, name)
			if readErr != nil || stored == nil {
				writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "status": storage.PublicationFailed, "reason": "PUBLICATION_STATUS_FAILED", "failed_step": "state_read"})
				return
			}
			if h.publicationSyncer == nil {
				writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "status": current.Status, "reason": "edge_sync_unavailable", "failed_step": "edge_sync"})
				return
			}
			pending := *stored
			pending.Status = storage.PublicationPublishing
			pending.Reason = "configuration_refresh_in_progress"
			pending.FailedStep = ""
			pending.NOSLAStatus, pending.BWGStatus = "pending", "pending"
			pending.UpdatedAt = time.Now().Unix()
			if err := h.store.SavePublication(ctx, pending); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "status": current.Status, "reason": "PUBLICATION_STATE_WRITE_FAILED", "failed_step": "db_write"})
				return
			}
			result, syncErr := h.publicationSyncer.Publish(ctx, plan)
			if syncErr != nil || result.NOSLA.Status != "synced" || result.BWG.Status != "synced" {
				failed := *stored
				failed.Reason = result.Reason
				if failed.Reason == "" {
					failed.Reason = "configuration_refresh_failed"
				}
				failed.FailedStep = result.FailedStep
				failed.UpdatedAt = time.Now().Unix()
				_ = h.store.SavePublication(ctx, failed)
				writeJSON(w, http.StatusOK, map[string]any{"ok": false, "status": failed.Status, "reason": failed.Reason, "failed_step": failed.FailedStep, "publication": failed})
				return
			}
			refreshed := *stored
			refreshed.Reason = "public_entry_configured"
			refreshed.FailedStep = ""
			refreshed.NOSLAStatus, refreshed.BWGStatus = "synced", "synced"
			refreshed.UpdatedAt = time.Now().Unix()
			if err := h.store.SavePublication(ctx, refreshed); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "status": current.Status, "reason": "PUBLICATION_STATE_WRITE_FAILED", "failed_step": "db_write"})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": refreshed.Status, "reason": "configuration_refreshed", "publication": refreshed})
			return
		}
		rawPublication, rawErr := h.store.GetPublication(ctx, uid, name)
		if rawErr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "status": storage.PublicationFailed, "reason": "PUBLICATION_STATUS_FAILED", "failed_step": "state_read"})
			return
		}
		artifacts, artifactErr := h.publicationArtifacts(ctx, plan, rawPublication)
		if artifactErr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "status": storage.PublicationFailed, "reason": "PUBLICATION_ARTIFACT_LOOKUP_FAILED", "failed_step": "db_read"})
			return
		}
		if rawPublication != nil && publicationNoArtifactState(rawPublication.Status) && !artifacts.any() {
			if _, normalizeErr := h.normalizeStalePublication(ctx, uid, plan, rawPublication, artifacts); normalizeErr != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "status": storage.PublicationFailed, "reason": "PUBLICATION_STATE_WRITE_FAILED", "failed_step": "db_write"})
				return
			}
			current.Status = storage.PublicationSavedUnpublished
		}
		if current.Status == storage.PublicationPublishing || current.Status == storage.PublicationUnpublishing || current.Status == storage.PublicationNeedsSync {
			reason := "PUBLICATION_REQUIRES_RECONCILIATION"
			if artifacts.any() {
				reason = "partial_artifacts_exist"
			}
			writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "status": current.Status, "reason": reason, "failed_step": "state_guard", "operation_id": plan.OperationID, "artifacts": artifacts})
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
			h.logPublication("publicationFailed", plan, storage.PublicationFailed, "edge_sync_unavailable")
			failed := publicationRecord(uid, plan, storage.PublicationFailed, "edge_sync_unavailable", "edge_sync", PublicationSyncResult{NOSLA: PublicationEdgeResult{Status: "not_configured"}, BWG: PublicationEdgeResult{Status: "not_configured"}})
			_ = h.store.SavePublication(ctx, failed)
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "status": failed.Status, "reason": failed.Reason, "failed_step": failed.FailedStep, "publication": failed})
			return
		}
		stagedRoute := storage.ManagedRoute{Slug: plan.RouteSlug, NodeName: plan.NodeName, Enabled: false, Public: false, DefaultLine: "main"}
		stagedLines := publicationManagedLines(plan)
		if err := h.store.SaveManagedRoute(ctx, stagedRoute, stagedLines); err != nil {
			failed := publicationRecord(uid, plan, storage.PublicationFailed, "managed_route_write_failed", "db_write", PublicationSyncResult{NOSLA: PublicationEdgeResult{Status: "pending"}, BWG: PublicationEdgeResult{Status: "pending"}})
			_ = h.store.SavePublication(ctx, failed)
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "status": failed.Status, "reason": failed.Reason, "failed_step": failed.FailedStep, "publication": failed})
			return
		}
		result, err := h.publicationSyncer.Publish(ctx, plan)
		if err != nil || result.NOSLA.Status != "synced" || result.BWG.Status != "synced" {
			h.logPublication("publicationEdgeSyncFailed", plan, storage.PublicationFailed, result.Reason)
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
		h.logPublication("publicationCompleted", plan, published.Status, "")
		return
	}
	if action == "publish/dry-run" {
		if planErr != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "status": storage.PublicationFailed, "reason": planErr.Error(), "failed_step": "plan"})
			return
		}
		readiness := unavailableSyncResult("edge_sync_unavailable", "edge_adapter_registration")
		ready := false
		if checker, ok := h.publicationSyncer.(publicationReadinessChecker); ok {
			var readinessErr error
			readiness, readinessErr = checker.Readiness(ctx, plan)
			ready = readinessErr == nil && readiness.NOSLA.Status == "ready" && readiness.BWG.Status == "ready"
		}
		h.logPublication("publicationDryRun", plan, "dry_run_ok", readiness.Reason)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "dry_run_ok", "dry_run": true, "plan": plan, "adapter_ready": ready, "readiness": readiness})
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
		if planErr != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "status": storage.PublicationFailed, "reason": planErr.Error(), "failed_step": "plan"})
			return
		}
		artifacts, artifactErr := h.publicationArtifacts(ctx, plan, p)
		if artifactErr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "status": storage.PublicationFailed, "reason": "PUBLICATION_ARTIFACT_LOOKUP_FAILED", "failed_step": "db_read"})
			return
		}
		if !artifacts.any() {
			unpublished, saveErr := h.normalizeStalePublication(ctx, uid, plan, p, artifacts)
			if saveErr != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "status": storage.PublicationFailed, "reason": "PUBLICATION_STATE_WRITE_FAILED", "failed_step": "db_write"})
				return
			}
			unpublished.Reason = "no_publication_to_unpublish"
			if saveErr = h.store.SavePublication(ctx, unpublished); saveErr != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "status": storage.PublicationFailed, "reason": "PUBLICATION_STATE_WRITE_FAILED", "failed_step": "db_write"})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": unpublished.Status, "reason": "no_publication_to_unpublish", "operation_id": plan.OperationID, "publication": unpublished, "artifacts": artifacts})
			return
		}
		if p.Status != storage.PublicationPublished {
			writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "status": storage.PublicationNeedsSync, "reason": "partial_artifacts_exist", "failed_step": "reconciliation", "operation_id": plan.OperationID, "artifacts": artifacts})
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
				reason = "helper_failed"
			}
			failedStep := result.FailedStep
			if failedStep == "" {
				failedStep = "edge_unpublish"
			}
			failed := publicationRecord(uid, plan, storage.PublicationNeedsSync, reason, failedStep, result)
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

func publicationOperationID(r *http.Request) string {
	if r == nil {
		return "publication"
	}
	if value, ok := r.Context().Value("requestID").(string); ok {
		value = strings.TrimSpace(value)
		if value != "" && len(value) <= 64 && !strings.ContainsAny(value, " \t\r\n/?#") {
			return value
		}
	}
	return "publication"
}

func (h *Handler) logPublication(event string, plan PublicationPlan, status, reason string) {
	if h == nil || h.log == nil {
		return
	}
	fields := map[string]any{
		"event": event, "operationId": plan.OperationID,
		"node": plan.NodeName, "routeSlug": plan.RouteSlug, "status": status,
	}
	if reason != "" {
		fields["reason"] = reason
	}
	h.log.Info("publication", "publication state changed", fields)
}
