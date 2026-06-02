package migrations

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

// Regression: the newsletter schema must be created by the migration runner.
// The newsletter port shipped only a goose .sql file, which migrations.go
// never loads — so the tables were never created. They are now part of
// migrationStatements; this test fails if that regresses.
func TestMigrateCreatesNewsletterTables(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := MigrateSQLite(db, "escalated_"); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	tables := []string{
		"escalated_newsletter_lists",
		"escalated_newsletter_list_members",
		"escalated_newsletter_templates",
		"escalated_newsletters",
		"escalated_newsletter_deliveries",
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

	// marketing_opt_out_at must be added to contacts (drives opt-out filtering).
	var col string
	err = db.QueryRow(
		"SELECT name FROM pragma_table_info('escalated_contacts') WHERE name = 'marketing_opt_out_at'",
	).Scan(&col)
	if err != nil {
		t.Errorf("expected escalated_contacts.marketing_opt_out_at column: %v", err)
	}
}
