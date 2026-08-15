package publicationagent

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"embyproxy/internal/proxyadapter"
	"embyproxy/internal/publicationprotocol"
	"embyproxy/internal/storage"

	_ "modernc.org/sqlite"
)

type Agent struct {
	config AgentConfig
	mu     sync.Mutex
}

func NewAgent(config AgentConfig) *Agent {
	return &Agent{config: config}
}

func (a *Agent) Run(ctx context.Context) error {
	if err := os.MkdirAll(path.Dir(a.config.SocketPath), 0750); err != nil {
		return errors.New("edge_adapter_socket_directory_failed")
	}
	_ = os.Remove(a.config.SocketPath)
	address := &net.UnixAddr{Name: a.config.SocketPath, Net: "unix"}
	listener, err := net.ListenUnix("unix", address)
	if err != nil {
		return errors.New("edge_adapter_listen_failed")
	}
	defer listener.Close()
	defer os.Remove(a.config.SocketPath)
	if err := os.Chmod(a.config.SocketPath, 0660); err != nil {
		return errors.New("edge_adapter_socket_permissions_failed")
	}
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()
	for {
		connection, err := listener.AcceptUnix()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			continue
		}
		go a.serveConnection(ctx, connection)
	}
}

func (a *Agent) serveConnection(ctx context.Context, connection *net.UnixConn) {
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(90 * time.Second))
	uid, err := peerUID(connection)
	if err != nil || uid != a.config.AllowedPeerUID {
		_ = json.NewEncoder(connection).Encode(protocolFailure("edge_sync_denied", "peer_credentials"))
		return
	}
	decoder := json.NewDecoder(bufio.NewReaderSize(io.LimitReader(connection, 64<<10), 16<<10))
	decoder.DisallowUnknownFields()
	var request publicationprotocol.Request
	if err := decoder.Decode(&request); err != nil {
		_ = json.NewEncoder(connection).Encode(protocolFailure("edge_adapter_request_invalid", "request_decode"))
		return
	}
	a.mu.Lock()
	response := a.Handle(ctx, request)
	a.mu.Unlock()
	_ = json.NewEncoder(connection).Encode(response)
}

func (a *Agent) Handle(ctx context.Context, request publicationprotocol.Request) publicationprotocol.Response {
	if err := validateRequest(request); err != nil {
		return protocolFailure(err.Error(), "request_validation")
	}
	manifest, err := a.manifestFromDatabase(ctx, request)
	if err != nil {
		return protocolFailure(err.Error(), "database_validation")
	}
	switch request.Action {
	case publicationprotocol.ActionCheck:
		return a.check(ctx, manifest)
	case publicationprotocol.ActionPublish:
		return a.publish(ctx, manifest)
	case publicationprotocol.ActionUnpublish:
		return a.unpublish(ctx, manifest)
	default:
		return protocolFailure("edge_adapter_request_invalid", "request_validation")
	}
}

func validateRequest(request publicationprotocol.Request) error {
	if request.Version != publicationprotocol.Version || request.NodeName != request.RouteSlug ||
		proxyadapter.ValidateManagedRouteSlug(request.NodeName) != nil {
		return errors.New("edge_adapter_request_invalid")
	}
	if request.Action != publicationprotocol.ActionCheck && request.Action != publicationprotocol.ActionPublish && request.Action != publicationprotocol.ActionUnpublish {
		return errors.New("edge_adapter_request_invalid")
	}
	if request.OperationID == "" || len(request.OperationID) > 64 {
		return errors.New("edge_adapter_request_invalid")
	}
	for _, r := range request.OperationID {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' && r != '_' {
			return errors.New("edge_adapter_request_invalid")
		}
	}
	return nil
}

