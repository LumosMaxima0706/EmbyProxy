package proxyadapter

import (
	"errors"
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
	ErrMultipleTarget = errors.New("multiple node targets are not supported by mock adapter")
)

var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,30}[a-z0-9]$|^[a-z0-9]$`)
var nodePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,32}$`)

var reservedSlugs = map[string]struct{}{
	"admin": {}, "api": {}, "health": {}, "http": {}, "https": {}, "s": {},
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

func splitRoutePath(raw string) ([]string, error) {
	trimmed := strings.Trim(raw, "/")
	if trimmed == "" {
		return nil, ErrNotFound
	}
	parts := strings.Split(trimmed, "/")
	decoded := make([]string, 0, len(parts))
	for _, part := range parts {
		value, err := url.PathUnescape(part)
		if err != nil || value == "" || strings.Contains(value, "/") {
			return nil, ErrNotFound
		}
		decoded = append(decoded, value)
	}
	return decoded, nil
}
