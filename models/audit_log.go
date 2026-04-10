package models

import (
	"encoding/json"
	"time"
)

// AuditLog represents an audit trail entry for changes to entities.
type AuditLog struct {
	ID            int64           `json:"id"`
	Action        string          `json:"action"`
	EntityType    string          `json:"entity_type"`
	EntityID      *int64          `json:"entity_id,omitempty"`
	PerformerType *string         `json:"performer_type,omitempty"`
	PerformerID   *int64          `json:"performer_id,omitempty"`
	OldValues     json.RawMessage `json:"old_values,omitempty"`
	NewValues     json.RawMessage `json:"new_values,omitempty"`
	IPAddress     *string         `json:"ip_address,omitempty"`
	UserAgent     *string         `json:"user_agent,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
}
