package models

import (
	"testing"
	"time"
)

func TestBusinessScheduleIsWithinBusinessHours(t *testing.T) {
	schedule := &BusinessSchedule{
		Timezone: "UTC",
		Hours:    DefaultBusinessHours(),
		Holidays: nil,
	}

	// Monday 10:00 UTC -> within hours
	monday := time.Date(2026, 4, 6, 10, 0, 0, 0, time.UTC)
	if !schedule.IsWithinBusinessHours(monday) {
		t.Error("expected Monday 10:00 to be within business hours")
	}

	// Saturday 10:00 UTC -> outside hours
	saturday := time.Date(2026, 4, 4, 10, 0, 0, 0, time.UTC)
	if schedule.IsWithinBusinessHours(saturday) {
		t.Error("expected Saturday 10:00 to be outside business hours")
	}

	// Monday 20:00 UTC -> outside hours
	lateMonday := time.Date(2026, 4, 6, 20, 0, 0, 0, time.UTC)
	if schedule.IsWithinBusinessHours(lateMonday) {
		t.Error("expected Monday 20:00 to be outside business hours")
	}
}

func TestBusinessScheduleHolidayExclusion(t *testing.T) {
	schedule := &BusinessSchedule{
		Timezone: "UTC",
		Hours:    DefaultBusinessHours(),
		Holidays: []Holiday{
			{Name: "Test Holiday", Date: time.Date(2026, 4, 6, 0, 0, 0, 0, time.UTC)},
		},
	}

	monday := time.Date(2026, 4, 6, 10, 0, 0, 0, time.UTC)
	if schedule.IsWithinBusinessHours(monday) {
		t.Error("expected holiday Monday to be outside business hours")
	}
}

func TestDefaultBusinessHours(t *testing.T) {
	hours := DefaultBusinessHours()

	if !hours["monday"].Enabled {
		t.Error("expected monday to be enabled")
	}
	if hours["saturday"].Enabled {
		t.Error("expected saturday to be disabled")
	}
	if hours["wednesday"].Start != "09:00" {
		t.Errorf("expected wednesday start 09:00, got %s", hours["wednesday"].Start)
	}
}
