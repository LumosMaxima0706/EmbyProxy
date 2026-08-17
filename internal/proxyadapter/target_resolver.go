package proxyadapter

import (
	"net/url"
	"strconv"
	"strings"

	"embyproxy/internal/mediaproxy"
)

func parseServerTarget(raw string) (mediaproxy.Target, error) {
	value := strings.TrimSpace(raw)
	parsed, err := url.Parse(value)
	if err != nil || parsed.User != nil || parsed.Fragment != "" || parsed.RawQuery != "" {
		return mediaproxy.Target{}, ErrInvalidTarget
	}
	if strings.HasSuffix(parsed.Host, ":") {
		return mediaproxy.Target{}, ErrInvalidTarget
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" || parsed.Hostname() == "" {
		return mediaproxy.Target{}, ErrInvalidTarget
	}
	port := 0
	if parsed.Port() != "" {
		port, err = strconv.Atoi(parsed.Port())
		if err != nil {
			return mediaproxy.Target{}, ErrInvalidTarget
		}
	} else if parsed.Scheme == "https" {
		port = 443
	} else {
		port = 80
	}
	basePath, err := normalizeSafeBasePath(parsed.EscapedPath())
	if err != nil {
		return mediaproxy.Target{}, ErrInvalidTarget
	}
	target, err := mediaproxy.ParseTarget(parsed.Scheme, parsed.Hostname(), port, basePath)
	if err != nil {
		return mediaproxy.Target{}, ErrInvalidTarget
	}
	return target, nil
}
