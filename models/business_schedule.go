package models

import (
	"encoding/json"
	"time"
)

// DaySchedule is one weekday's opening hours as wall-clock "HH:MM" times in
// the schedule's time zone (Laravel's schedule JSON: {"start":"09:00",
// "end":"17:00"}). A day with an empty start or end is closed. End "24:00"
// means midnight at the end of the day, and an end earlier than the start is
// an overnight window that closes on the following day. Enabled is optional:
// only an explicit false closes a day that has hours.
type DaySchedule struct {
	Start   string `json:"start"`
	End     string `json:"end"`
	Enabled *bool  `json:"enabled,omitempty"`
}

// Open reports whether the day has opening hours at all.
func (d DaySchedule) Open() bool {
	if d.Enabled != nil && !*d.Enabled {
		return false
	}
	return d.Start != "" && d.End != ""
}

// DefaultBusinessHours returns the default Mon-Fri 9-5 schedule (the shared
// form's default).
func DefaultBusinessHours() map[string]DaySchedule {
	open := DaySchedule{Start: "09:00", End: "17:00"}
	return map[string]DaySchedule{
		"monday":    open,
		"tuesday":   open,
		"wednesday": open,
		"thursday":  open,
		"friday":    open,
		"saturday":  {Start: "", End: ""},
		"sunday":    {Start: "", End: ""},
	}
}

// BusinessSchedule defines business operating hours and holidays (Laravel
// BusinessSchedule). Hours is stored in the hours column and serialised as
// "schedule", the key the shared admin pages read.
type BusinessSchedule struct {
	ID        int64                  `json:"id"`
	Name      string                 `json:"name"`
	Timezone  string                 `json:"timezone"`
	Hours     map[string]DaySchedule `json:"schedule"`
	IsDefault bool                   `json:"is_default"`
	IsActive  bool                   `json:"is_active"`

	Holidays []Holiday `json:"holidays"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// IsWithinBusinessHours reports whether t falls inside one of the schedule's
// opening windows (start inclusive, end exclusive).
func (bs *BusinessSchedule) IsWithinBusinessHours(t time.Time) bool {
	loc := bs.location()
	local := t.In(loc)
	day := civilDate(local.Year(), local.Month(), local.Day()-1)
	for i := 0; i < 2; i++ {
		if start, end, ok := bs.window(day, loc); ok && !t.Before(start) && t.Before(end) {
			return true
		}
		day = day.AddDate(0, 0, 1)
	}
	return false
}

// Holiday is a date on which a schedule's windows do not open. A recurring
// holiday matches its month and day in every year.
type Holiday struct {
	ID                 int64     `json:"id"`
	BusinessScheduleID int64     `json:"schedule_id"`
	Name               string    `json:"name"`
	Date               time.Time `json:"date"`
	IsRecurring        bool      `json:"recurring"`
	CreatedAt          time.Time `json:"created_at"`
}

// MarshalJSON writes the date as YYYY-MM-DD, as Laravel's date cast reads in
// the shared form.
func (h Holiday) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{
		"id":          h.ID,
		"schedule_id": h.BusinessScheduleID,
		"name":        h.Name,
		"date":        h.Date.Format("2006-01-02"),
		"recurring":   h.IsRecurring,
		"created_at":  h.CreatedAt,
	})
}

// matches reports whether the holiday falls on the civil date day.
func (h Holiday) matches(day time.Time) bool {
	if h.IsRecurring {
		return h.Date.Month() == day.Month() && h.Date.Day() == day.Day()
	}
	return h.Date.Year() == day.Year() && h.Date.Month() == day.Month() && h.Date.Day() == day.Day()
}
