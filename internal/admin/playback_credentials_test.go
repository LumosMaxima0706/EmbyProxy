package admin

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFilePlaybackCredentialStoreUses0600AndNeverReturnsMetadata(t *testing.T) {
	dir := t.TempDir()
	store, err := newFilePlaybackCredentialStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if store.PlaybackCredentialConfigured(context.Background(), "demo") {
		t.Fatal("missing credential reported configured")
	}
	if err := store.WritePlaybackCredential(context.Background(), "demo", "runtime-only-token"); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "demo.token")
	info, err := os.Stat(p)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("credential mode=%v err=%v", info.Mode().Perm(), err)
	}
	if got, err := store.ReadPlaybackCredential(context.Background(), "demo"); err != nil || got != "runtime-only-token" {
		t.Fatalf("read=%q err=%v", got, err)
	}
	if err := store.DeletePlaybackCredential(context.Background(), "demo"); err != nil {
		t.Fatal(err)
	}
	if store.PlaybackCredentialConfigured(context.Background(), "demo") {
		t.Fatal("deleted credential still configured")
	}
}

func TestFilePlaybackCredentialStoreRejectsUnsafeValues(t *testing.T) {
	store, err := newFilePlaybackCredentialStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"", "line\nbreak", "line\rbreak"} {
		if err := store.WritePlaybackCredential(context.Background(), "demo", value); err == nil {
			t.Fatalf("unsafe value accepted: %q", value)
		}
	}
}
