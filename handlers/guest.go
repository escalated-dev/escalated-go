package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/services"
	"github.com/escalated-dev/escalated-go/store"
)

// guestTokenLookup is the store capability the guest handler needs beyond the
// base Store interface. The concrete Postgres/SQLite stores implement it.
type guestTokenLookup interface {
	GetTicketByGuestToken(ctx context.Context, token string) (*models.Ticket, error)
}

// GuestTicketHandler serves anonymous (guest) ticket submission, reading and
// replies under /api/guest, consumed by the Flutter app and integrations.
//
// Creating a ticket needs an email code (POST /api/guest/verification with
// purpose "ticket"); the response carries an expiring grant
// (guest_access_token) that opens the ticket. See docs/guest-access.md.
type GuestTicketHandler struct {
	store   store.Store
	tickets *services.TicketService

	// Access checks codes and grants. The routers set it; without it guest
	// creation answers 503.
	Access *services.GuestAccess
}

// NewGuestTicketHandler creates a GuestTicketHandler.
func NewGuestTicketHandler(s store.Store, ts *services.TicketService) *GuestTicketHandler {
	return &GuestTicketHandler{store: s, tickets: ts}
}

// Create handles POST /api/guest/tickets.
func (h *GuestTicketHandler) Create(w http.ResponseWriter, r *http.Request) {
	setGuestPrivacyHeaders(w)
	var in struct {
		Name        string `json:"name"`
		Email       string `json:"email"`
		GuestName   string `json:"guest_name"`
		GuestEmail  string `json:"guest_email"`
		Subject     string `json:"subject"`
		Description string `json:"description"`
		Priority    int    `json:"priority"`
		guestProof
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body", "message": "Invalid request body."})
		return
	}
	if in.Email == "" {
		in.Email = in.GuestEmail
	}
	if in.Name == "" {
		in.Name = in.GuestName
	}
	errs := in.validate()
	if !services.ValidGuestEmail(in.Email) {
		errs["email"] = []string{"Enter a valid email address."}
	}
	if strings.TrimSpace(in.Subject) == "" {
		errs["subject"] = []string{"The subject field is required."}
	}
	if strings.TrimSpace(in.Description) == "" {
		errs["description"] = []string{"The description field is required."}
	}
	if len(errs) > 0 {
		writeGuestValidation(w, errs)
		return
	}
	if h.Access == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "guest access is not configured", "message": "Guest tickets are not available."})
		return
	}

	input := services.CreateTicketInput{
		Subject:     in.Subject,
		Description: in.Description,
		GuestName:   &in.Name,
	}
	if _, ok := models.PriorityName[in.Priority]; ok && in.Priority > 0 {
		input.Priority = in.Priority
	}

	t, token, expires, err := createVerifiedGuestTicket(r.Context(), h.Access, h.tickets, in.guestProof, in.Email, input)
	if !writeGuestAccessError(w, err) {
		return
	}

	view := guestTicketView(t)
	// guest_token keeps its place for older clients; it now holds the grant.
	view["guest_token"] = token
	view["guest_access_token"] = token
	view["guest_access_expires_at"] = expires.Format(time.RFC3339)
	writeJSON(w, http.StatusCreated, map[string]any{"data": view, "message": "Ticket created."})
}

// resolve opens the ticket a guest token names: a grant, or a legacy token
// while those are accepted.
func (h *GuestTicketHandler) resolve(r *http.Request) (*models.Ticket, bool) {
	token := pathParam(r, "token")
	if token == "" {
		return nil, false
	}
	if h.Access == nil {
		lookup, ok := h.store.(guestTokenLookup)
		if !ok {
			return nil, false
		}
		t, err := lookup.GetTicketByGuestToken(r.Context(), token)
		return t, err == nil && t != nil
	}
	id, err := h.Access.ResolveTicketID(r.Context(), token)
	if err != nil {
		return nil, false
	}
	t, err := h.store.GetTicket(r.Context(), id)
	return t, err == nil && t != nil
}

// Show handles GET /api/guest/tickets/{token}: the ticket and its public
// replies.
func (h *GuestTicketHandler) Show(w http.ResponseWriter, r *http.Request) {
	setGuestPrivacyHeaders(w)
	t, ok := h.resolve(r)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "ticket not found", "message": "Ticket not found."})
		return
	}
	view := guestTicketView(t)
	view["replies"] = guestReplies(r.Context(), h.store, t.ID)
	writeJSON(w, http.StatusOK, map[string]any{"data": view})
}

// Reply handles POST /api/guest/tickets/{token}/replies: {body, email}. The
// email must be the ticket's guest email; closed tickets take no replies.
func (h *GuestTicketHandler) Reply(w http.ResponseWriter, r *http.Request) {
	setGuestPrivacyHeaders(w)
	t, ok := h.resolve(r)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "ticket not found", "message": "Ticket not found."})
		return
	}
	var in struct {
		Body  string `json:"body"`
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body", "message": "Invalid request body."})
		return
	}
	errs := map[string][]string{}
	if strings.TrimSpace(in.Body) == "" || len(in.Body) > 65535 {
		errs["body"] = []string{"The body field is required."}
	}
	if !services.ValidGuestEmail(in.Email) {
		errs["email"] = []string{"Enter a valid email address."}
	}
	if len(errs) > 0 {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"message": "The given data was invalid.", "errors": errs})
		return
	}
	if t.Status == models.StatusClosed {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": "This ticket is closed."})
		return
	}
	if t.GuestEmail == nil || services.NormalizeGuestEmail(*t.GuestEmail) != services.NormalizeGuestEmail(in.Email) {
		writeJSON(w, http.StatusForbidden, map[string]string{"message": "The provided email does not match this ticket."})
		return
	}
	if _, err := h.tickets.AddReply(r.Context(), t.ID, in.Body, nil, nil, false); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "Could not send the reply."})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"message": "Reply sent."})
}

// guestReplies lists a ticket's public replies the way a guest sees them:
// body, whether an agent wrote it, and when. No author ids.
func guestReplies(ctx context.Context, s store.Store, ticketID int64) []map[string]any {
	internal := false
	replies, _ := s.ListReplies(ctx, models.ReplyFilters{TicketID: ticketID, Internal: &internal})
	out := []map[string]any{}
	for _, rp := range replies {
		if rp == nil || rp.IsInternal {
			continue
		}
		author := "You"
		if rp.AuthorType != nil {
			author = "Support"
		}
		out = append(out, map[string]any{
			"body":       rp.Body,
			"author":     author,
			"is_agent":   rp.AuthorType != nil,
			"created_at": rp.CreatedAt,
		})
	}
	return out
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
