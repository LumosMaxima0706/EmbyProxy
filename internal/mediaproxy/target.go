package mediaproxy

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"path"
	"strconv"
	"strings"
)

func ValidateTarget(target Target) error {
	if target.Scheme != "http" && target.Scheme != "https" {
		return ErrInvalidScheme
	}
	if strings.TrimSpace(target.Host) == "" || strings.ContainsAny(target.Host, "/?#@") {
		return ErrInvalidHost
	}
	if target.Port < 1 || target.Port > 65535 {
		return ErrInvalidPort
	}
	if target.BasePath != "" && !strings.HasPrefix(target.BasePath, "/") {
		return ErrInvalidBasePath
	}
	if hasTraversal(target.BasePath) {
		return ErrInvalidBasePath
	}
	return nil
}

func ParseTarget(scheme, host string, port int, basePath string) (Target, error) {
	target := Target{Scheme: strings.ToLower(strings.TrimSpace(scheme)), Host: strings.TrimSpace(host), Port: port, BasePath: cleanBasePath(basePath)}
	if err := ValidateTarget(target); err != nil {
		return Target{}, err
	}
	return target, nil
}

func (t Target) origin() string {
	return t.Scheme + "://" + net.JoinHostPort(t.Host, strconv.Itoa(t.Port))
}

func (t Target) URLForRequest(r *url.URL) (*url.URL, error) {
	if err := ValidateTarget(t); err != nil {
		return nil, err
	}
	tail := "/"
	if r != nil && r.Path != "" {
		tail = r.Path
	}
	if hasTraversal(tail) {
		return nil, ErrInvalidRequestPath
	}
	base := cleanBasePath(t.BasePath)
	joined := path.Join(base, tail)
	if strings.HasSuffix(tail, "/") && !strings.HasSuffix(joined, "/") {
		joined += "/"
	}
	result := &url.URL{Scheme: t.Scheme, Host: net.JoinHostPort(t.Host, strconv.Itoa(t.Port)), Path: joined}
	if r != nil {
		result.RawQuery = r.RawQuery
	}
	return result, nil
}

func hasTraversal(value string) bool {
	decoded, err := url.PathUnescape(value)
	if err != nil {
		return true
	}
	for _, segment := range strings.Split(decoded, "/") {
		if segment == "." || segment == ".." {
			return true
		}
	}
	return false
}

func (t Target) CheckPrivate(ctx context.Context, allow bool) error {
	if allow {
		return nil
	}
	private, err := hostIsPrivate(ctx, t.Host)
	if err != nil {
		return fmt.Errorf("target resolution failed: %w", err)
	}
	if private {
		return ErrPrivateTarget
	}
	return nil
}

func (e *Executor) resolveTarget(ctx context.Context, target Target) ([]netip.Addr, error) {
	resolver := e.cfg.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	if ip, err := netip.ParseAddr(strings.Trim(target.Host, "[]")); err == nil {
		if !e.cfg.AllowPrivateTargets && addrIsPrivate(ip) {
			return nil, ErrPrivateTarget
		}
		return []netip.Addr{ip}, nil
	}
	if strings.EqualFold(strings.TrimSuffix(target.Host, "."), "localhost") && !e.cfg.AllowPrivateTargets {
		return nil, ErrPrivateTarget
	}
	addresses, err := resolver.LookupNetIP(ctx, "ip", target.Host)
	if err != nil {
		return nil, fmt.Errorf("target resolution failed: %w", err)
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("target resolution returned no addresses")
	}
	for _, address := range addresses {
		if !e.cfg.AllowPrivateTargets && addrIsPrivate(address) {
			return nil, ErrPrivateTarget
		}
	}
	return append([]netip.Addr(nil), addresses...), nil
}

type resolvedTargetContextKey struct{}

type resolvedTargetInfo struct {
	host      string
	port      int
	addresses []netip.Addr
}

func withResolvedTarget(ctx context.Context, target Target, addresses []netip.Addr) context.Context {
	return context.WithValue(ctx, resolvedTargetContextKey{}, resolvedTargetInfo{
		host: target.Host, port: target.Port, addresses: append([]netip.Addr(nil), addresses...),
	})
}

func resolvedTargetFromContext(ctx context.Context) (resolvedTargetInfo, bool) {
	value, ok := ctx.Value(resolvedTargetContextKey{}).(resolvedTargetInfo)
	return value, ok
}

func cleanBasePath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "/" {
		return ""
	}
	return "/" + strings.Trim(value, "/")
}
