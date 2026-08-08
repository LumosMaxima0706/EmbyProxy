package mediaproxy

import (
	"net/url"
	"regexp"
	"strings"
)

var sensitiveUUID = regexp.MustCompile(`(?i)[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}`)

func sanitizeURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "<invalid-url>"
	}
	queryPresent := u.RawQuery != ""
	u.RawQuery = ""
	u.Fragment = ""
	host := u.Host
	if host == "" {
		host = "<relative>"
	}
	result := u.Scheme + "://" + host + u.EscapedPath()
	if u.Scheme == "" {
		result = host + u.EscapedPath()
	}
	if queryPresent {
		result += "?[redacted]"
	}
	return sensitiveUUID.ReplaceAllString(result, "[uuid-redacted]")
}

func sanitizeHeaderValue(name, value string) string {
	lower := strings.ToLower(name)
	if lower == "authorization" || lower == "cookie" || strings.Contains(lower, "token") || strings.Contains(lower, "session") || strings.Contains(lower, "secret") || strings.Contains(lower, "password") || strings.Contains(lower, "api_key") || strings.Contains(lower, "api-key") || lower == "apikey" {
		return "[redacted]"
	}
	return value
}
