package storage

import (
	"context"
	"path/filepath"
	"testing"
)

func TestPutJSONPairIsAtomic(t *testing.T) {
	store, err := New(filepath.Join(t.TempDir(), "kv.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	kv := store.KV()
	if err := kv.PutJSONPair(ctx, "history", "current", map[string]string{"phase": "old"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `CREATE TRIGGER fail_current BEFORE UPDATE ON proxy_kv WHEN NEW.k='current' BEGIN SELECT RAISE(FAIL, 'simulated failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := kv.PutJSONPair(ctx, "history", "current", map[string]string{"phase": "new"}); err == nil {
		t.Fatal("second write failure was ignored")
	}
	for _, key := range []string{"history", "current"} {
		var value map[string]string
		if ok, err := kv.GetJSON(ctx, key, &value); err != nil || !ok || value["phase"] != "old" {
			t.Fatalf("partial update %s: %v %v %v", key, ok, value, err)
		}
	}
}
