package publicationprotocol

const Version = 1

const (
	ActionCheck          = "check"
	ActionPublish        = "publish"
	ActionUnpublish      = "unpublish"
	ActionPlaybackCanary = "playback_canary"
)

type Request struct {
	Version     int    `json:"version"`
	Action      string `json:"action"`
	OperationID string `json:"operation_id"`
	NodeName    string `json:"node_name"`
	RouteSlug   string `json:"route_slug"`
	// PlaybackCanary is runtime-only and is sent over the peer-credential
	// protected Unix socket. The agent never logs or persists AccessToken.
	PlaybackCanary *PlaybackCanaryRequest `json:"playback_canary,omitempty"`
}

type PlaybackCanaryRequest struct {
	ItemID string `json:"item_id"`
	// ItemIDs is a bounded runtime-only sample set. ItemID remains supported
	// for older admin clients and is treated as a one-item sample.
	ItemIDs     []string `json:"item_ids,omitempty"`
	AccessToken string   `json:"access_token"`
}

type EdgeResult struct {
	Status     string `json:"status"`
	ErrorCode  string `json:"error_code,omitempty"`
	FailedStep string `json:"failed_step,omitempty"`
	BackupPath string `json:"backup_path,omitempty"`
}

type Response struct {
	OK         bool                    `json:"ok"`
	ErrorCode  string                  `json:"error_code,omitempty"`
	FailedStep string                  `json:"failed_step,omitempty"`
	NOSLA      EdgeResult              `json:"nosla"`
	BWG        EdgeResult              `json:"bwg"`
	Playback   *PlaybackCanaryResponse `json:"playback,omitempty"`
}

// PlaybackCanaryResponse deliberately exposes only classified evidence. It
// never includes a URL, host, item id, token, cookie or response body.
type PlaybackCanaryResponse struct {
	Status              string `json:"status"`
	FailureClass        string `json:"failure_class,omitempty"`
	ConnectivityStatus  int    `json:"connectivity_status"`
	PlaybackInfoStatus  int    `json:"playbackinfo_status"`
	VideoStreamStatus   int    `json:"videostream_status"`
	MediaStatus         int    `json:"media_status"`
	RedirectsFollowed   int    `json:"redirects_followed"`
	EndpointsDiscovered int    `json:"endpoints_discovered"`
	Samples             int    `json:"samples"`
	SamplesPassed       int    `json:"samples_passed"`
	BytesRead           int64  `json:"bytes_read"`
	ByteGrowth          bool   `json:"byte_growth"`
	ContentRange        bool   `json:"content_range"`
	AcceptRanges        bool   `json:"accept_ranges"`
}

type EdgeManifest struct {
	Version      int         `json:"version"`
	Action       string      `json:"action"`
	OperationID  string      `json:"operation_id"`
	Slug         string      `json:"slug"`
	UpstreamHost string      `json:"upstream_host"`
	UpstreamPort int         `json:"upstream_port"`
	BasePath     string      `json:"base_path,omitempty"`
	Routes       []EdgeRoute `json:"routes,omitempty"`
}

// EdgeRoute is derived only from saved managed-route lines or root-owned
// redirect endpoint/pattern allowlists. The public API cannot inject routes.
type EdgeRoute struct {
	LineID      string `json:"line_id"`
	Scheme      string `json:"scheme"`
	Host        string `json:"host,omitempty"`
	HostSuffix  string `json:"host_suffix,omitempty"`
	LabelLength int    `json:"label_length,omitempty"`
	Port        int    `json:"port"`
	BasePath    string `json:"base_path,omitempty"`
	Kind        string `json:"kind"`
	Position    int    `json:"position"`
}
