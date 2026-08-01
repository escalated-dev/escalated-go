package migrations

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

// Regression: the canned_responses table (ported from the Laravel
// create_escalated_canned_responses migration) must be created by the
// inline Migrate path — this port habitually shipped goose .sql files the
// runner never loads. The table is part of engineAddonStatements; this
// fails if that regresses or the is_shared column type stops accepting the
// `WHERE is_shared = TRUE` comparison the service uses.
func TestMigrateCreatesCannedResponsesTable(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := MigrateSQLite(db, "escalated_"); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	var name string
	if err := db.QueryRow(
		"SELECT name FROM sqlite_master WHERE type='table' AND name = ?",
		"escalated_canned_responses",
	).Scan(&name); err != nil {
		t.Fatalf("expected escalated_canned_responses to exist: %v", err)
	}

	// Round-trip a shared response with the exact visibility query shape the
	// service runs (WHERE is_shared = TRUE OR created_by = ?), including a NULL
	// category and NULL creator.
	if _, err := db.Exec(
		`INSERT INTO escalated_canned_responses (title, body, category, is_shared, created_by)
		 VALUES ('Greeting', 'Hello there', NULL, TRUE, NULL)`,
	); err != nil {
		t.Fatalf("insert canned response: %v", err)
	}
	var title string
	if err := db.QueryRow(
		"SELECT title FROM escalated_canned_responses WHERE is_shared = TRUE OR created_by = ?",
		"1",
	).Scan(&title); err != nil {
		t.Fatalf("query WHERE is_shared = TRUE: %v", err)
	}
	if title != "Greeting" {
		t.Errorf("expected 'Greeting', got %q", title)
	}
}
