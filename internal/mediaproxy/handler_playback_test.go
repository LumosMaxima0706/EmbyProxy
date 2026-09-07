package mediaproxy

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestRewritePlaybackInfoBodyReplacesPrivateMediaPath(t *testing.T) {
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"PlaySessionId":"session-1","MediaSources":[{"Id":"source-1","Path":"http://127.0.0.1:5244/private/file.mkv"}]}`)),
	}
	request, err := http.NewRequest(http.MethodPost, "https://stream.example/s/demo/emby/Items/item-1/PlaybackInfo?UserId=user-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	rewritten, ok := rewritePlaybackInfoBody(response, request, "/s/demo/")
	if !ok {
		t.Fatal("private media path was not rewritten")
	}
	body, err := io.ReadAll(rewritten.Body)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.Contains(text, "/s/demo/emby/videos/item-1/original.mkv?Static=true\\u0026MediaSourceId=source-1\\u0026PlaySessionId=session-1\\u0026UserId=user-1\\u0026DeviceId=embyproxy-canary") || strings.Contains(text, "127.0.0.1") {
		t.Fatalf("rewritten body=%s", text)
	}
}

func TestRewritePlaybackInfoBodyPreservesUnchangedBody(t *testing.T) {
	const original = `{"MediaSources":[{"Id":"source-1","Path":"https://media.example/file.mkv"}]}`
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(original)),
	}
	request, err := http.NewRequest(http.MethodPost, "https://stream.example/s/demo/emby/Items/item-1/PlaybackInfo", nil)
	if err != nil {
		t.Fatal(err)
	}
	unchanged, ok := rewritePlaybackInfoBody(response, request, "/s/demo/")
	if ok {
		t.Fatal("public media path was unexpectedly rewritten")
	}
	body, err := io.ReadAll(unchanged.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != original {
		t.Fatalf("body=%q want=%q", body, original)
	}
}
