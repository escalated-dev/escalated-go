package models

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/escalated-dev/escalated-go/migrations"
)

func TestUserID_Scan(t *testing.T) {
	var u UserID
	if err := u.Scan(int64(5)); err != nil || u != "5" {
		t.Fatalf("Scan(int64(5)) = %q, err = %v", u, err)
	}
	if err := u.Scan("uuid-x"); err != nil || u != "uuid-x" {
		t.Fatalf("Scan(uuid-x) = %q, err = %v", u, err)
	}
	if err := u.Scan(nil); err != nil || u != "" {
		t.Fatalf("Scan(nil) = %q, err = %v", u, err)
	}
}

func TestUserID_Value(t *testing.T) {
	u := UserID("5")
	v, err := u.Value()
	if err != nil || v != "5" {
		t.Fatalf("Value(5) = %v, err = %v", v, err)
	}
	empty := UserID("")
	v, err = empty.Value()
	if err != nil || v != nil {
		t.Fatalf("Value(empty) = %v, err = %v", v, err)
	}
}

func TestUserID_MarshalJSON(t *testing.T) {
	cases := []struct {
		in   UserID
		want string
	}{
		{"5", "5"},
		{"abc-1", `"abc-1"`},
		{"", "null"},
	}
	for _, tc := range cases {
		b, err := tc.in.MarshalJSON()
		if err != nil {
			t.Fatalf("MarshalJSON(%q): %v", tc.in, err)
		}
		if string(b) != tc.want {
			t.Fatalf("MarshalJSON(%q) = %s, want %s", tc.in, b, tc.want)
		}
	}
}

func TestUserID_UnmarshalJSON(t *testing.T) {
	var u UserID
	if err := json.Unmarshal([]byte(`5`), &u); err != nil || u != "5" {
		t.Fatalf("Unmarshal 5: %q, err = %v", u, err)
	}
	if err := json.Unmarshal([]byte(`"abc"`), &u); err != nil || u != "abc" {
		t.Fatalf("Unmarshal abc: %q, err = %v", u, err)
	}
	if err := json.Unmarshal([]byte(`null`), &u); err != nil || u != "" {
		t.Fatalf("Unmarshal null: %q, err = %v", u, err)
	}
}

func TestUserIDColumnType(t *testing.T) {
	prev, had := os.LookupEnv("ESCALATED_USER_KEY_TYPE")
	defer func() {
		if had {
			_ = os.Setenv("ESCALATED_USER_KEY_TYPE", prev)
		} else {
			_ = os.Unsetenv("ESCALATED_USER_KEY_TYPE")
		}
	}()

	_ = os.Unsetenv("ESCALATED_USER_KEY_TYPE")
	if got := migrations.UserIDColumnType(); got != "BIGINT" {
		t.Fatalf("default = %q, want BIGINT", got)
	}
	_ = os.Setenv("ESCALATED_USER_KEY_TYPE", "uuid")
	if got := migrations.UserIDColumnType(); got != "VARCHAR(255)" {
		t.Fatalf("uuid = %q, want VARCHAR(255)", got)
	}
}
