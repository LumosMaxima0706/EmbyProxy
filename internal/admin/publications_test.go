package admin

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"embyproxy/internal/config"
	"embyproxy/internal/storage"
)

type successfulPublicationSyncer struct{}

type successfulPlaybackCanarySyncer struct{ successfulPublicationSyncer }

type capturingPlaybackCanarySyncer struct {
	successfulPublicationSyncer
	token string
}

func (s *capturingPlaybackCanarySyncer) PlaybackCanary(_ context.Context, _ PublicationPlan, input PlaybackCanaryInput) (PlaybackCanaryResult, error) {
	s.token = input.AccessToken
	return PlaybackCanaryResult{Status: "healthy", ConnectivityStatus: 200, PlaybackInfoStatus: 200,
		VideoStreamStatus: 302, MediaStatus: 206, RedirectsFollowed: 2, EndpointsDiscovered: 2,
		Samples: len(input.ItemIDs), SamplesPassed: len(input.ItemIDs), BytesRead: 131072,
		ByteGrowth: true, ContentRange: true, AcceptRanges: true}, nil
}

func (successfulPlaybackCanarySyncer) PlaybackCanary(context.Context, PublicationPlan, PlaybackCanaryInput) (PlaybackCanaryResult, error) {
	return PlaybackCanaryResult{Status: "healthy", ConnectivityStatus: 200, PlaybackInfoStatus: 200,
		VideoStreamStatus: 302, MediaStatus: 206, RedirectsFollowed: 1, EndpointsDiscovered: 1,
		BytesRead: 65536, ByteGrowth: true, ContentRange: true, AcceptRanges: true}, nil
}

type failedPlaybackCanarySyncer struct{ successfulPublicationSyncer }

func (failedPlaybackCanarySyncer) PlaybackCanary(context.Context, PublicationPlan, PlaybackCanaryInput) (PlaybackCanaryResult, error) {
	return PlaybackCanaryResult{Status: "failed", FailureClass: "upstream_403", ConnectivityStatus: 200,
		PlaybackInfoStatus: 200, VideoStreamStatus: 302, MediaStatus: 403}, errors.New("playback_canary_failed")
}

type operationIDPublicationSyncer struct{ successfulPublicationSyncer }

func (operationIDPublicationSyncer) Publish(_ context.Context, plan PublicationPlan) (PublicationSyncResult, error) {
	if plan.OperationID == "" {
		return PublicationSyncResult{}, errors.New("operation id missing")
	}
	return successfulPublicationSyncer{}.Publish(context.Background(), plan)
}

func (successfulPublicationSyncer) Publish(context.Context, PublicationPlan) (PublicationSyncResult, error) {
	return PublicationSyncResult{
		NOSLA: PublicationEdgeResult{Status: "synced"},
		BWG:   PublicationEdgeResult{Status: "synced"},
	}, nil
}

func (successfulPublicationSyncer) Unpublish(context.Context, PublicationPlan) (PublicationSyncResult, error) {
	return PublicationSyncResult{
		NOSLA: PublicationEdgeResult{Status: "removed"},
		BWG:   PublicationEdgeResult{Status: "removed"},
	}, nil
}

type partialPublicationSyncer struct {
	rollbackSucceeds bool
}

type emptyUnpublishFailureSyncer struct{ successfulPublicationSyncer }

func (emptyUnpublishFailureSyncer) Unpublish(context.Context, PublicationPlan) (PublicationSyncResult, error) {
	return PublicationSyncResult{
		NOSLA: PublicationEdgeResult{Status: "failed"},
		BWG:   PublicationEdgeResult{Status: "failed"},
	}, nil
}

func (partialPublicationSyncer) Publish(context.Context, PublicationPlan) (PublicationSyncResult, error) {
	return PublicationSyncResult{
		NOSLA:      PublicationEdgeResult{Status: "synced"},
		BWG:        PublicationEdgeResult{Status: "failed"},
		FailedStep: "bwg_edge_route",
		Reason:     "bwg_edge_sync_failed",
	}, nil
}

func (s partialPublicationSyncer) Unpublish(context.Context, PublicationPlan) (PublicationSyncResult, error) {
	result := PublicationSyncResult{
		NOSLA: PublicationEdgeResult{Status: "removed"},
		BWG:   PublicationEdgeResult{Status: "removed"},
	}
	if !s.rollbackSucceeds {
		result.BWG.Status = "failed"
		result.FailedStep = "bwg_edge_rollback"
		result.Reason = "publication_rollback_failed"
	}
	return result, nil
}

func publicationTestHandler(t *testing.T) (*Handler, *http.Cookie) {
	t.Helper()
	cfg := config.Config{
		AdminToken: "strong-admin-token", PublicMediaBaseURL: "https://stream.example",
		OwnerAdminHost: "owner-admin.example", PublicMediaNodePaths: map[string]string{}, PlaybackCredentialDir: t.TempDir(),
	}
	handler := newAuthTestHandler(t, cfg)
	if err := handler.store.SaveNode(context.Background(), "admin", storage.Node{Name: "feimu", Target: "https://media.example"}); err != nil {
		t.Fatal(err)
	}
	login := serveAdminJSON(t, handler, http.MethodPost, "/admin/auth/login", map[string]any{"token": cfg.AdminToken}, nil)
	if login.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", login.Code, login.Body.String())
	}
	return handler, login.Result().Cookies()[0]
}

