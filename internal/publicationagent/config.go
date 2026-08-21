package publicationagent

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"

	"embyproxy/internal/proxyadapter"
)

type RemoteConfig struct {
	User           string `json:"user"`
	Host           string `json:"host"`
	IdentityFile   string `json:"identity_file"`
	KnownHostsFile string `json:"known_hosts_file"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

type AgentConfig struct {
	SocketPath      string              `json:"socket_path"`
	DatabasePath    string              `json:"database_path"`
	AllowedPeerUID  uint32              `json:"allowed_peer_uid"`
	LocalHelperPath string              `json:"local_helper_path"`
	BWGConfigPath   string              `json:"bwg_config_path"`
	PublicMediaHost string              `json:"public_media_host"`
	OwnerAdminHost  string              `json:"owner_admin_host"`
	RedirectHosts   map[string][]string `json:"redirect_hosts,omitempty"`
	// RedirectEndpoints is root-owned because redirects are observed at the
	// edge. It extends the legacy HTTPS/443 RedirectHosts form for controlled
	// media origins that use a non-standard scheme or port.
	RedirectEndpoints map[string][]RedirectEndpoint `json:"redirect_endpoints,omitempty"`
	// RedirectPatterns is a narrow allowance for CDNs that rotate only the
	// left-most short label below an observed root-owned suffix.
	RedirectPatterns map[string][]RedirectPattern `json:"redirect_patterns,omitempty"`
	// DiscoveredEndpointsPath is a root-owned runtime store populated only by
	// a successful bounded playback canary. It must never be committed to Git.
	DiscoveredEndpointsPath string       `json:"discovered_endpoints_path,omitempty"`
	NOSLA                   RemoteConfig `json:"nosla"`
}

type RedirectEndpoint struct {
	Scheme     string `json:"scheme"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	PathPrefix string `json:"path_prefix,omitempty"`
}

type RedirectPattern struct {
	Scheme      string `json:"scheme"`
	Suffix      string `json:"suffix"`
	Port        int    `json:"port"`
	LabelLength int    `json:"label_length"`
}

type EdgeConfig struct {
	NodeName         string `json:"node_name"`
	IncludeDir       string `json:"include_dir"`
	StreamConfig     string `json:"stream_config"`
	IncludeDirective string `json:"include_directive"`
	BackupRoot       string `json:"backup_root"`
}

func LoadAgentConfig(path string) (AgentConfig, error) {
	var cfg AgentConfig
	if err := loadRootConfig(path, &cfg); err != nil {
		return cfg, err
	}
	if !safeAbsoluteBelow(cfg.SocketPath, "/run") || !safeAbsolute(cfg.DatabasePath) ||
		!safeAbsolute(cfg.LocalHelperPath) || !safeAbsolute(cfg.BWGConfigPath) || cfg.AllowedPeerUID == 0 {
		return cfg, errors.New("agent_config_invalid")
	}
	if !safeHostname(cfg.PublicMediaHost) || !safeHostname(cfg.OwnerAdminHost) ||
		!safeRemote(cfg.NOSLA) {
		return cfg, errors.New("agent_config_invalid")
	}
	if cfg.DiscoveredEndpointsPath == "" {
		cfg.DiscoveredEndpointsPath = "/var/lib/embyproxy-publication-agent/redirect-endpoints.json"
	}
	if !safeAbsoluteBelow(cfg.DiscoveredEndpointsPath, "/var/lib/embyproxy-publication-agent") {
		return cfg, errors.New("agent_config_invalid")
	}
	for slug, hosts := range cfg.RedirectHosts {
		if proxyadapter.ValidateManagedRouteSlug(slug) != nil || len(hosts) > 16 {
			return cfg, errors.New("agent_config_invalid")
		}
		for _, host := range hosts {
			if !safeHostname(host) || strings.EqualFold(host, cfg.PublicMediaHost) || strings.EqualFold(host, cfg.OwnerAdminHost) {
				return cfg, errors.New("agent_config_invalid")
			}
		}
	}
	for slug, endpoints := range cfg.RedirectEndpoints {
		if proxyadapter.ValidateManagedRouteSlug(slug) != nil || len(endpoints) > 16 || len(endpoints)+len(cfg.RedirectHosts[slug])+len(cfg.RedirectPatterns[slug]) > 16 {
			return cfg, errors.New("agent_config_invalid")
		}
		for _, endpoint := range endpoints {
			if !safeRedirectEndpoint(endpoint, cfg.PublicMediaHost, cfg.OwnerAdminHost) {
				return cfg, errors.New("agent_config_invalid")
			}
		}
	}
	for slug, patterns := range cfg.RedirectPatterns {
		if proxyadapter.ValidateManagedRouteSlug(slug) != nil || len(patterns) > 16 || len(patterns)+len(cfg.RedirectHosts[slug])+len(cfg.RedirectEndpoints[slug]) > 16 {
			return cfg, errors.New("agent_config_invalid")
		}
		for _, pattern := range patterns {
			if !safeRedirectPattern(pattern, cfg.PublicMediaHost, cfg.OwnerAdminHost) {
				return cfg, errors.New("agent_config_invalid")
			}
		}
	}
	if cfg.NOSLA.TimeoutSeconds < 1 || cfg.NOSLA.TimeoutSeconds > 60 {
		cfg.NOSLA.TimeoutSeconds = 10
	}
	return cfg, nil
}

