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
	"regexp"
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
	var previous []byte
	if current, err := os.ReadFile(target); err == nil {
		if bytes.Equal(current, fragment) {
			if err := nginxTest(ctx, "/etc/nginx/nginx.conf"); err != nil {
				return edgeFailure("nginx_test_failed", "nginx_test", backupDir)
			}
			return publicationprotocol.EdgeResult{Status: "synced", BackupPath: backupDir}
		}
		if !bytes.HasPrefix(current, []byte("# embyproxy-publication slug="+manifest.Slug+"\n")) {
			return edgeFailure("route_conflict", "edge_route", backupDir)
		}
		previous = current
		if err := os.WriteFile(filepath.Join(backupDir, manifest.Slug+".conf"), current, 0600); err != nil {
			return edgeFailure("backup_failed", "backup_write", backupDir)
		}
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
		restoreFragment(target, previous)
		return edgeFailure("nginx_test_failed", "nginx_test", backupDir)
	}
	if err := reloadNginx(ctx); err != nil {
		restoreFragment(target, previous)
		if rollbackErr := nginxTest(ctx, "/etc/nginx/nginx.conf"); rollbackErr != nil || reloadNginx(ctx) != nil {
			return edgeFailure("rollback_failed", "reload_rollback", backupDir)
		}
		return edgeFailure("reload_failed", "nginx_reload", backupDir)
	}
	return publicationprotocol.EdgeResult{Status: "synced", BackupPath: backupDir}
}

func restoreFragment(target string, previous []byte) {
	if len(previous) == 0 {
		_ = os.Remove(target)
		return
	}
	_ = os.WriteFile(target, previous, 0640)
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
	routes := manifestRoutes(manifest)
	if len(routes) == 0 || len(routes) > 32 {
		return errors.New("edge_manifest_invalid")
	}
	seenPaths, seenLines := map[string]bool{}, map[string]bool{}
	for index, route := range routes {
		hostValid := safeHostname(route.Host)
		if route.Kind == "redirect" {
			hostValid = safeRedirectHost(route.Host)
		}
		patternValid := route.Kind == "redirect_pattern" && route.Host == "" && safeHostname(route.HostSuffix) && route.LabelLength >= 1 && route.LabelLength <= 16 && route.BasePath == ""
		if (route.Scheme != "http" && route.Scheme != "https") || (!hostValid && !patternValid) || route.Port < 1 || route.Port > 65535 ||
			(route.Kind != "upstream" && route.Kind != "redirect" && route.Kind != "redirect_pattern") ||
			route.LineID == "" || len(route.LineID) > 64 || strings.ContainsAny(route.LineID, " /\\.\t\r\n") ||
			route.Position < 1 || route.Position > 64 || seenLines[route.LineID] {
			return errors.New("edge_manifest_invalid")
		}
		if route.BasePath != "" && (strings.HasPrefix(route.BasePath, "/") || strings.ContainsAny(route.BasePath, "?#\r\n") || strings.Contains(route.BasePath, "..")) {
			return errors.New("edge_manifest_invalid")
		}
		key := route.Scheme + "://" + route.Host + ":" + strconv.Itoa(route.Port) + "/" + route.BasePath
		if route.Kind == "redirect_pattern" {
			key = route.Scheme + "://pattern/" + route.HostSuffix + ":" + strconv.Itoa(route.Port) + "/" + strconv.Itoa(route.LabelLength)
		}
		if seenPaths[key] {
			return errors.New("edge_manifest_invalid")
		}
		seenPaths[key], seenLines[route.LineID] = true, true
		if index == 0 && (route.Scheme != "https" || route.Host != manifest.UpstreamHost || route.Port != manifest.UpstreamPort || route.BasePath != manifest.BasePath || route.Kind != "upstream") {
			return errors.New("edge_manifest_invalid")
		}
	}
	return nil
}

func verifyIncludeHook(cfg EdgeConfig) error {
	raw, err := os.ReadFile(cfg.StreamConfig)
	if err != nil {
		return errors.New("edge_stream_config_unavailable")
	}
	expected := "include " + cfg.IncludeDirective + ";"
	// Production stream configs have separate HTTP redirect/challenge and HTTPS
	// serving blocks. A hook in only the HTTP block makes readiness look healthy
	// while every real media request falls through the HTTPS deny location.
	if strings.Count(string(raw), expected) < 2 {
		return errors.New("edge_include_hook_missing")
	}
	return nil
}

