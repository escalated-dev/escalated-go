package models

import (
	"encoding/json"
	"time"
)

// Macro represents an agent-applied, manual one-click action bundle.
//
// Distinct from Workflow (event-driven) and Automation (time-based) per
// escalated-developer-context/domain-model/workflows-automations-macros.md.
//
// No conditions, no triggers — agent picks a macro on a specific ticket
// and clicks "apply"; all actions in the bundle execute against that
// ticket at once.
type Macro struct {
	ID          int64           `json:"id"`
	Name        string          `json:"name"`
	Description *string         `json:"description,omitempty"`
	// Actions: list of {type, value} clauses, all executed in order.
	Actions   json.RawMessage `json:"actions"`
	// If true, all agents see and can apply this macro.
	// If false, only the creator (CreatedBy) sees it.
	IsShared  bool       `json:"is_shared"`
	// Host-app user id of the agent who created this macro.
	// Null only for system-seeded macros.
	CreatedBy *int64    `json:"created_by,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// MacroAction is the parsed form of a single Actions entry.
type MacroAction struct {
	Type  string      `json:"type"`
	Value interface{} `json:"value,omitempty"`
}
