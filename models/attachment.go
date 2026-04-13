package models

import "time"

// Attachment represents a file attached to a ticket or reply.
type Attachment struct {
	ID               int64  `json:"id"`
	TicketID         int64  `json:"ticket_id"`
	ReplyID          *int64 `json:"reply_id,omitempty"`
	OriginalFilename string `json:"original_filename"`
	MimeType         string `json:"mime_type"`
	Size             int64  `json:"size"`
	StoragePath      string `json:"-"`

	// URL is computed at read time — not stored in the database.
	URL string `json:"url"`

	CreatedAt time.Time `json:"created_at"`
}
