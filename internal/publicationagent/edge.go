package publicationagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"embyproxy/internal/proxyadapter"
	"embyproxy/internal/publicationprotocol"
)

func RunEdge(ctx context.Context, cfg EdgeConfig, input io.Reader, output io.Writer) error {
	decoder := json.NewDecoder(io.LimitReader(input, 64<<10))
	decoder.DisallowUnknownFields()
	var manifest publicationprotocol.EdgeManifest
	if err := decoder.Decode(&manifest); err != nil {
		return encodeEdgeResult(output, publicationprotocol.EdgeResult{Status: "failed", ErrorCode: "edge_manifest_invalid", FailedStep: "manifest_decode"})
	}
	result := applyEdge(ctx, cfg, manifest)
	return encodeEdgeResult(output, result)
}

func applyEdge(ctx context.Context, cfg EdgeConfig, manifest publicationprotocol.EdgeManifest) publicationprotocol.EdgeResult {
	if err := validateManifest(manifest); err != nil {
		return edgeFailure(err.Error(), "manifest_validation", "")
	}
	if err := verifyIncludeHook(cfg); err != nil {
		return edgeFailure(err.Error(), "include_hook", "")
	}
	if manifest.Action == publicationprotocol.ActionCheck {
		if err := nginxTest(ctx, "/etc/nginx/nginx.conf"); err != nil {
			return edgeFailure("nginx_test_failed", "nginx_test", "")
		}
		return publicationprotocol.EdgeResult{Status: "ready"}
	}
	if err := os.MkdirAll(cfg.IncludeDir, 0750); err != nil {
		return edgeFailure("edge_config_write_failed", "include_directory", "")
	}
	if err := os.MkdirAll(cfg.BackupRoot, 0700); err != nil {
		return edgeFailure("backup_failed", "backup_directory", "")
	}
	target := filepath.Join(cfg.IncludeDir, manifest.Slug+".conf")
	backupDir, err := createOperationBackupDir(cfg, manifest)
	if err != nil {
		return edgeFailure("backup_failed", "backup_directory", "")
	}
	if manifest.Action == publicationprotocol.ActionPublish {
		return publishEdge(ctx, cfg, manifest, target, backupDir)
	}
	return unpublishEdge(ctx, cfg, manifest, target, backupDir)
}

func createOperationBackupDir(cfg EdgeConfig, manifest publicationprotocol.EdgeManifest) (string, error) {
	prefix := time.Now().UTC().Format("20060102T150405.000000000Z") + "-" +
		manifest.OperationID + "-" + cfg.NodeName + "-" + manifest.Action + "-"
	return os.MkdirTemp(cfg.BackupRoot, prefix)
}

func publishEdge(ctx context.Context, cfg EdgeConfig, manifest publicationprotocol.EdgeManifest, target, backupDir string) publicationprotocol.EdgeResult {
	fragment := renderNginxFragment(cfg, manifest)
	if current, err := os.ReadFile(target); err == nil {
		if bytes.Equal(current, fragment) {
			if err := nginxTest(ctx, "/etc/nginx/nginx.conf"); err != nil {
				return edgeFailure("nginx_test_failed", "nginx_test", backupDir)
			}
			return publicationprotocol.EdgeResult{Status: "synced", BackupPath: backupDir}
		}
		return edgeFailure("route_conflict", "edge_route", backupDir)
	} else if !os.IsNotExist(err) {
		return edgeFailure("edge_config_read_failed", "edge_route", backupDir)
	}
	candidate := filepath.Join(backupDir, "candidate.conf")
	if err := os.WriteFile(candidate, fragment, 0600); err != nil {
		return edgeFailure("edge_config_write_failed", "candidate_write", backupDir)
	}
	if err := testCandidate(ctx, candidate, backupDir); err != nil {
		return edgeFailure("nginx_test_failed", "candidate_nginx_test", backupDir)
	}
	temporary := filepath.Join(cfg.IncludeDir, "."+manifest.Slug+"."+manifest.OperationID+".tmp")
	if err := os.WriteFile(temporary, fragment, 0640); err != nil {
		return edgeFailure("edge_config_write_failed", "atomic_write", backupDir)
	}
	if err := os.Rename(temporary, target); err != nil {
		_ = os.Remove(temporary)
		return edgeFailure("edge_config_write_failed", "atomic_replace", backupDir)
	}
	if err := nginxTest(ctx, "/etc/nginx/nginx.conf"); err != nil {
		_ = os.Remove(target)
		return edgeFailure("nginx_test_failed", "nginx_test", backupDir)
	}
	if err := reloadNginx(ctx); err != nil {
		if rollbackErr := removeAndReload(ctx, target); rollbackErr != nil {
			return edgeFailure("rollback_failed", "reload_rollback", backupDir)
		}
		return edgeFailure("reload_failed", "nginx_reload", backupDir)
	}
	return publicationprotocol.EdgeResult{Status: "synced", BackupPath: backupDir}
}

