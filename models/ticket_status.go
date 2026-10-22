package models

import "time"

// Custom ticket statuses (the shared statuses spec). A custom status belongs
// to one of five categories, and lifecycle logic keys off the category: a
// ticket given a custom status takes the category's built-in anchor status, so
// the integer status column stays the source of truth for transitions, SLA,
// reports and lifecycle events, and ticket_status_id records the sub-status.

// Status categories, in the order the admin pages list them.
const (
	StatusCategoryNew     = "new"
	StatusCategoryOpen    = "open"
	StatusCategoryPending = "pending"
	StatusCategoryOnHold  = "on_hold"
	StatusCategorySolved  = "solved"
)

// StatusCategories lists the categories in Laravel's order.
var StatusCategories = []string{
	StatusCategoryNew, StatusCategoryOpen, StatusCategoryPending, StatusCategoryOnHold, StatusCategorySolved,
}

// DefaultStatusColor is the colour a custom status gets when none is given.
const DefaultStatusColor = "#6b7280"

// TicketStatus is a row of the ticket_statuses table: a custom status, or a
// system row standing for one of the built-in statuses (IsSystem, slug equal
// to the built-in's StatusName).
type TicketStatus struct {
	ID          int64     `json:"id"`
	Label       string    `json:"label"`
	Slug        string    `json:"slug"`
	Category    string    `json:"category"`
	Color       string    `json:"color"`
	Description *string   `json:"description"`
	Position    int       `json:"position"`
	IsDefault   bool      `json:"is_default"`
	IsSystem    bool      `json:"is_system"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// IsStatusCategory reports whether v is one of the five categories.
func IsStatusCategory(v string) bool {
	for _, c := range StatusCategories {
		if c == v {
			return true
		}
	}
	return false
}

// CategoryAnchor is the built-in status a ticket takes when it gets a custom
// status of the category. An unknown category anchors at open.
func CategoryAnchor(category string) int {
	switch category {
	case StatusCategoryPending:
		return StatusWaitingOnCustomer
	case StatusCategoryOnHold:
		return StatusInProgress
	case StatusCategorySolved:
		return StatusResolved
	}
	return StatusOpen
}

// BuiltInStatusCategory is the category of a built-in status.
func BuiltInStatusCategory(status int) string {
	switch status {
	case StatusWaitingOnCustomer:
		return StatusCategoryPending
	case StatusSnoozed:
		return StatusCategoryOnHold
	case StatusResolved, StatusClosed:
		return StatusCategorySolved
	}
	return StatusCategoryOpen
}

// BuiltInStatus describes a built-in status's system row.
type BuiltInStatus struct {
	Status int
	Slug   string
	Label  string
	Color  string
}

// BuiltInStatuses lists the built-in statuses in their integer order, with the
// label and colour their system rows are seeded with. Live (the live chat
// status, defined beside the chat session) is last.
func BuiltInStatuses() []BuiltInStatus {
	return []BuiltInStatus{
		{StatusOpen, StatusName[StatusOpen], "Open", "#3B82F6"},
		{StatusInProgress, StatusName[StatusInProgress], "In Progress", "#8B5CF6"},
		{StatusWaitingOnCustomer, StatusName[StatusWaitingOnCustomer], "Waiting on Customer", "#F59E0B"},
		{StatusWaitingOnAgent, StatusName[StatusWaitingOnAgent], "Waiting on Agent", "#F97316"},
		{StatusEscalated, StatusName[StatusEscalated], "Escalated", "#EF4444"},
		{StatusResolved, StatusName[StatusResolved], "Resolved", "#10B981"},
		{StatusClosed, StatusName[StatusClosed], "Closed", "#6B7280"},
		{StatusReopened, StatusName[StatusReopened], "Reopened", "#3B82F6"},
		{StatusSnoozed, StatusName[StatusSnoozed], "Snoozed", "#A855F7"},
		{StatusLive, StatusName[StatusLive], "Live", "#22C55E"},
	}
}
