package migrations

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

// Regression: the event-driven Workflow schema (workflows + workflow_logs +
// delayed_actions) must be created by the migration runner. This port shipped
// the tables but only the workflow_engine.go pure evaluator — no runner,
// handler, route or lifecycle hook — so a workflow could never be stored or
// fire. The WorkflowRunner + admin CRUD wired alongside this test depend on the
// exact column shape asserted here (workflow_logs keeps a status string +
// actions_executed JSON, NOT a conditions_matched flag); this fails if the
// tables stop being created or those columns drift.
func TestMigrateCreatesWorkflowTables(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := MigrateSQLite(db, "escalated_"); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	tables := []string{
		"escalated_workflows",
		"escalated_workflow_logs",
		"escalated_delayed_actions",
	}
	for _, tbl := range tables {
		var name string
		err := db.QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name = ?", tbl,
		).Scan(&name)
		if err != nil {
			t.Errorf("expected table %q to exist after migration: %v", tbl, err)
		}
	}

	// Round-trip a workflow with the exact boolean query shape the runner uses
	// (WHERE is_active = TRUE AND trigger_event = ? ORDER BY position).
	if _, err := db.Exec(
		`INSERT INTO escalated_workflows (name, trigger_event, conditions, actions, is_active, stop_on_match, position)
		 VALUES ('wf', 'ticket.created', '{}', '[]', TRUE, FALSE, 0)`,
	); err != nil {
		t.Fatalf("insert workflow: %v", err)
	}
	var wfName string
	if err := db.QueryRow(
		"SELECT name FROM escalated_workflows WHERE is_active = TRUE AND trigger_event = ? ORDER BY position ASC",
		"ticket.created",
	).Scan(&wfName); err != nil {
		t.Fatalf("query workflow WHERE is_active = TRUE: %v", err)
	}

	// Round-trip a log row keyed by workflow_id, matching the columns the runner
	// writes (status string + actions_executed JSON).
	if _, err := db.Exec(
		`INSERT INTO escalated_workflow_logs (workflow_id, ticket_id, trigger_event, status, actions_executed)
		 VALUES (1, 1, 'ticket.created', 'matched', '[]')`,
	); err != nil {
		t.Fatalf("insert workflow log: %v", err)
	}
	var status string
	if err := db.QueryRow(
		"SELECT status FROM escalated_workflow_logs WHERE workflow_id = ?", 1,
	).Scan(&status); err != nil {
		t.Fatalf("query log by workflow_id: %v", err)
	}
	if status != "matched" {
		t.Errorf("workflow log status = %q, want matched", status)
	}

	// Round-trip a delayed action keyed by executed flag.
	if _, err := db.Exec(
		`INSERT INTO escalated_delayed_actions (workflow_id, ticket_id, action_data, execute_at, executed)
		 VALUES (1, 1, '[]', CURRENT_TIMESTAMP, FALSE)`,
	); err != nil {
		t.Fatalf("insert delayed action: %v", err)
	}
	var pending int
	if err := db.QueryRow(
		"SELECT COUNT(*) FROM escalated_delayed_actions WHERE executed = FALSE",
	).Scan(&pending); err != nil {
		t.Fatalf("query delayed actions WHERE executed = FALSE: %v", err)
	}
	if pending != 1 {
		t.Errorf("pending delayed actions = %d, want 1", pending)
	}
}
