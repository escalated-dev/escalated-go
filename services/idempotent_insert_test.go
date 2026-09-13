package services

import (
	"database/sql"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/escalated-dev/escalated-go/internal/sqldialect"
	"github.com/escalated-dev/escalated-go/models"
)

// The add_tag and add_follower actions used SQLite's `INSERT OR IGNORE`, which
// PostgreSQL rejects with `syntax error at or near "OR"`. The existing add_tag
// test tags with a name that has no escalated_tags row, so the runner returned
// at the lookup and the join insert never reached the database. These tests use
// a tag that exists, and run every action twice to prove the insert is still
// idempotent once it is portable.

func insertTagRow(t *testing.T, db *sql.DB, name string) int64 {
	t.Helper()
	now := time.Now()
	res, err := sqldialect.ExecInsert(db, `INSERT INTO escalated_tags (name, slug, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		name, name, now, now,
	)
	if err != nil {
		t.Fatalf("insert tag: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

func countWhere(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(sqldialect.Rebind(db, query), args...).Scan(&n); err != nil {
		t.Fatalf("count (%s): %v", query, err)
	}
	return n
}

// assertNoFailedWorkflowLogs fails with the stored error message, so a
// dialect error surfaces in the test output instead of only as a missing row.
func assertNoFailedWorkflowLogs(t *testing.T, db *sql.DB) {
	t.Helper()
	rows, err := db.Query(sqldialect.Rebind(db, `SELECT status, error_message FROM escalated_workflow_logs ORDER BY id`))
	if err != nil {
		t.Fatalf("read workflow logs: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var msg sql.NullString
		if err := rows.Scan(&status, &msg); err != nil {
			t.Fatalf("scan workflow log: %v", err)
		}
		if status != "success" {
			t.Fatalf("workflow log status = %q, want success (error: %s)", status, msg.String)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("workflow log rows: %v", err)
	}
}

func TestWorkflowRunnerAddTagWritesTheJoinRowOnce(t *testing.T) {
	db := newWorkflowTestDB(t)
	ticket := insertRunnerTicket(t, db, models.StatusOpen, models.PriorityLow)
	tagID := insertTagRow(t, db, "vip")
	insertWorkflow(t, db, "ticket.created", `{}`, `[{"type":"add_tag","value":"vip"}]`, true, false, 0)

	runner := NewWorkflowRunner(db, discardLogger())
	runner.RunForEvent("ticket.created", ticket)
	runner.RunForEvent("ticket.created", ticket)

	assertNoFailedWorkflowLogs(t, db)
	if n := countWhere(t, db, `SELECT COUNT(*) FROM escalated_ticket_tags WHERE ticket_id = ? AND tag_id = ?`, ticket.ID, tagID); n != 1 {
		t.Fatalf("ticket_tags rows = %d, want 1", n)
	}
}

func TestWorkflowRunnerAddFollowerWritesTheRowOnce(t *testing.T) {
	db := newWorkflowTestDB(t)
	ticket := insertRunnerTicket(t, db, models.StatusOpen, models.PriorityLow)
	insertWorkflow(t, db, "ticket.created", `{}`, `[{"type":"add_follower","value":"42"}]`, true, false, 0)

	runner := NewWorkflowRunner(db, discardLogger())
	runner.RunForEvent("ticket.created", ticket)
	runner.RunForEvent("ticket.created", ticket)

	assertNoFailedWorkflowLogs(t, db)
	if n := countWhere(t, db, `SELECT COUNT(*) FROM escalated_ticket_followers WHERE ticket_id = ? AND user_id = ?`, ticket.ID, 42); n != 1 {
		t.Fatalf("ticket_followers rows = %d, want 1", n)
	}
}

func TestAutomationRunnerAddTagAndFollowerWriteRowsOnce(t *testing.T) {
	db := newWorkflowTestDB(t)
	ticket := insertRunnerTicket(t, db, models.StatusOpen, models.PriorityLow)
	tagID := insertTagRow(t, db, "vip")

	runner := NewAutomationRunner(db, discardLogger())
	for i := 0; i < 2; i++ {
		if err := runner.runAction(models.Automation{}, models.Ticket{ID: ticket.ID}, models.AutomationAction{Type: "add_tag", Value: "vip"}); err != nil {
			t.Fatalf("automation add_tag run %d: %v", i+1, err)
		}
		if err := runner.runAction(models.Automation{}, models.Ticket{ID: ticket.ID}, models.AutomationAction{Type: "add_follower", Value: "42"}); err != nil {
			t.Fatalf("automation add_follower run %d: %v", i+1, err)
		}
	}

	if n := countWhere(t, db, `SELECT COUNT(*) FROM escalated_ticket_tags WHERE ticket_id = ? AND tag_id = ?`, ticket.ID, tagID); n != 1 {
		t.Fatalf("ticket_tags rows = %d, want 1", n)
	}
	if n := countWhere(t, db, `SELECT COUNT(*) FROM escalated_ticket_followers WHERE ticket_id = ? AND user_id = ?`, ticket.ID, 42); n != 1 {
		t.Fatalf("ticket_followers rows = %d, want 1", n)
	}
}

func TestMacroAddTagWritesTheJoinRowOnce(t *testing.T) {
	db := newWorkflowTestDB(t)
	ticket := insertRunnerTicket(t, db, models.StatusOpen, models.PriorityLow)
	tagID := insertTagRow(t, db, "vip")

	svc := NewMacroService(db, discardLogger())
	for i := 0; i < 2; i++ {
		if err := svc.runAction(models.MacroAction{Type: "add_tag", Value: "vip"}, ticket.ID, "7"); err != nil {
			t.Fatalf("macro add_tag run %d: %v", i+1, err)
		}
	}

	if n := countWhere(t, db, `SELECT COUNT(*) FROM escalated_ticket_tags WHERE ticket_id = ? AND tag_id = ?`, ticket.ID, tagID); n != 1 {
		t.Fatalf("ticket_tags rows = %d, want 1", n)
	}
}

// Everything outside the SQLite store runs against both databases, so SQLite's
// conflict clauses must not appear there. store/sqlite.go is SQLite-only.
func TestNoSQLiteOnlyConflictClausesOutsideTheSQLiteStore(t *testing.T) {
	root := ".."
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if name := d.Name(); name == ".git" || name == "node_modules" || name == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if filepath.ToSlash(path) == "../store/sqlite.go" {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		upper := strings.ToUpper(string(src))
		for _, clause := range []string{"INSERT OR IGNORE", "INSERT OR REPLACE"} {
			if strings.Contains(upper, clause) {
				t.Errorf("%s uses %q, which PostgreSQL rejects; use ON CONFLICT DO NOTHING", filepath.ToSlash(path), clause)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
}
