package publicationprotocol

const Version = 1

const (
	ActionCheck     = "check"
	ActionPublish   = "publish"
	ActionUnpublish = "unpublish"
)

type Request struct {
	Version     int    `json:"version"`
	Action      string `json:"action"`
	OperationID string `json:"operation_id"`
	NodeName    string `json:"node_name"`
	RouteSlug   string `json:"route_slug"`
}

type EdgeResult struct {
	Status     string `json:"status"`
	ErrorCode  string `json:"error_code,omitempty"`
	FailedStep string `json:"failed_step,omitempty"`
	BackupPath string `json:"backup_path,omitempty"`
}

type Response struct {
	OK         bool       `json:"ok"`
	ErrorCode  string     `json:"error_code,omitempty"`
	FailedStep string     `json:"failed_step,omitempty"`
	NOSLA      EdgeResult `json:"nosla"`
	BWG        EdgeResult `json:"bwg"`
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
