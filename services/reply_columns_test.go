package services

import (
	"database/sql"
	"testing"

	"github.com/escalated-dev/escalated-go/internal/sqldialect"
	"github.com/escalated-dev/escalated-go/models"
)

// The automation and macro actions below wrote replies with raw SQL naming
// columns escalated_replies has never had (is_internal_note, metadata). Every
// one of them failed on insert, on SQLite and PostgreSQL alike, and nothing ran
// them against a real table. These tests do.

type storedReply struct {
	body       string
	authorID   sql.NullString
	isInternal bool
	isSystem   bool
}

func onlyReplyOn(t *testing.T, db *sql.DB, ticketID int64) storedReply {
	t.Helper()
	rows, err := db.Query(
		sqldialect.Rebind(db, `SELECT body, author_id, is_internal, is_system FROM escalated_replies WHERE ticket_id = ?`),
		ticketID,
	)
	if err != nil {
		t.Fatalf("query replies: %v", err)
	}
	defer rows.Close()

	var got []storedReply
	for rows.Next() {
		var r storedReply
		if err := rows.Scan(&r.body, &r.authorID, &r.isInternal, &r.isSystem); err != nil {
			t.Fatalf("scan reply: %v", err)
		}
		got = append(got, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate replies: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want exactly 1 reply on ticket %d, got %d", ticketID, len(got))
	}
	return got[0]
}

func TestAutomationAddNoteWritesAnInternalSystemNote(t *testing.T) {
	db := newWorkflowTestDB(t)
	ticket := insertRunnerTicket(t, db, 0, 1)
	runner := NewAutomationRunner(db, nil)

	err := runner.runAction(models.Automation{ID: 7}, *ticket, models.AutomationAction{Type: "add_note", Value: "No reply in 48 hours"})
	if err != nil {
		t.Fatalf("add_note: %v", err)
	}

	got := onlyReplyOn(t, db, ticket.ID)
	if got.body != "No reply in 48 hours" || !got.isInternal || !got.isSystem || got.authorID.Valid {
		t.Fatalf("want an authorless internal system note, got %+v", got)
	}
}

func TestMacroAddNoteWritesAnInternalNoteByTheAgent(t *testing.T) {
	db := newWorkflowTestDB(t)
	ticket := insertRunnerTicket(t, db, 0, 1)
	macros := NewMacroService(db, nil)

	if err := macros.runAction(models.MacroAction{Type: "add_note", Value: "Checked the logs"}, ticket.ID, models.UserID("42")); err != nil {
		t.Fatalf("add_note: %v", err)
	}

	got := onlyReplyOn(t, db, ticket.ID)
	if got.body != "Checked the logs" || !got.isInternal || got.isSystem || got.authorID.String != "42" {
		t.Fatalf("want an internal note by agent 42, got %+v", got)
	}
}

func TestMacroReplyActionsWritePublicRepliesByTheAgent(t *testing.T) {
	for _, actionType := range []string{"add_reply", "insert_canned_reply"} {
		t.Run(actionType, func(t *testing.T) {
			db := newWorkflowTestDB(t)
			ticket := insertRunnerTicket(t, db, 0, 1)
			macros := NewMacroService(db, nil)

			if err := macros.runAction(models.MacroAction{Type: actionType, Value: "We're on it"}, ticket.ID, models.UserID("42")); err != nil {
				t.Fatalf("%s: %v", actionType, err)
			}

			got := onlyReplyOn(t, db, ticket.ID)
			if got.body != "We're on it" || got.isInternal || got.isSystem || got.authorID.String != "42" {
				t.Fatalf("want a public reply by agent 42, got %+v", got)
			}
		})
	}
}
