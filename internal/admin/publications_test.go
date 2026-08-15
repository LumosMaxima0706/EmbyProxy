package admin

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"embyproxy/internal/config"
	"embyproxy/internal/storage"
)

type successfulPublicationSyncer struct{}

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
		OwnerAdminHost: "owner-admin.example", PublicMediaNodePaths: map[string]string{},
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

func TestPublicationAPIsRequireAuthentication(t *testing.T) {
	handler, _ := publicationTestHandler(t)
	for _, endpoint := range []struct{ method, path string }{
		{http.MethodGet, "/api/admin/emby-servers/feimu/publish-status"},
		{http.MethodPost, "/api/admin/emby-servers/feimu/publish/dry-run"},
		{http.MethodPost, "/api/admin/emby-servers/feimu/publish"},
		{http.MethodPost, "/api/admin/emby-servers/feimu/unpublish"},
	} {
		response := serveAdminJSON(t, handler, endpoint.method, endpoint.path, nil, nil)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s status=%d", endpoint.method, endpoint.path, response.Code)
		}
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
	if published.Code != http.StatusOK || !strings.Contains(published.Body.String(), `"status":"published"`) || !strings.Contains(published.Body.String(), `"public_url":"https://stream.example/https/media.example/443"`) {
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

func TestPublishedNodeRequiresUnpublishBeforeTargetChange(t *testing.T) {
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
	if !strings.Contains(changed.Body.String(), "PUBLISHED_REQUIRES_UNPUBLISH") {
		t.Fatalf("published target change was not blocked: %s", changed.Body.String())
	}
	node, err := handler.store.GetNode(context.Background(), "admin", "feimu")
	if err != nil || node == nil || node.Target != "https://media.example" {
		t.Fatalf("published target changed: node=%+v err=%v", node, err)
	}
}

func TestPublicationUIIsPrimaryOwnerWorkflow(t *testing.T) {
	for _, marker := range []string{
		"/api/admin/emby-servers",
		"function publishNode(",
		"function unpublishNode(",
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
