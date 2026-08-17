package mediaproxy

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

func newTransport(cfg Config) *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	baseDial := cfg.DialContext
	if baseDial == nil {
		dialer := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
		baseDial = dialer.DialContext
	}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if resolved, ok := resolvedTargetFromContext(ctx); ok && resolvedMatches(address, resolved) {
			return dialResolved(ctx, network, resolved, baseDial)
		}
		return baseDial(ctx, network, address)
	}
	if cfg.TLSConfig != nil {
		transport.TLSClientConfig = cfg.TLSConfig.Clone()
	} else {
		transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	if !cfg.TrustProxyEnv {
		transport.Proxy = nil
		return transport
	}
	transport.Proxy = func(request *http.Request) (*url.URL, error) {
		if noProxyMatch(request.URL.Hostname(), os.Getenv("NO_PROXY")) {
			return nil, nil
		}
		keys := []string{"HTTP_PROXY", "http_proxy"}
		if request.URL.Scheme == "https" {
			keys = []string{"HTTPS_PROXY", "https_proxy"}
		}
		for _, key := range keys {
			if raw := strings.TrimSpace(os.Getenv(key)); raw != "" {
				return url.Parse(raw)
			}
		}
		for _, key := range []string{"ALL_PROXY", "all_proxy"} {
			if raw := strings.TrimSpace(os.Getenv(key)); raw != "" {
				return url.Parse(raw)
			}
		}
		return nil, nil
	}
	return transport
}

func resolvedMatches(address string, resolved resolvedTargetInfo) bool {
	host, port, err := net.SplitHostPort(address)
	if err != nil || port != strconv.Itoa(resolved.port) {
		return false
	}
	return strings.EqualFold(strings.Trim(host, "[]"), strings.Trim(resolved.host, "[]"))
}

func dialResolved(ctx context.Context, network string, resolved resolvedTargetInfo, dial DialContextFunc) (net.Conn, error) {
	var lastErr error
	for _, address := range resolved.addresses {
		conn, err := dial(ctx, network, net.JoinHostPort(address.String(), strconv.Itoa(resolved.port)))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = ErrUpstreamUnavailable
	}
	return nil, lastErr
}

func noProxyMatch(host, raw string) bool {
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(strings.ToLower(item))
		if item == "" {
			continue
		}
		if item == "*" {
			return true
		}
		if hostWithPort, _, err := net.SplitHostPort(item); err == nil {
			item = strings.Trim(hostWithPort, "[]")
		}
		item = strings.TrimPrefix(item, ".")
		if strings.EqualFold(host, item) || strings.HasSuffix(strings.ToLower(host), "."+item) {
			return true
		}
	}
	return false
}
