package statslog

import (
	"strings"
	"testing"
	"time"
)

func TestParseSanitizedMediaLine(t *testing.T) {
	line := "2026-08-15T00:00:00+08:00 host=stream.149077530.xyz remote_addr=192.0.2.10 status=206 method=GET uri=/https/v1-vod1.uhdnow.com/443/video.mp4 request_length=1024 bytes_sent=4096 upstream_response_time=0.250 request_time=1.234"
	event, err := Parse(line)
	if err != nil {
		t.Fatal(err)
	}
	if event.PathClass != VideoStream || !event.Partial || event.ResponseBytes != 4096 || event.DurationMS != 1234 {
		t.Fatalf("event = %+v", event)
	}
	if event.Host != "stream.149077530.xyz" || !event.OccurredAt.Equal(time.Date(2026, 8, 15, 0, 0, 0, 0, time.FixedZone("+08", 8*60*60))) {
		t.Fatalf("metadata = %+v", event)
	}
}

func TestParseClassifiesSafePathTypes(t *testing.T) {
	tests := map[string]PathClass{
		"/Items/1/PlaybackInfo":      PlaybackInfo,
		"/Sessions/Playing/Progress": SessionsPlaying,
		"/video/master.m3u8":         HLSManifest,
		"/video/segment.m4s":         HLSSegment,
		"/Items/1/Images/Primary":    Image,
		"/Videos/1/Subtitles/0.vtt":  Subtitle,
		"/health":                    Health,
	}
	for uri, want := range tests {
		line := "2026-08-15T00:00:00Z host=stream-b.149077530.xyz status=200 method=GET uri=" + uri + " request_length=0 bytes_sent=1 request_time=0.001"
		event, err := Parse(line)
		if err != nil || event.PathClass != want {
			t.Fatalf("uri=%s event=%+v err=%v want=%s", uri, event, err, want)
		}
	}
}

func TestParseRejectsQueryAndMalformedNumbers(t *testing.T) {
	base := "2026-08-15T00:00:00Z host=stream.149077530.xyz status=200 method=GET uri=/Videos/1 request_length=0 bytes_sent=1 request_time=0.001"
	for _, line := range []string{base + "?token=redacted", strings.Replace(base, "bytes_sent=1", "bytes_sent=-1", 1)} {
		if _, err := Parse(line); err == nil {
			t.Fatalf("accepted unsafe line: %s", line)
		}
	}
}
