package main

import (
	"strings"
	"testing"
)

func TestParseLinesCountsAcceptedAndDroppedWithoutRawOutput(t *testing.T) {
	input := strings.NewReader("2026-08-15T00:00:00Z host=stream.example.invalid status=206 method=GET uri=/video.mp4 request_length=1 bytes_sent=2 request_time=0.010\n2026-08-15T00:00:00Z host=stream.example.invalid status=200 method=GET uri=/video.mp4?redacted request_length=1 bytes_sent=2 request_time=0.010\n")
	events, parsed, dropped, err := parseLines(input)
	if err != nil || parsed != 1 || dropped != 1 || len(events) != 1 || !events[0].Partial {
		t.Fatalf("events=%+v parsed=%d dropped=%d err=%v", events, parsed, dropped, err)
	}
}
