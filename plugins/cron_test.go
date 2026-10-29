package plugins

import (
	"testing"
	"time"
)

func at(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestCronDue(t *testing.T) {
	cases := []struct {
		expr string
		at   string
		want bool
	}{
		{"* * * * *", "2026-10-29T13:37:00Z", true},
		{"37 13 * * *", "2026-10-29T13:37:45Z", true},
		{"38 13 * * *", "2026-10-29T13:37:00Z", false},
		{"*/15 * * * *", "2026-10-29T13:45:00Z", true},
		{"*/15 * * * *", "2026-10-29T13:46:00Z", false},
		{"0-10/5 9-17 * * *", "2026-10-29T09:10:00Z", true},
		{"0-10/5 9-17 * * *", "2026-10-29T18:10:00Z", false},
		{"1,2,3 * * * *", "2026-10-29T13:02:00Z", true},
		// 2026-10-29 is a Thursday; 2026-11-01 a Sunday.
		{"0 9 * * thu", "2026-10-29T09:00:00Z", true},
		{"0 9 * * MON-WED", "2026-10-29T09:00:00Z", false},
		{"0 0 * * 7", "2026-11-01T00:00:00Z", true},
		{"0 0 1 nov *", "2026-11-01T00:00:00Z", true},
		// Both day fields restricted: either one matching is enough.
		{"0 0 13 * 4", "2026-10-29T00:00:00Z", true},
		{"0 0 13 * 5", "2026-10-29T00:00:00Z", false},
		{"0 0 29 * *", "2026-10-29T00:00:00Z", true},
		// Evaluated in UTC whatever the time's zone.
		{"0 9 * * *", "2026-10-29T05:00:00-04:00", true},
		{"@hourly", "2026-10-29T13:00:00Z", true},
		{"@daily", "2026-10-29T13:00:00Z", false},
		{"@weekly", "2026-11-01T00:00:00Z", true},
	}
	for _, c := range cases {
		got, err := cronDue(c.expr, at(t, c.at))
		if err != nil || got != c.want {
			t.Errorf("cronDue(%q, %s) = %v, %v; want %v", c.expr, c.at, got, err, c.want)
		}
	}
}

func TestCronDueRefusesWhatItCannotRead(t *testing.T) {
	for _, bad := range []string{"", "* * * *", "61 * * * *", "* * * * * *", "a b c d e", "*/0 * * * *", "5-1 * * * *"} {
		if _, err := cronDue(bad, at(t, "2026-10-29T13:37:00Z")); err == nil {
			t.Errorf("cronDue(%q) read an unreadable expression", bad)
		}
	}
}