func unpublishEdge(ctx context.Context, cfg EdgeConfig, manifest publicationprotocol.EdgeManifest, target, backupDir string) publicationprotocol.EdgeResult {
	current, err := os.ReadFile(target)
	if os.IsNotExist(err) {
		return publicationprotocol.EdgeResult{Status: "not_configured", BackupPath: backupDir}
	}
	if err != nil {
		return edgeFailure("edge_config_read_failed", "edge_route", backupDir)
	}
	if !bytes.Equal(current, renderNginxFragment(cfg, manifest)) {
		return edgeFailure("route_conflict", "edge_route", backupDir)
	}
	backup := filepath.Join(backupDir, manifest.Slug+".conf")
	if err := os.WriteFile(backup, current, 0600); err != nil {
		return edgeFailure("backup_failed", "backup_write", backupDir)
	}
	hidden := filepath.Join(cfg.IncludeDir, "."+manifest.Slug+"."+manifest.OperationID+".rollback")
	if err := os.Rename(target, hidden); err != nil {
		return edgeFailure("edge_config_write_failed", "atomic_remove", backupDir)
	}
	if err := nginxTest(ctx, "/etc/nginx/nginx.conf"); err != nil {
		_ = os.Rename(hidden, target)
		return edgeFailure("nginx_test_failed", "nginx_test", backupDir)
	}
	if err := reloadNginx(ctx); err != nil {
		if rollbackErr := os.Rename(hidden, target); rollbackErr != nil || nginxTest(ctx, "/etc/nginx/nginx.conf") != nil || reloadNginx(ctx) != nil {
			return edgeFailure("rollback_failed", "reload_rollback", backupDir)
		}
		return edgeFailure("reload_failed", "nginx_reload", backupDir)
	}
	if err := os.Remove(hidden); err != nil {
		return edgeFailure("edge_config_remove_failed", "post_reload_remove", backupDir)
	}
	return publicationprotocol.EdgeResult{Status: "removed", BackupPath: backupDir}
}

func validateManifest(manifest publicationprotocol.EdgeManifest) error {
	if manifest.Version != publicationprotocol.Version || proxyadapter.ValidateManagedRouteSlug(manifest.Slug) != nil ||
		!safeHostname(manifest.UpstreamHost) || manifest.UpstreamPort < 1 || manifest.UpstreamPort > 65535 {
		return errors.New("edge_manifest_invalid")
	}
	if manifest.Action != publicationprotocol.ActionCheck && manifest.Action != publicationprotocol.ActionPublish && manifest.Action != publicationprotocol.ActionUnpublish {
		return errors.New("edge_manifest_invalid")
	}
	if manifest.OperationID == "" || len(manifest.OperationID) > 64 || strings.ContainsAny(manifest.OperationID, " /\\.\t\r\n") {
		return errors.New("edge_manifest_invalid")
	}
	if manifest.BasePath != "" && (strings.HasPrefix(manifest.BasePath, "/") || strings.ContainsAny(manifest.BasePath, "?#\r\n") || strings.Contains(manifest.BasePath, "..")) {
		return errors.New("edge_manifest_invalid")
	}
	return nil
}

