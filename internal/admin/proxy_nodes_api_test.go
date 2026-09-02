package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"embyproxy/internal/config"
)

func TestProxyNodeAPICreatesOneTimeEnrollmentAndHeartbeat(t *testing.T) {
	h := newAuthTestHandler(t, config.Config{AdminToken: "strong-admin-token"})
	login := serveAdminJSON(t, h, http.MethodPost, "/admin/auth/login", map[string]any{"token": "strong-admin-token"}, nil)
	cookie := login.Result().Cookies()[0]
	created := serveAdminJSON(t, h, http.MethodPost, "/api/admin/proxy-nodes", map[string]any{"name": "edge-test", "public_address": "edge.example", "quota_bytes": 1000, "reset_day": 1, "reset_timezone": "Asia/Shanghai"}, cookie)
	if created.Code != http.StatusCreated || !strings.Contains(created.Body.String(), `"install_command"`) {
		t.Fatalf("created=%d %s", created.Code, created.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(created.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	enrollment := body["enrollment"].(map[string]any)
	command := body["install_command"].(string)
	commandURL := ""
	for _, field := range strings.Fields(command) {
		if strings.HasPrefix(field, "https://") {
			commandURL = field
			break
		}
	}
	if commandURL == "" {
		t.Fatal("enrollment URL missing")
	}
	parts := strings.Split(commandURL, "/")
	token := parts[len(parts)-1]
	id := enrollment["id"].(string)
	enroll := serveAdminJSON(t, h, http.MethodPost, "/api/edge/enroll/"+id+"/"+token, map[string]any{"version": "v1", "commit": "abc"}, nil)
	if enroll.Code != http.StatusOK {
		t.Fatalf("enroll=%d %s", enroll.Code, enroll.Body.String())
	}
	var enrolled map[string]any
	_ = json.Unmarshal(enroll.Body.Bytes(), &enrolled)
	beat := serveAdminJSON(t, h, http.MethodPost, "/api/edge/heartbeat/"+enrolled["node_id"].(string), map[string]any{"credential": enrolled["credential"], "version": "v1", "commit": "abc", "state": "healthy", "playbackHealthy": true, "configSynced": true}, nil)
	if beat.Code != http.StatusOK {
		t.Fatalf("heartbeat=%d %s", beat.Code, beat.Body.String())
	}
	list, err := h.store.ListProxyNodes(context.Background())
	if err != nil || len(list) != 1 || !list[0].PlaybackHealthy {
		t.Fatalf("list=%+v err=%v", list, err)
	}
}
