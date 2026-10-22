package models

import (
	"testing"
	"time"
)

func weekdays(start, end string) map[string]DaySchedule {
	open := DaySchedule{Start: start, End: end}
	return map[string]DaySchedule{
		"monday": open, "tuesday": open, "wednesday": open, "thursday": open, "friday": open,
		"saturday": {}, "sunday": {},
	}
}

func utc(y int, m time.Month, d, h, min int) time.Time {
	return time.Date(y, m, d, h, min, 0, 0, time.UTC)
}

func holiday(y int, m time.Month, d int, recurring bool) Holiday {
	return Holiday{Name: "h", Date: time.Date(y, m, d, 0, 0, 0, 0, time.UTC), IsRecurring: recurring}
}

func wantTime(t *testing.T, what string, got, want time.Time) {
	t.Helper()
	if !got.Equal(want) {
		t.Errorf("%s: got %s, want %s", what, got.UTC().Format(time.RFC3339), want.UTC().Format(time.RFC3339))
	}
	if got.Location() != time.UTC {
		t.Errorf("%s: result is in %s, want UTC", what, got.Location())
	}
}

func TestAddBusinessMinutesWithinOneDay(t *testing.T) {
	s := &BusinessSchedule{Timezone: "UTC", Hours: weekdays("09:00", "17:00")}
	wantTime(t, "inside hours", s.AddBusinessMinutes(utc(2026, 4, 7, 10, 0), 90), utc(2026, 4, 7, 11, 30))
	wantTime(t, "before opening", s.AddBusinessMinutes(utc(2026, 4, 7, 7, 0), 30), utc(2026, 4, 7, 9, 30))
	wantTime(t, "exactly to closing", s.AddBusinessMinutes(utc(2026, 4, 7, 16, 0), 60), utc(2026, 4, 7, 17, 0))
}

func TestAddBusinessMinutesAfterHoursRollsToNextOpening(t *testing.T) {
	s := &BusinessSchedule{Timezone: "UTC", Hours: weekdays("09:00", "17:00")}
	wantTime(t, "evening", s.AddBusinessMinutes(utc(2026, 4, 7, 20, 0), 60), utc(2026, 4, 8, 10, 0))
	wantTime(t, "spill over closing", s.AddBusinessMinutes(utc(2026, 4, 7, 16, 0), 120), utc(2026, 4, 8, 10, 0))
	wantTime(t, "two full days", s.AddBusinessMinutes(utc(2026, 4, 7, 9, 0), 16*60), utc(2026, 4, 8, 17, 0))
}

func TestAddBusinessMinutesSkipsTheWeekend(t *testing.T) {
	s := &BusinessSchedule{Timezone: "UTC", Hours: weekdays("09:00", "17:00")}
	// Friday 2026-04-10 16:00 + 2h: one hour on Friday, one on Monday.
	wantTime(t, "friday afternoon", s.AddBusinessMinutes(utc(2026, 4, 10, 16, 0), 120), utc(2026, 4, 13, 10, 0))
	wantTime(t, "saturday", s.AddBusinessMinutes(utc(2026, 4, 11, 12, 0), 60), utc(2026, 4, 13, 10, 0))
}

func TestAddBusinessMinutesSkipsHolidays(t *testing.T) {
	s := &BusinessSchedule{Timezone: "UTC", Hours: weekdays("09:00", "17:00"),
		Holidays: []Holiday{holiday(2026, 4, 13, false)}}
	wantTime(t, "fixed holiday monday", s.AddBusinessMinutes(utc(2026, 4, 10, 16, 0), 120), utc(2026, 4, 14, 10, 0))

	// 2026-12-25 is a Friday. A recurring holiday from another year applies;
	// a one-off holiday from another year does not.
	recurring := &BusinessSchedule{Timezone: "UTC", Hours: weekdays("09:00", "17:00"),
		Holidays: []Holiday{holiday(2019, 12, 25, true)}}
	wantTime(t, "recurring", recurring.AddBusinessMinutes(utc(2026, 12, 24, 16, 0), 120), utc(2026, 12, 28, 10, 0))
	oneOff := &BusinessSchedule{Timezone: "UTC", Hours: weekdays("09:00", "17:00"),
		Holidays: []Holiday{holiday(2025, 12, 25, false)}}
	wantTime(t, "one-off from another year", oneOff.AddBusinessMinutes(utc(2026, 12, 24, 16, 0), 120), utc(2026, 12, 25, 10, 0))
}

