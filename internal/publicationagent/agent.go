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
	"log"
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
	log.Printf("publication operation action=%s slug=%s operation_id=%s ok=%t nosla_status=%s nosla_error=%s nosla_step=%s bwg_status=%s bwg_error=%s bwg_step=%s",
		request.Action, request.RouteSlug, request.OperationID, response.OK,
		response.NOSLA.Status, response.NOSLA.ErrorCode, response.NOSLA.FailedStep,
		response.BWG.Status, response.BWG.ErrorCode, response.BWG.FailedStep)
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
	if len(targets) == 0 || len(targets) > 16 {
		return publicationprotocol.EdgeManifest{}, errors.New("upstream_invalid")
	}
	routes := make([]publicationprotocol.EdgeRoute, 0, len(targets)+len(a.config.RedirectHosts[request.RouteSlug])+len(a.config.RedirectEndpoints[request.RouteSlug])+len(a.config.RedirectPatterns[request.RouteSlug]))
	seenHosts := map[string]bool{}
	for index, target := range targets {
		parsed, parseErr := url.Parse(target)
		if parseErr != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
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
		routes = append(routes, publicationprotocol.EdgeRoute{
			LineID: publicationLineID(index), Scheme: "https", Host: host, Port: port, BasePath: basePath,
			Kind: "upstream", Position: index + 1,
		})
		seenHosts["https://"+host+":"+strconv.Itoa(port)+"/"+basePath] = true
	}
	redirectIndex := 0
	appendRedirect := func(endpoint RedirectEndpoint) {
		scheme := strings.ToLower(strings.TrimSpace(endpoint.Scheme))
		host := strings.ToLower(strings.TrimSpace(endpoint.Host))
		key := scheme + "://" + host + ":" + strconv.Itoa(endpoint.Port) + "/"
		if seenHosts[key] {
			return
		}
		seenHosts[key] = true
		redirectIndex++
		routes = append(routes, publicationprotocol.EdgeRoute{
			LineID: fmt.Sprintf("redirect-%d", redirectIndex), Scheme: scheme, Host: host, Port: endpoint.Port,
			Kind: "redirect", Position: len(targets) + redirectIndex,
		})
	}
	for _, rawHost := range a.config.RedirectHosts[request.RouteSlug] {
		host := strings.ToLower(strings.TrimSpace(rawHost))
		appendRedirect(RedirectEndpoint{Scheme: "https", Host: host, Port: 443})
	}
	for _, endpoint := range a.config.RedirectEndpoints[request.RouteSlug] {
		appendRedirect(endpoint)
	}
	patternIndex := 0
	for _, pattern := range a.config.RedirectPatterns[request.RouteSlug] {
		patternIndex++
		routes = append(routes, publicationprotocol.EdgeRoute{
			LineID: fmt.Sprintf("redirect-pattern-%d", patternIndex), Scheme: strings.ToLower(strings.TrimSpace(pattern.Scheme)),
			HostSuffix: strings.ToLower(strings.TrimSpace(pattern.Suffix)), LabelLength: pattern.LabelLength, Port: pattern.Port,
			Kind: "redirect_pattern", Position: len(targets) + redirectIndex + patternIndex,
		})
	}
	primary := routes[0]
	manifest := publicationprotocol.EdgeManifest{
		Version: publicationprotocol.Version, Action: request.Action, OperationID: request.OperationID,
		Slug: request.RouteSlug, UpstreamHost: primary.Host, UpstreamPort: primary.Port, BasePath: primary.BasePath,
		Routes: routes,
	}
	if request.Action == publicationprotocol.ActionPublish {
		if err := validateStagedRoute(ctx, database, request.RouteSlug, targets); err != nil {
			return publicationprotocol.EdgeManifest{}, err
		}
	}
	return manifest, nil
}

func validateStagedRoute(ctx context.Context, database *sql.DB, slug string, targets []string) error {
	var nodeName, defaultLine string
	var enabled, public int
	err := database.QueryRowContext(ctx, `
SELECT node_name, enabled, public, default_line FROM managed_routes WHERE slug = ?`, slug).
		Scan(&nodeName, &enabled, &public, &defaultLine)
	if err != nil || nodeName != slug || defaultLine != "main" || !((enabled == 0 && public == 0) || (enabled == 1 && public == 1)) {
		return errors.New("managed_route_stage_invalid")
	}
	rows, err := database.QueryContext(ctx, `
SELECT line_slug, target, enabled, position FROM managed_route_lines
WHERE route_slug = ? ORDER BY position, line_slug`, slug)
	if err != nil {
		return errors.New("managed_route_stage_invalid")
	}
	defer rows.Close()
	index := 0
	for rows.Next() {
		var lineID, target string
		var lineEnabled, position int
		if rows.Scan(&lineID, &target, &lineEnabled, &position) != nil || index >= len(targets) ||
			lineID != publicationLineID(index) || target != targets[index] || lineEnabled != 1 || position != index+1 {
			return errors.New("managed_route_stage_invalid")
		}
		index++
	}
	if rows.Err() != nil || index != len(targets) {
		return errors.New("managed_route_stage_invalid")
	}
	var publicationStatus string
	if err := database.QueryRowContext(ctx, `SELECT status FROM emby_publications WHERE uid='admin' AND node_name=?`, slug).Scan(&publicationStatus); err != nil || publicationStatus != storage.PublicationPublishing {
		return errors.New("publication_state_invalid")
	}
	return nil
}

func publicationLineID(index int) string {
	if index == 0 {
		return "main"
	}
	return fmt.Sprintf("backup-%d", index+1)
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
	if nosla.Status != expected && nosla.Status != "not_attempted" {
		failed = nosla
		node = "nosla"
	} else if bwg.Status == expected && nosla.Status != expected {
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