func TestStoredPlaybackCredentialFeedsCanaryWithoutReturningSecret(t *testing.T) {
	handler, cookie := publicationTestHandler(t)
	syncer := &capturingPlaybackCanarySyncer{}
	handler.SetPublicationSyncer(syncer)
	published := serveAdminJSON(t, handler, http.MethodPost, "/api/admin/emby-servers/feimu/publish", nil, cookie)
	if published.Code != http.StatusOK {
		t.Fatalf("publish status=%d body=%s", published.Code, published.Body.String())
	}
	configured := serveAdminJSON(t, handler, http.MethodPost, "/api/admin/emby-servers/feimu/playback-credential", map[string]any{"access_token": "stored-runtime-secret"}, cookie)
	if configured.Code != http.StatusOK || !strings.Contains(configured.Body.String(), `"credential_configured":true`) || strings.Contains(configured.Body.String(), "stored-runtime-secret") {
		t.Fatalf("configure status=%d body=%s", configured.Code, configured.Body.String())
	}
	verified := serveAdminJSON(t, handler, http.MethodPost, "/api/admin/emby-servers/feimu/playback-canary", map[string]any{"item_ids": []string{"625260", "601953"}}, cookie)
	if verified.Code != http.StatusOK || !strings.Contains(verified.Body.String(), `"playback_status":"healthy"`) || syncer.token != "stored-runtime-secret" {
		t.Fatalf("verify status=%d token=%q body=%s", verified.Code, syncer.token, verified.Body.String())
	}
	if strings.Contains(verified.Body.String(), "stored-runtime-secret") {
		t.Fatal("stored credential leaked in canary response")
	}
	status := serveAdminJSON(t, handler, http.MethodGet, "/api/admin/emby-servers/feimu/publish-status", nil, cookie)
	if strings.Contains(status.Body.String(), "stored-runtime-secret") || !strings.Contains(status.Body.String(), `"playback_credential_configured":true`) {
		t.Fatalf("status leaked credential or configured flag missing: %s", status.Body.String())
	}
}

func TestAuthenticateEmbyForPlaybackUsesOfficialExchangeWithoutReturningPassword(t *testing.T) {
	var seenUser, seenPassword string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/emby/Users/AuthenticateByName" {
			t.Fatalf("request=%s %s", r.Method, r.URL.Path)
		}
		var body struct {
			Username string `json:"Username"`
			Password string `json:"Pw"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		seenUser, seenPassword = body.Username, body.Password
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"AccessToken":"runtime-token"}`))
	}))
	defer server.Close()
	client := server.Client()
	token, err := authenticateEmbyForPlayback(context.Background(), server.URL, "alice", "not-for-storage", client)
	if err != nil || token != "runtime-token" {
		t.Fatalf("token=%q err=%v", token, err)
	}
	if seenUser != "alice" || seenPassword != "not-for-storage" {
		t.Fatalf("authentication payload user=%q password=%q", seenUser, seenPassword)
	}
}

func TestAuthenticateEmbyForPlaybackClassifiesAuthFailureWithoutCredentialLeak(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	_, err := authenticateEmbyForPlayback(context.Background(), server.URL, "alice", "secret-value", server.Client())
	if err == nil || !strings.Contains(err.Error(), "credential_auth_http_401") || strings.Contains(err.Error(), "secret-value") || strings.Contains(err.Error(), "alice") {
		t.Fatalf("unexpected auth error=%v", err)
	}
}

