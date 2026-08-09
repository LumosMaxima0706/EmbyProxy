package config

import (
	"bufio"
	"fmt"
	"net"
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
	CWD              string
	DBPath           string
	Port             int
	ListenAddr       string
	AdminListenAddr  string
	AdminToken       string
	Admin2FADisabled bool
	MediaProxyRoutes bool
	Defaults         Defaults
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
	cfg := Config{
		CWD:              cwd,
		DBPath:           envString("DB_PATH", filepath.Join(cwd, "data", "proxy.db")),
		Port:             port,
		ListenAddr:       listenAddr,
		AdminListenAddr:  adminListenAddr,
		AdminToken:       os.Getenv("ADMIN_TOKEN"),
		Admin2FADisabled: envBool("ADMIN_2FA_DISABLED", false),
		MediaProxyRoutes: envBool("MEDIAPROXY_ROUTES_ENABLED", false),
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
	return cfg, nil
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
