package migrations

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

// Regression: the outbound-webhooks schema (webhooks + webhook_deliveries)
// must be created by the migration runner. Like the other engine ports it is
// wired into engineAddonStatements rather than shipped as a standalone goose
// .sql file, so this test fails if either table stops being created or the
// boolean `active` column reverts to a type the `= TRUE` query rejects.
func TestMigrateCreatesWebhookTables(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := MigrateSQLite(db, "escalated_"); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	tables := []string{
		"escalated_webhooks",
		"escalated_webhook_deliveries",
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

	// Round-trip a webhook with the exact boolean query shape the dispatcher
	// uses (WHERE active = TRUE) plus a delivery row keyed by webhook_id + event.
	if _, err := db.Exec(
		`INSERT INTO escalated_webhooks (url, events, secret, active)
		 VALUES ('https://example.test/hook', '["ticket.created"]', 'shh', TRUE)`,
	); err != nil {
		t.Fatalf("insert webhook: %v", err)
	}
	var url string
	if err := db.QueryRow(
		"SELECT url FROM escalated_webhooks WHERE active = TRUE",
	).Scan(&url); err != nil {
		t.Fatalf("query webhook WHERE active = TRUE: %v", err)
	}

	if _, err := db.Exec(
		`INSERT INTO escalated_webhook_deliveries (webhook_id, event, payload, attempts)
		 VALUES (1, 'ticket.created', '{}', 1)`,
	); err != nil {
		t.Fatalf("insert webhook delivery: %v", err)
	}
	var event string
	if err := db.QueryRow(
		"SELECT event FROM escalated_webhook_deliveries WHERE webhook_id = ? AND event = ?",
		1, "ticket.created",
	).Scan(&event); err != nil {
		t.Fatalf("query delivery by webhook_id + event: %v", err)
	}
}
