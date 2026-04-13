package models

import "time"

// Reply represents a response on a ticket — public reply, internal note, or system message.
type Reply struct {
	ID       int64  `json:"id"`
	TicketID int64  `json:"ticket_id"`
	Body     string `json:"body"`

	// Polymorphic author
	AuthorType *string `json:"author_type,omitempty"`
	AuthorID   *int64  `json:"author_id,omitempty"`

	IsInternal bool `json:"is_internal"`
	IsSystem   bool `json:"is_system"`
	IsPinned   bool `json:"is_pinned"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Loaded relationships (not persisted directly)
	Attachments []Attachment `json:"attachments,omitempty"`

	// Computed (populated by handlers, not persisted)
	AuthorName *string `json:"author_name,omitempty"`
}

// IsPublic returns true if this is a public (customer-visible) reply.
func (r *Reply) IsPublic() bool {
	return !r.IsInternal
}

// ReplyFilters holds query parameters for listing replies.
type ReplyFilters struct {
	TicketID   int64 `json:"ticket_id"`
	Internal   *bool `json:"internal,omitempty"`
	System     *bool `json:"system,omitempty"`
	Pinned     *bool `json:"pinned,omitempty"`
	Descending bool  `json:"descending"`
}
