package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/escalated-dev/escalated-go/models"
)

// SatisfactionHandler accepts CSAT ratings on tickets. A ticket can be
// rated exactly once, and only once it is resolved or closed. Mirrors the
// Laravel SatisfactionRatingController's customer flow.
//
// Guest (by-token) submission and the admin CSAT settings surface are
// follow-ups; the Go backend has no guest-ticket route group yet.
type SatisfactionHandler struct {
	DB *sql.DB
}

// NewSatisfactionHandler constructs the handler.
func NewSatisfactionHandler(db *sql.DB) *SatisfactionHandler {
	return &SatisfactionHandler{DB: db}
}

// Rate handles POST /tickets/{id}/rate.
func (h *SatisfactionHandler) Rate(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	h.rateByTicketID(w, r, id)
}

// GuestRate handles POST /api/guest/tickets/{token}/rate — a guest rates
// their own resolved/closed ticket using its guest token.
func (h *SatisfactionHandler) GuestRate(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	if token == "" {
		token = chi.URLParam(r, "token")
	}
	if token == "" {
		http.Error(w, "token is required", http.StatusBadRequest)
		return
	}

	var id int64
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT id FROM escalated_tickets WHERE guest_token = ?`, token).Scan(&id)
	if err == sql.ErrNoRows {
		http.Error(w, "ticket not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.rateByTicketID(w, r, id)
}

func (h *SatisfactionHandler) rateByTicketID(w http.ResponseWriter, r *http.Request, id int64) {
	var in struct {
		Rating  int     `json:"rating"`
		Comment *string `json:"comment"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || !ratingValid(in.Rating) {
		http.Error(w, "rating must be an integer between 1 and 5", http.StatusBadRequest)
		return
	}

	var status int
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT status FROM escalated_tickets WHERE id = ?`, id).Scan(&status)
	if err == sql.ErrNoRows {
		http.Error(w, "ticket not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !ticketRateable(status) {
		http.Error(w, "only resolved or closed tickets can be rated", http.StatusUnprocessableEntity)
		return
	}

	var existing int
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(1) FROM escalated_satisfaction_ratings WHERE ticket_id = ?`, id).Scan(&existing); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if existing > 0 {
		http.Error(w, "this ticket has already been rated", http.StatusUnprocessableEntity)
		return
	}

	if _, err := h.DB.ExecContext(r.Context(),
		`INSERT INTO escalated_satisfaction_ratings (ticket_id, rating, comment, created_at)
		 VALUES (?, ?, ?, ?)`,
		id, in.Rating, in.Comment, time.Now(),
	); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"ok": true})
}

// ratingValid reports whether a rating is within the 1-5 CSAT scale.
func ratingValid(rating int) bool {
	return rating >= 1 && rating <= 5
}

// ticketRateable reports whether a ticket in the given status may be rated
// (resolved or closed only).
func ticketRateable(status int) bool {
	return status == models.StatusResolved || status == models.StatusClosed
}
