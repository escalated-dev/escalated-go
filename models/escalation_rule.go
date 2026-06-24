package models

import (
	"encoding/json"
	"time"
)

// EscalationRule is a time-based rule that matches open tickets by its
// Conditions and applies its Actions (escalate, change priority, (re)assign,
// change department). Evaluated on a recurring schedule by
// services.EscalationService. Mirrors the Laravel EscalationRule model.
type EscalationRule struct {
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
	TriggerType *string `json:"trigger_type,omitempty"`
	// Conditions: list of {field, value} clauses (AND).
	Conditions json.RawMessage `json:"conditions"`
	// Actions: list of {type, value} clauses applied to each matching ticket.
	Actions   json.RawMessage `json:"actions"`
	Order     int             `json:"order"`
	IsActive  bool            `json:"is_active"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// EscalationCondition is the parsed form of a single Conditions entry.
type EscalationCondition struct {
	Field string      `json:"field"`
	Value interface{} `json:"value,omitempty"`
}

// EscalationAction is the parsed form of a single Actions entry.
type EscalationAction struct {
	Type  string      `json:"type"`
	Value interface{} `json:"value,omitempty"`
}
