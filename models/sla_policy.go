package models

import (
	"encoding/json"
	"time"
)

// SLAPolicy defines response and resolution time targets per priority level.
type SLAPolicy struct {
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`

	// FirstResponseHours is a JSON map of priority -> hours, e.g. {"low":24,"medium":8,"high":4}
	FirstResponseHours json.RawMessage `json:"first_response_hours"`
	// ResolutionHours is a JSON map of priority -> hours, e.g. {"low":72,"medium":24,"high":8}
	ResolutionHours json.RawMessage `json:"resolution_hours"`

	IsActive  bool `json:"is_active"`
	IsDefault bool `json:"is_default"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// FirstResponseHoursFor returns the first-response target in hours for a given priority name.
func (s *SLAPolicy) FirstResponseHoursFor(priority string) (float64, bool) {
	return hoursFor(s.FirstResponseHours, priority)
}

// ResolutionHoursFor returns the resolution target in hours for a given priority name.
func (s *SLAPolicy) ResolutionHoursFor(priority string) (float64, bool) {
	return hoursFor(s.ResolutionHours, priority)
}

func hoursFor(raw json.RawMessage, priority string) (float64, bool) {
	if len(raw) == 0 {
		return 0, false
	}
	var m map[string]float64
	if err := json.Unmarshal(raw, &m); err != nil {
		return 0, false
	}
	v, ok := m[priority]
	return v, ok
}