func TestDiscoverEmbyPlaybackItemsReturnsBoundedMultiSampleSet(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Emby-Token") != "runtime-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/emby/Users/Me":
			_, _ = w.Write([]byte(`{"Id":"user-1"}`))
		case r.URL.Path == "/emby/Users/user-1/Items":
			_, _ = w.Write([]byte(`{"Items":[{"Id":"625260"},{"Id":"601953"},{"Id":"third"}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	items, err := discoverEmbyPlaybackItems(context.Background(), server.URL, "runtime-token", server.Client())
	if err != nil || len(items) != 3 || items[0] != "625260" || items[1] != "601953" {
		t.Fatalf("items=%v err=%v", items, err)
	}
}

func TestDiscoverEmbyPlaybackItemsFallsBackWhenUsersMeIsUnsupported(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Emby-Token") != "runtime-token" { w.WriteHeader(http.StatusUnauthorized); return }
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/emby/Users/Me" { w.WriteHeader(http.StatusInternalServerError); return }
		if r.URL.Path == "/emby/Items" { _, _ = w.Write([]byte(`{"Items":[{"Id":"625260"},{"Id":"601953"}]}`)); return }
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	items, err := discoverEmbyPlaybackItems(context.Background(), server.URL, "runtime-token", server.Client())
	if err != nil || len(items) != 2 { t.Fatalf("items=%v err=%v", items, err) }
}

func TestMissingPlaybackCredentialBlocksCanary(t *testing.T) {
	handler, cookie := publicationTestHandler(t)
	handler.SetPublicationSyncer(&capturingPlaybackCanarySyncer{})
	if response := serveAdminJSON(t, handler, http.MethodPost, "/api/admin/emby-servers/feimu/publish", nil, cookie); response.Code != http.StatusOK {
		t.Fatalf("publish status=%d body=%s", response.Code, response.Body.String())
	}
	response := serveAdminJSON(t, handler, http.MethodPost, "/api/admin/emby-servers/feimu/playback-canary", map[string]any{"item_ids": []string{"625260"}}, cookie)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"reason":"credential_missing"`) || strings.Contains(response.Body.String(), "access_token") {
		t.Fatalf("missing credential status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestPublicationAPIsRequireAuthentication(t *testing.T) {
	handler, _ := publicationTestHandler(t)
	for _, endpoint := range []struct{ method, path string }{
		{http.MethodGet, "/api/admin/emby-servers/feimu/publish-status"},
		{http.MethodPost, "/api/admin/emby-servers/feimu/publish/dry-run"},
		{http.MethodPost, "/api/admin/emby-servers/feimu/publish"},
		{http.MethodPost, "/api/admin/emby-servers/feimu/unpublish"},
		{http.MethodPost, "/api/admin/emby-servers/feimu/publish/reconcile"},
		{http.MethodPost, "/api/admin/emby-servers/feimu/publish/cleanup"},
		{http.MethodPost, "/api/admin/emby-servers/feimu/playback-verify"},
		{http.MethodPost, "/api/admin/emby-servers/feimu/playback-canary"},
	} {
		response := serveAdminJSON(t, handler, endpoint.method, endpoint.path, nil, nil)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s status=%d", endpoint.method, endpoint.path, response.Code)
		}
	}
}

func TestPlaybackVerificationPersistsForSyncedPublication(t *testing.T) {
	handler, cookie := publicationTestHandler(t)
	handler.SetPublicationSyncer(successfulPlaybackCanarySyncer{})
	published := serveAdminJSON(t, handler, http.MethodPost, "/api/admin/emby-servers/feimu/publish", nil, cookie)
	if published.Code != http.StatusOK || !strings.Contains(published.Body.String(), `"status":"published"`) {
		t.Fatalf("publish status=%d body=%s", published.Code, published.Body.String())
	}
	verified := serveAdminJSON(t, handler, http.MethodPost, "/api/admin/emby-servers/feimu/playback-canary", map[string]any{"item_id": "item-1", "access_token": "runtime-only-token"}, cookie)
	if verified.Code != http.StatusOK || !strings.Contains(verified.Body.String(), `"playback_status":"healthy"`) {
		t.Fatalf("verify status=%d body=%s", verified.Code, verified.Body.String())
	}
	status := serveAdminJSON(t, handler, http.MethodGet, "/api/admin/emby-servers/feimu/publish-status", nil, cookie)
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"playback_status":"healthy"`) || !strings.Contains(status.Body.String(), `"playback_verified_at":`) {
		t.Fatalf("status=%d body=%s", status.Code, status.Body.String())
	}
	if strings.Contains(verified.Body.String(), "runtime-only-token") {
		t.Fatal("runtime token leaked in canary response")
	}
}

func TestPlaybackVerifyCannotBypassCanaryAndAPISuccessDoesNotMarkHealthy(t *testing.T) {
	handler, cookie := publicationTestHandler(t)
	handler.SetPublicationSyncer(failedPlaybackCanarySyncer{})
	published := serveAdminJSON(t, handler, http.MethodPost, "/api/admin/emby-servers/feimu/publish", nil, cookie)
	if published.Code != http.StatusOK {
		t.Fatalf("publish status=%d body=%s", published.Code, published.Body.String())
	}
	bypass := serveAdminJSON(t, handler, http.MethodPost, "/api/admin/emby-servers/feimu/playback-verify", nil, cookie)
	if bypass.Code != http.StatusConflict || !strings.Contains(bypass.Body.String(), "PLAYBACK_CANARY_REQUIRED") {
		t.Fatalf("bypass status=%d body=%s", bypass.Code, bypass.Body.String())
	}
	failed := serveAdminJSON(t, handler, http.MethodPost, "/api/admin/emby-servers/feimu/playback-canary", map[string]any{"item_id": "item-1", "access_token": "runtime-only-token"}, cookie)
	if failed.Code != http.StatusOK || !strings.Contains(failed.Body.String(), `"playback_status":"failed"`) {
		t.Fatalf("failed status=%d body=%s", failed.Code, failed.Body.String())
	}
	status := serveAdminJSON(t, handler, http.MethodGet, "/api/admin/emby-servers/feimu/publish-status", nil, cookie)
	if strings.Contains(status.Body.String(), `"playback_status":"healthy"`) || !strings.Contains(status.Body.String(), `"playback_failure_class":"upstream_403"`) {
		t.Fatalf("status=%d body=%s", status.Code, status.Body.String())
	}
}

func TestStaleFailedPublicationReconcilesAndUnpublishIsIdempotent(t *testing.T) {
	handler, cookie := publicationTestHandler(t)
	ctx := context.Background()
	plan, err := handler.buildPublicationPlan(ctx, "admin", "feimu")
	if err != nil {
		t.Fatal(err)
	}
	if err := handler.store.SavePublication(ctx, storage.Publication{
		UID: "admin", NodeName: "feimu", RouteSlug: "feimu", Status: storage.PublicationNeedsSync,
		Reason: "edge_unpublish_failed", NOSLAStatus: "not_configured", BWGStatus: "not_configured",
	}); err != nil {
		t.Fatal(err)
	}
	reconciled := serveAdminJSON(t, handler, http.MethodPost, "/api/admin/emby-servers/feimu/publish/reconcile", nil, cookie)
	if reconciled.Code != http.StatusOK || !strings.Contains(reconciled.Body.String(), `"reason":"stale_failed_state_only"`) {
		t.Fatalf("reconcile status=%d body=%s", reconciled.Code, reconciled.Body.String())
	}
	publication, err := handler.store.GetPublication(ctx, "admin", "feimu")
	if err != nil || publication == nil || publication.Status != storage.PublicationSavedUnpublished {
		t.Fatalf("publication after reconcile=%+v err=%v", publication, err)
	}
	unpublished := serveAdminJSON(t, handler, http.MethodPost, "/api/admin/emby-servers/feimu/unpublish", nil, cookie)
	if unpublished.Code != http.StatusOK || !strings.Contains(unpublished.Body.String(), `"reason":"no_publication_to_unpublish"`) {
		t.Fatalf("unpublish status=%d body=%s", unpublished.Code, unpublished.Body.String())
	}
	route, err := handler.store.GetManagedRoute(ctx, plan.RouteSlug)
	if err != nil || route != nil {
		t.Fatalf("unexpected route=%+v err=%v", route, err)
	}
}

func TestCleanupWithoutArtifactsIsNoOp(t *testing.T) {
	handler, cookie := publicationTestHandler(t)
	ctx := context.Background()
	if err := handler.store.SavePublication(ctx, storage.Publication{
		UID: "admin", NodeName: "feimu", RouteSlug: "feimu", Status: storage.PublicationFailed,
		Reason: "edge_sync_failed", NOSLAStatus: "not_configured", BWGStatus: "not_configured",
	}); err != nil {
		t.Fatal(err)
	}
	cleaned := serveAdminJSON(t, handler, http.MethodPost, "/api/admin/emby-servers/feimu/publish/cleanup", nil, cookie)
	if cleaned.Code != http.StatusOK || !strings.Contains(cleaned.Body.String(), `"status":"saved_unpublished"`) {
		t.Fatalf("cleanup status=%d body=%s", cleaned.Code, cleaned.Body.String())
	}
}

func TestUnpublishEmptyHelperFailureHasActionableCode(t *testing.T) {
	handler, cookie := publicationTestHandler(t)
	handler.SetPublicationSyncer(emptyUnpublishFailureSyncer{})
	published := serveAdminJSON(t, handler, http.MethodPost, "/api/admin/emby-servers/feimu/publish", nil, cookie)
	if published.Code != http.StatusOK || !strings.Contains(published.Body.String(), `"status":"published"`) {
		t.Fatalf("publish status=%d body=%s", published.Code, published.Body.String())
	}
	unpublished := serveAdminJSON(t, handler, http.MethodPost, "/api/admin/emby-servers/feimu/unpublish", nil, cookie)
	body := unpublished.Body.String()
	if unpublished.Code != http.StatusOK || !strings.Contains(body, `"reason":"helper_failed"`) || !strings.Contains(body, `"failed_step":"edge_unpublish"`) {
		t.Fatalf("unpublish status=%d body=%s", unpublished.Code, body)
	}
}

func TestPublicationDryRunIsSafeAndDoesNotMutate(t *testing.T) {
	handler, cookie := publicationTestHandler(t)
	response := serveAdminJSON(t, handler, http.MethodPost, "/api/admin/emby-servers/feimu/publish/dry-run", nil, cookie)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"dry_run":true`) || !strings.Contains(response.Body.String(), `"public_path":"/https/\u003csaved-host\u003e/443"`) {
		t.Fatalf("dry-run status=%d body=%s", response.Code, response.Body.String())
	}
	for _, forbidden := range []string{"media.example", "https://media.example/base", "token=", "Authorization"} {
		if strings.Contains(response.Body.String(), forbidden) {
			t.Fatalf("dry-run leaked %q: %s", forbidden, response.Body.String())
		}
	}
	publication, err := handler.store.GetPublication(context.Background(), "admin", "feimu")
	if err != nil || publication != nil {
		t.Fatalf("dry-run publication=%+v err=%v", publication, err)
	}
	route, err := handler.store.GetManagedRoute(context.Background(), "feimu")
	if err != nil || route != nil {
		t.Fatalf("dry-run route=%+v err=%v", route, err)
	}
}

func TestPublicationDryRunPreservesButRedactsSavedBasePath(t *testing.T) {
	handler, cookie := publicationTestHandler(t)
	if err := handler.store.SaveNode(context.Background(), "admin", storage.Node{Name: "withbase", Target: "https://base.example/private-base"}); err != nil {
		t.Fatal(err)
	}
	response := serveAdminJSON(t, handler, http.MethodPost, "/api/admin/emby-servers/withbase/publish/dry-run", nil, cookie)
	body := response.Body.String()
	if response.Code != http.StatusOK || !strings.Contains(body, `"public_path":"/https/\u003csaved-host\u003e/443/\u003csaved-base-path\u003e"`) || !strings.Contains(body, `"has_base_path":true`) {
		t.Fatalf("dry-run status=%d body=%s", response.Code, body)
	}
	for _, forbidden := range []string{"base.example", "private-base"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("dry-run leaked %q: %s", forbidden, body)
		}
	}
}

