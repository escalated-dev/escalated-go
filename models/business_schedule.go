package models

import (
	"strings"
	"time"
)

// DaySchedule represents business hours for a single day.
type DaySchedule struct {
	Start   string `json:"start"`
	End     string `json:"end"`
	Enabled bool   `json:"enabled"`
}

// DefaultBusinessHours returns the default Mon-Fri 9-5 schedule.
func DefaultBusinessHours() map[string]DaySchedule {
	return map[string]DaySchedule{
		"monday":    {Start: "09:00", End: "17:00", Enabled: true},
		"tuesday":   {Start: "09:00", End: "17:00", Enabled: true},
		"wednesday": {Start: "09:00", End: "17:00", Enabled: true},
		"thursday":  {Start: "09:00", End: "17:00", Enabled: true},
		"friday":    {Start: "09:00", End: "17:00", Enabled: true},
		"saturday":  {Start: "09:00", End: "17:00", Enabled: false},
		"sunday":    {Start: "09:00", End: "17:00", Enabled: false},
	}
}

// BusinessSchedule defines business operating hours and holidays.
type BusinessSchedule struct {
	ID        int64                  `json:"id"`
	Name      string                 `json:"name"`
	Timezone  string                 `json:"timezone"`
	Hours     map[string]DaySchedule `json:"hours"`
	IsDefault bool                   `json:"is_default"`
	IsActive  bool                   `json:"is_active"`

	Holidays []Holiday `json:"holidays,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// IsWithinBusinessHours checks if a given time falls within the schedule.
func (bs *BusinessSchedule) IsWithinBusinessHours(t time.Time) bool {
	loc, err := time.LoadLocation(bs.Timezone)
	if err != nil {
		return false
	}
	t = t.In(loc)

	dayName := strings.ToLower(t.Weekday().String())
	dayConfig, ok := bs.Hours[dayName]
	if !ok || !dayConfig.Enabled {
		return false
	}

	dateStr := t.Format("2006-01-02")
	for _, h := range bs.Holidays {
		if h.Date.Format("2006-01-02") == dateStr {
			return false
		}
	}

	timeStr := t.Format("15:04")
	return timeStr >= dayConfig.Start && timeStr <= dayConfig.End
}

// Holiday represents a day excluded from business hours.
type Holiday struct {
	ID                 int64     `json:"id"`
	BusinessScheduleID int64     `json:"business_schedule_id"`
	Name               string    `json:"name"`
	Date               time.Time `json:"date"`
	IsRecurring        bool      `json:"is_recurring"`
	CreatedAt          time.Time `json:"created_at"`
}
