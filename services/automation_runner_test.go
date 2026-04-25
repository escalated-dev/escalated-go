package services

import (
	"testing"
	"time"
)

func TestFlipOperator(t *testing.T) {
	cases := map[string]string{
		">":   "<",
		">=":  "<=",
		"<":   ">",
		"<=":  ">=",
		"=":   "=",
		"foo": "<", // default
	}
	for input, want := range cases {
		if got := flipOperator(input); got != want {
			t.Errorf("flipOperator(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestHoursAgo(t *testing.T) {
	threshold := hoursAgo(48)
	delta := time.Since(threshold)
	if delta < 47*time.Hour || delta > 49*time.Hour {
		t.Errorf("hoursAgo(48) returned %v, expected ~48h ago", threshold)
	}
}

func TestToInt(t *testing.T) {
	cases := []struct {
		in   interface{}
		want int
	}{
		{42, 42},
		{int64(42), 42},
		{float64(42), 42},
		{"42", 42},
		{"not-a-number", 0},
		{nil, 0},
	}
	for _, c := range cases {
		if got := toInt(c.in); got != c.want {
			t.Errorf("toInt(%v) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestToString(t *testing.T) {
	if got := toString("hello"); got != "hello" {
		t.Errorf("toString(\"hello\") = %q", got)
	}
	if got := toString(42); got != "42" {
		t.Errorf("toString(42) = %q, want %q", got, "42")
	}
}
