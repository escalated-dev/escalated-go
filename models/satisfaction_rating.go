package models

import "time"

// SatisfactionRating is a CSAT rating left on a ticket. One per ticket,
// submittable only once the ticket is resolved or closed. Mirrors the
// Laravel SatisfactionRating model. RatedBy* is an optional polymorphic
// reference to the rater (absent for guest/anonymous submissions).
type SatisfactionRating struct {
	ID          int64     `json:"id"`
	TicketID    int64     `json:"ticket_id"`
	Rating      int       `json:"rating"`
	Comment     *string   `json:"comment,omitempty"`
	RatedByType *string   `json:"rated_by_type,omitempty"`
	RatedByID   *UserID   `json:"rated_by_id,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}
