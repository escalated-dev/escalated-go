package migrations_test

import (
	"testing"

	"github.com/escalated-dev/escalated-go/internal/testdb"
	"github.com/escalated-dev/escalated-go/migrations"
)

// Installs that already ran the migrations have escalated_escalation_rules with
// an INTEGER is_active, which PostgreSQL will not accept a boolean for. Migrate
// is re-run on every start, so it has to convert that column in place, keep the
// stored values, and do nothing the second time.
func TestPostgresMigrateConvertsEscalationRuleIsActiveToBoolean(t *testing.T) {
	if testdb.Driver(t) != "postgres" {
		t.Skip("SQLite stores booleans as integers; there is nothing to convert")
	}

	db := testdb.Open(t)
	table := testdb.Prefix + "escalation_rules"

	columnType := func() string {
		t.Helper()
		var dataType, columnDefault string
		err := db.QueryRow(
			`SELECT data_type, COALESCE(column_default, '')
			   FROM information_schema.columns
			  WHERE table_schema = current_schema() AND table_name = $1 AND column_name = 'is_active'`,
			table,
		).Scan(&dataType, &columnDefault)
		if err != nil {
			t.Fatalf("reading is_active column: %v", err)
		}
		return dataType + " default " + columnDefault
	}

	if got := columnType(); got != "boolean default true" {
		t.Fatalf("fresh install: is_active is %s, want boolean default true", got)
	}

	// Stand in for an existing install: the table exactly as it shipped, with rows.
	for _, stmt := range []string{
		`DROP TABLE ` + table,
		`CREATE TABLE ` + table + ` (
			id BIGSERIAL PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			description TEXT,
			trigger_type VARCHAR(255),
			conditions TEXT NOT NULL DEFAULT '[]',
			actions TEXT NOT NULL DEFAULT '[]',
			sort_order INTEGER NOT NULL DEFAULT 0,
			is_active INTEGER NOT NULL DEFAULT 1,
			created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`INSERT INTO ` + table + ` (name, is_active) VALUES ('on', 1), ('off', 0)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("recreating the shipped table: %v\nSQL: %s", err, stmt)
		}
	}

	for run := 1; run <= 2; run++ {
		if err := migrations.Migrate(db, testdb.Prefix); err != nil {
			t.Fatalf("Migrate run %d over an INTEGER is_active: %v", run, err)
		}
		if got := columnType(); got != "boolean default true" {
			t.Fatalf("after Migrate run %d: is_active is %s, want boolean default true", run, got)
		}
	}

	for name, want := range map[string]bool{"on": true, "off": false} {
		var got bool
		if err := db.QueryRow(`SELECT is_active FROM `+table+` WHERE name = $1`, name).Scan(&got); err != nil {
			t.Fatalf("reading %q: %v", name, err)
		}
		if got != want {
			t.Errorf("rule %q is_active = %v after conversion, want %v", name, got, want)
		}
	}

	var defaulted bool
	if err := db.QueryRow(`INSERT INTO ` + table + ` (name) VALUES ('new') RETURNING is_active`).Scan(&defaulted); err != nil {
		t.Fatalf("inserting with the default: %v", err)
	}
	if !defaulted {
		t.Errorf("default is_active = false, want true")
	}
}
