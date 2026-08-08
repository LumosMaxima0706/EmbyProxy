package failover

import (
	"errors"
	"net/url"
	"strings"
)

var ErrRedirectTargetNotAllowed = errors.New("redirect target is not allowlisted")

func BuildRedirect(node Node, allowlist map[string]bool, path string, query string) (string, error) {
	if !allowlist[node.PublicHost] || strings.TrimSpace(node.PublicHost) == "" {
		return "", ErrRedirectTargetNotAllowed
	}
	u := url.URL{Scheme: "https", Host: node.PublicHost, Path: "/" + strings.TrimLeft(path, "/"), RawQuery: query}
	return u.String(), nil
}