func TestPublicationFailsClosedWithoutEdgeSyncer(t *testing.T) {
	handler, cookie := publicationTestHandler(t)
	response := serveAdminJSON(t, handler, http.MethodPost, "/api/admin/emby-servers/feimu/publish", nil, cookie)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status":"publish_failed"`) || !strings.Contains(response.Body.String(), `"failed_step":"edge_sync"`) {
		t.Fatalf("publish status=%d body=%s", response.Code, response.Body.String())
	}
	route, err := handler.store.GetManagedRoute(context.Background(), "feimu")
	if err != nil || route != nil {
		t.Fatalf("failed publish route=%+v err=%v", route, err)
	}
}

func TestPartialPublishRollsBackAndDoesNotLeaveManagedRoute(t *testing.T) {
	handler, cookie := publicationTestHandler(t)
	handler.SetPublicationSyncer(partialPublicationSyncer{rollbackSucceeds: true})
	response := serveAdminJSON(t, handler, http.MethodPost, "/api/admin/emby-servers/feimu/publish", nil, cookie)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status":"publish_failed"`) {
		t.Fatalf("publish status=%d body=%s", response.Code, response.Body.String())
	}
	publication, err := handler.store.GetPublication(context.Background(), "admin", "feimu")
	if err != nil || publication == nil || publication.NOSLAStatus != "removed" || publication.BWGStatus != "removed" {
		t.Fatalf("publication=%+v err=%v", publication, err)
	}
	route, err := handler.store.GetManagedRoute(context.Background(), "feimu")
	if err != nil || route != nil {
		t.Fatalf("rollback route=%+v err=%v", route, err)
	}
}