func TestAddBusinessMinutesOvernightWindow(t *testing.T) {
	// Friday 22:00 until Saturday 06:00, nothing else.
	s := &BusinessSchedule{Timezone: "UTC", Hours: map[string]DaySchedule{"friday": {Start: "22:00", End: "06:00"}}}
	wantTime(t, "into saturday", s.AddBusinessMinutes(utc(2026, 4, 10, 23, 0), 120), utc(2026, 4, 11, 1, 0))
	// Saturday 03:00 is inside the window Friday opened.
	wantTime(t, "inside yesterday's window", s.AddBusinessMinutes(utc(2026, 4, 11, 3, 0), 60), utc(2026, 4, 11, 4, 0))
	wantTime(t, "to next week", s.AddBusinessMinutes(utc(2026, 4, 11, 5, 0), 120), utc(2026, 4, 17, 23, 0))
	// The window belongs to the day it starts on: a Friday holiday closes it.
	s.Holidays = []Holiday{holiday(2026, 4, 10, false)}
	wantTime(t, "holiday friday", s.AddBusinessMinutes(utc(2026, 4, 11, 3, 0), 60), utc(2026, 4, 17, 23, 0))
}

func TestAddBusinessMinutesMidnightEnd(t *testing.T) {
	s := &BusinessSchedule{Timezone: "UTC", Hours: map[string]DaySchedule{"monday": {Start: "20:00", End: "24:00"}}}
	wantTime(t, "24:00", s.AddBusinessMinutes(utc(2026, 4, 6, 23, 0), 120), utc(2026, 4, 13, 21, 0))
	if !s.IsWithinBusinessHours(utc(2026, 4, 6, 23, 59)) {
		t.Error("23:59 should be inside a window ending 24:00")
	}
}

func TestAddBusinessMinutesInAnotherTimezone(t *testing.T) {
	s := &BusinessSchedule{Timezone: "America/New_York", Hours: weekdays("09:00", "17:00")}
	// 12:00 UTC is 08:00 EDT: opens at 13:00 UTC, one hour later is 14:00 UTC.
	wantTime(t, "new york morning", s.AddBusinessMinutes(utc(2026, 4, 6, 12, 0), 60), utc(2026, 4, 6, 14, 0))
	// Monday 23:00 UTC is still Monday 19:00 in New York: after hours.
	wantTime(t, "local evening", s.AddBusinessMinutes(utc(2026, 4, 6, 23, 0), 60), utc(2026, 4, 7, 14, 0))
	// Tuesday 02:00 UTC is Monday 22:00 in New York, so a Tuesday holiday is
	// judged on the local date.
	s.Holidays = []Holiday{holiday(2026, 4, 7, false)}
	wantTime(t, "local holiday", s.AddBusinessMinutes(utc(2026, 4, 7, 2, 0), 60), utc(2026, 4, 8, 14, 0))
}

