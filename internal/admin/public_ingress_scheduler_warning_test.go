package admin

import (
	"context"
	"testing"

	"embyproxy/internal/config"
)

func TestPublicIngressSchedulerWarningIsPersistedAndCleared(t *testing.T) {
	h := newAuthTestHandler(t, config.Config{PublicIngressHost: "stream.example.com"})
	s := newPublicIngressSwitcher(h)
	ctx := context.Background()
	stuck := publicIngressState{OperationID: "stuck", Phase: "submitting_dns", ActiveNodeID: "old"}
	if err := s.save(ctx, stuck); err != nil {
		t.Fatal(err)
	}
	if err := s.reconcileWithWarning(ctx); err == nil {
		t.Fatal("in-flight state was ignored")
	}
	if status := newPublicIngressSwitcher(h).schedulerStatus(ctx); status.Error != "recovery_required" || status.Trigger != "automatic" {
		t.Fatalf("warning=%+v", status)
	}
	before := s.schedulerStatus(ctx)
	if err := s.reconcileWithWarning(ctx); err == nil {
		t.Fatal("repeat in-flight evaluation succeeded")
	}
	if after := s.schedulerStatus(ctx); after.UpdatedAt != before.UpdatedAt || after.Error != before.Error {
		t.Fatalf("duplicate warning refreshed: before=%+v after=%+v", before, after)
	}
	stuck.Phase, stuck.Mode = "verified", "fixed"
	if err := s.save(ctx, stuck); err != nil {
		t.Fatal(err)
	}
	if err := s.reconcileWithWarning(ctx); err != nil {
		t.Fatal(err)
	}
	if status := s.schedulerStatus(ctx); status.Error != "" {
		t.Fatalf("fixed mode retained warning: %+v", status)
	}
}

func TestPublicIngressUninitializedWarningUsesFixedCode(t *testing.T) {
	h := newAuthTestHandler(t, config.Config{PublicIngressHost: "stream.example.com"})
	s := newPublicIngressSwitcher(h)
	ctx := context.Background()
	if err := s.reconcileWithWarning(ctx); err == nil {
		t.Fatal("empty state accepted")
	}
	if got := s.schedulerStatus(ctx); got.Error != "state_unavailable" {
		t.Fatalf("warning=%+v", got)
	}
}

func TestPublicIngressNoCandidateWarningKeepsReason(t *testing.T) {
	h := newAuthTestHandler(t, config.Config{PublicIngressHost: "stream.example.com"})
	s := newPublicIngressSwitcher(h)
	ctx := context.Background()
	state := publicIngressState{OperationID: "recent", Phase: "verified", Mode: "preferred", ActiveNodeID: "missing"}
	if err := s.save(ctx, state); err != nil {
		t.Fatal(err)
	}
	if err := s.reconcileWithWarning(ctx); err == nil {
		t.Fatal("no candidate reported success")
	}
	if got := s.schedulerStatus(ctx); got.Error != "no_eligible_public_ingress_candidate" || got.Trigger != "automatic_health" {
		t.Fatalf("reason changed: %+v", got)
	}
}
