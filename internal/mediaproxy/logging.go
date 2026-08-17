package mediaproxy

import (
	"fmt"
	"net/http"
	"strings"
)

func (e *Executor) logEvent(event string, fields map[string]any) {
	if e == nil || e.log == nil {
		return
	}
	e.log(event, fields)
}

func requestLogFields(r *http.Request, target Target) map[string]any {
	fields := map[string]any{
		"method":        r.Method,
		"path_segments": pathSegmentCount(r.URL.Path),
		"scheme":        target.Scheme,
		"port":          fmt.Sprint(target.Port),
	}
	return fields
}

func pathSegmentCount(value string) int {
	trimmed := strings.Trim(value, "/")
	if trimmed == "" {
		return 0
	}
	return len(strings.Split(trimmed, "/"))
}
