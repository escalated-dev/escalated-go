package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"unicode/utf8"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/store"
)

// SavedViewHandler serves the saved view endpoints the shared frontend calls
// (escalated.admin.saved-views.*), after escalated-laravel's
// Admin\SavedViewController: an agent sees their own views and every shared
// one; only the owner changes or deletes a view, and a view with no owner can
// be changed by anyone who can reach these routes.
type SavedViewHandler struct {
	store  store.Store
	userID func(r *http.Request) models.UserID
}

// NewSavedViewHandler creates a new SavedViewHandler.
func NewSavedViewHandler(s store.Store, userIDFunc func(r *http.Request) models.UserID) *SavedViewHandler {
	return &SavedViewHandler{
		store:  s,
		userID: userIDFunc,
	}
}

// savedViewInput is StoreSavedViewRequest: name and filters are required on
// both store and update; the rest is optional.
type savedViewInput struct {
	Name      string
	Filters   json.RawMessage
	IsShared  *bool
	IsDefault *bool
	Icon      *string
	Color     *string
	hasIcon   bool
	hasColor  bool
}

// readSavedViewInput validates the body as Laravel's StoreSavedViewRequest
// does, writing a 422 with field errors when it fails.
func readSavedViewInput(w http.ResponseWriter, r *http.Request) (*savedViewInput, bool) {
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid request body"})
		return nil, false
	}
	in := &savedViewInput{}
	errs := map[string]string{}

	isNull := func(v json.RawMessage) bool { return len(v) == 0 || string(bytes.TrimSpace(v)) == "null" }

	if v, ok := raw["name"]; !ok || isNull(v) || json.Unmarshal(v, &in.Name) != nil || in.Name == "" {
		if ok && !isNull(v) && json.Unmarshal(v, new(string)) != nil {
			errs["name"] = "The name field must be a string."
		} else {
			errs["name"] = "The name field is required."
		}
	} else if utf8.RuneCountInString(in.Name) > 255 {
		errs["name"] = "The name field must not be greater than 255 characters."
	}

	if v, ok := raw["filters"]; !ok || isNull(v) {
		errs["filters"] = "The filters field is required."
	} else if t := bytes.TrimSpace(v); len(t) == 0 || (t[0] != '{' && t[0] != '[') || !json.Valid(t) {
		errs["filters"] = "The filters field must be an array."
	} else if string(t) == "{}" || string(t) == "[]" {
		errs["filters"] = "The filters field is required."
	} else {
		in.Filters = append(json.RawMessage(nil), t...)
	}

	readBool := func(key string, dst **bool) {
		v, ok := raw[key]
		if !ok || isNull(v) {
			return
		}
		var b bool
		if err := json.Unmarshal(v, &b); err != nil {
			var n int
			if json.Unmarshal(v, &n) != nil || (n != 0 && n != 1) {
				errs[key] = "The " + key + " field must be true or false."
				return
			}
			b = n == 1
		}
		*dst = &b
	}
	readBool("is_shared", &in.IsShared)
	readBool("is_default", &in.IsDefault)

	readString := func(key string, max int, dst **string, has *bool) {
		v, ok := raw[key]
		if !ok {
			return
		}
		*has = true
		if isNull(v) {
			return
		}
		var s string
		if err := json.Unmarshal(v, &s); err != nil {
			errs[key] = "The " + key + " field must be a string."
			return
		}
		if utf8.RuneCountInString(s) > max {
			errs[key] = "The " + key + " field is too long."
			return
		}
		*dst = &s
	}
	readString("icon", 100, &in.Icon, &in.hasIcon)
	readString("color", 50, &in.Color, &in.hasColor)

	if len(errs) > 0 {
		writeValidationErrors(w, errs)
		return nil, false
	}
	return in, true
}

// writeValidationErrors writes Laravel's 422 shape: a message (the first
// error) and every field's errors.
func writeValidationErrors(w http.ResponseWriter, errs map[string]string) {
	fields := map[string][]string{}
	message := ""
	for _, key := range []string{"name", "filters", "is_shared", "is_default", "icon", "color", "ids"} {
		if msg, ok := errs[key]; ok {
			fields[key] = []string{msg}
			if message == "" {
				message = msg
			}
		}
	}
	writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"message": message, "errors": fields})
}

