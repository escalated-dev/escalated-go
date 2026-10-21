package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/services"
	"github.com/escalated-dev/escalated-go/store"
)

// TicketMergeHandler serves the admin ticket merge (Laravel
// TicketMergeController): a search for merge targets, and the merge itself.
type TicketMergeHandler struct {
	Merges *services.TicketMergeService
	Store  store.Store
	UserID func(r *http.Request) models.UserID
}

// Search handles GET /admin/tickets/merge-search?q= -> {tickets: [{id,
// reference, subject, status, created_at}]}, at most 10 unmerged tickets.
func (h *TicketMergeHandler) Search(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"message": "The q field is required.",
			"errors":  map[string][]string{"q": {"The q field is required."}},
		})
		return
	}
	tickets, err := h.Merges.SearchTargets(r.Context(), q)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "Search failed."})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tickets": tickets})
}

// Merge handles POST /admin/tickets/{ticket}/merge with {target_reference}:
// {ticket} (an id or a reference) is merged into the target and closed.
func (h *TicketMergeHandler) Merge(w http.ResponseWriter, r *http.Request) {
	var in struct {
		TargetReference string `json:"target_reference"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || strings.TrimSpace(in.TargetReference) == "" {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"message": "The target reference field is required.",
			"errors":  map[string][]string{"target_reference": {"The target reference field is required."}},
		})
		return
	}

	key := pathParam(r, "ticket")
	var sourceID int64
	if id, err := strconv.ParseInt(key, 10, 64); err == nil {
		sourceID = id
	} else if t, err := h.Store.GetTicketByReference(r.Context(), key); err == nil && t != nil {
		sourceID = t.ID
	}
	if sourceID == 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Ticket not found."})
		return
	}

	var by *models.UserID
	if h.UserID != nil {
		if uid := h.UserID(r); !uid.Empty() {
			by = &uid
		}
	}
	_, ref, err := h.Merges.Merge(r.Context(), sourceID, in.TargetReference, by)
	switch {
	case errors.Is(err, services.ErrMergeIntoSelf):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": "Cannot merge a ticket into itself."})
	case errors.Is(err, services.ErrMergeTicketNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Ticket not found."})
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "The merge failed."})
	default:
		writeJSON(w, http.StatusOK, map[string]string{"message": "Ticket merged into " + ref + ".", "target_reference": ref})
	}
}
