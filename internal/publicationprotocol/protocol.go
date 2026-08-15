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
	Version      int    `json:"version"`
	Action       string `json:"action"`
	OperationID  string `json:"operation_id"`
	Slug         string `json:"slug"`
	UpstreamHost string `json:"upstream_host"`
	UpstreamPort int    `json:"upstream_port"`
	BasePath     string `json:"base_path,omitempty"`
}
