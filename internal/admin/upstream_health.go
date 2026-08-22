package admin

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"embyproxy/internal/storage"
)

// checkSavedUpstreamLinesWithClient is the bounded, query-free health probe
// used by the publication regression tests and admin status path. It reports
// only classified status, never credentials or response bodies.
func checkSavedUpstreamLinesWithClient(ctx context.Context, node storage.Node, client *http.Client) []map[string]any {
	targets := storage.SplitTargets(node.Target)
	results := make([]map[string]any, len(targets))
	var wg sync.WaitGroup
	for index, target := range targets {
		index, target := index, target
		wg.Add(1)
		go func() {
			defer wg.Done()
			result := map[string]any{"line_id": publicationPlanLineID(index), "priority": index + 1, "status": 0, "health": "unreachable"}
			base, err := url.Parse(target)
			if err != nil || base.Scheme != "https" || base.Hostname() == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
				result["health"], result["error"] = "invalid", "upstream_invalid"
				results[index] = result
				return
			}
			base.Path = strings.TrimRight(base.Path, "/") + "/emby/System/Info/Public"
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base.String(), nil)
			req.Header.Set("User-Agent", "emby-proxy-line-check/1.0")
			started := time.Now()
			response, requestErr := client.Do(req)
			result["ms"] = time.Since(started).Milliseconds()
			if requestErr != nil {
				if errors.Is(requestErr, context.DeadlineExceeded) || strings.Contains(strings.ToLower(requestErr.Error()), "timeout") {
					result["health"], result["error"] = "timeout", "upstream_timeout"
				} else {
					result["error"] = "upstream_unreachable"
				}
				results[index] = result
				return
			}
			_ = response.Body.Close()
			result["status"] = response.StatusCode
			switch {
			case response.StatusCode >= 200 && response.StatusCode < 400:
				result["health"] = "reachable"
			case response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden:
				result["health"] = "unauthorized"
			default:
				result["health"] = "unhealthy"
			}
			results[index] = result
		}()
	}
	wg.Wait()
	return results
}
