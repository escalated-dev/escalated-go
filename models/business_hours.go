package models

import (
	"strconv"
	"strings"
	"time"
)

// maxBusinessScanDays bounds AddBusinessMinutes. A schedule whose windows are
// all closed by holidays for ten years is treated like one with no hours.
const maxBusinessScanDays = 3660

var weekdayKeys = [...]string{"sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday"}

// AddBusinessMinutes returns the instant that lies minutes of business time
// after from (Laravel BusinessHoursCalculator::addBusinessHours, in minutes).
//
// Opening hours are wall-clock times in the schedule's time zone, but time is
// counted in real elapsed minutes, so a window on a daylight-saving change day
// is an hour shorter or longer than its wall-clock span. A window belongs to
// the local date it opens on: a holiday on that date closes it, overnight part
// included. A schedule with no usable window falls back to calendar time, so
// a policy is never left without a deadline. The result is in UTC.
func (bs *BusinessSchedule) AddBusinessMinutes(from time.Time, minutes float64) time.Time {
	remaining := time.Duration(minutes * float64(time.Minute))
	if remaining <= 0 {
		return from.UTC()
	}
	calendar := from.Add(remaining).UTC()
	if !bs.hasOpenDay() {
		return calendar
	}

	loc := bs.location()
	local := from.In(loc)
	// Start a day early: an overnight window opened yesterday may still be open.
	day := civilDate(local.Year(), local.Month(), local.Day()-1)
	cursor := from
	for i := 0; i < maxBusinessScanDays; i++ {
		if start, end, ok := bs.window(day, loc); ok && end.After(cursor) {
			if cursor.After(start) {
				start = cursor
			}
			available := end.Sub(start)
			if remaining <= available {
				return start.Add(remaining).UTC()
			}
			remaining -= available
			cursor = end
		}
		day = day.AddDate(0, 0, 1)
	}
	return calendar
}

// location is the schedule's time zone, UTC when it is empty or unknown.
func (bs *BusinessSchedule) location() *time.Location {
	if bs.Timezone == "" {
		return time.UTC
	}
	loc, err := time.LoadLocation(bs.Timezone)
	if err != nil {
		return time.UTC
	}
	return loc
}

// hasOpenDay reports whether any weekday has a usable window.
func (bs *BusinessSchedule) hasOpenDay() bool {
	for _, key := range weekdayKeys {
		if _, _, ok := dayBounds(bs.Hours[key]); ok {
			return true
		}
	}
	return false
}

// window returns the opening window that starts on the civil date day (a
// time.Time whose year, month and day are the date), or ok=false when the
// day is closed or a holiday.
func (bs *BusinessSchedule) window(day time.Time, loc *time.Location) (time.Time, time.Time, bool) {
	startMin, endMin, ok := dayBounds(bs.Hours[weekdayKeys[day.Weekday()]])
	if !ok {
		return time.Time{}, time.Time{}, false
	}
	for _, h := range bs.Holidays {
		if h.matches(day) {
			return time.Time{}, time.Time{}, false
		}
	}
	start := wallClock(day, startMin, loc)
	endDay := day
	if endMin <= startMin {
		endDay = day.AddDate(0, 0, 1)
	}
	end := wallClock(endDay, endMin, loc)
	if !end.After(start) {
		return time.Time{}, time.Time{}, false
	}
	return start, end, true
}

// dayBounds parses a day's start and end into minutes after midnight. End
// 24:00 is 1440. Equal start and end, or anything unparseable, is closed.
func dayBounds(d DaySchedule) (int, int, bool) {
	if !d.Open() {
		return 0, 0, false
	}
	start, ok := clockMinutes(d.Start)
	if !ok || start >= 24*60 {
		return 0, 0, false
	}
	end, ok := clockMinutes(d.End)
	if !ok || end == start {
		return 0, 0, false
	}
	return start, end, true
}

// clockMinutes parses "HH:MM" (00:00 to 24:00).
func clockMinutes(s string) (int, bool) {
	hh, mm, found := strings.Cut(strings.TrimSpace(s), ":")
	if !found {
		return 0, false
	}
	h, err1 := strconv.Atoi(hh)
	m, err2 := strconv.Atoi(mm)
	if err1 != nil || err2 != nil || h < 0 || m < 0 || m > 59 || h > 24 || (h == 24 && m != 0) {
		return 0, false
	}
	return h*60 + m, true
}

// civilDate is a calendar date carried in a time.Time (noon UTC, so adding
// days never trips over a time zone).
func civilDate(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 12, 0, 0, 0, time.UTC)
}

// wallClock is the instant the clock in loc reads minutes past midnight on
// day. 1440 is the next midnight. A wall time skipped by a daylight-saving
// change resolves to the moment the clocks go forward.
func wallClock(day time.Time, minutes int, loc *time.Location) time.Time {
	if minutes >= 24*60 {
		day = day.AddDate(0, 0, 1)
		minutes -= 24 * 60
	}
	h, m := minutes/60, minutes%60
	t := time.Date(day.Year(), day.Month(), day.Day(), h, m, 0, 0, loc)
	if t.Hour() == h && t.Minute() == m && t.Day() == day.Day() {
		return t
	}
	// In a gap: t landed either side of the transition. ZoneBounds gives the
	// transition instant whichever side it is.
	zoneStart, zoneEnd := t.ZoneBounds()
	wanted := time.Date(day.Year(), day.Month(), day.Day(), h, m, 0, 0, time.UTC)
	got := time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), 0, 0, time.UTC)
	if got.Before(wanted) && !zoneEnd.IsZero() {
		return zoneEnd
	}
	if !zoneStart.IsZero() {
		return zoneStart
	}
	return t
}