func TestPartialPublishRollbackFailureRequiresReconciliation(t *testing.T) {
	handler, cookie := publicationTestHandler(t)
	handler.SetPublicationSyncer(partialPublicationSyncer{})
	response := serveAdminJSON(t, handler, http.MethodPost, "/api/admin/emby-servers/feimu/publish", nil, cookie)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status":"needs_sync"`) {
		t.Fatalf("publish status=%d body=%s", response.Code, response.Body.String())
	}
	deleted := serveAdminJSON(t, handler, http.MethodPost, "/admin/api", map[string]any{"action": "delete", "name": "feimu"}, cookie)
	if !strings.Contains(deleted.Body.String(), "PUBLISHED_REQUIRES_UNPUBLISH") {
		t.Fatalf("delete was not blocked: %s", deleted.Body.String())
	}
	node, err := handler.store.GetNode(context.Background(), "admin", "feimu")
	if err != nil || node == nil {
		t.Fatalf("node was deleted: node=%+v err=%v", node, err)
	}
}

func TestPublishDoesNotOverwriteExistingManagedRoute(t *testing.T) {
	handler, cookie := publicationTestHandler(t)
	handler.SetPublicationSyncer(successfulPublicationSyncer{})
	ctx := context.Background()
	existing := storage.ManagedRoute{Slug: "feimu", NodeName: "other", Enabled: true, Public: true, DefaultLine: "existing"}
	lines := []storage.ManagedRouteLine{{RouteSlug: "feimu", LineSlug: "existing", Target: "https://existing.example", Enabled: true, Position: 1}}
	if err := handler.store.SaveManagedRoute(ctx, existing, lines); err != nil {
		t.Fatal(err)
	}
	response := serveAdminJSON(t, handler, http.MethodPost, "/api/admin/emby-servers/feimu/publish", nil, cookie)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "MANAGED_ROUTE_REQUIRES_RECONCILIATION") {
		t.Fatalf("publish status=%d body=%s", response.Code, response.Body.String())
	}
	got, err := handler.store.GetManagedRoute(ctx, "feimu")
	if err != nil || got == nil || got.NodeName != "other" || got.DefaultLine != "existing" {
		t.Fatalf("existing route changed: route=%+v err=%v", got, err)
	}
}