func verifyIncludeHook(cfg EdgeConfig) error {
	raw, err := os.ReadFile(cfg.StreamConfig)
	if err != nil {
		return errors.New("edge_stream_config_unavailable")
	}
	expected := "include " + cfg.IncludeDirective + ";"
	if !strings.Contains(string(raw), expected) {
		return errors.New("edge_include_hook_missing")
	}
	return nil
}

func renderNginxFragment(cfg EdgeConfig, manifest publicationprotocol.EdgeManifest) []byte {
	publicPath := "/https/" + manifest.UpstreamHost + "/" + strconv.Itoa(manifest.UpstreamPort)
	if manifest.BasePath != "" {
		publicPath += "/" + manifest.BasePath
	}
	connectionUpgrade := "$stream_connection_upgrade"
	if cfg.NodeName == "bwg" {
		connectionUpgrade = "$stream_bwg_connection_upgrade"
	}
	block := func(location string) string {
		return fmt.Sprintf(`    location %s {
        proxy_pass http://127.0.0.1:18080;
        proxy_http_version 1.1;
        proxy_set_header Host $http_host;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Forwarded-Host $http_host;
        proxy_set_header X-Forwarded-Port $server_port;
        proxy_set_header X-Forwarded-For $remote_addr;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection %s;
        proxy_set_header Range $http_range;
        proxy_set_header If-Range $http_if_range;
        proxy_set_header Authorization $http_authorization;
        proxy_set_header X-Emby-Authorization $http_x_emby_authorization;
        proxy_set_header X-Emby-Token $http_x_emby_token;
        proxy_set_header X-MediaBrowser-Token $http_x_mediabrowser_token;
        proxy_set_header Cookie $http_cookie;
        proxy_set_header User-Agent $http_user_agent;
        proxy_set_header Accept $http_accept;
        proxy_set_header Accept-Encoding $http_accept_encoding;
        proxy_pass_header Content-Type;
        proxy_pass_header Content-Length;
        proxy_pass_header Content-Range;
        proxy_pass_header Accept-Ranges;
        proxy_pass_header Cache-Control;
        proxy_request_buffering off;
        proxy_buffering off;
        proxy_cache off;
        proxy_max_temp_file_size 0;
        gzip off;
        proxy_connect_timeout 15s;
        proxy_send_timeout 3600s;
        proxy_read_timeout 3600s;
    }
`, location, connectionUpgrade)
	}
	return []byte("# embyproxy-publication slug=" + manifest.Slug + "\n" +
		block("= "+publicPath) + block("^~ "+publicPath+"/"))
}

func testCandidate(ctx context.Context, fragment, directory string) error {
	config := filepath.Join(directory, "nginx-candidate.conf")
	content := "worker_processes 1;\nerror_log stderr;\npid " + filepath.Join(directory, "nginx.pid") + ";\n" +
		"events {}\nhttp { map $http_upgrade $stream_connection_upgrade { default upgrade; '' close; } map $http_upgrade $stream_bwg_connection_upgrade { default upgrade; '' close; } server { listen 127.0.0.1:18089; include " + fragment + "; } }\n"
	if err := os.WriteFile(config, []byte(content), 0600); err != nil {
		return err
	}
	return nginxTest(ctx, config)
}

func nginxTest(ctx context.Context, config string) error {
	// Keep the host's configured prefix so dynamically loaded Nginx modules
	// resolve exactly as they do for the production service.
	command := exec.CommandContext(ctx, "/usr/sbin/nginx", "-t", "-c", config)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	return command.Run()
}

func reloadNginx(ctx context.Context) error {
	command := exec.CommandContext(ctx, "/bin/systemctl", "reload", "nginx.service")
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	return command.Run()
}

func removeAndReload(ctx context.Context, target string) error {
	if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := nginxTest(ctx, "/etc/nginx/nginx.conf"); err != nil {
		return err
	}
	return reloadNginx(ctx)
}

func edgeFailure(code, step, backup string) publicationprotocol.EdgeResult {
	return publicationprotocol.EdgeResult{Status: "failed", ErrorCode: code, FailedStep: step, BackupPath: backup}
}

func encodeEdgeResult(output io.Writer, result publicationprotocol.EdgeResult) error {
	return json.NewEncoder(output).Encode(result)
}
