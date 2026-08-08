package failover

import (
	"errors"
	"net/url"
	"strings"
)

var ErrRedirectTargetNotAllowed = errors.New("redirect target is not allowlisted")

func BuildRedirect(node Node, allowlist map[string]bool, path string, query string) (string, error) {
	host := strings.TrimSpace(node.PublicHost)
	if !allowlist[host] || host == "" || !validRedirectHost(host) {
		return "", ErrRedirectTargetNotAllowed
	}
	u := url.URL{Scheme: "https", Host: host, Path: "/" + strings.TrimLeft(path, "/"), RawQuery: query}
	return u.String(), nil
}

func validRedirectHost(host string) bool {
	parsed, err := url.Parse("https://" + host)
	return err == nil && parsed.User == nil && parsed.Host == host && parsed.Hostname() != "" && parsed.Path == "" && parsed.RawQuery == "" && parsed.Fragment == ""
}