func TestLegacyPublishedNodeCannotLeaveOrphans(t *testing.T) {
	cfg := config.Config{
		AdminToken: "strong-admin-token", PublicMediaBaseURL: "https://stream.example",
		PublicMediaNodePaths: map[string]string{"uhd": "/https/media.example/443"},
	}
	handler := newAuthTestHandler(t, cfg)
	if err := handler.store.SaveNode(context.Background(), "admin", storage.Node{Name: "uhd", Target: "https://media.example"}); err != nil {
		t.Fatal(err)
	}
	login := serveAdminJSON(t, handler, http.MethodPost, "/admin/auth/login", map[string]any{"token": cfg.AdminToken}, nil)
	cookie := login.Result().Cookies()[0]
	unpublish := serveAdminJSON(t, handler, http.MethodPost, "/api/admin/emby-servers/uhd/unpublish", nil, cookie)
	if !strings.Contains(unpublish.Body.String(), "legacy_publication_requires_migration") {
		t.Fatalf("legacy unpublish body=%s", unpublish.Body.String())
	}
	deleteResponse := serveAdminJSON(t, handler, http.MethodPost, "/admin/api", map[string]any{"action": "delete", "name": "uhd"}, cookie)
	if !strings.Contains(deleteResponse.Body.String(), "PUBLISHED_REQUIRES_UNPUBLISH") {
		t.Fatalf("legacy delete body=%s", deleteResponse.Body.String())
	}
	node, err := handler.store.GetNode(context.Background(), "admin", "uhd")
	if err != nil || node == nil {
		t.Fatalf("legacy node was deleted: node=%+v err=%v", node, err)
	}
}

