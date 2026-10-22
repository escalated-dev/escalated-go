package migrations_test

import (
	"database/sql"
	"testing"

	"github.com/escalated-dev/escalated-go/internal/testdb"
	"github.com/escalated-dev/escalated-go/migrations"
)

// An installation from before the custom field admin gains placeholder,
// validation_rules and conditions, and keeps its fields.
func TestCustomFieldColumnsUpgrade(t *testing.T) {
	const prefix = "cf_"
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
	for _, query := range []string{
		"ALTER TABLE " + prefix + "custom_fields DROP COLUMN placeholder",
		"ALTER TABLE " + prefix + "custom_fields DROP COLUMN validation_rules",
		"ALTER TABLE " + prefix + "custom_fields DROP COLUMN conditions",
		"INSERT INTO " + prefix + "custom_fields (name, slug, field_type) VALUES ('Old', 'old', 'text')",
	} {
		if _, err := db.Exec(query); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
	}
	if err := migrate(); err != nil {
		t.Fatal(err)
	}
	if err := migrate(); err != nil {
		t.Fatalf("second upgrade: %v", err)
	}
	var name string
	var placeholder, rules, conditions sql.NullString
	if err := db.QueryRow("SELECT name, placeholder, validation_rules, conditions FROM "+prefix+"custom_fields WHERE slug = 'old'").
		Scan(&name, &placeholder, &rules, &conditions); err != nil || name != "Old" || placeholder.Valid || rules.Valid || conditions.Valid {
		t.Fatalf("legacy field: %s %v %v %v %v", name, placeholder, rules, conditions, err)
	}
}
