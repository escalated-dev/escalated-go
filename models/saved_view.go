package models

import (
	"encoding/json"
	"time"
)

// SavedView represents a saved ticket filter/view configuration.
type SavedView struct {
	ID       int64           `json:"id"`
	Name     string          `json:"name"`
	Filters  json.RawMessage `json:"filters"`
	UserID   int64           `json:"user_id"`
	IsShared bool            `json:"is_shared"`
	Position int             `json:"position"`
	Icon     string          `json:"icon,omitempty"`
	Color    string          `json:"color,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SavedViewFilters represents the decoded filter criteria stored in a SavedView.
type SavedViewFilters struct {
	Status       *int    `json:"status,omitempty"`
	Priority     *int    `json:"priority,omitempty"`
	TicketType   *string `json:"ticket_type,omitempty"`
	DepartmentID *int64  `json:"department_id,omitempty"`
	AssignedTo   *int64  `json:"assigned_to,omitempty"`
	RequesterID  *int64  `json:"requester_id,omitempty"`
	Search       string  `json:"search,omitempty"`
	SLABreached  *bool   `json:"sla_breached,omitempty"`
	Unassigned   bool    `json:"unassigned,omitempty"`
	SortBy       string  `json:"sort_by,omitempty"`
	SortOrder    string  `json:"sort_order,omitempty"`
	TagIDs       []int64 `json:"tag_ids,omitempty"`
}

// DecodeFilters parses the JSON filters into a SavedViewFilters struct.
func (sv *SavedView) DecodeFilters() (*SavedViewFilters, error) {
	if len(sv.Filters) == 0 {
		return &SavedViewFilters{}, nil
	}
	var f SavedViewFilters
	if err := json.Unmarshal(sv.Filters, &f); err != nil {
		return nil, err
	}
	return &f, nil
}

// ToTicketFilters converts SavedViewFilters to a TicketFilters for querying.
func (f *SavedViewFilters) ToTicketFilters(limit, offset int) TicketFilters {
	return TicketFilters{
		Status:       f.Status,
		Priority:     f.Priority,
		TicketType:   f.TicketType,
		DepartmentID: f.DepartmentID,
		AssignedTo:   f.AssignedTo,
		RequesterID:  f.RequesterID,
		Search:       f.Search,
		SLABreached:  f.SLABreached,
		Unassigned:   f.Unassigned,
		Limit:        limit,
		Offset:       offset,
		SortBy:       f.SortBy,
		SortOrder:    f.SortOrder,
	}
}
