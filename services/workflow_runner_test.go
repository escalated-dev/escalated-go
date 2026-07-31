package services

import (
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/escalated-dev/escalated-go/migrations"
	"github.com/escalated-dev/escalated-go/models"
)

func newWorkflowTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	if err := migrations.MigrateSQLite(db, "escalated_"); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func insertRunnerTicket(t *testing.T, db *sql.DB, status, priority int) *models.Ticket {
	t.Helper()
	now := time.Now()
	res, err := db.Exec(
		`INSERT INTO escalated_tickets (reference, subject, description, status, priority, ticket_type, metadata, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, '{}', ?, ?)`,
		"ESC-1", "Login is broken", "Cannot sign in", status, priority, "problem", now, now,
	)
	if err != nil {
		t.Fatalf("insert ticket: %v", err)
	}
	id, _ := res.LastInsertId()
	return &models.Ticket{
		ID:         id,
		Reference:  "ESC-1",
		Subject:    "Login is broken",
		Status:     status,
		Priority:   priority,
		TicketType: "problem",
	}
}

func insertWorkflow(t *testing.T, db *sql.DB, event, conditions, actions string, active, stopOnMatch bool, position int) int64 {
	t.Helper()
	res, err := db.Exec(
		`INSERT INTO escalated_workflows (name, trigger_event, conditions, actions, is_active, stop_on_match, position)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"wf", event, conditions, actions, active, stopOnMatch, position,
	)
	if err != nil {
		t.Fatalf("insert workflow: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

// A matching, active workflow must fire on its trigger event: its actions apply
// to the ticket and exactly one success log row is written. This is the P0 the
// change fixes — before it, the engine had no runner so nothing here happened.
func TestWorkflowRunnerFiresMatchingWorkflowAndLogs(t *testing.T) {
	db := newWorkflowTestDB(t)
	ticket := insertRunnerTicket(t, db, models.StatusOpen, models.PriorityMedium)

	wfID := insertWorkflow(t, db, "ticket.created",
		`{"all":[{"field":"status","operator":"equals","value":"open"}]}`,
		`[{"type":"change_priority","value":"3"},{"type":"add_note","value":"Auto-escalated {{reference}}"}]`,
		true, false, 0,
	)

	NewWorkflowRunner(db, discardLogger()).RunForEvent("ticket.created", ticket)

	// Action 1: priority raised to urgent (3).
	var priority int
	if err := db.QueryRow(`SELECT priority FROM escalated_tickets WHERE id = ?`, ticket.ID).Scan(&priority); err != nil {
		t.Fatalf("read ticket priority: %v", err)
	}
	if priority != models.PriorityUrgent {
		t.Errorf("ticket priority = %d, want %d (change_priority action did not run)", priority, models.PriorityUrgent)
	}

	// Action 2: an internal note with the interpolated reference.
	var noteBody string
	if err := db.QueryRow(
		`SELECT body FROM escalated_replies WHERE ticket_id = ? AND is_internal = TRUE`, ticket.ID,
	).Scan(&noteBody); err != nil {
		t.Fatalf("read workflow note: %v", err)
	}
	if noteBody != "Auto-escalated ESC-1" {
		t.Errorf("note body = %q, want %q (interpolation failed)", noteBody, "Auto-escalated ESC-1")
	}

	// Exactly one success log row, keyed to the workflow + ticket + event.
	var count int
	var status, event string
	if err := db.QueryRow(
		`SELECT COUNT(*), COALESCE(MAX(status), ''), COALESCE(MAX(trigger_event), '')
		   FROM escalated_workflow_logs WHERE workflow_id = ? AND ticket_id = ?`,
		wfID, ticket.ID,
	).Scan(&count, &status, &event); err != nil {
		t.Fatalf("count logs: %v", err)
	}
	if count != 1 {
		t.Fatalf("workflow_logs rows = %d, want 1", count)
	}
	if status != "success" {
		t.Errorf("log status = %q, want success", status)
	}
	if event != "ticket.created" {
		t.Errorf("log trigger_event = %q, want ticket.created", event)
	}
}

// A workflow whose conditions do not match must NOT touch the ticket, but must
// still record a "skipped" log row (the run is observable either way).
func TestWorkflowRunnerSkipsNonMatchingWorkflow(t *testing.T) {
	db := newWorkflowTestDB(t)
	ticket := insertRunnerTicket(t, db, models.StatusOpen, models.PriorityMedium)

	insertWorkflow(t, db, "ticket.created",
		`{"all":[{"field":"status","operator":"equals","value":"closed"}]}`,
		`[{"type":"change_priority","value":"4"}]`,
		true, false, 0,
	)

	NewWorkflowRunner(db, discardLogger()).RunForEvent("ticket.created", ticket)

	var priority int
	if err := db.QueryRow(`SELECT priority FROM escalated_tickets WHERE id = ?`, ticket.ID).Scan(&priority); err != nil {
		t.Fatalf("read ticket priority: %v", err)
	}
	if priority != models.PriorityMedium {
		t.Errorf("ticket priority = %d, want unchanged %d", priority, models.PriorityMedium)
	}

	var status string
	if err := db.QueryRow(`SELECT status FROM escalated_workflow_logs WHERE ticket_id = ?`, ticket.ID).Scan(&status); err != nil {
		t.Fatalf("read log: %v", err)
	}
	if status != "skipped" {
		t.Errorf("log status = %q, want skipped", status)
	}
}

// Only workflows subscribed to the fired event run; an inactive workflow and a
// workflow on a different trigger must be left untouched.
func TestWorkflowRunnerFiltersByEventAndActive(t *testing.T) {
	db := newWorkflowTestDB(t)
	ticket := insertRunnerTicket(t, db, models.StatusOpen, models.PriorityLow)

	// Different event — must not run.
	insertWorkflow(t, db, "ticket.assigned", `{}`, `[{"type":"change_priority","value":"4"}]`, true, false, 0)
	// Inactive — must not run.
	insertWorkflow(t, db, "ticket.created", `{}`, `[{"type":"change_priority","value":"4"}]`, false, false, 1)

	NewWorkflowRunner(db, discardLogger()).RunForEvent("ticket.created", ticket)

	var priority int
	_ = db.QueryRow(`SELECT priority FROM escalated_tickets WHERE id = ?`, ticket.ID).Scan(&priority)
	if priority != models.PriorityLow {
		t.Errorf("ticket priority = %d, want unchanged %d (wrong workflow ran)", priority, models.PriorityLow)
	}

	var logs int
	_ = db.QueryRow(`SELECT COUNT(*) FROM escalated_workflow_logs`).Scan(&logs)
	if logs != 0 {
		t.Errorf("workflow_logs rows = %d, want 0 (no subscribed active workflow)", logs)
	}
}

// stop_on_match halts evaluation after the first matching workflow, so a later
// active workflow on the same event never runs.
func TestWorkflowRunnerHonorsStopOnMatch(t *testing.T) {
	db := newWorkflowTestDB(t)
	ticket := insertRunnerTicket(t, db, models.StatusOpen, models.PriorityLow)

	// position 0: matches everything, stop_on_match = true.
	insertWorkflow(t, db, "ticket.created", `{}`, `[{"type":"add_tag","value":"first"}]`, true, true, 0)
	// position 1: would also match, but must be skipped.
	insertWorkflow(t, db, "ticket.created", `{}`, `[{"type":"change_priority","value":"4"}]`, true, false, 1)

	NewWorkflowRunner(db, discardLogger()).RunForEvent("ticket.created", ticket)

	var priority int
	_ = db.QueryRow(`SELECT priority FROM escalated_tickets WHERE id = ?`, ticket.ID).Scan(&priority)
	if priority != models.PriorityLow {
		t.Errorf("ticket priority = %d, want unchanged %d (second workflow ran despite stop_on_match)", priority, models.PriorityLow)
	}

	var logs int
	_ = db.QueryRow(`SELECT COUNT(*) FROM escalated_workflow_logs`).Scan(&logs)
	if logs != 1 {
		t.Errorf("workflow_logs rows = %d, want 1 (only the first workflow should have run)", logs)
	}
}