func renderNginxFragment(cfg EdgeConfig, manifest publicationprotocol.EdgeManifest) []byte {
	routes := manifestRoutes(manifest)
	connectionUpgrade := "$stream_connection_upgrade"
	if cfg.NodeName == "bwg" {
		connectionUpgrade = "$stream_bwg_connection_upgrade"
	}
	absRedirects := renderAbsoluteRedirects(routes)
	block := func(location, fallback, relativePath string) string {
		fallbackDirectives := ""
		if fallback != "" {
			fallbackDirectives = "        proxy_intercept_errors on;\n        recursive_error_pages on;\n        error_page 403 404 416 429 500 502 503 504 = " + fallback + ";\n"
		}
		redirectDirectives := absRedirects
		if relativePath != "" {
			// Relative media redirects belong to the current allowlisted route.
			// Already encoded /https/... locations are deliberately left intact.
			redirectDirectives = "        proxy_redirect ~^/(?!https/)(.*)$ $scheme://$host" + relativePath + "/$1;\n" + redirectDirectives
		}
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
	%s%s
    }
	`, location, connectionUpgrade, fallbackDirectives, redirectDirectives)
	}
	publicPath := func(route publicationprotocol.EdgeRoute) string {
		value := "/" + route.Scheme + "/" + route.Host + "/" + strconv.Itoa(route.Port)
		if route.BasePath != "" {
			value += "/" + route.BasePath
		}
		return value
	}
	upstreams := make([]publicationprotocol.EdgeRoute, 0, len(routes))
	for _, route := range routes {
		if route.Kind == "upstream" {
			upstreams = append(upstreams, route)
		}
	}
	var result strings.Builder
	result.WriteString("# embyproxy-publication slug=" + manifest.Slug + "\n")
	for index, route := range routes {
		if route.Kind == "redirect_pattern" {
			// Nginx tokenizes an unquoted location regex before it sees `{n}`.
			// Repeating the fixed class keeps the generated pattern literal and
			// avoids introducing a regex string from configuration.
			pattern := "^/" + route.Scheme + "/" + strings.Repeat("[a-z0-9]", route.LabelLength) + "\\." + regexp.QuoteMeta(route.HostSuffix) + "/" + strconv.Itoa(route.Port) + "(?:/|$)"
			result.WriteString(block("~ "+pattern, "", ""))
			continue
		}
		fallback := ""
		if route.Kind == "upstream" && index == 0 && len(upstreams) > 1 {
			fallback = "@embyproxy_" + manifest.Slug + "_backup_2"
		}
		path := publicPath(route)
		result.WriteString(block("= "+path, fallback, path))
		result.WriteString(block("^~ "+path+"/", fallback, path))
	}
	primaryPath := publicPath(upstreams[0])
	for index := 1; index < len(upstreams); index++ {
		next := ""
		if index+1 < len(upstreams) {
			next = "@embyproxy_" + manifest.Slug + "_backup_" + strconv.Itoa(index+2)
		}
		rewrite := "        rewrite ^" + regexp.QuoteMeta(primaryPath) + "(.*)$ " + publicPath(upstreams[index]) + "$1 break;\n"
		// Named fallback locations rewrite only to a manifest-listed target,
		// then use the same streaming proxy settings as normal locations.
		named := block("@embyproxy_"+manifest.Slug+"_backup_"+strconv.Itoa(index+1), next, publicPath(upstreams[index]))
		named = strings.Replace(named, "        proxy_pass http://127.0.0.1:18080;\n", rewrite+"        proxy_pass http://127.0.0.1:18080;\n", 1)
		result.WriteString(named)
	}
	return []byte(result.String())
}

// renderAbsoluteRedirects rewrites only destinations that came from the
// manifest. Manifest routes are assembled from saved upstreams and root-owned
// redirect rules, so a response cannot create an arbitrary stream proxy path.
// The replacement intentionally retains the upstream query in transit; access
// logs use $uri and never record it.
func renderAbsoluteRedirects(routes []publicationprotocol.EdgeRoute) string {
	var result strings.Builder
	publicPath := func(route publicationprotocol.EdgeRoute) string {
		value := "/" + route.Scheme + "/" + route.Host + "/" + strconv.Itoa(route.Port)
		if route.BasePath != "" {
			value += "/" + route.BasePath
		}
		return value
	}
	portPattern := func(route publicationprotocol.EdgeRoute) string {
		if (route.Scheme == "https" && route.Port == 443) || (route.Scheme == "http" && route.Port == 80) {
			return "(?::" + strconv.Itoa(route.Port) + ")?"
		}
		return ":" + strconv.Itoa(route.Port)
	}
	for _, route := range routes {
		switch route.Kind {
		case "upstream", "redirect":
			pathPrefix := ""
			if route.BasePath != "" {
				pathPrefix = "/" + regexp.QuoteMeta(route.BasePath)
			}
			pattern := "^" + route.Scheme + "://" + regexp.QuoteMeta(route.Host) + portPattern(route) + pathPrefix + "(.*)$"
			result.WriteString("        proxy_redirect ~" + pattern + " $scheme://$host" + publicPath(route) + "$1;\n")
		case "redirect_pattern":
			label := "(" + strings.Repeat("[a-z0-9]", route.LabelLength) + ")"
			pattern := "^" + route.Scheme + "://" + label + "\\." + regexp.QuoteMeta(route.HostSuffix) + portPattern(route) + "(.*)$"
			result.WriteString("        proxy_redirect ~" + pattern + " $scheme://$host/" + route.Scheme + "/$1." + route.HostSuffix + "/" + strconv.Itoa(route.Port) + "$2;\n")
		}
	}
	return result.String()
}

func manifestRoutes(manifest publicationprotocol.EdgeManifest) []publicationprotocol.EdgeRoute {
	if len(manifest.Routes) > 0 {
		return manifest.Routes
	}
	return []publicationprotocol.EdgeRoute{{
		LineID: "main", Scheme: "https", Host: manifest.UpstreamHost, Port: manifest.UpstreamPort,
		BasePath: manifest.BasePath, Kind: "upstream", Position: 1,
	}}
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
