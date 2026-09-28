package admin

import (
	"context"
	"net/http"
	"testing"

	"embyproxy/internal/config"
)

func TestPublicIngressRecoveryAPIRequiresAdminConfirmation(t *testing.T) {
	h := newAuthTestHandler(t, config.Config{AdminToken: "strong-admin-token", PublicIngressHost: "stream.example.com"})
	h.publicIngress = newPublicIngressSwitcher(h)
	ctx := context.Background()
	state := publicIngressState{OperationID: "locked", Phase: "rollback_failed", PreviousAddress: "1.1.1.1", ActiveNodeID: "old-node"}
	if err := h.publicIngress.save(ctx, state); err != nil {
		t.Fatal(err)
	}
	path := "/api/admin/public-ingress/confirm-previous"
	unauth := serveAdminJSON(t, h, http.MethodPost, path, map[string]any{"operation_id": "locked", "confirm": true}, nil)
	if unauth.Code == http.StatusOK {
		t.Fatal("unauthenticated recovery accepted")
	}
	login := serveAdminJSON(t, h, http.MethodPost, "/admin/auth/login", map[string]any{"token": "strong-admin-token"}, nil)
	if login.Code != http.StatusOK || len(login.Result().Cookies()) == 0 {
		t.Fatalf("login=%d", login.Code)
	}
	cookie := login.Result().Cookies()[0]
	noConfirm := serveAdminJSON(t, h, http.MethodPost, path, map[string]any{"operation_id": "locked"}, cookie)
	if noConfirm.Code != http.StatusBadRequest {
		t.Fatalf("missing confirmation=%d", noConfirm.Code)
	}
	wrongID := serveAdminJSON(t, h, http.MethodPost, path, map[string]any{"operation_id": "different", "confirm": true}, cookie)
	if wrongID.Code != http.StatusConflict {
		t.Fatalf("wrong operation=%d", wrongID.Code)
	}
	if got := h.publicIngress.status(ctx); got.OperationID != "locked" || got.Phase != "rollback_failed" {
		t.Fatalf("recovery API unlocked state: %+v", got)
	}
	state.PreviousAddress = ""
	if err := h.publicIngress.save(ctx, state); err != nil {
		t.Fatal(err)
	}
	missing := serveAdminJSON(t, h, http.MethodPost, path, map[string]any{"operation_id": "locked", "confirm": true}, cookie)
	if missing.Code != http.StatusConflict {
		t.Fatalf("unknown original address accepted: %d", missing.Code)
	}
	if got := h.publicIngress.status(ctx); got.Phase != "rollback_failed" {
		t.Fatalf("missing evidence unlocked state: %+v", got)
	}
}
