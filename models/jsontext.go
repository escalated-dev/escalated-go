package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// JSONText is a JSON column that can be scanned from any driver.
//
// json.RawMessage is a []byte, and database/sql will only scan a []byte into
// it. SQLite's driver hands back []byte for a text column, so that worked;
// lib/pq hands back a string, and every read of a JSON column failed with
// "unsupported Scan, storing driver.Value type string into type
// *json.RawMessage" -- on the database this package documents as its default.
//
// It marshals and unmarshals exactly as json.RawMessage does, so nothing about
// the JSON these models produce changes.
type JSONText []byte

// Scan implements sql.Scanner.
func (j *JSONText) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*j = nil
	case []byte:
		// Copied: the driver may reuse the buffer for the next row.
		*j = append(JSONText(nil), v...)
	case string:
		*j = JSONText(v)
	default:
		return fmt.Errorf("models: cannot scan %T into a JSON column", src)
	}

	return nil
}

// Value implements driver.Valuer. A JSON column is written as text, which every
// driver here accepts.
func (j JSONText) Value() (driver.Value, error) {
	if len(j) == 0 {
		return nil, nil
	}

	return string(j), nil
}

// MarshalJSON writes the value through unchanged, as json.RawMessage does.
func (j JSONText) MarshalJSON() ([]byte, error) {
	if len(j) == 0 {
		return []byte("null"), nil
	}

	return j, nil
}

// UnmarshalJSON keeps the value unchanged, as json.RawMessage does.
func (j *JSONText) UnmarshalJSON(data []byte) error {
	if j == nil {
		return fmt.Errorf("models: UnmarshalJSON on a nil JSONText")
	}

	*j = append((*j)[0:0], data...)

	return nil
}

// Raw returns the value as a JSONText, for callers that want one.
func (j JSONText) Raw() json.RawMessage {
	return json.RawMessage(j)
}
