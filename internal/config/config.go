package config

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Defaults struct {
	CacheTTL           int64
	ListCacheTTL       int64
	MaxRetryBodyBytes  int64
	ImageCacheTTL      int
	PingCacheTTL       int
	StaticCacheTTL     int
	ProgressThrottleMS int64
}

type Config struct {
	CWD                    string
	DBPath                 string
	GlobalStatsDBPath      string
	Port                   int
	ListenAddr             string
	AdminListenAddr        string
	AdminToken             string
	Admin2FADisabled       bool
	OwnerAdminAuthMode     string
	OwnerAdminHost         string
	PublicMediaBaseURL     string
	PublicMediaNodePaths   map[string]string
	PublicationAgentSocket string
	// PlaybackCredentialDir stores per-publication Emby tokens outside SQLite.
	// It is intentionally a local runtime directory, never a Git path.
	PlaybackCredentialDir     string
	MediaProxyRoutes          bool
	FailoverDNSProviderMode   string
	FailoverDNSAllowedRecords string
	FailoverDNSRealApply      bool
	FailoverMockFixture       bool
	FailoverStateFile         string
	Defaults                  Defaults
}

type ProxyEnv struct {
	CORSAllowOrigin    string
	CapyStripEmby      string
	EmosCompat         bool
	EmosMatchHosts     string
	EmosProxyID        string
	EmosProxyName      string
	ExternalAllowHosts string
	ExternalAllowAny   bool
}

func Load() (Config, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return Config{}, err
	}
	_ = loadDotEnv(filepath.Join(cwd, ".env"))
	port, err := configuredListenPort("PORT", 8787)
	if err != nil {
		return Config{}, err
	}
	listenAddr, err := proxyListenAddr(port)
	if err != nil {
		return Config{}, err
	}
	adminListenAddr, err := adminListenAddr()
	if err != nil {
		return Config{}, err
	}
	effectiveListenAddr := listenAddr
	if effectiveListenAddr == "" {
		effectiveListenAddr = net.JoinHostPort("0.0.0.0", strconv.Itoa(port))
	}
	if adminListenAddr != "" && adminListenAddr == effectiveListenAddr {
		return Config{}, fmt.Errorf("ADMIN_LISTEN_ADDR must differ from the proxy listen address")
	}
	publicMediaBaseURL, err := normalizePublicMediaBaseURL(os.Getenv("PUBLIC_MEDIA_BASE_URL"))
	if err != nil {
		return Config{}, err
	}
	publicMediaNodePaths, err := parsePublicMediaNodePaths(os.Getenv("PUBLIC_MEDIA_NODE_PATHS_JSON"))
	if err != nil {
		return Config{}, err
	}
	cfg := Config{
		CWD:                       cwd,
		DBPath:                    envString("DB_PATH", filepath.Join(cwd, "data", "proxy.db")),
		GlobalStatsDBPath:         envString("GLOBAL_STATS_DB_PATH", "/var/lib/embyproxy-gsy-sidecar/global-stats.db"),
		Port:                      port,
		ListenAddr:                listenAddr,
		AdminListenAddr:           adminListenAddr,
		AdminToken:                os.Getenv("ADMIN_TOKEN"),
		Admin2FADisabled:          envBool("ADMIN_2FA_DISABLED", false),
		OwnerAdminAuthMode:        strings.ToLower(strings.TrimSpace(os.Getenv("OWNER_ADMIN_AUTH_MODE"))),
		OwnerAdminHost:            strings.ToLower(strings.TrimSpace(os.Getenv("OWNER_ADMIN_HOST"))),
		PublicMediaBaseURL:        publicMediaBaseURL,
		PublicMediaNodePaths:      publicMediaNodePaths,
		PublicationAgentSocket:    strings.TrimSpace(os.Getenv("PUBLICATION_AGENT_SOCKET")),
		PlaybackCredentialDir:     envString("PLAYBACK_CREDENTIAL_DIR", "/var/lib/embyproxy-gsy-sidecar/playback-credentials"),
		MediaProxyRoutes:          envBool("MEDIAPROXY_ROUTES_ENABLED", false),
		FailoverDNSProviderMode:   strings.ToLower(strings.TrimSpace(os.Getenv("FAILOVER_DNS_PROVIDER_MODE"))),
		FailoverDNSAllowedRecords: strings.TrimSpace(os.Getenv("FAILOVER_DNS_ALLOWED_RECORDS")),
		FailoverDNSRealApply:      envBool("FAILOVER_DNS_REAL_APPLY_ENABLED", false),
		FailoverMockFixture:       envBool("FAILOVER_MOCK_FIXTURE_ENABLED", false),
		FailoverStateFile:         envString("FAILOVER_STATE_FILE", "/var/lib/embyproxy-gsy-sidecar/failover-state.json"),
		Defaults: Defaults{
			CacheTTL:           10000,
			ListCacheTTL:       180000,
			MaxRetryBodyBytes:  32 * 1024 * 1024,
			ImageCacheTTL:      30 * 24 * 60 * 60,
			PingCacheTTL:       60,
			StaticCacheTTL:     604800,
			ProgressThrottleMS: 1200,
		},
	}
	if cfg.OwnerAdminAuthMode != "" && cfg.OwnerAdminAuthMode != "basic_only" {
		return Config{}, fmt.Errorf("OWNER_ADMIN_AUTH_MODE must be empty or basic_only")
	}
	if cfg.OwnerAdminAuthMode == "basic_only" &&
		(cfg.OwnerAdminHost == "" || strings.ContainsAny(cfg.OwnerAdminHost, " /\\:\t\r\n")) {
		return Config{}, fmt.Errorf("OWNER_ADMIN_HOST must be an exact hostname for basic_only mode")
	}
	if cfg.PublicMediaBaseURL != "" {
		publicURL, _ := url.Parse(cfg.PublicMediaBaseURL)
		if strings.EqualFold(publicURL.Hostname(), cfg.OwnerAdminHost) {
			return Config{}, fmt.Errorf("PUBLIC_MEDIA_BASE_URL must not use OWNER_ADMIN_HOST")
		}
	}
	if cfg.PublicationAgentSocket != "" &&
		(!strings.HasPrefix(cfg.PublicationAgentSocket, "/run/") || strings.ContainsAny(cfg.PublicationAgentSocket, "\x00\r\n")) {
		return Config{}, fmt.Errorf("PUBLICATION_AGENT_SOCKET must be an absolute path below /run")
	}
	return cfg, nil
}

func normalizePublicMediaBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", fmt.Errorf("PUBLIC_MEDIA_BASE_URL must be an HTTPS origin without credentials, path, query, or fragment")
	}
	return "https://" + parsed.Host, nil
}

func parsePublicMediaNodePaths(raw string) (map[string]string, error) {
	paths := map[string]string{}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return paths, nil
	}
	if err := json.Unmarshal([]byte(raw), &paths); err != nil {
		return nil, fmt.Errorf("PUBLIC_MEDIA_NODE_PATHS_JSON must be a JSON object")
	}
	normalized := make(map[string]string, len(paths))
	for name, publicPath := range paths {
		name = strings.ToLower(strings.TrimSpace(name))
		publicPath = strings.TrimSpace(publicPath)
		parsed, err := url.ParseRequestURI(publicPath)
		if name == "" || len(name) > 64 || strings.ContainsAny(name, "/\\?#\t\r\n") ||
			err != nil || !strings.HasPrefix(publicPath, "/") || strings.HasPrefix(publicPath, "//") ||
			parsed.IsAbs() || parsed.Host != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return nil, fmt.Errorf("PUBLIC_MEDIA_NODE_PATHS_JSON contains an unsafe node or path")
		}
		normalized[name] = publicPath
	}
	return normalized, nil
}

func (c Config) Addr() string {
	if c.ListenAddr != "" {
		return c.ListenAddr
	}
	return net.JoinHostPort("0.0.0.0", strconv.Itoa(c.Port))
}

func (c Config) AdminAddr() string {
	return c.AdminListenAddr
}

func (c Config) ProxyPort() int {
	_, rawPort, err := net.SplitHostPort(c.Addr())
	if err == nil {
		if port, parseErr := strconv.Atoi(rawPort); parseErr == nil {
			return port
		}
	}
	return c.Port
}

