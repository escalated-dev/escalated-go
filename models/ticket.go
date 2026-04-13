package models

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Ticket status constants.
const (
	StatusOpen              = 0
	StatusInProgress        = 1
	StatusWaitingOnCustomer = 2
	StatusWaitingOnAgent    = 3
	StatusEscalated         = 4
	StatusResolved          = 5
	StatusClosed            = 6
	StatusReopened          = 7
	StatusSnoozed           = 8
)

// StatusName maps status integers to human-readable names.
var StatusName = map[int]string{
	StatusOpen:              "open",
	StatusInProgress:        "in_progress",
	StatusWaitingOnCustomer: "waiting_on_customer",
	StatusWaitingOnAgent:    "waiting_on_agent",
	StatusEscalated:         "escalated",
	StatusResolved:          "resolved",
	StatusClosed:            "closed",
	StatusReopened:          "reopened",
	StatusSnoozed:           "snoozed",
}

// Ticket priority constants.
const (
	PriorityLow      = 0
	PriorityMedium   = 1
	PriorityHigh     = 2
	PriorityUrgent   = 3
	PriorityCritical = 4
)

// PriorityName maps priority integers to human-readable names.
var PriorityName = map[int]string{
	PriorityLow:      "low",
	PriorityMedium:   "medium",
	PriorityHigh:     "high",
	PriorityUrgent:   "urgent",
	PriorityCritical: "critical",
}

// Valid ticket types.
var TicketTypes = []string{"question", "problem", "incident", "task"}

// Ticket represents a support ticket.
type Ticket struct {
	ID          int64  `json:"id"`
	Reference   string `json:"reference"`
	Subject     string `json:"subject"`
	Description string `json:"description"`
	Status      int    `json:"status"`
	Priority    int    `json:"priority"`
	TicketType  string `json:"ticket_type"`

	// Requester (polymorphic in Rails — here we use type+id strings)
	RequesterType *string `json:"requester_type,omitempty"`
	RequesterID   *int64  `json:"requester_id,omitempty"`

	// Guest ticket fields
	GuestName  *string `json:"guest_name,omitempty"`
	GuestEmail *string `json:"guest_email,omitempty"`
	GuestToken *string `json:"guest_token,omitempty"`

	// Assignee
	AssignedTo *int64 `json:"assigned_to,omitempty"`

	// Relationships
	DepartmentID *int64 `json:"department_id,omitempty"`
	SLAPolicyID  *int64 `json:"sla_policy_id,omitempty"`
	MergedIntoID *int64 `json:"merged_into_id,omitempty"`
	SplitFromID  *int64 `json:"split_from_id,omitempty"`

	// Snooze fields
	SnoozedUntil       *time.Time `json:"snoozed_until,omitempty"`
	SnoozedBy          *int64     `json:"snoozed_by,omitempty"`
	StatusBeforeSnooze *int       `json:"status_before_snooze,omitempty"`

	// SLA tracking
	SLAFirstResponseDueAt *time.Time `json:"sla_first_response_due_at,omitempty"`
	SLAResolutionDueAt    *time.Time `json:"sla_resolution_due_at,omitempty"`
	SLABreached           bool       `json:"sla_breached"`

	// Timestamps
	FirstResponseAt *time.Time `json:"first_response_at,omitempty"`
	ResolvedAt      *time.Time `json:"resolved_at,omitempty"`
	ClosedAt        *time.Time `json:"closed_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`

	// Chat fields
	Channel      *string         `json:"channel,omitempty"`
	ChatEndedAt  *time.Time      `json:"chat_ended_at,omitempty"`
	ChatMetadata json.RawMessage `json:"chat_metadata,omitempty"`

	// Metadata stored as JSON
	Metadata json.RawMessage `json:"metadata,omitempty"`

	// Loaded relationships (not persisted directly)
	Department  *Department  `json:"department,omitempty"`
	SLAPolicy   *SLAPolicy   `json:"sla_policy,omitempty"`
	Tags        []Tag        `json:"tags,omitempty"`
	Replies     []Reply      `json:"replies,omitempty"`
	Attachments []Attachment `json:"attachments,omitempty"`

	// Computed fields (populated at serialization time, not persisted)
	RequesterName        *string         `json:"requester_name,omitempty"`
	RequesterEmail       *string         `json:"requester_email,omitempty"`
	LastReplyAt          *time.Time      `json:"last_reply_at,omitempty"`
	LastReplyAuthor      *string         `json:"last_reply_author,omitempty"`
	IsLiveChatFlag       bool            `json:"is_live_chat"`
	IsSnoozedFlag        bool            `json:"is_snoozed"`
	ChatSessionID        *int64          `json:"chat_session_id,omitempty"`
	ChatStartedAt        *time.Time      `json:"chat_started_at,omitempty"`
	ChatMessages         []ChatMessage   `json:"chat_messages,omitempty"`
	RequesterTicketCount *int            `json:"requester_ticket_count,omitempty"`
	RelatedTickets       []RelatedTicket `json:"related_tickets,omitempty"`
}

// PopulateComputedOpts holds optional data used to populate computed fields.
type PopulateComputedOpts struct {
	ChatSession          *ChatSession
	ChatMessages         []ChatMessage
	RequesterTicketCount *int
	RelatedTickets       []RelatedTicket
}

