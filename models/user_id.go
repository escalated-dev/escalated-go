package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strconv"
)

// UserID is a host-application user identifier. Host user primary keys may be
// integers or UUID/strings, so UserID stores the value as a string internally
// and accepts either form from JSON and the database. Integer-keyed hosts are
// unaffected: numeric ids round-trip as JSON numbers and store fine in BIGINT.
type UserID string

func (u *UserID) Scan(value any) error {
	switch v := value.(type) {
	case nil:
		*u = ""
	case int64:
		*u = UserID(strconv.FormatInt(v, 10))
	case string:
		*u = UserID(v)
	case []byte:
		*u = UserID(string(v))
	default:
		return fmt.Errorf("unsupported UserID scan type %T", value)
	}
	return nil
}

func (u UserID) Value() (driver.Value, error) {
	if u == "" {
		return nil, nil
	}
	return string(u), nil
}

// MarshalJSON emits a JSON number when the id is purely numeric (back-compat for
// integer-keyed hosts) and a JSON string otherwise (UUID/string ids).
func (u UserID) MarshalJSON() ([]byte, error) {
	if u == "" {
		return []byte("null"), nil
	}
	if _, err := strconv.ParseInt(string(u), 10, 64); err == nil {
		return []byte(string(u)), nil
	}
	return json.Marshal(string(u))
}

func (u *UserID) UnmarshalJSON(b []byte) error {
	s := string(b)
	if s == "null" {
		*u = ""
		return nil
	}
	if len(b) > 0 && b[0] == '"' {
		var str string
		if err := json.Unmarshal(b, &str); err != nil {
			return err
		}
		*u = UserID(str)
		return nil
	}
	*u = UserID(s) // bare number literal
	return nil
}

// Empty reports whether the id is unset.
func (u UserID) Empty() bool { return u == "" }
