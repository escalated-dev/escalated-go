package services

import (
	"testing"
)

func TestMacroToInt(t *testing.T) {
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
		if got := macroToInt(c.in); got != c.want {
			t.Errorf("macroToInt(%v) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestMacroToString(t *testing.T) {
	if got := macroToString("hello"); got != "hello" {
		t.Errorf("macroToString(\"hello\") = %q", got)
	}
	if got := macroToString(42); got != "42" {
		t.Errorf("macroToString(42) = %q, want %q", got, "42")
	}
}
