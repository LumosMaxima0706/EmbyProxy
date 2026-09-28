package admin

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"embyproxy/internal/config"
)

func TestPublicIngressCorruptStateFailsClosed(t *testing.T) {
	h := newAuthTestHandler(t, config.Config{PublicIngressHost: "stream.example.com", AdminToken: "strong-admin-token"})
	s := newPublicIngressSwitcher(h)
	ctx := context.Background()
	if err := h.store.KV().Put(ctx, publicIngressStateKey, "{not-json"); err != nil {
		t.Fatal(err)
	}
	if got := s.status(ctx); got.Phase != "recovery_required" || got.Error != "public_ingress_state_unreadable" {
		t.Fatalf("corrupt state=%+v", got)
	}
	h.publicIngress = s
	login := serveAdminJSON(t, h, http.MethodPost, "/admin/auth/login", map[string]any{"token": "strong-admin-token"}, nil)
	if login.Code != http.StatusOK || len(login.Result().Cookies()) == 0 {
		t.Fatalf("login=%d", login.Code)
	}
	response := serveAdminJSON(t, h, http.MethodGet, "/api/admin/public-ingress/status", nil, login.Result().Cookies()[0])
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "PUBLIC_INGRESS_STATE_UNREADABLE") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if got, err := s.switchTo(ctx, "node", "admin_manual", "preferred"); err == nil || got.Phase != "recovery_required" {
		t.Fatalf("manual switch accepted corrupt state: %+v err=%v", got, err)
	}
	if err := s.reconcileWithWarning(ctx); err == nil {
		t.Fatal("automatic switch accepted corrupt state")
	}
	if value, found, err := h.store.KV().Get(ctx, publicIngressStateKey); err != nil || !found || value != "{not-json" {
		t.Fatalf("corrupt state overwritten: %q %v %v", value, found, err)
	}
	for _, raw := range []string{"{}", `{"operation_id":"x"}`, `{"phase":"verified"}`} {
		if err := h.store.KV().Put(ctx, publicIngressStateKey, raw); err != nil {
			t.Fatal(err)
		}
		if state := s.status(ctx); state.Phase != "recovery_required" {
			t.Fatalf("incomplete JSON accepted: %q %+v", raw, state)
		}
	}
}
