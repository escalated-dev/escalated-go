package models

import "time"

// CustomField field type constants.
const (
	FieldTypeText        = "text"
	FieldTypeTextarea    = "textarea"
	FieldTypeNumber      = "number"
	FieldTypeSelect      = "select"
	FieldTypeMultiSelect = "multi_select"
	FieldTypeCheckbox    = "checkbox"
	FieldTypeDate        = "date"
	FieldTypeURL         = "url"
)

// TicketFieldTypes are the types the custom field admin accepts (Laravel's
// list). FieldTypeURL predates it and is kept for existing rows.
var TicketFieldTypes = []string{
	FieldTypeText, FieldTypeTextarea, FieldTypeSelect, FieldTypeMultiSelect,
	FieldTypeCheckbox, FieldTypeDate, FieldTypeNumber,
}

// CustomFieldContexts are the entities a custom field can belong to.
var CustomFieldContexts = []string{"ticket", "user", "organization"}

// FieldTypes lists all valid custom field types.
var FieldTypes = []string{
	FieldTypeText, FieldTypeTextarea, FieldTypeNumber, FieldTypeSelect,
	FieldTypeMultiSelect, FieldTypeCheckbox, FieldTypeDate, FieldTypeURL,
}

// CustomField represents a custom field definition.
type CustomField struct {
	ID           int64    `json:"id"`
	Name         string   `json:"name"`
	Slug         string   `json:"slug"`
	FieldType    string   `json:"field_type"`
	Description  *string  `json:"description,omitempty"`
	IsRequired   bool     `json:"is_required"`
	Options      []string `json:"options,omitempty"`
	DefaultValue *string  `json:"default_value,omitempty"`
	Placeholder  *string  `json:"placeholder,omitempty"`
	// ValidationRules and Conditions are stored JSON, kept as sent.
	ValidationRules JSONText `json:"validation_rules,omitempty"`
	Conditions      JSONText `json:"conditions,omitempty"`
	EntityType      string   `json:"entity_type"`
	Position        int      `json:"position"`
	IsActive        bool     `json:"is_active"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CustomFieldValue represents a stored value for a custom field on an entity.
type CustomFieldValue struct {
	ID            int64   `json:"id"`
	CustomFieldID int64   `json:"custom_field_id"`
	EntityType    string  `json:"entity_type"`
	EntityID      int64   `json:"entity_id"`
	Value         *string `json:"value,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
