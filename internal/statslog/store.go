package statslog

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

type StatRow struct {
	Day            string `json:"day"`
	Node           string `json:"node"`
	Client         string `json:"client"`
	Plays          int64  `json:"plays"`
	PlaybackMillis int64  `json:"playback_ms"`
	Bytes          int64  `json:"bytes"`
	InboundBytes   int64  `json:"inbound_bytes"`
	OutboundBytes  int64  `json:"outbound_bytes"`
	Sessions       int64  `json:"sessions"`
	Errors         int64  `json:"errors"`
	LastActivityAt int64  `json:"last_activity_at"`
	LastActivity   string `json:"last_activity"`
}

type Activity struct {
	At            int64  `json:"at"`
	SourceNode    string `json:"source_node"`
	PathClass     string `json:"path_class"`
	Status        int    `json:"status"`
	RequestCount  int64  `json:"request_count"`
	BytesOut      int64  `json:"bytes_out"`
	PlaybackHints int64  `json:"playback_start_hint"`
}

type SnapshotRow struct {
	BucketStart       int64  `json:"bucket_start"`
	SourceNode        string `json:"source_node"`
	PathClass         string `json:"path_class"`
	Status            int    `json:"status"`
	ClientClass       string `json:"client_class"`
	BytesIn           int64  `json:"bytes_in"`
	BytesOut          int64  `json:"bytes_out"`
	DurationMS        int64  `json:"duration_ms"`
	IsRange           bool   `json:"is_range"`
	Is206             bool   `json:"is_206"`
	RequestCount      int64  `json:"request_count"`
	PlaybackStartHint int64  `json:"playback_start_hint"`
	LastActivityAt    int64  `json:"last_activity_at"`
}

type AggregateResult struct {
	Parsed           int64
	Dropped          int64
	PlaybackHints    int64
	Partial          int64
	ResponseBytes    int64
	PlaybackInfo     int64
	VideoStream      int64
	HLSRequests      int64
	RecentActivities int64
}

func Summarize(events []Event) AggregateResult {
	result := AggregateResult{}
	for _, event := range events {
		result.Parsed++
		result.ResponseBytes += event.ResponseBytes
		if event.Partial {
			result.Partial++
		}
		switch event.PathClass {
		case PlaybackInfo:
			result.PlaybackInfo++
			if event.Status >= 200 && event.Status < 300 {
				result.PlaybackHints++
			}
		case VideoStream:
			result.VideoStream++
		case HLSManifest, HLSSegment:
			result.HLSRequests++
		}
	}
	return result
}

