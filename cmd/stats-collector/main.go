package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"embyproxy/internal/statslog"
)

type cursor struct {
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
	Offset int64  `json:"offset"`
}

func main() {
	source := flag.String("source-node", "", "sanitized source node: nosla or bwg")
	logPath := flag.String("log", "", "query-free Nginx access log")
	dbPath := flag.String("db", "/var/lib/embyproxy-gsy-sidecar/global-stats.db", "central stats database")
	cursorPath := flag.String("cursor", "", "cursor JSON path")
	snapshotPath := flag.String("snapshot-file", "", "safe aggregate snapshot JSON path")
	dryRun := flag.Bool("dry-run", false, "parse without writing DB or cursor")
	flag.Parse()
	if *source != "nosla" && *source != "bwg" {
		fatal("invalid source node")
	}
	if *logPath == "" {
		if *source == "nosla" {
			*logPath = "/var/log/nginx/stream-proxy-access.log"
		} else {
			*logPath = "/var/log/nginx/stream-b-proxy-access.log"
		}
	}
	if *cursorPath == "" {
		*cursorPath = "/var/lib/embyproxy-gsy-sidecar/stats-cursors/" + *source + ".json"
	}
	file, err := os.Open(*logPath)
	if err != nil {
		fatal("log open failed")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		fatal("log stat failed")
	}
	device, inode := fileIdentity(info)
	state := readCursor(*cursorPath)
	if state.Device != device || state.Inode != inode || state.Offset > info.Size() {
		state = cursor{Device: device, Inode: inode}
	}
	if _, err := file.Seek(state.Offset, io.SeekStart); err != nil {
		fatal("log seek failed")
	}
	events, _, dropped, err := parseLines(file)
	if err != nil {
		fatal("log scan failed")
	}
	result := statslog.Summarize(events)
	result.Dropped = dropped
	if !*dryRun {
		store, err := statslog.Open(*dbPath)
		if err != nil {
			fatal("stats store open failed")
		}
		result, err = store.Ingest(context.Background(), *source, events)
		_ = store.Close()
		if err != nil {
			fatal("stats ingest failed")
		}
		state.Offset = info.Size()
		writeCursor(*cursorPath, state)
		if *snapshotPath != "" {
			if err := writeSnapshot(*snapshotPath, *source, *dbPath); err != nil {
				fatal("snapshot write failed")
			}
		}
	}
	fmt.Printf("source_node=%s parsed=%d dropped=%d playback_info=%d video_stream=%d hls=%d playback_hints=%d partial=%d bytes_out=%d dry_run=%t\n", *source, result.Parsed, result.Dropped, result.PlaybackInfo, result.VideoStream, result.HLSRequests, result.PlaybackHints, result.Partial, result.ResponseBytes, *dryRun)
}

type snapshot struct {
	SourceNode string                 `json:"source_node"`
	Generated  time.Time              `json:"generated_at"`
	Rows       []statslog.SnapshotRow `json:"rows"`
}

func writeSnapshot(path, source, dbPath string) error {
	store, err := statslog.Open(dbPath)
	if err != nil {
		return err
	}
	defer store.Close()
	rows, err := store.ExportSnapshot(context.Background(), source, time.Now().Add(-48*time.Hour))
	if err != nil {
		return err
	}
	raw, err := json.Marshal(snapshot{SourceNode: source, Generated: time.Now().UTC(), Rows: rows})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return err
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, raw, 0640); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

func parseLines(reader io.Reader) ([]statslog.Event, int64, int64, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	events := make([]statslog.Event, 0, 1024)
	var parsed, dropped int64
	for scanner.Scan() {
		event, parseErr := statslog.Parse(scanner.Text())
		if parseErr != nil {
			dropped++
			continue
		}
		parsed++
		events = append(events, event)
	}
	return events, parsed, dropped, scanner.Err()
}

func fileIdentity(info os.FileInfo) (uint64, uint64) {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		return uint64(stat.Dev), uint64(stat.Ino)
	}
	return 0, 0
}

func readCursor(path string) cursor {
	raw, err := os.ReadFile(path)
	if err != nil {
		return cursor{}
	}
	var state cursor
	if json.Unmarshal(raw, &state) != nil || state.Offset < 0 {
		return cursor{}
	}
	return state
}

func writeCursor(path string, state cursor) {
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		fatal("cursor directory failed")
	}
	raw, err := json.Marshal(state)
	if err != nil {
		fatal("cursor encode failed")
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, raw, 0600); err != nil {
		fatal("cursor write failed")
	}
	if err := os.Rename(temporary, path); err != nil {
		fatal("cursor commit failed")
	}
}

func fatal(message string) {
	fmt.Fprintln(os.Stderr, message)
	os.Exit(1)
}
