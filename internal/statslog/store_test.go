package statslog

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreIngestAndQueryAggregatesWithoutRawURI(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "global-stats.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	events := []Event{
		{OccurredAt: time.Now(), Host: "stream.example.invalid", Status: 200, PathClass: PlaybackInfo, RequestBytes: 10, ResponseBytes: 20, DurationMS: 5},
		{OccurredAt: time.Now(), Host: "stream.example.invalid", Status: 206, PathClass: VideoStream, RequestBytes: 5, ResponseBytes: 100, DurationMS: 40, Partial: true},
	}
	result, err := store.Ingest(context.Background(), "nosla", events)
	if err != nil || result.Parsed != 2 || result.PlaybackHints != 1 || result.Partial != 1 || result.ResponseBytes != 120 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	rows, err := store.QueryStats(context.Background(), 1)
	if err != nil || len(rows) == 0 {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	if rows[0].Node != "nosla" || rows[0].Client != "unknown" {
		t.Fatalf("row=%+v", rows[0])
	}
	activities, err := store.Recent(context.Background(), 10)
	if err != nil || len(activities) == 0 || activities[0].SourceNode != "nosla" {
		t.Fatalf("activities=%+v err=%v", activities, err)
	}
	snapshot, err := store.ExportSnapshot(context.Background(), "nosla", time.Now().Add(-time.Hour))
	if err != nil || len(snapshot) == 0 || snapshot[0].SourceNode != "nosla" {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
	other, err := Open(filepath.Join(t.TempDir(), "other.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if err := other.ImportSnapshot(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}
	rows, err = other.QueryStats(context.Background(), 1)
	if err != nil || len(rows) == 0 || rows[0].Node != "nosla" {
		t.Fatalf("imported rows=%+v err=%v", rows, err)
	}
}
