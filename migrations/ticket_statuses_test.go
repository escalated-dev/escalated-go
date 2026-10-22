package migrations_test

import (
	"testing"

	"github.com/escalated-dev/escalated-go/internal/testdb"
	"github.com/escalated-dev/escalated-go/migrations"
	"github.com/escalated-dev/escalated-go/models"
)

// Custom statuses: the ticket_statuses table with one system row per built-in
// status, and a nullable tickets.ticket_status_id, on an existing install
// whose tickets keep their status.
func TestTicketStatusesMigration(t *testing.T) {
	const prefix = "ts_"
	db := testdb.Open(t)
	migrate := func() error {
		if testdb.Driver(t) == "postgres" {
			return migrations.Migrate(db, prefix)
		}
		return migrations.MigrateSQLite(db, prefix)
	}
	if err := migrate(); err != nil {
		t.Fatal(err)
	}

	// An install from before custom statuses: no column, no table, a ticket.
	for _, query := range []string{
		"DROP INDEX " + prefix + "tickets_ticket_status_idx",
		"ALTER TABLE " + prefix + "tickets DROP COLUMN ticket_status_id",
		"DROP TABLE " + prefix + "ticket_statuses",
		"INSERT INTO " + prefix + "tickets (reference, subject, description, status) VALUES ('ESC-1', 'Old', 'x', 5)",
	} {
		if _, err := db.Exec(query); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
	}
	if err := migrate(); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	if err := migrate(); err != nil {
		t.Fatalf("second upgrade: %v", err)
	}

	var status int
	var custom *int64
	if err := db.QueryRow("SELECT status, ticket_status_id FROM "+prefix+"tickets WHERE reference = 'ESC-1'").Scan(&status, &custom); err != nil {
		t.Fatal(err)
	}
	if status != models.StatusResolved || custom != nil {
		t.Fatalf("existing ticket: status %d, custom %v", status, custom)
	}

	rows, err := db.Query("SELECT slug, label, category, color, is_system, is_default FROM " + prefix + "ticket_statuses ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seen := map[string]string{}
	builtIns := map[string]models.BuiltInStatus{}
	for _, b := range models.BuiltInStatuses() {
		builtIns[b.Slug] = b
	}
	for rows.Next() {
		var slug, label, category, color string
		var system, isDefault bool
		if err := rows.Scan(&slug, &label, &category, &color, &system, &isDefault); err != nil {
			t.Fatal(err)
		}
		if !system || isDefault || label == "" || color == "" {
			t.Errorf("system row %s: label %q color %q system %v default %v", slug, label, color, system, isDefault)
		}
		if b := builtIns[slug]; b.Label != label || b.Color != color {
			t.Errorf("system row %s: %q %q, want the built-in's %q %q", slug, label, color, b.Label, b.Color)
		}
		if _, dup := seen[slug]; dup {
			t.Errorf("duplicate system row %s", slug)
		}
		seen[slug] = category
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(seen) != len(models.StatusName) {
		t.Fatalf("system rows %v, want one per built-in", seen)
	}
	for status, slug := range models.StatusName {
		if seen[slug] != models.BuiltInStatusCategory(status) {
			t.Errorf("system row %s category %q", slug, seen[slug])
		}
	}

	// The slug is unique.
	if _, err := db.Exec("INSERT INTO " + prefix + "ticket_statuses (label, slug, category) VALUES ('Again', 'open', 'open')"); err == nil {
		t.Error("a duplicate slug was accepted")
	}
}