func (a *Agent) manifestFromDatabase(ctx context.Context, request publicationprotocol.Request) (publicationprotocol.EdgeManifest, error) {
	database, err := sql.Open("sqlite", "file:"+a.config.DatabasePath+"?mode=ro")
	if err != nil {
		return publicationprotocol.EdgeManifest{}, errors.New("central_db_unavailable")
	}
	defer database.Close()
	var packed string
	key := "u:admin:node:" + request.NodeName
	if err := database.QueryRowContext(ctx, `SELECT v FROM proxy_kv WHERE k = ?`, key).Scan(&packed); err != nil {
		return publicationprotocol.EdgeManifest{}, errors.New("upstream_not_saved")
	}
	node, ok := storage.UnpackNode(request.NodeName, packed)
	if !ok {
		return publicationprotocol.EdgeManifest{}, errors.New("upstream_invalid")
	}
	targets := storage.SplitTargets(node.Target)
	if len(targets) != 1 {
		return publicationprotocol.EdgeManifest{}, errors.New("upstream_invalid")
	}
	parsed, err := url.Parse(targets[0])
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return publicationprotocol.EdgeManifest{}, errors.New("upstream_invalid")
	}
	host := strings.ToLower(parsed.Hostname())
	if !safeHostname(host) || host == a.config.PublicMediaHost || host == a.config.OwnerAdminHost {
		return publicationprotocol.EdgeManifest{}, errors.New("upstream_invalid")
	}
	port := 443
	if parsed.Port() != "" {
		port, err = strconv.Atoi(parsed.Port())
		if err != nil || port < 1 || port > 65535 {
			return publicationprotocol.EdgeManifest{}, errors.New("upstream_invalid")
		}
	}
	basePath := strings.Trim(path.Clean(parsed.EscapedPath()), "/.")
	manifest := publicationprotocol.EdgeManifest{
		Version: publicationprotocol.Version, Action: request.Action, OperationID: request.OperationID,
		Slug: request.RouteSlug, UpstreamHost: host, UpstreamPort: port, BasePath: basePath,
	}
	if request.Action == publicationprotocol.ActionPublish {
		if err := validateStagedRoute(ctx, database, request.RouteSlug, targets[0]); err != nil {
			return publicationprotocol.EdgeManifest{}, err
		}
	}
	return manifest, nil
}

func validateStagedRoute(ctx context.Context, database *sql.DB, slug, target string) error {
	var nodeName, lineTarget string
	var enabled, public, lineEnabled, position int
	err := database.QueryRowContext(ctx, `
SELECT r.node_name, r.enabled, r.public, l.target, l.enabled, l.position
FROM managed_routes r JOIN managed_route_lines l ON l.route_slug = r.slug
WHERE r.slug = ? AND l.line_slug = 'main'`, slug).Scan(&nodeName, &enabled, &public, &lineTarget, &lineEnabled, &position)
	if err != nil || nodeName != slug || enabled != 0 || public != 0 || lineEnabled != 1 || position != 1 || lineTarget != target {
		return errors.New("managed_route_stage_invalid")
	}
	var publicationStatus string
	if err := database.QueryRowContext(ctx, `SELECT status FROM emby_publications WHERE uid='admin' AND node_name=?`, slug).Scan(&publicationStatus); err != nil || publicationStatus != storage.PublicationPublishing {
		return errors.New("publication_state_invalid")
	}
	return nil
}

func (a *Agent) check(ctx context.Context, manifest publicationprotocol.EdgeManifest) publicationprotocol.Response {
	bwg := a.invokeLocal(ctx, manifest)
	nosla := a.invokeRemote(ctx, manifest)
	return combineEdges(publicationprotocol.ActionCheck, nosla, bwg)
}

func (a *Agent) publish(ctx context.Context, manifest publicationprotocol.EdgeManifest) publicationprotocol.Response {
	bwg := a.invokeLocal(ctx, manifest)
	if bwg.Status != "synced" {
		return combineEdges(publicationprotocol.ActionPublish, publicationprotocol.EdgeResult{Status: "not_attempted"}, bwg)
	}
	nosla := a.invokeRemote(ctx, manifest)
	if nosla.Status == "synced" {
		return combineEdges(publicationprotocol.ActionPublish, nosla, bwg)
	}
	rollbackManifest := manifest
	rollbackManifest.Action = publicationprotocol.ActionUnpublish
	rollback := a.invokeLocal(ctx, rollbackManifest)
	if rollback.Status == "removed" || rollback.Status == "not_configured" {
		bwg = rollback
	} else {
		bwg = publicationprotocol.EdgeResult{Status: "failed", ErrorCode: "rollback_failed", FailedStep: "bwg_rollback", BackupPath: rollback.BackupPath}
	}
	response := combineEdges(publicationprotocol.ActionPublish, nosla, bwg)
	response.ErrorCode = "edge_sync_partial"
	response.FailedStep = "nosla_edge_sync"
	return response
}

