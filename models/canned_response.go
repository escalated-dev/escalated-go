package models

import (
	"time"
)

// CannedResponse is an agent-authored, reusable reply template.
//
// Distinct from a Macro (an action bundle): a canned response is just a
// stored title/body an agent can insert into a reply. The macro action
// type "insert_canned_reply" resolves such a template frontend-side and
// POSTs the resolved text; this entity is the server-side store of the
// templates themselves (list/create/update/delete).
//
// Visibility mirrors Macro: a shared response (IsShared) is visible to
// every agent; a private one is visible only to its creator (CreatedBy).
type CannedResponse struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Body  string `json:"body"`
	// Optional grouping label (e.g. "Billing", "Onboarding"). Null when unset.
	Category *string `json:"category,omitempty"`
	// If true, all agents see this response. If false, only the creator does.
	IsShared bool `json:"is_shared"`
	// Host-app user id of the agent who created this response.
	// Null only for system-seeded responses.
	CreatedBy *UserID   `json:"created_by,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
