package store

import (
	"errors"
	"fmt"
	"testing"
)

// pgxError has the shape of pgconn.PgError: a SQLState method and the index
// name quoted in the message. CI runs lib/pq, so this is the only coverage the
// pgx path gets.
type pgxError struct {
	code, message string
}

func (e *pgxError) Error() string    { return "ERROR: " + e.message + " (SQLSTATE " + e.code + ")" }
func (e *pgxError) SQLState() string { return e.code }

func TestReferenceTaken(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		prefix string
		want   bool
	}{
		{name: "nil", err: nil, prefix: "escalated_", want: false},
		{
			name:   "pgx reference index",
			err:    &pgxError{code: "23505", message: `duplicate key value violates unique constraint "idx_escalated_tkt_ref"`},
			prefix: "escalated_",
			want:   true,
		},
		{
			name:   "pgx reference index, wrapped",
			err:    fmt.Errorf("creating ticket: %w", &pgxError{code: "23505", message: `duplicate key value violates unique constraint "idx_escalated_tkt_ref"`}),
			prefix: "escalated_",
			want:   true,
		},
		{
			name:   "pgx guest token index",
			err:    &pgxError{code: "23505", message: `duplicate key value violates unique constraint "idx_escalated_tkt_guest"`},
			prefix: "escalated_",
			want:   false,
		},
		{
			name:   "pgx another table's reference index",
			err:    &pgxError{code: "23505", message: `duplicate key value violates unique constraint "idx_other_tkt_ref"`},
			prefix: "escalated_",
			want:   false,
		},
		{
			name:   "pgx not null violation naming the index",
			err:    &pgxError{code: "23502", message: `null value in "idx_escalated_tkt_ref"`},
			prefix: "escalated_",
			want:   false,
		},
		{
			name:   "sqlite reference",
			err:    errors.New("constraint failed: UNIQUE constraint failed: escalated_tickets.reference (2067)"),
			prefix: "escalated_",
			want:   true,
		},
		{
			name:   "sqlite guest token",
			err:    errors.New("constraint failed: UNIQUE constraint failed: escalated_tickets.guest_token (2067)"),
			prefix: "escalated_",
			want:   false,
		},
		{
			name:   "sqlite custom prefix",
			err:    errors.New("UNIQUE constraint failed: help_tickets.reference"),
			prefix: "help_",
			want:   true,
		},
		{name: "unrelated", err: errors.New("connection refused"), prefix: "escalated_", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := referenceTaken(tt.err, tt.prefix); got != tt.want {
				t.Errorf("referenceTaken(%v, %q) = %v, want %v", tt.err, tt.prefix, got, tt.want)
			}
		})
	}
}
