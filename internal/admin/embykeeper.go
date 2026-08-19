package admin

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"regexp"
	"time"
)

const embykeeperStatusMaxBytes = 64 * 1024

var embykeeperErrorCodePattern = regexp.MustCompile(`^[A-Z0-9_.:-]{0,128}$`)

type embykeeperStatus struct {
	LastSuccess          string `json:"last_success"`
	NextRun              string `json:"next_run"`
	LastError            string `json:"last_error"`
	EnabledProfilesCount int    `json:"enabled_profiles_count"`
	FailedProfilesCount  int    `json:"failed_profiles_count"`
}

type embykeeperIntegrationView struct {
	Enabled      bool              `json:"enabled"`
	DisplayName  string            `json:"display_name"`
	ExternalURL  string            `json:"external_url,omitempty"`
	StatusState  string            `json:"status_state"`
	StatusReason string            `json:"status_reason,omitempty"`
	Status       *embykeeperStatus `json:"status,omitempty"`
}

func (h *Handler) handleEmbykeeperAPI(w http.ResponseWriter, r *http.Request, path string) {
	w.Header().Set("Cache-Control", "no-store")
	switch {
	case r.Method == http.MethodGet && path == "/api/admin/embykeeper":
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "integration": h.embykeeperIntegration()})
	case r.Method == http.MethodGet && path == "/api/admin/embykeeper/template":
		w.Header().Set("Content-Type", "application/toml; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="embykeeper-profile.example.toml"`)
		_, _ = io.WriteString(w, embykeeperPlaceholderTemplate)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false, "error": "METHOD_NOT_ALLOWED"})
	}
}

func (h *Handler) embykeeperIntegration() embykeeperIntegrationView {
	view := embykeeperIntegrationView{
		Enabled:     h.cfg.EmbykeeperIntegrationEnabled,
		DisplayName: h.cfg.EmbykeeperDisplayName,
		ExternalURL: h.cfg.EmbykeeperExternalURL,
		StatusState: "disabled",
	}
	if view.DisplayName == "" {
		view.DisplayName = "Embykeeper"
	}
	if !view.Enabled {
		return view
	}
	view.StatusState = "unavailable"
	if h.cfg.EmbykeeperStatusFile == "" {
		view.StatusReason = "status_file_not_configured"
		return view
	}
	status, reason := readEmbykeeperStatus(h.cfg.EmbykeeperStatusFile)
	if reason != "" {
		view.StatusReason = reason
		return view
	}
	view.StatusState = "available"
	view.Status = &status
	return view
}

func readEmbykeeperStatus(path string) (embykeeperStatus, string) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return embykeeperStatus{}, "status_file_missing"
	}
	if err != nil {
		return embykeeperStatus{}, "status_file_unreadable"
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return embykeeperStatus{}, "status_file_unsafe"
	}
	if info.Size() < 0 || info.Size() > embykeeperStatusMaxBytes {
		return embykeeperStatus{}, "status_file_too_large"
	}
	file, err := os.Open(path)
	if err != nil {
		return embykeeperStatus{}, "status_file_unreadable"
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	currentInfo, currentErr := os.Lstat(path)
	if err != nil || currentErr != nil || !openedInfo.Mode().IsRegular() || currentInfo.Mode()&os.ModeSymlink != 0 ||
		!os.SameFile(info, openedInfo) || !os.SameFile(openedInfo, currentInfo) {
		return embykeeperStatus{}, "status_file_unsafe"
	}
	raw, err := io.ReadAll(io.LimitReader(file, embykeeperStatusMaxBytes+1))
	if err != nil {
		return embykeeperStatus{}, "status_file_unreadable"
	}
	if len(raw) > embykeeperStatusMaxBytes {
		return embykeeperStatus{}, "status_file_too_large"
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var status embykeeperStatus
	if err := decoder.Decode(&status); err != nil {
		return embykeeperStatus{}, "status_file_malformed"
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return embykeeperStatus{}, "status_file_malformed"
	}
	if !validEmbykeeperTimestamp(status.LastSuccess) || !validEmbykeeperTimestamp(status.NextRun) ||
		status.EnabledProfilesCount < 0 || status.EnabledProfilesCount > 10000 ||
		status.FailedProfilesCount < 0 || status.FailedProfilesCount > status.EnabledProfilesCount ||
		!embykeeperErrorCodePattern.MatchString(status.LastError) {
		return embykeeperStatus{}, "status_file_invalid"
	}
	return status, ""
}

func validEmbykeeperTimestamp(value string) bool {
	if value == "" {
		return true
	}
	_, err := time.Parse(time.RFC3339, value)
	return err == nil
}

const embykeeperPlaceholderTemplate = `# Placeholder-only profile template. Fill secrets in the standalone Embykeeper secrets directory.
[[emby.account]]
url = "https://emby.example.invalid"
username = "REPLACE_IN_LOCAL_SECRET_FILE"
password = "REPLACE_IN_LOCAL_SECRET_FILE"
enabled = false

[emby]
time_range = "03:00"
interval_days = 7
concurrency = 1
`
