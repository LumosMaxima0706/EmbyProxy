package admin

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"embyproxy/internal/config"
	"embyproxy/internal/storage"
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

func TestProxyNodeBootstrapIsNoStoreAndDoesNotExposeAdminSecret(t *testing.T) {
	h := newAuthTestHandler(t, config.Config{AdminToken: "strong-admin-token", EnrollmentControllerURL: "https://admin.example"})
	login := serveAdminJSON(t, h, http.MethodPost, "/admin/auth/login", map[string]any{"token": "strong-admin-token"}, nil)
	cookie := login.Result().Cookies()[0]
	created := serveAdminJSON(t, h, http.MethodPost, "/api/admin/proxy-nodes", map[string]any{"name": "edge-bootstrap", "public_address": "edge.example", "reset_day": 1}, cookie)
	var body map[string]any
	if err := json.Unmarshal(created.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	enrollment := body["enrollment"].(map[string]any)
	command := body["install_command"].(string)
	if !strings.Contains(command, "https://admin.example/api/edge/bootstrap/") || strings.Contains(command, "strong-admin-token") {
		t.Fatalf("unsafe command: %s", command)
	}
	parts := strings.Split(strings.TrimPrefix(command, "curl --fail --silent --show-error --proto '=https' --tlsv1.2 https://admin.example/api/edge/bootstrap/"), " | sudo sh")
	if len(parts) != 2 {
		t.Fatalf("unexpected command format: %s", command)
	}
	path := "/api/edge/bootstrap/" + strings.TrimSpace(parts[0])
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("bootstrap=%d headers=%v", rec.Code, rec.Header())
	}
	if strings.Contains(rec.Body.String(), "strong-admin-token") || !strings.Contains(rec.Body.String(), "api/edge/enroll/") || !strings.Contains(rec.Body.String(), "EMBYPROXY_INSTALL_ROOT") || !strings.Contains(rec.Body.String(), "sha256sum") || !strings.Contains(rec.Body.String(), "edge agent checksum verification failed") || !strings.Contains(rec.Body.String(), `\$CREDENTIAL`) || !strings.Contains(rec.Body.String(), `\$payload`) {
		t.Fatal("bootstrap leaked secret or omitted enrollment endpoint")
	}
	_ = enrollment
}

func TestEdgeArtifactAndSnapshotRequireNodeCredential(t *testing.T) {
	artifact := filepath.Join(t.TempDir(), "edge-agent")
	payload := []byte("edge-agent-test-artifact")
	if err := os.WriteFile(artifact, payload, 0700); err != nil {
		t.Fatal(err)
	}
	h := newAuthTestHandler(t, config.Config{AdminToken: "strong-admin-token", EdgeAgentBinaryPath: artifact})
	enrollment, token, err := h.store.CreateProxyNode(context.Background(), storage.ProxyNode{Name: "edge-artifact", ResetDay: 1}, 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	node, credential, err := h.store.CompleteEnrollment(context.Background(), enrollment.ID, token, "v1", "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.store.SaveManagedRoute(context.Background(), storage.ManagedRoute{Slug: "demo", NodeName: "demo", Enabled: true, Public: true, DefaultLine: "main"}, []storage.ManagedRouteLine{{RouteSlug: "demo", LineSlug: "main", Target: "https://media.example", Enabled: true, Position: 1}}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/edge/artifact/" + node.ID + "/edge-agent", "/api/edge/config/" + node.ID} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("missing credential %s status=%d", path, rec.Code)
		}
		req.Header.Set("X-EmbyProxy-Node-Credential", "wrong")
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("wrong credential %s status=%d", path, rec.Code)
		}
	}
	artifactReq := httptest.NewRequest(http.MethodGet, "/api/edge/artifact/"+node.ID+"/edge-agent", nil)
	artifactReq.Header.Set("X-EmbyProxy-Node-Credential", credential)
	artifactRec := httptest.NewRecorder()
	h.ServeHTTP(artifactRec, artifactReq)
	sum := sha256.Sum256(payload)
	if artifactRec.Code != http.StatusOK || artifactRec.Header().Get("X-EmbyProxy-Artifact-SHA256") != fmt.Sprintf("%x", sum) || string(artifactRec.Body.Bytes()) != string(payload) {
		t.Fatalf("artifact response status=%d hash=%q body=%q", artifactRec.Code, artifactRec.Header().Get("X-EmbyProxy-Artifact-SHA256"), artifactRec.Body.String())
	}
	snapshotReq := httptest.NewRequest(http.MethodGet, "/api/edge/config/"+node.ID, nil)
	snapshotReq.Header.Set("X-EmbyProxy-Node-Credential", credential)
	snapshotRec := httptest.NewRecorder()
	h.ServeHTTP(snapshotRec, snapshotReq)
	if snapshotRec.Code != http.StatusOK || !strings.Contains(snapshotRec.Body.String(), `"slug":"demo"`) || strings.Contains(snapshotRec.Body.String(), "strong-admin-token") || strings.Contains(snapshotRec.Body.String(), credential) {
		t.Fatalf("snapshot response status=%d body=%s", snapshotRec.Code, snapshotRec.Body.String())
	}
}
