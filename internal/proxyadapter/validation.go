package proxyadapter

import (
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

var (
	ErrNotFound       = errors.New("adapter route not found")
	ErrInvalidSlug    = errors.New("invalid slug")
	ErrReservedSlug   = errors.New("reserved slug")
	ErrInvalidTarget  = errors.New("invalid server target")
	ErrInvalidNode    = errors.New("invalid node")
	ErrMissingSecret  = errors.New("missing node secret")
	ErrInvalidSecret  = errors.New("invalid node secret")
	ErrMultipleTarget = errors.New("multiple node targets are not supported by this route adapter")
	ErrResolver       = errors.New("route resolver unavailable")
	ErrRouteDisabled  = errors.New("managed route is disabled or private")
)

var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,30}[a-z0-9]$|^[a-z0-9]$`)
var nodePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,32}$`)

var reservedSlugs = map[string]struct{}{
	"admin": {}, "api": {}, "health": {}, "http": {}, "https": {}, "s": {},
}

func requestPath(req *http.Request) (string, error) {
	if req == nil || req.URL == nil {
		return "", ErrNotFound
	}
	raw := req.RequestURI
	if raw == "" {
		raw = req.URL.EscapedPath()
	} else if idx := strings.IndexByte(raw, '?'); idx >= 0 {
		raw = raw[:idx]
	}
	if raw == "" || !strings.HasPrefix(raw, "/") {
		return "", ErrNotFound
	}
	return raw, nil
}

func validateSlug(slug string) error {
	if !slugPattern.MatchString(slug) {
		return ErrInvalidSlug
	}
	if _, reserved := reservedSlugs[slug]; reserved {
		return ErrReservedSlug
	}
	return nil
}

func validateNodeName(name string) error {
	if !nodePattern.MatchString(name) {
		return ErrInvalidNode
	}
	return nil
}

// ValidateManagedRouteSlug exposes the same route constraints used by the
// production resolver to management-plane callers.
func ValidateManagedRouteSlug(slug string) error {
	return validateSlug(slug)
}

// ValidateManagedRouteNode exposes the node-name constraints used by managed
// route storage callers.
func ValidateManagedRouteNode(name string) error {
	return validateNodeName(name)
}

// ValidateManagedTarget checks a managed line before it is persisted.
func ValidateManagedTarget(raw string) error {
	_, err := parseServerTarget(raw)
	return err
}

func splitRoutePath(raw string) ([]string, error) {
	if err := validateRawPath(raw); err != nil {
		return nil, err
	}
	trimmed := strings.Trim(raw, "/")
	if trimmed == "" {
		return nil, ErrNotFound
	}
	parts := strings.Split(trimmed, "/")
	decoded := make([]string, 0, len(parts))
	for _, part := range parts {
		value, err := url.PathUnescape(part)
		if err != nil || value == "" || strings.ContainsAny(value, "/\\") || value == "." || value == ".." {
			return nil, ErrNotFound
		}
		decoded = append(decoded, value)
	}
	return decoded, nil
}

func validateRawPath(raw string) error {
	if raw == "" || !strings.HasPrefix(raw, "/") || strings.ContainsAny(raw, "?#") {
		return ErrNotFound
	}
	current := raw
	for pass := 0; pass < 8; pass++ {
		lower := strings.ToLower(current)
		if strings.Contains(lower, "%2f") || strings.Contains(lower, "%5c") || strings.Contains(lower, "%2e") {
			return ErrNotFound
		}
		decoded, err := url.PathUnescape(current)
		if err != nil {
			return ErrNotFound
		}
		if strings.ContainsAny(decoded, "\\") {
			return ErrNotFound
		}
		for _, segment := range strings.Split(decoded, "/") {
			if segment == "." || segment == ".." {
				return ErrNotFound
			}
		}
		if decoded == current {
			return nil
		}
		current = decoded
	}
	return ErrNotFound
}

func normalizeSafeBasePath(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "/" {
		return "", nil
	}
	if err := validateRawPath(raw); err != nil {
		return "", ErrInvalidTarget
	}
	return raw, nil
}
