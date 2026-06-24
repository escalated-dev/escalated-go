package models

import "time"

// Side conversation channels and statuses.
const (
	SideConversationChannelInternal = "internal"
	SideConversationChannelEmail    = "email"
	SideConversationStatusOpen      = "open"
	SideConversationStatusClosed    = "closed"
)

// SideConversationChannels is the set of valid channels.
var SideConversationChannels = []string{SideConversationChannelInternal, SideConversationChannelEmail}

// SideConversation is a thread attached to a ticket — an internal-note
// thread or an external email side-channel. Mirrors the Laravel
// SideConversation model.
type SideConversation struct {
	ID        int64                   `json:"id"`
	TicketID  int64                   `json:"ticket_id"`
	Subject   string                  `json:"subject"`
	Channel   string                  `json:"channel"`
	Status    string                  `json:"status"`
	CreatedBy *UserID                 `json:"created_by,omitempty"`
	CreatedAt time.Time               `json:"created_at"`
	UpdatedAt time.Time               `json:"updated_at"`
	Replies   []SideConversationReply `json:"replies"`
}

// SideConversationReply is a reply within a SideConversation. Mirrors the
// Laravel SideConversationReply model.
type SideConversationReply struct {
	ID                 int64     `json:"id"`
	SideConversationID int64     `json:"side_conversation_id"`
	Body               string    `json:"body"`
	AuthorID           *UserID   `json:"author_id,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// ValidSideConversationChannel reports whether c is a recognised channel.
func ValidSideConversationChannel(c string) bool {
	for _, ch := range SideConversationChannels {
		if ch == c {
			return true
		}
	}
	return false
}
