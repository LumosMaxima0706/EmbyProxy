package admin

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"embyproxy/internal/proxyadapter"
	"embyproxy/internal/storage"
)

type managedRouteRequest struct {
	NodeName    string                     `json:"node_name"`
	Enabled     bool                       `json:"enabled"`
	Public      bool                       `json:"public"`
	DefaultLine string                     `json:"default_line"`
	Lines       []storage.ManagedRouteLine `json:"lines"`
}

type managedRouteView struct {
	Slug        string                     `json:"slug"`
	NodeName    string                     `json:"node_name"`
	Enabled     bool                       `json:"enabled"`
	Public      bool                       `json:"public"`
	DefaultLine string                     `json:"default_line"`
	Lines       []storage.ManagedRouteLine `json:"lines"`
}

func (h *Handler) handleManagedRoutesAPI(w http.ResponseWriter, r *http.Request, path string) {
	w.Header().Set("Cache-Control", "no-store")
	if h.store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "STORAGE_UNAVAILABLE"})
		return
	}
	const prefix = "/api/admin/managed-routes"
	suffix := strings.TrimPrefix(path, prefix)
	if suffix != "" && !strings.HasPrefix(suffix, "/") {
		http.NotFound(w, r)
		return
	}
	slug := strings.Trim(strings.TrimPrefix(suffix, "/"), "/")
	if strings.Contains(slug, "/") {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "INVALID_ROUTE_PATH"})
		return
	}

	switch {
	case r.Method == http.MethodGet && slug == "":
		routes, err := h.store.ListManagedRoutes(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "MANAGED_ROUTE_LIST_FAILED"})
			return
		}
		views := make([]managedRouteView, 0, len(routes))
		for _, route := range routes {
			view, err := h.managedRouteView(r, route)
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "MANAGED_ROUTE_LIST_FAILED"})
				return
			}
			views = append(views, view)
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "routes": views})
	case r.Method == http.MethodGet && slug != "":
		if err := proxyadapter.ValidateManagedRouteSlug(slug); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "INVALID_ROUTE_SLUG"})
			return
		}
		route, err := h.store.GetManagedRoute(r.Context(), slug)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "MANAGED_ROUTE_GET_FAILED"})
			return
		}
		if route == nil {
			writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "MANAGED_ROUTE_NOT_FOUND"})
			return
		}
		view, err := h.managedRouteView(r, *route)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "MANAGED_ROUTE_GET_FAILED"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "route": view})
	case r.Method == http.MethodPut && slug != "":
		if err := proxyadapter.ValidateManagedRouteSlug(slug); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "INVALID_ROUTE_SLUG"})
			return
		}
		var body managedRouteRequest
		if !decodeManagedRouteJSON(w, r, &body) {
			return
		}
		if err := validateManagedRouteRequest(body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		route := storage.ManagedRoute{
			Slug: slug, NodeName: strings.TrimSpace(body.NodeName), Enabled: body.Enabled,
			Public: body.Public, DefaultLine: strings.TrimSpace(body.DefaultLine),
		}
		for i := range body.Lines {
			body.Lines[i].RouteSlug = slug
			body.Lines[i].LineSlug = strings.TrimSpace(body.Lines[i].LineSlug)
			body.Lines[i].Target = strings.TrimSpace(body.Lines[i].Target)
		}
		if err := h.store.SaveManagedRoute(r.Context(), route, body.Lines); err != nil {
			writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": "MANAGED_ROUTE_SAVE_FAILED"})
			return
		}
		view := managedRouteView{Slug: route.Slug, NodeName: route.NodeName, Enabled: route.Enabled, Public: route.Public, DefaultLine: route.DefaultLine, Lines: body.Lines}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "route": view})
	case r.Method == http.MethodDelete && slug != "":
		if err := proxyadapter.ValidateManagedRouteSlug(slug); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "INVALID_ROUTE_SLUG"})
			return
		}
		route, err := h.store.GetManagedRoute(r.Context(), slug)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "MANAGED_ROUTE_GET_FAILED"})
			return
		}
		if route == nil {
			writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "MANAGED_ROUTE_NOT_FOUND"})
			return
		}
		if err := h.store.DeleteManagedRoute(r.Context(), slug); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "MANAGED_ROUTE_DELETE_FAILED"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false, "error": "METHOD_NOT_ALLOWED"})
	}
}

func (h *Handler) managedRouteView(r *http.Request, route storage.ManagedRoute) (managedRouteView, error) {
	lines, err := h.store.ListManagedRouteLines(r.Context(), route.Slug)
	if err != nil {
		return managedRouteView{}, err
	}
	return managedRouteView{Slug: route.Slug, NodeName: route.NodeName, Enabled: route.Enabled, Public: route.Public, DefaultLine: route.DefaultLine, Lines: lines}, nil
}

func decodeManagedRouteJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "INVALID_MANAGED_ROUTE_JSON"})
		return false
	}
	return true
}

func validateManagedRouteRequest(body managedRouteRequest) error {
	if err := proxyadapter.ValidateManagedRouteNode(strings.TrimSpace(body.NodeName)); err != nil {
		return errors.New("INVALID_NODE_NAME")
	}
	if len(body.Lines) > 64 {
		return errors.New("TOO_MANY_ROUTE_LINES")
	}
	seen := make(map[string]struct{}, len(body.Lines))
	enabledLines := 0
	defaultFound := body.DefaultLine == ""
	for _, line := range body.Lines {
		lineSlug := strings.TrimSpace(line.LineSlug)
		if lineSlug == "" || len(lineSlug) > 32 || strings.ContainsAny(lineSlug, "/\\?#") {
			return errors.New("INVALID_LINE_SLUG")
		}
		if _, exists := seen[lineSlug]; exists {
			return errors.New("DUPLICATE_LINE_SLUG")
		}
		seen[lineSlug] = struct{}{}
		if lineSlug == strings.TrimSpace(body.DefaultLine) {
			defaultFound = true
		}
		if line.Enabled {
			enabledLines++
		}
		if err := proxyadapter.ValidateManagedTarget(strings.TrimSpace(line.Target)); err != nil {
			return errors.New("INVALID_LINE_TARGET")
		}
	}
	if body.DefaultLine != "" && !defaultFound {
		return errors.New("DEFAULT_LINE_NOT_FOUND")
	}
	if body.Enabled && body.Public && enabledLines == 0 {
		return errors.New("PUBLIC_ROUTE_REQUIRES_ENABLED_LINE")
	}
	return nil
}
