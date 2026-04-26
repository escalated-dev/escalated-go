package models

import (
	"encoding/json"
	"time"
)

// Automation represents a time-based admin rule that runs on a recurring
// schedule. Distinct from Workflow (event-driven) and Macro (agent manual).
//
// See escalated-developer-context/domain-model/workflows-automations-macros.md
// for the canonical taxonomy.
type Automation struct {
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
	// Conditions: list of {field, operator, value} clauses (AND).
	Conditions json.RawMessage `json:"conditions"`
	// Actions: list of {type, value} clauses, executed on each matching ticket.
	Actions   json.RawMessage `json:"actions"`
	Active    bool            `json:"active"`
	Position  int             `json:"position"`
	LastRunAt *time.Time      `json:"last_run_at,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// AutomationCondition is the parsed form of a single Conditions entry.
type AutomationCondition struct {
	Field    string      `json:"field"`
	Operator string      `json:"operator,omitempty"`
	Value    interface{} `json:"value,omitempty"`
}

// AutomationAction is the parsed form of a single Actions entry.
type AutomationAction struct {
	Type  string      `json:"type"`
	Value interface{} `json:"value,omitempty"`
}
