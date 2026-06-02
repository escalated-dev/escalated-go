package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/services"
)

// TicketSubjectHandler serves ticket subject attach/detach endpoints.
type TicketSubjectHandler struct {
	subjects *services.TicketSubjectService
	tickets  *services.TicketService
}

// NewTicketSubjectHandler creates a TicketSubjectHandler.
func NewTicketSubjectHandler(ss *services.TicketSubjectService, ts *services.TicketService) *TicketSubjectHandler {
	return &TicketSubjectHandler{subjects: ss, tickets: ts}
}

// AttachSubject handles POST /api/tickets/{id}/subjects
func (h *TicketSubjectHandler) AttachSubject(w http.ResponseWriter, r *http.Request) {
	ticketID, err := idFromPath(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid ticket id"})
		return
	}

	t, err := h.tickets.Get(r.Context(), ticketID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if t == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "ticket not found"})
		return
	}

	var in struct {
		Type string  `json:"type"`
		ID   string  `json:"id"`
		Role *string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if in.Type == "" {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"error":  "validation failed",
			"fields": map[string]string{"type": "type is required"},
		})
		return
	}
	if in.ID == "" {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"error":  "validation failed",
			"fields": map[string]string{"id": "id is required"},
		})
		return
	}

	link, err := h.subjects.AttachSubject(r.Context(), ticketID, in.Type, in.ID, in.Role, nil, true)
	if err != nil {
		if errors.Is(err, services.ErrTicketSubjectAPIDisabled) ||
			errors.Is(err, services.ErrTicketSubjectTypeNotAllowed) {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"error":  "validation failed",
				"fields": map[string]string{"type": err.Error()},
			})
			return
		}
		if errors.Is(err, services.ErrTicketSubjectNotFound) {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"error":  "validation failed",
				"fields": map[string]string{"id": err.Error()},
			})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	views, _ := h.subjects.ListViews(r.Context(), ticketID)
	writeJSON(w, http.StatusOK, map[string]any{
		"subject":  link,
		"subjects": views,
	})
}

// DetachSubject handles DELETE /api/tickets/{id}/subjects/{subject}
func (h *TicketSubjectHandler) DetachSubject(w http.ResponseWriter, r *http.Request) {
	ticketID, err := idFromPath(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid ticket id"})
		return
	}

	linkID, err := strconv.ParseInt(r.PathValue("subject"), 10, 64)
	if err != nil || linkID <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid subject id"})
		return
	}

	t, err := h.tickets.Get(r.Context(), ticketID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if t == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "ticket not found"})
		return
	}

	ok, err := h.subjects.DetachSubject(r.Context(), ticketID, linkID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "subject not found"})
		return
	}

	views, _ := h.subjects.ListViews(r.Context(), ticketID)
	writeJSON(w, http.StatusOK, map[string]any{"subjects": views})
}

// populateTicketSubjects loads and serializes subjects onto t when the service is set.
func populateTicketSubjects(ctx context.Context, h *APIHandler, t *models.Ticket) {
	if h.Subjects == nil || t == nil {
		return
	}
	views, err := h.Subjects.ListViews(ctx, t.ID)
	if err == nil {
		t.Subjects = views
	}
}