func (a *Agent) unpublish(ctx context.Context, manifest publicationprotocol.EdgeManifest) publicationprotocol.Response {
	bwg := a.invokeLocal(ctx, manifest)
	nosla := a.invokeRemote(ctx, manifest)
	return combineEdges(publicationprotocol.ActionUnpublish, nosla, bwg)
}

func (a *Agent) invokeLocal(ctx context.Context, manifest publicationprotocol.EdgeManifest) publicationprotocol.EdgeResult {
	return invokeJSON(ctx, a.config.LocalHelperPath, []string{"--mode=edge", "--config", a.config.BWGConfigPath}, manifest)
}

func (a *Agent) invokeRemote(ctx context.Context, manifest publicationprotocol.EdgeManifest) publicationprotocol.EdgeResult {
	timeout := a.config.NOSLA.TimeoutSeconds
	args := []string{
		"-F", "/dev/null", "-i", a.config.NOSLA.IdentityFile,
		"-o", "UserKnownHostsFile=" + a.config.NOSLA.KnownHostsFile,
		"-o", "StrictHostKeyChecking=yes", "-o", "BatchMode=yes", "-o", "IdentitiesOnly=yes",
		"-o", fmt.Sprintf("ConnectTimeout=%d", timeout), "-T",
		a.config.NOSLA.User + "@" + a.config.NOSLA.Host,
	}
	return invokeJSON(ctx, "/usr/bin/ssh", args, manifest)
}

func invokeJSON(ctx context.Context, executable string, args []string, manifest publicationprotocol.EdgeManifest) publicationprotocol.EdgeResult {
	raw, err := json.Marshal(manifest)
	if err != nil {
		return publicationprotocol.EdgeResult{Status: "failed", ErrorCode: "edge_manifest_invalid", FailedStep: "manifest_encode"}
	}
	command := exec.CommandContext(ctx, executable, args...)
	command.Stdin = bytes.NewReader(append(raw, '\n'))
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		return publicationprotocol.EdgeResult{Status: "failed", ErrorCode: "edge_command_failed", FailedStep: "edge_command"}
	}
	decoder := json.NewDecoder(io.LimitReader(&output, 64<<10))
	decoder.DisallowUnknownFields()
	var result publicationprotocol.EdgeResult
	if err := decoder.Decode(&result); err != nil || result.Status == "" {
		return publicationprotocol.EdgeResult{Status: "failed", ErrorCode: "edge_response_invalid", FailedStep: "edge_response"}
	}
	return result
}

func combineEdges(action string, nosla, bwg publicationprotocol.EdgeResult) publicationprotocol.Response {
	response := publicationprotocol.Response{NOSLA: nosla, BWG: bwg}
	expected := "ready"
	if action == publicationprotocol.ActionPublish {
		expected = "synced"
	} else if action == publicationprotocol.ActionUnpublish {
		if edgeRemoved(nosla.Status) && edgeRemoved(bwg.Status) {
			response.OK = true
			return response
		}
		expected = "removed"
	}
	if nosla.Status == expected && bwg.Status == expected {
		response.OK = true
		return response
	}
	failed := bwg
	node := "bwg"
	if nosla.Status != expected {
		failed = nosla
		node = "nosla"
	}
	response.ErrorCode = failed.ErrorCode
	if response.ErrorCode == "" {
		response.ErrorCode = node + "_edge_sync_failed"
	}
	response.FailedStep = failed.FailedStep
	if response.FailedStep == "" {
		response.FailedStep = node + "_edge_sync"
	}
	return response
}

func edgeRemoved(status string) bool {
	return status == "removed" || status == "not_configured"
}

func protocolFailure(code, step string) publicationprotocol.Response {
	edge := publicationprotocol.EdgeResult{Status: "unavailable", ErrorCode: code, FailedStep: step}
	return publicationprotocol.Response{OK: false, ErrorCode: code, FailedStep: step, NOSLA: edge, BWG: edge}
}