func (c Config) ProxyEnv() ProxyEnv {
	return ProxyEnv{}
}

func loadDotEnv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		idx := strings.IndexByte(line, '=')
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		value := strings.TrimSpace(line[idx+1:])
		value = stripInlineComment(value)
		value = strings.TrimSpace(value)
		if len(value) >= 2 {
			quote := value[0]
			if (quote == '\'' || quote == '"') && value[len(value)-1] == quote {
				value = value[1 : len(value)-1]
			}
		}
		if _, ok := os.LookupEnv(key); !ok {
			_ = os.Setenv(key, value)
		}
	}
	return s.Err()
}

func stripInlineComment(value string) string {
	inSingle := false
	inDouble := false
	for i, r := range value {
		switch r {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '#':
			if !inSingle && !inDouble && i > 0 && value[i-1] == ' ' {
				return strings.TrimSpace(value[:i])
			}
		}
	}
	return value
}

func envString(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	v, ok := os.LookupEnv(key)
	if !ok {
		return fallback
	}
	s := strings.TrimSpace(strings.ToLower(v))
	if s == "" || s == "0" || s == "false" || s == "off" || s == "no" {
		return false
	}
	return s == "1" || s == "true" || s == "yes" || s == "on"
}

func proxyListenAddr(defaultPort int) (string, error) {
	if raw := strings.TrimSpace(os.Getenv("LISTEN_ADDR")); raw != "" {
		return validateListenAddr("LISTEN_ADDR", raw)
	}
	host := strings.TrimSpace(os.Getenv("HOST"))
	if host == "" {
		return "", nil
	}
	port := defaultPort
	if rawPort := strings.TrimSpace(os.Getenv("PORT")); rawPort != "" {
		parsed, err := parseListenPort("PORT", rawPort)
		if err != nil {
			return "", err
		}
		port = parsed
	}
	return validateListenAddr("HOST/PORT", net.JoinHostPort(trimIPv6Brackets(host), strconv.Itoa(port)))
}

func adminListenAddr() (string, error) {
	if raw := strings.TrimSpace(os.Getenv("ADMIN_LISTEN_ADDR")); raw != "" {
		return validateListenAddr("ADMIN_LISTEN_ADDR", raw)
	}
	host := strings.TrimSpace(os.Getenv("ADMIN_HOST"))
	adminPort := strings.TrimSpace(os.Getenv("ADMIN_PORT"))
	dyndnsPort := strings.TrimSpace(os.Getenv("DYNDNS_PORT"))
	if host == "" {
		if adminPort != "" {
			return "", fmt.Errorf("ADMIN_HOST is required with ADMIN_PORT")
		}
		// DYNDNS_PORT predates the optional admin listener. Preserve its old
		// behavior unless ADMIN_HOST explicitly opts into the new listener.
		return "", nil
	}
	rawPort := adminPort
	portKey := "ADMIN_PORT"
	if rawPort == "" {
		rawPort = dyndnsPort
		portKey = "DYNDNS_PORT"
	}
	if rawPort == "" {
		return "", fmt.Errorf("ADMIN_PORT or DYNDNS_PORT is required with ADMIN_HOST")
	}
	port, err := parseListenPort(portKey, rawPort)
	if err != nil {
		return "", err
	}
	return validateListenAddr("ADMIN_HOST/"+portKey, net.JoinHostPort(trimIPv6Brackets(host), strconv.Itoa(port)))
}

func validateListenAddr(key, raw string) (string, error) {
	host, rawPort, err := net.SplitHostPort(strings.TrimSpace(raw))
	if err != nil || strings.TrimSpace(host) == "" || strings.ContainsAny(host, " \t\r\n") {
		return "", fmt.Errorf("%s must be a valid host:port", key)
	}
	port, err := parseListenPort(key, rawPort)
	if err != nil {
		return "", err
	}
	return net.JoinHostPort(host, strconv.Itoa(port)), nil
}

func parseListenPort(key, raw string) (int, error) {
	port, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("%s must use a port between 1 and 65535", key)
	}
	return port, nil
}

func configuredListenPort(key string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	return parseListenPort(key, raw)
}

func trimIPv6Brackets(host string) string {
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		return strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	}
	return host
}
