package services

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/escalated-dev/escalated-go/internal/sqldialect"
	"github.com/escalated-dev/escalated-go/models"
)

var (
	// ErrMergeIntoSelf is returned when a ticket would be merged into itself.
	ErrMergeIntoSelf = errors.New("cannot merge a ticket into itself")
	// ErrMergeTicketNotFound is returned when the source ticket does not exist
	// or the target reference names no unmerged ticket.
	ErrMergeTicketNotFound = errors.New("ticket not found")
)

// TicketMergeService merges one ticket into another, as escalated-laravel's
// TicketMergeService does: every reply moves from the source to the target,
// each ticket gets an internal system note, and the source is closed with
// merged_into_id pointing at the target. All of it commits together.
type TicketMergeService struct {
	DB *sql.DB
	// Prefix is the table prefix ("escalated_" when empty).
	Prefix string
	// Tickets records the activity and fires ticket.updated for both tickets.
	// Nil skips both.
	Tickets *TicketService
}

func (m *TicketMergeService) q(query string) string {
	if m.Prefix != "" && m.Prefix != "escalated_" {
		query = strings.ReplaceAll(query, "escalated_", m.Prefix)
	}
	return sqldialect.Rebind(m.DB, query)
}

type mergeTicket struct {
	id        int64
	reference string
	merged    bool
}

func (m *TicketMergeService) find(ctx context.Context, where string, arg any) (*mergeTicket, error) {
	var t mergeTicket
	var mergedInto sql.NullInt64
	err := m.DB.QueryRowContext(ctx, m.q(`SELECT id, reference, merged_into_id FROM escalated_tickets WHERE `+where), arg).
		Scan(&t.id, &t.reference, &mergedInto)
	if err == sql.ErrNoRows {
		return nil, ErrMergeTicketNotFound
	}
	if err != nil {
		return nil, err
	}
	t.merged = mergedInto.Valid
	return &t, nil
}

// Merge merges the source ticket into the unmerged ticket whose reference is
// targetReference, and returns the target's id and reference.
func (m *TicketMergeService) Merge(ctx context.Context, sourceID int64, targetReference string, mergedBy *models.UserID) (int64, string, error) {
	source, err := m.find(ctx, "id = ?", sourceID)
	if err != nil {
		return 0, "", err
	}
	target, err := m.find(ctx, "reference = ?", strings.TrimSpace(targetReference))
	if err != nil {
		return 0, "", err
	}
	if target.merged {
		return 0, "", ErrMergeTicketNotFound
	}
	if target.id == source.id {
		return 0, "", ErrMergeIntoSelf
	}

	tx, err := m.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, "", err
	}
	defer func() { _ = tx.Rollback() }()

	// The system notes carry no author, as in Laravel; who merged is on the
	// activity log.
	now := time.Now()
	steps := []struct {
		query string
		args  []any
	}{
		{`UPDATE escalated_replies SET ticket_id = ? WHERE ticket_id = ?`, []any{target.id, source.id}},
		{`INSERT INTO escalated_replies (ticket_id, body, is_internal, is_system, is_pinned, created_at, updated_at) VALUES (?, ?, TRUE, TRUE, FALSE, ?, ?)`,
			[]any{target.id, fmt.Sprintf("Ticket %s was merged into this ticket.", source.reference), now, now}},
		{`INSERT INTO escalated_replies (ticket_id, body, is_internal, is_system, is_pinned, created_at, updated_at) VALUES (?, ?, TRUE, TRUE, FALSE, ?, ?)`,
			[]any{source.id, fmt.Sprintf("This ticket was merged into %s.", target.reference), now, now}},
		{`UPDATE escalated_tickets SET status = ?, merged_into_id = ?, closed_at = COALESCE(closed_at, ?), updated_at = ? WHERE id = ?`,
			[]any{models.StatusClosed, target.id, now, now, source.id}},
	}
	for _, step := range steps {
		if _, err := tx.ExecContext(ctx, m.q(step.query), step.args...); err != nil {
			return 0, "", err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, "", err
	}

	if ts := m.Tickets; ts != nil {
		ts.logActivity(ctx, source.id, models.ActionTicketMerged, mergedBy, map[string]any{"merged_into": target.reference, "merged_into_id": target.id})
		ts.logActivity(ctx, target.id, models.ActionTicketMerged, mergedBy, map[string]any{"merged_from": source.reference, "merged_from_id": source.id})
		for _, id := range []int64{source.id, target.id} {
			if t, err := ts.store.GetTicket(ctx, id); err == nil && t != nil {
				ts.emitUpdated(t)
			}
		}
	}
	return target.id, target.reference, nil
}

// MergeCandidate is one ticket offered by SearchTargets.
type MergeCandidate struct {
	ID        int64     `json:"id"`
	Reference string    `json:"reference"`
	Subject   string    `json:"subject"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// SearchTargets returns up to 10 unmerged tickets whose reference or subject
// contains term, ignoring case (Laravel TicketMergeController::search).
func (m *TicketMergeService) SearchTargets(ctx context.Context, term string) ([]MergeCandidate, error) {
	like := "%" + strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(strings.ToLower(term)) + "%"
	rows, err := m.DB.QueryContext(ctx, m.q(`SELECT id, reference, subject, status, created_at FROM escalated_tickets
		WHERE merged_into_id IS NULL AND (LOWER(reference) LIKE ? ESCAPE '!' OR LOWER(subject) LIKE ? ESCAPE '!')
		ORDER BY id DESC LIMIT 10`), like, like)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MergeCandidate{}
	for rows.Next() {
		var c MergeCandidate
		var status int
		if err := rows.Scan(&c.ID, &c.Reference, &c.Subject, &status, &c.CreatedAt); err != nil {
			return nil, err
		}
		c.Status = models.StatusName[status]
		out = append(out, c)
	}
	return out, rows.Err()
}
