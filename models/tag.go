package models

import "time"

// Tag is a label that can be applied to tickets for categorisation.
type Tag struct {
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	Slug        string  `json:"slug"`
	Color       *string `json:"color,omitempty"`
	Description *string `json:"description,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