func safeRedirectPattern(pattern RedirectPattern, publicMediaHost, ownerAdminHost string) bool {
	scheme := strings.ToLower(strings.TrimSpace(pattern.Scheme))
	suffix := strings.ToLower(strings.TrimSpace(pattern.Suffix))
	return (scheme == "http" || scheme == "https") && pattern.Port >= 1 && pattern.Port <= 65535 &&
		pattern.LabelLength >= 1 && pattern.LabelLength <= 16 && safeHostname(suffix) &&
		!strings.EqualFold(suffix, publicMediaHost) && !strings.EqualFold(suffix, ownerAdminHost)
}

func safeRedirectEndpoint(endpoint RedirectEndpoint, publicMediaHost, ownerAdminHost string) bool {
	scheme := strings.ToLower(strings.TrimSpace(endpoint.Scheme))
	host := strings.ToLower(strings.TrimSpace(endpoint.Host))
	return (scheme == "http" || scheme == "https") && endpoint.Port >= 1 && endpoint.Port <= 65535 &&
		safeRedirectHost(host) && safeRedirectPathPrefix(endpoint.PathPrefix) &&
		!strings.EqualFold(host, publicMediaHost) && !strings.EqualFold(host, ownerAdminHost)
}

func safeRedirectPathPrefix(value string) bool {
	if value == "" {
		return true
	}
	if len(value) > 64 || strings.ContainsAny(value, "/\\?#%\r\n\t ") || value == "." || value == ".." {
		return false
	}
	for _, r := range value {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && !strings.ContainsRune("._~-", r) {
			return false
		}
	}
	return true
}

// Redirect endpoints are root-owned observed media origins. They may be a
// literal globally routable address because some Emby backends issue a signed
// redirect to an IP:port. Saved upstreams remain DNS-only; this exception is
// limited to exact redirect endpoint entries and never comes from the API.
func safeRedirectHost(value string) bool {
	if ip := net.ParseIP(strings.TrimSpace(value)); ip != nil {
		return !ip.IsUnspecified() && !ip.IsLoopback() && !ip.IsMulticast() && !ip.IsLinkLocalUnicast() && !ip.IsPrivate()
	}
	return safeHostname(value)
}

func LoadEdgeConfig(path string) (EdgeConfig, error) {
	var cfg EdgeConfig
	if err := loadRootConfig(path, &cfg); err != nil {
		return cfg, err
	}
	if cfg.NodeName != "nosla" && cfg.NodeName != "bwg" {
		return cfg, errors.New("edge_config_invalid")
	}
	if !safeAbsoluteBelow(cfg.IncludeDir, "/etc/nginx") || !safeAbsoluteBelow(cfg.StreamConfig, "/etc/nginx") ||
		!safeAbsoluteBelowOrEqual(cfg.BackupRoot, "/var/backups/embyproxy-publication-agent") {
		return cfg, errors.New("edge_config_invalid")
	}
	if strings.TrimSpace(cfg.IncludeDirective) == "" || strings.ContainsAny(cfg.IncludeDirective, "\r\n{};") {
		return cfg, errors.New("edge_config_invalid")
	}
	return cfg, nil
}

func loadRootConfig(path string, dst any) error {
	if !safeAbsolute(path) {
		return errors.New("config_path_invalid")
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return errors.New("config_permissions_invalid")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return errors.New("config_read_failed")
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return errors.New("config_parse_failed")
	}
	return nil
}

func safeAbsolute(path string) bool {
	return path != "" && filepath.IsAbs(path) && filepath.Clean(path) == path && !strings.ContainsAny(path, "\x00\r\n")
}

func safeAbsoluteBelow(path, root string) bool {
	if !safeAbsolute(path) {
		return false
	}
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func safeAbsoluteBelowOrEqual(path, root string) bool {
	if path == root {
		return safeAbsolute(path)
	}
	return safeAbsoluteBelow(path, root)
}

func safeHostname(value string) bool {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" || len(value) > 253 || net.ParseIP(value) != nil || strings.ContainsAny(value, " /\\:@\t\r\n") {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
				return false
			}
		}
	}
	return true
}

func safeRemote(cfg RemoteConfig) bool {
	return cfg.User != "" && !strings.ContainsAny(cfg.User, " /\\:@\t\r\n") && safeRemoteHost(cfg.Host) &&
		safeAbsolute(cfg.IdentityFile) && safeAbsolute(cfg.KnownHostsFile)
}

func safeRemoteHost(value string) bool {
	value = strings.TrimSpace(strings.ToLower(value))
	if ip := net.ParseIP(value); ip != nil {
		return !ip.IsUnspecified() && !ip.IsMulticast()
	}
	return safeHostname(value)
}
