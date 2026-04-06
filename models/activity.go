package models

import (
	"encoding/json"
	"time"
)

// Known activity actions.
const (
	ActionTicketCreated    = "ticket_created"
	ActionTicketUpdated    = "ticket_updated"
	ActionStatusChanged    = "status_changed"
	ActionTicketAssigned   = "ticket_assigned"
	ActionTicketUnassigned = "ticket_unassigned"
	ActionReplyAdded       = "reply_added"
	ActionInternalNote     = "internal_note_added"
	ActionTagsAdded        = "tags_added"
	ActionTagsRemoved      = "tags_removed"
	ActionDeptChanged      = "department_changed"
	ActionPriorityChanged  = "priority_changed"
	ActionSLABreached      = "sla_breached"
	ActionTicketEscalated  = "ticket_escalated"
	ActionTicketMerged     = "ticket_merged"
)

// Activity records an event that occurred on a ticket.
type Activity struct {
	ID       int64  `json:"id"`
	TicketID int64  `json:"ticket_id"`
	Action   string `json:"action"`

	// Polymorphic causer — nil for system-generated events
	CauserType *string `json:"causer_type,omitempty"`
	CauserID   *int64  `json:"causer_id,omitempty"`

	Details json.RawMessage `json:"details,omitempty"`

	CreatedAt time.Time `json:"created_at"`
}

// IsSystemActivity returns true if no causer was recorded (system-generated).
func (a *Activity) IsSystemActivity() bool {
	return a.CauserType == nil
}
