package storage

import (
	"context"
	"database/sql"
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
		Reason: "sync_in_progress", NOSLAStatus: "pending", BWGStatus: "pending", PlaybackStatus: "unverified", UpdatedAt: 1,
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
	if err != nil || got == nil || got.Status != PublicationPublishing || got.NOSLAStatus != "pending" || got.PlaybackStatus != "unverified" {
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

func TestPublicationSchemaMigratesPlaybackVerificationColumns(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
CREATE TABLE emby_publications (
  uid TEXT NOT NULL, node_name TEXT NOT NULL, route_slug TEXT NOT NULL,
  public_url TEXT NOT NULL DEFAULT '', status TEXT NOT NULL,
  reason TEXT NOT NULL DEFAULT '', failed_step TEXT NOT NULL DEFAULT '',
  nosla_status TEXT NOT NULL DEFAULT 'unknown', bwg_status TEXT NOT NULL DEFAULT 'unknown',
  updated_at INTEGER NOT NULL, PRIMARY KEY(uid, node_name)
);
`); err != nil {
		t.Fatal(err)
	}
	publicURL := "https://" + "stream.example/https/" + "media.example/443"
	if _, err := db.Exec(`INSERT INTO emby_publications
  (uid,node_name,route_slug,public_url,status,reason,failed_step,nosla_status,bwg_status,updated_at)
	VALUES ('admin','demo','demo',?,'published','public_entry_configured','','synced','synced',1)`, publicURL); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	publication, err := store.GetPublication(ctx, "admin", "demo")
	if err != nil || publication == nil || publication.PlaybackStatus != "unverified" || publication.PlaybackVerifiedAt != 0 {
		t.Fatalf("publication=%+v err=%v", publication, err)
	}
	if err := store.SetPublicationPlaybackVerified(ctx, "admin", "demo", 123); err != nil {
		t.Fatal(err)
	}
	publication, err = store.GetPublication(ctx, "admin", "demo")
	if err != nil || publication == nil || publication.PlaybackStatus != "ok" || publication.PlaybackVerifiedAt != 123 {
		t.Fatalf("verified publication=%+v err=%v", publication, err)
	}
}
