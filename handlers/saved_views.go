package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/store"
)

// SavedViewHandler serves saved view CRUD and reorder endpoints.
type SavedViewHandler struct {
	store  store.Store
	userID func(r *http.Request) int64
}

// NewSavedViewHandler creates a new SavedViewHandler.
func NewSavedViewHandler(s store.Store, userIDFunc func(r *http.Request) int64) *SavedViewHandler {
	return &SavedViewHandler{
		store:  s,
		userID: userIDFunc,
	}
}

// List handles GET /api/saved-views
func (h *SavedViewHandler) List(w http.ResponseWriter, r *http.Request) {
	uid := h.userID(r)
	views, err := h.store.ListSavedViews(r.Context(), uid, true)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"saved_views": views})
}

// Create handles POST /api/saved-views
func (h *SavedViewHandler) Create(w http.ResponseWriter, r *http.Request) {
	uid := h.userID(r)

	var in struct {
		Name     string          `json:"name"`
		Filters  json.RawMessage `json:"filters"`
		IsShared bool            `json:"is_shared"`
		Icon     string          `json:"icon"`
		Color    string          `json:"color"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if in.Name == "" {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "name is required"})
		return
	}

	sv := &models.SavedView{
		Name:     in.Name,
		Filters:  in.Filters,
		UserID:   uid,
		IsShared: in.IsShared,
		Icon:     in.Icon,
		Color:    in.Color,
	}

	if err := h.store.CreateSavedView(r.Context(), sv); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"saved_view": sv})
}

// Show handles GET /api/saved-views/{id}
func (h *SavedViewHandler) Show(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}

	sv, err := h.store.GetSavedView(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "saved view not found"})
		return
	}

	// Check access: must be owner or shared
	uid := h.userID(r)
	if sv.UserID != uid && !sv.IsShared {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "access denied"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"saved_view": sv})
}

// Update handles PATCH /api/saved-views/{id}
func (h *SavedViewHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}

	sv, err := h.store.GetSavedView(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "saved view not found"})
		return
	}

	uid := h.userID(r)
	if sv.UserID != uid {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "only the owner can update this view"})
		return
	}

	var updates map[string]any
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if v, ok := updates["name"].(string); ok {
		sv.Name = v
	}
	if v, ok := updates["filters"]; ok {
		raw, _ := json.Marshal(v)
		sv.Filters = raw
	}
	if v, ok := updates["is_shared"].(bool); ok {
		sv.IsShared = v
	}
	if v, ok := updates["icon"].(string); ok {
		sv.Icon = v
	}
	if v, ok := updates["color"].(string); ok {
		sv.Color = v
	}

	if err := h.store.UpdateSavedView(r.Context(), sv); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"saved_view": sv})
}

// Delete handles DELETE /api/saved-views/{id}
func (h *SavedViewHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}

	sv, err := h.store.GetSavedView(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "saved view not found"})
		return
	}

	uid := h.userID(r)
	if sv.UserID != uid {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "only the owner can delete this view"})
		return
	}

	if err := h.store.DeleteSavedView(r.Context(), id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// Reorder handles POST /api/saved-views/reorder
func (h *SavedViewHandler) Reorder(w http.ResponseWriter, r *http.Request) {
	uid := h.userID(r)

	var in struct {
		IDs []int64 `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if len(in.IDs) == 0 {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "ids is required"})
		return
	}

	if err := h.store.ReorderSavedViews(r.Context(), uid, in.IDs); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "reordered"})
}
