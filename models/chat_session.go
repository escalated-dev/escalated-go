package models

import (
	"encoding/json"
	"time"
)

// Chat session status constants.
const (
	ChatStatusWaiting   = 0
	ChatStatusActive    = 1
	ChatStatusEnded     = 2
	ChatStatusAbandoned = 3
)

// ChatStatusName maps chat session status integers to human-readable names.
var ChatStatusName = map[int]string{
	ChatStatusWaiting:   "waiting",
	ChatStatusActive:    "active",
	ChatStatusEnded:     "ended",
	ChatStatusAbandoned: "abandoned",
}

// Channel constants for ticket origin.
const (
	ChannelEmail = "email"
	ChannelWeb   = "web"
	ChannelChat  = "chat"
	ChannelAPI   = "api"
)

// StatusLive is the ticket status for an active live chat.
const StatusLive = 9

func init() {
	StatusName[StatusLive] = "live"
}

// ChatSession tracks a real-time chat conversation linked to a ticket.
type ChatSession struct {
	ID               int64           `json:"id"`
	TicketID         int64           `json:"ticket_id"`
	Status           int             `json:"status"`
	AgentID          *int64          `json:"agent_id,omitempty"`
	VisitorUserAgent *string         `json:"visitor_user_agent,omitempty"`
	VisitorIP        *string         `json:"visitor_ip,omitempty"`
	VisitorPageURL   *string         `json:"visitor_page_url,omitempty"`
	AgentJoinedAt    *time.Time      `json:"agent_joined_at,omitempty"`
	LastActivityAt   *time.Time      `json:"last_activity_at,omitempty"`
	EndedAt          *time.Time      `json:"ended_at,omitempty"`
	RawMetadata      json.RawMessage `json:"metadata,omitempty"`
	CreatedAt        time.Time       `json:"created_at"`
}

// IsActive returns true if the chat session is currently active.
func (s *ChatSession) IsActive() bool {
	return s.Status == ChatStatusActive
}

// IsWaiting returns true if the chat session is waiting for an agent.
func (s *ChatSession) IsWaiting() bool {
	return s.Status == ChatStatusWaiting
}

// StatusString returns the human-readable status name.
func (s *ChatSession) StatusString() string {
	if name, ok := ChatStatusName[s.Status]; ok {
		return name
	}
	return "unknown"
}

// Metadata returns the raw JSON metadata for the session, or nil if empty.
func (s *ChatSession) Metadata() json.RawMessage {
	return s.RawMetadata
}

// Duration returns the duration of the chat from agent join to end.
// Returns zero if the agent hasn't joined or the session hasn't ended.
func (s *ChatSession) Duration() time.Duration {
	if s.AgentJoinedAt == nil || s.EndedAt == nil {
		return 0
	}
	return s.EndedAt.Sub(*s.AgentJoinedAt)
}

// ChatMessage represents a single message within a chat session.
type ChatMessage struct {
	ID            int64     `json:"id"`
	ChatSessionID int64     `json:"chat_session_id"`
	SenderType    string    `json:"sender_type"` // "agent", "visitor", "system"
	SenderID      *int64    `json:"sender_id,omitempty"`
	Body          string    `json:"body"`
	CreatedAt     time.Time `json:"created_at"`
}

// ChatSessionFilters holds query parameters for listing chat sessions.
type ChatSessionFilters struct {
	Status  *int   `json:"status,omitempty"`
	AgentID *int64 `json:"agent_id,omitempty"`
	Active  bool   `json:"active,omitempty"`
}
