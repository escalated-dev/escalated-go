package models

import (
	"encoding/json"
	"fmt"
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
	ActionTicketSnoozed    = "ticket_snoozed"
	ActionTicketUnsnoozed  = "ticket_unsnoozed"
	ActionTicketSplit      = "ticket_split"
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

	CreatedAt      time.Time `json:"created_at"`
	CreatedAtHuman string    `json:"created_at_human,omitempty"`
}

// IsSystemActivity returns true if no causer was recorded (system-generated).
func (a *Activity) IsSystemActivity() bool {
	return a.CauserType == nil
}

// PopulateHumanTime sets CreatedAtHuman to a human-friendly relative time
// string (e.g. "2 hours ago", "3 days ago").
func (a *Activity) PopulateHumanTime() {
	a.CreatedAtHuman = HumanTime(a.CreatedAt)
}

// HumanTime returns a human-friendly relative time string.
func HumanTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d.Minutes())
		if m == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", m)
	case d < 24*time.Hour:
		h := int(d.Hours())
		if h == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", h)
	case d < 30*24*time.Hour:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	default:
		return t.Format("Jan 2, 2006")
	}
}