func Open(path string) (*Store, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("stats database path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;`); err != nil {
		_ = db.Close()
		return nil, err
	}
	store := &Store{db: db}
	if err := store.init(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) init(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS stats_events (
  bucket_start INTEGER NOT NULL,
  source_node TEXT NOT NULL CHECK (source_node IN ('nosla','bwg')),
  path_class TEXT NOT NULL,
  status INTEGER NOT NULL,
  client_class TEXT NOT NULL DEFAULT 'unknown',
  bytes_in INTEGER NOT NULL DEFAULT 0,
  bytes_out INTEGER NOT NULL DEFAULT 0,
  duration_ms INTEGER NOT NULL DEFAULT 0,
  is_range INTEGER NOT NULL DEFAULT 0,
  is_206 INTEGER NOT NULL DEFAULT 0,
  request_count INTEGER NOT NULL DEFAULT 0,
  playback_start_hint INTEGER NOT NULL DEFAULT 0,
  last_activity_at INTEGER NOT NULL,
  PRIMARY KEY (bucket_start, source_node, path_class, status, client_class)
);
CREATE INDEX IF NOT EXISTS idx_stats_events_activity ON stats_events(last_activity_at);
CREATE TABLE IF NOT EXISTS stats_cursors (
  source_node TEXT PRIMARY KEY,
  log_identity TEXT NOT NULL DEFAULT '',
  byte_offset INTEGER NOT NULL DEFAULT 0,
  updated_at INTEGER NOT NULL
);`)
	return err
}

func (s *Store) Ingest(ctx context.Context, source string, events []Event) (AggregateResult, error) {
	source = strings.ToLower(strings.TrimSpace(source))
	if source != "nosla" && source != "bwg" {
		return AggregateResult{}, errors.New("invalid stats source")
	}
	type key struct {
		bucket int64
		path   PathClass
		status int
	}
	type value struct {
		in, out, duration, requests, hints int64
		partial, status206                 int64
		last                               int64
	}
	aggregates := map[key]value{}
	result := AggregateResult{}
	for _, event := range events {
		result.Parsed++
		switch event.PathClass {
		case PlaybackInfo:
			result.PlaybackInfo++
		case VideoStream:
			result.VideoStream++
		case HLSManifest, HLSSegment:
			result.HLSRequests++
		}
		bucket := event.OccurredAt.Truncate(time.Minute).Unix()
		item := aggregates[key{bucket: bucket, path: event.PathClass, status: event.Status}]
		item.in += event.RequestBytes
		item.out += event.ResponseBytes
		item.duration += event.DurationMS
		item.requests++
		if event.Partial {
			item.partial = 1
			item.status206++
			result.Partial++
		}
		if event.PathClass == PlaybackInfo && event.Status >= 200 && event.Status < 300 {
			item.hints++
			result.PlaybackHints++
		}
		if event.OccurredAt.Unix() > item.last {
			item.last = event.OccurredAt.Unix()
		}
		aggregates[key{bucket: bucket, path: event.PathClass, status: event.Status}] = item
		result.ResponseBytes += event.ResponseBytes
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AggregateResult{}, err
	}
	for itemKey, item := range aggregates {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO stats_events (bucket_start, source_node, path_class, status, client_class,
 bytes_in, bytes_out, duration_ms, is_range, is_206, request_count,
 playback_start_hint, last_activity_at)
VALUES (?, ?, ?, ?, 'unknown', ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(bucket_start, source_node, path_class, status, client_class) DO UPDATE SET
 bytes_in=bytes_in+excluded.bytes_in, bytes_out=bytes_out+excluded.bytes_out,
 duration_ms=duration_ms+excluded.duration_ms, is_range=MAX(is_range, excluded.is_range),
 is_206=MAX(is_206, excluded.is_206), request_count=request_count+excluded.request_count,
 playback_start_hint=playback_start_hint+excluded.playback_start_hint,
 last_activity_at=MAX(last_activity_at, excluded.last_activity_at)
`, itemKey.bucket, source, itemKey.path, itemKey.status, item.in, item.out, item.duration, item.partial, item.status206, item.requests, item.hints, item.last); err != nil {
			_ = tx.Rollback()
			return AggregateResult{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return AggregateResult{}, err
	}
	return result, nil
}

func (s *Store) QueryStats(ctx context.Context, days int) ([]StatRow, error) {
	if days < 1 {
		days = 1
	}
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	now := time.Now().In(location)
	cutoff := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location).AddDate(0, 0, -(days - 1)).Unix()
	rows, err := s.db.QueryContext(ctx, `
SELECT bucket_start, source_node, client_class,
       SUM(playback_start_hint), SUM(bytes_in), SUM(bytes_out), MAX(last_activity_at)
FROM stats_events WHERE bucket_start >= ?
GROUP BY bucket_start, source_node, client_class ORDER BY bucket_start DESC`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	grouped := map[string]*StatRow{}
	for rows.Next() {
		var bucket, plays, inBytes, outBytes, last int64
		var node, client string
		if err := rows.Scan(&bucket, &node, &client, &plays, &inBytes, &outBytes, &last); err != nil {
			return nil, err
		}
		day := time.Unix(bucket, 0).In(location).Format("2006-01-02")
		key := day + "\x00" + node + "\x00" + client
		row := grouped[key]
		if row == nil {
			row = &StatRow{Day: day, Node: node, Client: client}
			grouped[key] = row
		}
		row.Plays += plays
		row.InboundBytes += inBytes
		row.OutboundBytes += outBytes
		row.Bytes = row.InboundBytes + row.OutboundBytes
		if last > row.LastActivityAt {
			row.LastActivityAt = last
			row.LastActivity = time.Unix(last, 0).In(location).Format("2006-01-02 15:04:05")
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]StatRow, 0, len(grouped))
	for _, row := range grouped {
		out = append(out, *row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastActivityAt > out[j].LastActivityAt })
	return out, nil
}

func (s *Store) Recent(ctx context.Context, limit int) ([]Activity, error) {
	if limit < 1 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT bucket_start, source_node, path_class, status, request_count, bytes_out, playback_start_hint
FROM stats_events ORDER BY last_activity_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	activities := []Activity{}
	for rows.Next() {
		var item Activity
		if err := rows.Scan(&item.At, &item.SourceNode, &item.PathClass, &item.Status, &item.RequestCount, &item.BytesOut, &item.PlaybackHints); err != nil {
			return nil, err
		}
		activities = append(activities, item)
	}
	return activities, rows.Err()
}

func (s *Store) ExportSnapshot(ctx context.Context, source string, since time.Time) ([]SnapshotRow, error) {
	source = strings.ToLower(strings.TrimSpace(source))
	if source != "nosla" && source != "bwg" {
		return nil, errors.New("invalid stats source")
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT bucket_start, source_node, path_class, status, client_class, bytes_in, bytes_out,
       duration_ms, is_range, is_206, request_count, playback_start_hint, last_activity_at
FROM stats_events WHERE source_node = ? AND bucket_start >= ? ORDER BY bucket_start ASC`, source, since.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []SnapshotRow{}
	for rows.Next() {
		var row SnapshotRow
		var isRange, is206 int
		if err := rows.Scan(&row.BucketStart, &row.SourceNode, &row.PathClass, &row.Status, &row.ClientClass, &row.BytesIn, &row.BytesOut, &row.DurationMS, &isRange, &is206, &row.RequestCount, &row.PlaybackStartHint, &row.LastActivityAt); err != nil {
			return nil, err
		}
		row.IsRange = isRange != 0
		row.Is206 = is206 != 0
		result = append(result, row)
	}
	return result, rows.Err()
}

func (s *Store) ImportSnapshot(ctx context.Context, rows []SnapshotRow) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for _, row := range rows {
		source := strings.ToLower(strings.TrimSpace(row.SourceNode))
		if (source != "nosla" && source != "bwg") || row.BucketStart < 0 || row.Status < 100 || row.Status > 599 || row.ClientClass == "" {
			_ = tx.Rollback()
			return errors.New("invalid stats snapshot row")
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO stats_events (bucket_start, source_node, path_class, status, client_class,
 bytes_in, bytes_out, duration_ms, is_range, is_206, request_count,
 playback_start_hint, last_activity_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(bucket_start, source_node, path_class, status, client_class) DO UPDATE SET
 bytes_in=MAX(bytes_in, excluded.bytes_in), bytes_out=MAX(bytes_out, excluded.bytes_out),
 duration_ms=MAX(duration_ms, excluded.duration_ms), is_range=MAX(is_range, excluded.is_range),
 is_206=MAX(is_206, excluded.is_206), request_count=MAX(request_count, excluded.request_count),
 playback_start_hint=MAX(playback_start_hint, excluded.playback_start_hint),
 last_activity_at=MAX(last_activity_at, excluded.last_activity_at)
`, row.BucketStart, source, row.PathClass, row.Status, row.ClientClass, row.BytesIn, row.BytesOut, row.DurationMS, boolInt(row.IsRange), boolInt(row.Is206), row.RequestCount, row.PlaybackStartHint, row.LastActivityAt); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
