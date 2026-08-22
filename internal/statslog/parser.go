// Package statslog parses the query-free Nginx access format used by the
// stream proxy nodes. It intentionally returns classifications, not raw URIs.
package statslog

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

type PathClass string

const (
	PlaybackInfo    PathClass = "PlaybackInfo"
	SessionsPlaying PathClass = "SessionsPlaying"
	VideoStream     PathClass = "VideoStream"
	HLSManifest     PathClass = "HLSManifest"
	HLSSegment      PathClass = "HLSSegment"
	Image           PathClass = "Image"
	Subtitle        PathClass = "Subtitle"
	Health          PathClass = "Health"
	Other           PathClass = "Other"
)

type Event struct {
	OccurredAt    time.Time
	Host          string
	Method        string
	Status        int
	PathClass     PathClass
	RequestBytes  int64
	ResponseBytes int64
	DurationMS    int64
	Partial       bool
}

var errInvalidLine = errors.New("invalid sanitized access line")

func Parse(line string) (Event, error) {
	fields := make(map[string]string)
	parts := strings.Fields(strings.TrimSpace(line))
	if len(parts) == 0 {
		return Event{}, errInvalidLine
	}
	if !strings.Contains(parts[0], "=") {
		fields["time_iso8601"] = parts[0]
		parts = parts[1:]
	}
	for _, field := range parts {
		key, value, ok := strings.Cut(field, "=")
		if !ok || key == "" {
			continue
		}
		fields[key] = value
	}
	if strings.ContainsAny(fields["uri"], "?#\r\n") || fields["uri"] == "" {
		return Event{}, errInvalidLine
	}
	when, err := time.Parse(time.RFC3339, fields["time_iso8601"])
	if err != nil {
		return Event{}, errInvalidLine
	}
	status, err := strconv.Atoi(fields["status"])
	if err != nil || status < 100 || status > 599 {
		return Event{}, errInvalidLine
	}
	requestBytes, err := parseNonNegative(fields["request_length"])
	if err != nil {
		return Event{}, errInvalidLine
	}
	responseBytes, err := parseNonNegative(fields["bytes_sent"])
	if err != nil {
		return Event{}, errInvalidLine
	}
	duration, err := strconv.ParseFloat(fields["request_time"], 64)
	if err != nil || duration < 0 {
		return Event{}, errInvalidLine
	}
	return Event{
		OccurredAt:    when,
		Host:          safeHost(fields["host"]),
		Method:        fields["method"],
		Status:        status,
		PathClass:     classifyPath(fields["uri"]),
		RequestBytes:  requestBytes,
		ResponseBytes: responseBytes,
		DurationMS:    int64(duration*1000 + 0.5),
		Partial:       status == 206,
	}, nil
}

func parseNonNegative(value string) (int64, error) {
	if value == "" {
		return 0, errInvalidLine
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n < 0 {
		return 0, errInvalidLine
	}
	return n, nil
}

func safeHost(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "stream.149077530.xyz" || value == "stream-b.149077530.xyz" {
		return value
	}
	return "unknown"
}

func classifyPath(uri string) PathClass {
	path := strings.ToLower(uri)
	switch {
	case strings.Contains(path, "/playbackinfo"):
		return PlaybackInfo
	case strings.Contains(path, "/sessions/playing"):
		return SessionsPlaying
	case strings.HasSuffix(path, ".m3u8") || strings.Contains(path, "master.m3u8"):
		return HLSManifest
	case strings.HasSuffix(path, ".ts") || strings.HasSuffix(path, ".m4s"):
		return HLSSegment
	case strings.Contains(path, "subtitle") || strings.HasSuffix(path, ".vtt") || strings.HasSuffix(path, ".srt"):
		return Subtitle
	case strings.Contains(path, "/videos/") || strings.Contains(path, "/audio/") ||
		strings.HasSuffix(path, ".mp4") || strings.Contains(path, "/stream") || strings.Contains(path, "/https/"):
		return VideoStream
	case strings.Contains(path, "/images/") || strings.Contains(path, "/image"):
		return Image
	case path == "/health":
		return Health
	default:
		return Other
	}
}
