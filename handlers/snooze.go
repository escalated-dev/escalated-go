package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/escalated-dev/escalated-go/services"
)

// SnoozeHandler serves the ticket snooze endpoints.
type SnoozeHandler struct {
	snooze *services.SnoozeService
	userID func(r *http.Request) int64
}

// NewSnoozeHandler creates a new SnoozeHandler.
func NewSnoozeHandler(ss *services.SnoozeService, userIDFunc func(r *http.Request) int64) *SnoozeHandler {
	return &SnoozeHandler{
		snooze: ss,
		userID: userIDFunc,
	}
}

// Snooze handles POST /api/tickets/{id}/snooze
func (h *SnoozeHandler) Snooze(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid ticket id"})
		return
	}

	var in struct {
		Until string `json:"until"` // RFC3339 timestamp
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if in.Until == "" {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "until is required"})
		return
	}

	until, err := time.Parse(time.RFC3339, in.Until)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "until must be a valid RFC3339 timestamp"})
		return
	}

	uid := h.userID(r)
	var snoozedBy *int64
	if uid > 0 {
		snoozedBy = &uid
	}

	if err := h.snooze.SnoozeTicket(r.Context(), id, until, snoozedBy); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "snoozed"})
}

// Unsnooze handles POST /api/tickets/{id}/unsnooze
func (h *SnoozeHandler) Unsnooze(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid ticket id"})
		return
	}

	uid := h.userID(r)
	var causerID *int64
	if uid > 0 {
		causerID = &uid
	}

	if err := h.snooze.UnsnoozeTicket(r.Context(), id, causerID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "unsnoozed"})
}