// PopulateComputed fills the computed JSON fields from existing ticket and
// reply data so the frontend receives the values it expects.
func (t *Ticket) PopulateComputed(replies []*Reply) {
	// requester_name / requester_email: prefer guest fields, fall back to
	// RequesterType-based lookup (handled by the caller if needed).
	if t.GuestName != nil {
		t.RequesterName = t.GuestName
	}
	if t.GuestEmail != nil {
		t.RequesterEmail = t.GuestEmail
	}

	// last_reply_at / last_reply_author: find the most recent reply.
	if len(replies) > 0 {
		var latest *Reply
		for _, r := range replies {
			if latest == nil || r.CreatedAt.After(latest.CreatedAt) {
				latest = r
			}
		}
		if latest != nil {
			t.LastReplyAt = &latest.CreatedAt
			if latest.AuthorName != nil {
				t.LastReplyAuthor = latest.AuthorName
			}
		}
	}

	// is_live_chat
	t.IsLiveChatFlag = t.IsLiveChat()

	// is_snoozed: snoozed_until is set and in the future
	t.IsSnoozedFlag = t.SnoozedUntil != nil && t.SnoozedUntil.After(time.Now())
}

// PopulateComputedFull calls PopulateComputed and also fills chat, requester
// count, and related ticket fields from the provided options.
func (t *Ticket) PopulateComputedFull(replies []*Reply, opts PopulateComputedOpts) {
	t.PopulateComputed(replies)

	if opts.ChatSession != nil {
		t.ChatSessionID = &opts.ChatSession.ID
		t.ChatStartedAt = &opts.ChatSession.CreatedAt
		// Prefer the session-level metadata; fall back to ticket ChatMetadata.
		t.ChatMetadata = opts.ChatSession.Metadata()
	}

	if len(opts.ChatMessages) > 0 {
		t.ChatMessages = opts.ChatMessages
	}

	if opts.RequesterTicketCount != nil {
		t.RequesterTicketCount = opts.RequesterTicketCount
	}

	if len(opts.RelatedTickets) > 0 {
		t.RelatedTickets = opts.RelatedTickets
	}
}

// IsOpen returns true if the ticket is in an open state.
func (t *Ticket) IsOpen() bool {
	switch t.Status {
	case StatusOpen, StatusInProgress, StatusWaitingOnCustomer,
		StatusWaitingOnAgent, StatusEscalated, StatusReopened:
		return true
	}
	return false
}

// IsSnoozed returns true if the ticket is currently snoozed.
func (t *Ticket) IsSnoozed() bool {
	return t.Status == StatusSnoozed && t.SnoozedUntil != nil
}

// IsLiveChat returns true if this ticket originated from a live chat.
func (t *Ticket) IsLiveChat() bool {
	return t.Channel != nil && *t.Channel == ChannelChat
}

// IsChatActive returns true if this is an active live chat session.
func (t *Ticket) IsChatActive() bool {
	return t.IsLiveChat() && t.Status == StatusLive && t.ChatEndedAt == nil
}

// IsGuest returns true if this is a guest ticket (no authenticated requester).
func (t *Ticket) IsGuest() bool {
	return t.RequesterType == nil && t.GuestToken != nil
}

// StatusString returns the human-readable status name.
func (t *Ticket) StatusString() string {
	if name, ok := StatusName[t.Status]; ok {
		return name
	}
	return "unknown"
}

// PriorityString returns the human-readable priority name.
func (t *Ticket) PriorityString() string {
	if name, ok := PriorityName[t.Priority]; ok {
		return name
	}
	return "unknown"
}

// SLAFirstResponseBreached returns true if first response SLA is breached.
func (t *Ticket) SLAFirstResponseBreached() bool {
	if t.SLAFirstResponseDueAt == nil || t.FirstResponseAt != nil {
		return false
	}
	return time.Now().After(*t.SLAFirstResponseDueAt)
}

// SLAResolutionBreached returns true if resolution SLA is breached.
func (t *Ticket) SLAResolutionBreached() bool {
	if t.SLAResolutionDueAt == nil || t.ResolvedAt != nil {
		return false
	}
	return time.Now().After(*t.SLAResolutionDueAt)
}

// TimeToFirstResponse returns the duration from creation to first response, or zero if not yet responded.
func (t *Ticket) TimeToFirstResponse() time.Duration {
	if t.FirstResponseAt == nil {
		return 0
	}
	return t.FirstResponseAt.Sub(t.CreatedAt)
}

// TimeToResolution returns the duration from creation to resolution, or zero if not yet resolved.
func (t *Ticket) TimeToResolution() time.Duration {
	if t.ResolvedAt == nil {
		return 0
	}
	return t.ResolvedAt.Sub(t.CreatedAt)
}

// GenerateReference creates a ticket reference like "ESC-2604-A1B2C3".
func GenerateReference(prefix string) string {
	if prefix == "" {
		prefix = "ESC"
	}
	timestamp := time.Now().Format("0601")
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	seq := strings.ToUpper(fmt.Sprintf("%X", b))
	return fmt.Sprintf("%s-%s-%s", prefix, timestamp, seq)
}

// RelatedTicket is a lightweight representation of a linked ticket.
type RelatedTicket struct {
	ID        int64  `json:"id"`
	Reference string `json:"reference"`
	Subject   string `json:"subject"`
	Status    int    `json:"status"`
}

// TicketFilters holds query parameters for listing tickets.
type TicketFilters struct {
	Status       *int    `json:"status,omitempty"`
	Priority     *int    `json:"priority,omitempty"`
	TicketType   *string `json:"ticket_type,omitempty"`
	DepartmentID *int64  `json:"department_id,omitempty"`
	AssignedTo   *int64  `json:"assigned_to,omitempty"`
	RequesterID  *int64  `json:"requester_id,omitempty"`
	Search       string  `json:"search,omitempty"`
	SLABreached  *bool   `json:"sla_breached,omitempty"`
	Unassigned   bool    `json:"unassigned,omitempty"`

	// Pagination
	Limit  int `json:"limit"`
	Offset int `json:"offset"`

	// Sorting
	SortBy    string `json:"sort_by"`
	SortOrder string `json:"sort_order"` // "asc" or "desc"
}
