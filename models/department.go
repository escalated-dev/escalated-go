package models

import "time"

// Department groups tickets and agents into functional areas.
type Department struct {
	ID                 int64   `json:"id"`
	Name               string  `json:"name"`
	Slug               string  `json:"slug"`
	Description        *string `json:"description,omitempty"`
	Email              *string `json:"email,omitempty"`
	IsActive           bool    `json:"is_active"`
	DefaultSLAPolicyID *int64  `json:"default_sla_policy_id,omitempty"`
	// BusinessScheduleID is the schedule business-hours SLA policies count
	// against for this department's tickets, unless the policy names one.
	BusinessScheduleID *int64 `json:"business_schedule_id"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
