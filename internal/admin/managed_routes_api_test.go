package admin

import (
	"net/http"
	"strings"
	"testing"
)

func TestManagedRoutesAPIRequiresAuthentication(t *testing.T) {
	handler := newAuthTestHandler(t, failoverTestConfig())
	response := serveAdminJSON(t, handler, http.MethodGet, "/api/admin/managed-routes", nil, nil)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestManagedRoutesAPICreateListUpdateDelete(t *testing.T) {
	handler := newAuthTestHandler(t, failoverTestConfig())
	login := serveAdminJSON(t, handler, http.MethodPost, "/admin/auth/login", map[string]any{"token": "strong-admin-token"}, nil)
	if login.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", login.Code, login.Body.String())
	}
	cookie := login.Result().Cookies()[0]
	body := map[string]any{
		"node_name":    "nosla-node",
		"enabled":      true,
		"public":       true,
		"default_line": "main",
		"lines": []any{
			map[string]any{"line_slug": "main", "target": "https://media.example/base", "enabled": true, "position": 1},
		},
	}
	created := serveAdminJSON(t, handler, http.MethodPut, "/api/admin/managed-routes/demo", body, cookie)
	if created.Code != http.StatusOK || !strings.Contains(created.Body.String(), `"slug":"demo"`) {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}

	listed := serveAdminJSON(t, handler, http.MethodGet, "/api/admin/managed-routes", nil, cookie)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"line_slug":"main"`) {
		t.Fatalf("list status=%d body=%s", listed.Code, listed.Body.String())
	}

	body["node_name"] = "bwg-node"
	body["default_line"] = "fallback"
	body["lines"] = []any{
		map[string]any{"line_slug": "fallback", "target": "https://backup.example/base", "enabled": true, "position": 1},
	}
	updated := serveAdminJSON(t, handler, http.MethodPut, "/api/admin/managed-routes/demo", body, cookie)
	if updated.Code != http.StatusOK || !strings.Contains(updated.Body.String(), `"node_name":"bwg-node"`) {
		t.Fatalf("update status=%d body=%s", updated.Code, updated.Body.String())
	}

	deleted := serveAdminJSON(t, handler, http.MethodDelete, "/api/admin/managed-routes/demo", nil, cookie)
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", deleted.Code, deleted.Body.String())
	}
	missing := serveAdminJSON(t, handler, http.MethodGet, "/api/admin/managed-routes/demo", nil, cookie)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing status=%d body=%s", missing.Code, missing.Body.String())
	}
}

func TestManagedRoutesAPIRejectsUnsafeOrInvalidRoutes(t *testing.T) {
	handler := newAuthTestHandler(t, failoverTestConfig())
	login := serveAdminJSON(t, handler, http.MethodPost, "/admin/auth/login", map[string]any{"token": "strong-admin-token"}, nil)
	cookie := login.Result().Cookies()[0]
	invalidSlug := serveAdminJSON(t, handler, http.MethodPut, "/api/admin/managed-routes/../bad", map[string]any{}, cookie)
	if invalidSlug.Code != http.StatusBadRequest && invalidSlug.Code != http.StatusNotFound {
		t.Fatalf("invalid slug status=%d body=%s", invalidSlug.Code, invalidSlug.Body.String())
	}
	unsafeTarget := serveAdminJSON(t, handler, http.MethodPut, "/api/admin/managed-routes/demo", map[string]any{
		"node_name": "node",
		"enabled":   true,
		"public":    true,
		"lines": []any{
			map[string]any{"line_slug": "main", "target": "https://media.example/base?token=hidden", "enabled": true},
		},
	}, cookie)
	if unsafeTarget.Code != http.StatusBadRequest || !strings.Contains(unsafeTarget.Body.String(), "INVALID_LINE_TARGET") {
		t.Fatalf("unsafe target status=%d body=%s", unsafeTarget.Code, unsafeTarget.Body.String())
	}
}
