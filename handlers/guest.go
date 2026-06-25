package handlers

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/services"
	"github.com/escalated-dev/escalated-go/store"
)

// guestTokenLookup is the store capability the guest handler needs beyond the
// base Store interface. The concrete Postgres/SQLite stores implement it.
type guestTokenLookup interface {
	GetTicketByGuestToken(ctx context.Context, token string) (*models.Ticket, error)
}

// GuestTicketHandler serves anonymous (guest) ticket submission and lookup
// under /api/guest, consumed by the Flutter app and integrations.
type GuestTicketHandler struct {
	store   store.Store
	tickets *services.TicketService
}

// NewGuestTicketHandler creates a GuestTicketHandler.
func NewGuestTicketHandler(s store.Store, ts *services.TicketService) *GuestTicketHandler {
	return &GuestTicketHandler{store: s, tickets: ts}
}

// Create handles POST /api/guest/tickets.
func (h *GuestTicketHandler) Create(w http.ResponseWriter, r *http.Request) {
	var in struct {
		GuestName   string `json:"guest_name"`
		GuestEmail  string `json:"guest_email"`
		Subject     string `json:"subject"`
		Description string `json:"description"`
		Priority    int    `json:"priority"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if in.GuestEmail == "" || in.Subject == "" || in.Description == "" {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "guest_email, subject, and description are required"})
		return
	}

	input := services.CreateTicketInput{
		Subject:     in.Subject,
		Description: in.Description,
		GuestName:   &in.GuestName,
		GuestEmail:  &in.GuestEmail,
	}
	if in.Priority > 0 {
		input.Priority = in.Priority
	}

	t, err := h.tickets.Create(r.Context(), input)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"data": guestTicketView(t)})
}

// Show handles GET /api/guest/tickets/{token}.
func (h *GuestTicketHandler) Show(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	if token == "" {
		token = chi.URLParam(r, "token")
	}
	if token == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "token is required"})
		return
	}

	lookup, ok := h.store.(guestTokenLookup)
	if !ok {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "guest lookup is not supported by this store"})
		return
	}

	t, err := lookup.GetTicketByGuestToken(r.Context(), token)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if t == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "ticket not found"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"data": guestTicketView(t)})
}

func guestTicketView(t *models.Ticket) map[string]any {
	return map[string]any{
		"reference":   t.Reference,
		"subject":     t.Subject,
		"description": t.Description,
		"status":      t.Status,
		"priority":    t.Priority,
		"guest_token": t.GuestToken,
		"created_at":  t.CreatedAt,
	}
}
