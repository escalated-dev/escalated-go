package migrations

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

// Regression: the engine-domain schema (escalation, satisfaction, capacity,
// ticket links, side conversations) must be created by the migration runner.
// These ports originally shipped only goose .sql files, which migrations.go
// never loads — so the tables were never created by the inline Migrate path
// the library actually runs. They are now part of migrationStatements; this
// test fails if that regresses.
func TestMigrateCreatesEngineTables(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := MigrateSQLite(db, "escalated_"); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	tables := []string{
		"escalated_escalation_rules",
		"escalated_satisfaction_ratings",
		"escalated_agent_capacity",
		"escalated_ticket_links",
		"escalated_side_conversations",
		"escalated_side_conversation_replies",
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

	// Round-trip an escalation rule to confirm the is_active INTEGER column
	// works with the `WHERE is_active = 1` query and bool scan the handler uses.
	if _, err := db.Exec(
		`INSERT INTO escalated_escalation_rules (name, conditions, actions, sort_order, is_active)
		 VALUES ('rule', '[]', '[]', 0, 1)`,
	); err != nil {
		t.Fatalf("insert escalation rule: %v", err)
	}
	var active bool
	if err := db.QueryRow(
		"SELECT is_active FROM escalated_escalation_rules WHERE is_active = 1",
	).Scan(&active); err != nil {
		t.Fatalf("query is_active = 1 + bool scan: %v", err)
	}
	if !active {
		t.Errorf("expected is_active to scan as true")
	}
}