func TestAddBusinessMinutesAcrossDaylightSaving(t *testing.T) {
	ny := &BusinessSchedule{Timezone: "America/New_York", Hours: weekdays("09:00", "17:00")}
	// Friday 2026-03-06 16:00 EST (21:00 UTC); DST starts Sunday 03-08, so
	// Monday opens at 09:00 EDT = 13:00 UTC.
	wantTime(t, "spring forward weekend", ny.AddBusinessMinutes(utc(2026, 3, 6, 21, 0), 120), utc(2026, 3, 9, 14, 0))
	// Friday 2026-10-30 16:00 EDT (20:00 UTC); DST ends Sunday 11-01, so
	// Monday opens at 09:00 EST = 14:00 UTC.
	wantTime(t, "fall back weekend", ny.AddBusinessMinutes(utc(2026, 10, 30, 20, 0), 120), utc(2026, 11, 2, 15, 0))

	// On the transition day itself the window is measured in real time:
	// 01:00-05:00 on 2026-03-08 is three hours, not four.
	spring := &BusinessSchedule{Timezone: "America/New_York", Hours: map[string]DaySchedule{"sunday": {Start: "01:00", End: "05:00"}}}
	wantTime(t, "short day", spring.AddBusinessMinutes(utc(2026, 3, 8, 6, 0), 180), utc(2026, 3, 8, 9, 0))
	wantTime(t, "short day spills", spring.AddBusinessMinutes(utc(2026, 3, 8, 6, 0), 240), utc(2026, 3, 15, 6, 0))

	// A window opening inside the gap (02:30 does not exist that day) opens
	// when the clocks go forward, 03:00 EDT = 07:00 UTC.
	gap := &BusinessSchedule{Timezone: "America/New_York", Hours: map[string]DaySchedule{"sunday": {Start: "02:30", End: "04:00"}}}
	wantTime(t, "gap start", gap.AddBusinessMinutes(utc(2026, 3, 8, 5, 0), 30), utc(2026, 3, 8, 7, 30))

	// 00:00-04:00 on 2026-11-01 is five hours long.
	fall := &BusinessSchedule{Timezone: "America/New_York", Hours: map[string]DaySchedule{"sunday": {Start: "00:00", End: "04:00"}}}
	wantTime(t, "long day", fall.AddBusinessMinutes(utc(2026, 11, 1, 4, 0), 300), utc(2026, 11, 1, 9, 0))
}

func TestAddBusinessMinutesFallsBackToCalendarTime(t *testing.T) {
	from := utc(2026, 4, 7, 10, 0)
	for name, s := range map[string]*BusinessSchedule{
		"no days":      {Timezone: "UTC", Hours: map[string]DaySchedule{}},
		"start is end": {Timezone: "UTC", Hours: map[string]DaySchedule{"tuesday": {Start: "09:00", End: "09:00"}}},
		"disabled":     {Timezone: "UTC", Hours: map[string]DaySchedule{"tuesday": {Start: "09:00", End: "17:00", Enabled: boolPtr(false)}}},
		"garbage":      {Timezone: "UTC", Hours: map[string]DaySchedule{"tuesday": {Start: "nine", End: "17:00"}}},
	} {
		wantTime(t, name, s.AddBusinessMinutes(from, 90), from.Add(90*time.Minute))
	}
	open := &BusinessSchedule{Timezone: "UTC", Hours: weekdays("09:00", "17:00")}
	wantTime(t, "zero minutes", open.AddBusinessMinutes(utc(2026, 4, 11, 10, 0), 0), utc(2026, 4, 11, 10, 0))
}

func TestAddBusinessMinutesUnknownTimezoneIsUTC(t *testing.T) {
	s := &BusinessSchedule{Timezone: "Mars/Olympus_Mons", Hours: weekdays("09:00", "17:00")}
	wantTime(t, "utc", s.AddBusinessMinutes(utc(2026, 4, 7, 8, 0), 60), utc(2026, 4, 7, 10, 0))
}

func TestIsWithinBusinessHoursEdges(t *testing.T) {
	s := &BusinessSchedule{Timezone: "UTC", Hours: weekdays("09:00", "17:00")}
	cases := map[string]struct {
		at   time.Time
		want bool
	}{
		"opening":       {utc(2026, 4, 7, 9, 0), true},
		"before close":  {utc(2026, 4, 7, 16, 59), true},
		"closing":       {utc(2026, 4, 7, 17, 0), false},
		"saturday noon": {utc(2026, 4, 11, 12, 0), false},
	}
	for name, c := range cases {
		if got := s.IsWithinBusinessHours(c.at); got != c.want {
			t.Errorf("%s: got %v, want %v", name, got, c.want)
		}
	}
	overnight := &BusinessSchedule{Timezone: "UTC", Hours: map[string]DaySchedule{"friday": {Start: "22:00", End: "06:00"}}}
	if !overnight.IsWithinBusinessHours(utc(2026, 4, 11, 3, 0)) {
		t.Error("saturday 03:00 is inside friday's overnight window")
	}
	if overnight.IsWithinBusinessHours(utc(2026, 4, 11, 7, 0)) {
		t.Error("saturday 07:00 is after friday's overnight window")
	}
}

func boolPtr(b bool) *bool { return &b }
