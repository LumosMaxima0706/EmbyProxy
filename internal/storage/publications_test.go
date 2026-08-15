package storage

import (
	"context"
	"path/filepath"
	"testing"
)

func TestPublicationStatePersistsAndDeletes(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "proxy.db")
	store, err := New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	p := Publication{
		UID: "admin", NodeName: "demo", RouteSlug: "demo", Status: PublicationPublishing,
		Reason: "sync_in_progress", NOSLAStatus: "pending", BWGStatus: "pending", UpdatedAt: 1,
	}
	if err := store.SavePublication(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, err := reopened.GetPublication(ctx, "admin", "demo")
	if err != nil || got == nil || got.Status != PublicationPublishing || got.NOSLAStatus != "pending" {
		t.Fatalf("publication=%+v err=%v", got, err)
	}
	if err := reopened.DeletePublication(ctx, "admin", "demo"); err != nil {
		t.Fatal(err)
	}
	got, err = reopened.GetPublication(ctx, "admin", "demo")
	if err != nil || got != nil {
		t.Fatalf("deleted publication=%+v err=%v", got, err)
	}
}
