package failover

import (
	"regexp"
	"strings"
)

var uuidLikeRE = regexp.MustCompile(`(?i)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

func RedactReason(reason string) string {
	value := strings.TrimSpace(reason)
	if value == "" {
		return "unspecified"
	}
	for _, forbidden := range []string{"authorization", "cookie", "api_key", "token", "password", "session", "secret", "uuid"} {
		if strings.Contains(strings.ToLower(value), forbidden) {
			return "redacted"
		}
	}
	if strings.ContainsAny(value, "?&=/\\") || strings.Contains(value, "://") || uuidLikeRE.MatchString(value) {
		return "redacted"
	}
	if len(value) > 80 {
		return value[:80]
	}
	return value
}
