package publicationagent

import (
	"testing"

	"embyproxy/internal/publicationprotocol"
)

func healthyRedirectEvidence(location string) PlaybackCanaryEvidence {
	return PlaybackCanaryEvidence{
		ConnectivityStatus: 200,
		PlaybackInfoStatus: 200,
		VideoStreamStatus:  302,
		Redirects:          []PlaybackRedirect{{Status: 302, Location: location}},
		Media: PlaybackMediaResult{StatusCode: 206, BytesRead: 65536, GrowthBytes: 65536,
			ContentRange: true, AcceptRanges: true, RedirectsFollowed: 1},
	}
}

func TestPlaybackCanaryAcceptsDifferentMediaHostAndHTTP80(t *testing.T) {
	result, err := ValidatePlaybackCanary(healthyRedirectEvidence("http://media.example.test/stream"))
	if err != nil || result.Status != "healthy" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if len(result.RedirectEndpoints) != 1 {
		t.Fatalf("endpoints=%+v", result.RedirectEndpoints)
	}
	endpoint := result.RedirectEndpoints[0]
	if endpoint.Scheme != "http" || endpoint.Host != "media.example.test" || endpoint.Port != 80 || endpoint.PathPrefix != "stream" {
		t.Fatalf("endpoint=%+v, want exact HTTP/80 endpoint", endpoint)
	}
}

func TestPlaybackCanaryRejectsUnknownOrPrivateRedirectEndpoint(t *testing.T) {
	for _, location := range []string{
		"http://127.0.0.1/private",
		"http://10.0.0.2/private",
		"file:///private",
		"/relative",
	} {
		result, err := ValidatePlaybackCanary(healthyRedirectEvidence(location))
		if err == nil || result.Status != "failed" || result.FailureClass != "unknown_media_host" {
			t.Fatalf("location=%q result=%+v err=%v", location, result, err)
		}
	}
}

func TestPlaybackCanaryDoesNotMarkAPIOnlySuccessHealthy(t *testing.T) {
	evidence := healthyRedirectEvidence("https://media.example.test/stream")
	evidence.Media = PlaybackMediaResult{StatusCode: 403}
	result, err := ValidatePlaybackCanary(evidence)
	if err == nil || result.Status != "failed" || result.FailureClass != "upstream_403" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestPlaybackCanaryRequiresRangeHeadersAndByteGrowth(t *testing.T) {
	evidence := healthyRedirectEvidence("https://media.example.test/stream")
	evidence.Media.GrowthBytes = 0
	result, err := ValidatePlaybackCanary(evidence)
	if err == nil || result.FailureClass != "no_byte_growth" {
		t.Fatalf("no-growth result=%+v err=%v", result, err)
	}

	evidence = healthyRedirectEvidence("https://media.example.test/stream")
	evidence.Media.ContentRange = false
	result, err = ValidatePlaybackCanary(evidence)
	if err == nil || result.FailureClass != "invalid_range" {
		t.Fatalf("range result=%+v err=%v", result, err)
	}
}

func TestPlaybackCanaryAcceptsDirectMedia206(t *testing.T) {
	evidence := PlaybackCanaryEvidence{ConnectivityStatus: 200, PlaybackInfoStatus: 200, VideoStreamStatus: 206,
		Media: PlaybackMediaResult{StatusCode: 206, BytesRead: 65536, GrowthBytes: 65536, ContentRange: true, AcceptRanges: true}}
	result, err := ValidatePlaybackCanary(evidence)
	if err != nil || result.Status != "healthy" || len(result.RedirectEndpoints) != 0 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestRedirectEndpointFromPublicEncodedLocationPreservesHTTP80(t *testing.T) {
	endpoint, err := redirectEndpointFromLocation("https://stream.example/http/media.example.test/80/path", "stream.example")
	if err != nil {
		t.Fatal(err)
	}
	if endpoint.Scheme != "http" || endpoint.Host != "media.example.test" || endpoint.Port != 80 || endpoint.PathPrefix != "path" {
		t.Fatalf("endpoint=%+v", endpoint)
	}
}

func TestCanaryPublicURLDoesNotDoublePrefixEncodedPublicPath(t *testing.T) {
	got, err := canaryPublicURL("https://stream.example/https/upstream.example/443", "stream.example", "/https/upstream.example/443/emby/Videos/item/stream?X-Emby-Token=runtime")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://stream.example/https/upstream.example/443/emby/Videos/item/stream?X-Emby-Token=runtime" {
		t.Fatalf("got %q", got)
	}
}

func TestPlaybackStreamPathAlwaysUsesVideoStreamEndpoint(t *testing.T) {
	source := struct {
		ID                   string `json:"Id"`
		Path                 string `json:"Path"`
		Protocol             string `json:"Protocol"`
		IsRemote             bool   `json:"IsRemote"`
		SupportsDirectStream bool   `json:"SupportsDirectStream"`
		SupportsDirectPlay   bool   `json:"SupportsDirectPlay"`
		DirectStreamURL      string `json:"DirectStreamUrl"`
	}{ID: "media-source", Path: "/upload/series/file.mkv", IsRemote: true, DirectStreamURL: "https://cdn.example/file.mkv"}
	if got := playbackStreamPath("625260", source, "session"); got != "/emby/Videos/625260/stream?Static=true&MediaSourceId=media-source&PlaySessionId=session" {
		t.Fatalf("got %q", got)
	}
}

func TestHeterogeneousMediaSamplesRequireEveryItemToPass(t *testing.T) {
	// Regression: one title used media host X and played, while another title
	// from the same publication used media host Y and returned 403. A lone
	// successful sample must never imply the server is playback healthy.
	first, err := ValidatePlaybackCanary(healthyRedirectEvidence("https://media-x.example.test/stream"))
	if err != nil || first.Status != "healthy" || len(first.RedirectEndpoints) != 1 || first.RedirectEndpoints[0].Host != "media-x.example.test" {
		t.Fatalf("first item result=%+v err=%v", first, err)
	}
	secondEvidence := healthyRedirectEvidence("http://media-y.example.test/segment")
	secondEvidence.Media.StatusCode = 403
	second, err := ValidatePlaybackCanary(secondEvidence)
	if err == nil || second.Status != "failed" || second.FailureClass != "upstream_403" || len(second.RedirectEndpoints) != 1 || second.RedirectEndpoints[0].Host != "media-y.example.test" {
		t.Fatalf("second item result=%+v err=%v", second, err)
	}
}

func TestNormalizedCanaryItemsSupportsBoundedDistinctSampleSet(t *testing.T) {
	items, err := normalizedCanaryItems(publicationprotocol.PlaybackCanaryRequest{ItemIDs: []string{"item-a", "item-b"}, AccessToken: "runtime-token"})
	if err != nil || len(items) != 2 || items[0] != "item-a" || items[1] != "item-b" {
		t.Fatalf("items=%v err=%v", items, err)
	}
	if _, err := normalizedCanaryItems(publicationprotocol.PlaybackCanaryRequest{ItemIDs: []string{"item-a", "item-a"}, AccessToken: "runtime-token"}); err == nil {
		t.Fatal("duplicate sample set was accepted")
	}
}
