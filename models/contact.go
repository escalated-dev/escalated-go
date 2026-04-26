package models

import (
	"strings"
	"time"
)

// Contact is the first-class identity for guest requesters (Pattern B).
//
// Deduped by email (unique index; value is normalized via
// NormalizeEmail before insert / lookup). UserID links to a host-app
// user once the guest accepts a signup invite.
//
// Coexists with the inline Guest* fields on Ticket for one release —
// a separate backfill routine populates ContactID from GuestEmail.
// New code should resolve contacts via FindOrCreateByEmailDecision
// and its caller.
type Contact struct {
	ID        int64          `json:"id"`
	Email     string         `json:"email"`
	Name      *string        `json:"name,omitempty"`
	UserID    *int64         `json:"user_id,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// NormalizeEmail trims surrounding whitespace and lowercases. Call
// on any caller-supplied email before inserting or looking up.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// ContactAction is the decision made by FindOrCreateByEmail given
// the lookup result and incoming name.
type ContactAction string

const (
	ContactActionCreate         ContactAction = "create"
	ContactActionUpdateName     ContactAction = "update-name"
	ContactActionReturnExisting ContactAction = "return-existing"
)

// DecideContactAction is a pure branch-selection helper. Returns
//   - "create" when no existing contact matches
//   - "update-name" when the existing row has a blank name and
//     a non-blank incoming name is supplied
//   - "return-existing" otherwise
//
// Extracted for testability without a database. Callers run the
// actual SQL based on the returned action.
func DecideContactAction(existing *Contact, incomingName string) ContactAction {
	if existing == nil {
		return ContactActionCreate
	}
	existingName := ""
	if existing.Name != nil {
		existingName = *existing.Name
	}
	if existingName == "" && incomingName != "" {
		return ContactActionUpdateName
	}
	return ContactActionReturnExisting
}