// visibleViews are the user's own views and every shared one, by position.
func (h *SavedViewHandler) visibleViews(r *http.Request) ([]*models.SavedView, error) {
	views, err := h.store.ListSavedViews(r.Context(), h.userID(r), true)
	if views == nil {
		views = []*models.SavedView{}
	}
	return views, err
}

// List handles GET admin/saved-views: the user's own and shared views.
func (h *SavedViewHandler) List(w http.ResponseWriter, r *http.Request) {
	views, err := h.visibleViews(r)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "could not list saved views"})
		return
	}
	writeJSON(w, http.StatusOK, views)
}

// Create handles POST admin/saved-views. The view goes after every view the
// user can see.
func (h *SavedViewHandler) Create(w http.ResponseWriter, r *http.Request) {
	in, ok := readSavedViewInput(w, r)
	if !ok {
		return
	}
	views, err := h.visibleViews(r)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "could not save the view"})
		return
	}
	maxPosition := 0
	for _, v := range views {
		maxPosition = max(maxPosition, v.Position)
	}

	sv := &models.SavedView{
		Name:     in.Name,
		Filters:  models.JSONText(in.Filters),
		UserID:   h.userID(r),
		Position: maxPosition + 1,
		Icon:     in.Icon,
		Color:    in.Color,
	}
	if in.IsShared != nil {
		sv.IsShared = *in.IsShared
	}
	if in.IsDefault != nil {
		sv.IsDefault = *in.IsDefault
	}
	if err := h.store.CreateSavedView(r.Context(), sv); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "could not save the view"})
		return
	}
	writeJSON(w, http.StatusCreated, sv)
}

// ownView loads the view the path names, writing a 404 when there is none and
// a 403 when it belongs to someone else.
func (h *SavedViewHandler) ownView(w http.ResponseWriter, r *http.Request) (*models.SavedView, bool) {
	id, err := idFromPathName(r, "id")
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not found."})
		return nil, false
	}
	sv, err := h.store.GetSavedView(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && sv == nil) {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not found."})
		return nil, false
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "could not load the view"})
		return nil, false
	}
	if !sv.UserID.Empty() && sv.UserID != h.userID(r) {
		writeJSON(w, http.StatusForbidden, map[string]string{"message": "This action is unauthorized."})
		return nil, false
	}
	return sv, true
}

// Update handles PUT/PATCH admin/saved-views/{id}.
func (h *SavedViewHandler) Update(w http.ResponseWriter, r *http.Request) {
	sv, ok := h.ownView(w, r)
	if !ok {
		return
	}
	in, ok := readSavedViewInput(w, r)
	if !ok {
		return
	}
	sv.Name = in.Name
	sv.Filters = models.JSONText(in.Filters)
	if in.IsShared != nil {
		sv.IsShared = *in.IsShared
	}
	if in.IsDefault != nil {
		sv.IsDefault = *in.IsDefault
	}
	if in.hasIcon {
		sv.Icon = in.Icon
	}
	if in.hasColor {
		sv.Color = in.Color
	}
	if err := h.store.UpdateSavedView(r.Context(), sv); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "could not save the view"})
		return
	}
	writeJSON(w, http.StatusOK, sv)
}

// Delete handles DELETE admin/saved-views/{id}.
func (h *SavedViewHandler) Delete(w http.ResponseWriter, r *http.Request) {
	sv, ok := h.ownView(w, r)
	if !ok {
		return
	}
	if err := h.store.DeleteSavedView(r.Context(), sv.ID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "could not delete the view"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "View deleted."})
}

// Reorder handles POST admin/saved-views/reorder: each id takes its index as
// its position, for the user's own and shared views.
func (h *SavedViewHandler) Reorder(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs []int64 `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeValidationErrors(w, map[string]string{"ids": "The ids field must be an array of integers."})
		return
	}
	if len(in.IDs) == 0 {
		writeValidationErrors(w, map[string]string{"ids": "The ids field is required."})
		return
	}
	if err := h.store.ReorderSavedViews(r.Context(), h.userID(r), in.IDs); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "could not reorder the views"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Views reordered."})
}