func TestPublicationPublishStatusUnpublishAndDelete(t *testing.T) {
	handler, cookie := publicationTestHandler(t)
	handler.SetPublicationSyncer(successfulPublicationSyncer{})
	published := serveAdminJSON(t, handler, http.MethodPost, "/api/admin/emby-servers/feimu/publish", nil, cookie)
	expectedPublicURL := `"public_url":"https://` + `stream.example/https/` + `media.example/443"`
	if published.Code != http.StatusOK || !strings.Contains(published.Body.String(), `"status":"published"`) || !strings.Contains(published.Body.String(), expectedPublicURL) {
		t.Fatalf("publish status=%d body=%s", published.Code, published.Body.String())
	}
	route, err := handler.store.GetManagedRoute(context.Background(), "feimu")
	if err != nil || route == nil || !route.Enabled || !route.Public || route.NodeName != "feimu" {
		t.Fatalf("route=%+v err=%v", route, err)
	}
	status := serveAdminJSON(t, handler, http.MethodGet, "/api/admin/emby-servers/feimu/publish-status", nil, cookie)
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"nosla_status":"synced"`) || !strings.Contains(status.Body.String(), `"bwg_status":"synced"`) {
		t.Fatalf("status=%d body=%s", status.Code, status.Body.String())
	}
	blockedDelete := serveAdminJSON(t, handler, http.MethodPost, "/admin/api", map[string]any{"action": "delete", "name": "feimu"}, cookie)
	if !strings.Contains(blockedDelete.Body.String(), "PUBLISHED_REQUIRES_UNPUBLISH") {
		t.Fatalf("published delete body=%s", blockedDelete.Body.String())
	}
	unpublished := serveAdminJSON(t, handler, http.MethodPost, "/api/admin/emby-servers/feimu/unpublish", nil, cookie)
	if unpublished.Code != http.StatusOK || !strings.Contains(unpublished.Body.String(), `"status":"saved_unpublished"`) {
		t.Fatalf("unpublish status=%d body=%s", unpublished.Code, unpublished.Body.String())
	}
	route, err = handler.store.GetManagedRoute(context.Background(), "feimu")
	if err != nil || route != nil {
		t.Fatalf("unpublished route=%+v err=%v", route, err)
	}
	node, err := handler.store.GetNode(context.Background(), "admin", "feimu")
	if err != nil || node == nil {
		t.Fatalf("upstream was removed: node=%+v err=%v", node, err)
	}
	deleted := serveAdminJSON(t, handler, http.MethodPost, "/admin/api", map[string]any{"action": "delete", "name": "feimu"}, cookie)
	if deleted.Code != http.StatusOK || !strings.Contains(deleted.Body.String(), `"ok":true`) {
		t.Fatalf("delete status=%d body=%s", deleted.Code, deleted.Body.String())
	}
}

func TestPublishedNodeRejectsPrimaryTargetChange(t *testing.T) {
	handler, cookie := publicationTestHandler(t)
	handler.SetPublicationSyncer(successfulPublicationSyncer{})
	published := serveAdminJSON(t, handler, http.MethodPost, "/api/admin/emby-servers/feimu/publish", nil, cookie)
	if published.Code != http.StatusOK || !strings.Contains(published.Body.String(), `"status":"published"`) {
		t.Fatalf("publish status=%d body=%s", published.Code, published.Body.String())
	}
	changed := serveAdminJSON(t, handler, http.MethodPost, "/admin/api", map[string]any{
		"action": "save",
		"node":   map[string]any{"name": "feimu", "oldName": "feimu", "target": "https://changed.example"},
	}, cookie)
	if !strings.Contains(changed.Body.String(), "PUBLISHED_PRIMARY_UPSTREAM_CANNOT_CHANGE") {
		t.Fatalf("published target change was not blocked: %s", changed.Body.String())
	}
	node, err := handler.store.GetNode(context.Background(), "admin", "feimu")
	if err != nil || node == nil || node.Target != "https://media.example" {
		t.Fatalf("published target changed: node=%+v err=%v", node, err)
	}
}

func TestPublicationSupportsMultipleSavedUpstreams(t *testing.T) {
	handler, cookie := publicationTestHandler(t)
	handler.SetPublicationSyncer(successfulPublicationSyncer{})
	saved := serveAdminJSON(t, handler, http.MethodPost, "/admin/api", map[string]any{
		"action": "save",
		"node": map[string]any{"name": "feimu", "oldName": "feimu",
			"target": "https://media.example\nhttps://backup.example"},
	}, cookie)
	if !strings.Contains(saved.Body.String(), `"ok":true`) {
		t.Fatalf("multi-line save failed: %s", saved.Body.String())
	}
	published := serveAdminJSON(t, handler, http.MethodPost, "/api/admin/emby-servers/feimu/publish", nil, cookie)
	if published.Code != http.StatusOK || !strings.Contains(published.Body.String(), `"status":"published"`) {
		t.Fatalf("multi-line publish status=%d body=%s", published.Code, published.Body.String())
	}
	lines, err := handler.store.ListManagedRouteLines(context.Background(), "feimu")
	if err != nil || len(lines) != 2 || lines[0].LineSlug != "main" || lines[1].LineSlug != "backup-2" {
		t.Fatalf("managed lines=%+v err=%v", lines, err)
	}
}

func TestSavedUpstreamLinesCheckControlledTLSMocksIndependently(t *testing.T) {
	var primaryRequests, backupRequests atomic.Int32
	primary := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		primaryRequests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer primary.Close()
	backup := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		backupRequests.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer backup.Close()
	client := &http.Client{
		Timeout:       2 * time.Second,
		Transport:     &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}, // Test servers use ephemeral certificates.
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}
	results := checkSavedUpstreamLinesWithClient(context.Background(), storage.Node{
		Name: "staging-multiline", Target: primary.URL + "\n" + backup.URL,
	}, client)
	if len(results) != 2 || results[0]["line_id"] != "main" || results[0]["health"] != "reachable" ||
		results[1]["line_id"] != "backup-2" || results[1]["health"] != "unhealthy" {
		t.Fatalf("line results=%+v", results)
	}
	if primaryRequests.Load() != 1 || backupRequests.Load() != 1 {
		t.Fatalf("request counts primary=%d backup=%d", primaryRequests.Load(), backupRequests.Load())
	}
}

func TestMultilineStagingWorkflowPreservesPublicURLAndUnrelatedRoute(t *testing.T) {
	handler, cookie := publicationTestHandler(t)
	handler.SetPublicationSyncer(operationIDPublicationSyncer{})
	ctx := context.Background()
	if err := handler.store.SaveManagedRoute(ctx, storage.ManagedRoute{
		Slug: "unrelated", NodeName: "unrelated", Enabled: true, Public: true, DefaultLine: "main",
	}, []storage.ManagedRouteLine{{
		RouteSlug: "unrelated", LineSlug: "main", Target: "https://unrelated.example", Enabled: true, Position: 1,
	}}); err != nil {
		t.Fatal(err)
	}
	saved := serveAdminJSON(t, handler, http.MethodPost, "/admin/api", map[string]any{
		"action": "save", "node": map[string]any{
			"name": "staging-multiline", "target": "https://primary.mock.example; https://backup.mock.example",
		},
	}, cookie)
	if !strings.Contains(saved.Body.String(), `"ok":true`) {
		t.Fatalf("save body=%s", saved.Body.String())
	}
	dryRun := serveAdminJSON(t, handler, http.MethodPost, "/api/admin/emby-servers/staging-multiline/publish/dry-run", nil, cookie)
	if dryRun.Code != http.StatusOK || !strings.Contains(dryRun.Body.String(), `"line_count":2`) ||
		strings.Contains(dryRun.Body.String(), "PUBLISH_REQUIRES_ONE_SAVED_UPSTREAM") {
		t.Fatalf("dry-run status=%d body=%s", dryRun.Code, dryRun.Body.String())
	}
	published := serveAdminJSON(t, handler, http.MethodPost, "/api/admin/emby-servers/staging-multiline/publish", nil, cookie)
	if published.Code != http.StatusOK || !strings.Contains(published.Body.String(), `"status":"published"`) {
		t.Fatalf("publish status=%d body=%s", published.Code, published.Body.String())
	}
	before, _ := handler.store.GetPublication(ctx, "admin", "staging-multiline")
	for _, target := range []string{
		"https://primary.mock.example\nhttps://backup.mock.example\nhttps://backup-3.mock.example",
		"https://primary.mock.example\nhttps://backup.mock.example",
	} {
		changed := serveAdminJSON(t, handler, http.MethodPost, "/admin/api", map[string]any{
			"action": "save", "node": map[string]any{
				"name": "staging-multiline", "oldName": "staging-multiline", "target": target,
			},
		}, cookie)
		if !strings.Contains(changed.Body.String(), `"publication_sync":"synced"`) {
			t.Fatalf("line update body=%s", changed.Body.String())
		}
	}
	after, err := handler.store.GetPublication(ctx, "admin", "staging-multiline")
	if err != nil || before == nil || after == nil || before.PublicURL != after.PublicURL || after.Status != storage.PublicationPublished {
		t.Fatalf("publication before=%+v after=%+v err=%v", before, after, err)
	}
	lines, err := handler.store.ListManagedRouteLines(ctx, "staging-multiline")
	if err != nil || len(lines) != 2 || lines[0].LineSlug != "main" || lines[1].LineSlug != "backup-2" {
		t.Fatalf("managed lines=%+v err=%v", lines, err)
	}
	unrelated, err := handler.store.ListManagedRouteLines(ctx, "unrelated")
	if err != nil || len(unrelated) != 1 || unrelated[0].Target != "https://unrelated.example" {
		t.Fatalf("unrelated lines=%+v err=%v", unrelated, err)
	}
}

func TestPublishedPublicationCanRefreshEdgeConfiguration(t *testing.T) {
	handler, cookie := publicationTestHandler(t)
	handler.SetPublicationSyncer(successfulPublicationSyncer{})
	first := serveAdminJSON(t, handler, http.MethodPost, "/api/admin/emby-servers/feimu/publish", nil, cookie)
	if first.Code != http.StatusOK || !strings.Contains(first.Body.String(), `"status":"published"`) {
		t.Fatalf("initial publish status=%d body=%s", first.Code, first.Body.String())
	}
	refresh := serveAdminJSON(t, handler, http.MethodPost, "/api/admin/emby-servers/feimu/publish", nil, cookie)
	if refresh.Code != http.StatusOK || !strings.Contains(refresh.Body.String(), `"reason":"configuration_refreshed"`) {
		t.Fatalf("refresh status=%d body=%s", refresh.Code, refresh.Body.String())
	}
	publication, err := handler.store.GetPublication(context.Background(), "admin", "feimu")
	if err != nil || publication == nil || publication.Status != storage.PublicationPublished || publication.PublicURL == "" {
		t.Fatalf("publication=%+v err=%v", publication, err)
	}
}

func TestPublishedNodeCanAddAndRemoveBackupWithoutChangingPublicURL(t *testing.T) {
	handler, cookie := publicationTestHandler(t)
	handler.SetPublicationSyncer(operationIDPublicationSyncer{})
	published := serveAdminJSON(t, handler, http.MethodPost, "/api/admin/emby-servers/feimu/publish", nil, cookie)
	if published.Code != http.StatusOK || !strings.Contains(published.Body.String(), `"status":"published"`) {
		t.Fatalf("publish status=%d body=%s", published.Code, published.Body.String())
	}
	before, _ := handler.store.GetPublication(context.Background(), "admin", "feimu")
	for _, target := range []string{
		"https://media.example\nhttps://backup.example",
		"https://media.example",
	} {
		changed := serveAdminJSON(t, handler, http.MethodPost, "/admin/api", map[string]any{
			"action": "save",
			"node":   map[string]any{"name": "feimu", "oldName": "feimu", "target": target},
		}, cookie)
		if !strings.Contains(changed.Body.String(), `"publication_sync":"synced"`) {
			t.Fatalf("published line update failed: %s", changed.Body.String())
		}
	}
	after, err := handler.store.GetPublication(context.Background(), "admin", "feimu")
	if err != nil || after == nil || before == nil || after.PublicURL != before.PublicURL || after.Status != storage.PublicationPublished {
		t.Fatalf("publication before=%+v after=%+v err=%v", before, after, err)
	}
	lines, err := handler.store.ListManagedRouteLines(context.Background(), "feimu")
	if err != nil || len(lines) != 1 || lines[0].Target != "https://media.example" {
		t.Fatalf("managed lines=%+v err=%v", lines, err)
	}
}

func TestPublicationUIIsPrimaryOwnerWorkflow(t *testing.T) {
	for _, marker := range []string{
		"/api/admin/emby-servers",
		"function publishNode(",
		"function unpublishNode(",
		"function reconcilePublication(",
		"function cleanupPublication(",
		"function showPublicationDryRun(",
		"function checkPublishedProxy(",
		"发布反代",
		"取消发布",
		"复制 Yamby 地址",
		"已保存上游，尚未发布公网反代入口",
		"已发布公网反代入口",
		"NOSLA:",
		"BWG:",
		"高级路由",
	} {
		if !strings.Contains(indexHTML, marker) {
			t.Fatalf("publication UI marker %q is missing", marker)
		}
	}
}
