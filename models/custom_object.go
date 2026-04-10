package models

import (
	"encoding/json"
	"time"
)

// CustomObject represents a user-defined data model.
type CustomObject struct {
	ID               int64           `json:"id"`
	Name             string          `json:"name"`
	Slug             string          `json:"slug"`
	Description      *string         `json:"description,omitempty"`
	FieldDefinitions json.RawMessage `json:"field_definitions,omitempty"`
	IsActive         bool            `json:"is_active"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CustomObjectRecord represents a single record of a custom object.
type CustomObjectRecord struct {
	ID               int64           `json:"id"`
	CustomObjectID   int64           `json:"custom_object_id"`
	Title            *string         `json:"title,omitempty"`
	Data             json.RawMessage `json:"data"`
	LinkedEntityType *string         `json:"linked_entity_type,omitempty"`
	LinkedEntityID   *int64          `json:"linked_entity_id,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
