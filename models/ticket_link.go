package models

import "time"

// Ticket link types.
const (
	LinkTypeProblemIncident = "problem_incident"
	LinkTypeParentChild     = "parent_child"
	LinkTypeRelated         = "related"
)

// TicketLinkTypes is the set of valid link types.
var TicketLinkTypes = []string{LinkTypeProblemIncident, LinkTypeParentChild, LinkTypeRelated}

// TicketLink is a typed link between two tickets. Mirrors the Laravel
// TicketLink model. Unique per (parent, child, link_type).
type TicketLink struct {
	ID             int64     `json:"id"`
	ParentTicketID int64     `json:"parent_ticket_id"`
	ChildTicketID  int64     `json:"child_ticket_id"`
	LinkType       string    `json:"link_type"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// ValidLinkType reports whether t is a recognised ticket link type.
func ValidLinkType(t string) bool {
	for _, lt := range TicketLinkTypes {
		if lt == t {
			return true
		}
	}
	return false
}
