package migrations_test

import (
	"testing"

	"github.com/escalated-dev/escalated-go/internal/testdb"
)

// The PostgreSQL migrations had never been executed by anything. Two of them
// were broken -- escalated_replies and escalated_ticket_activities passed their
// Sprintf arguments in the wrong order, asking PostgreSQL for
// `REFERENCES BIGINT(id)` and a column typed `escalated_tickets`. Both are
// refused outright, so the package could not create its own schema on the
// database it documents as the default.
//
// testdb.Open runs the migrations, so reaching the body at all is most of the
// assertion. The rest checks the two statements that were wrong.
func TestPostgresMigrationsCreateTheSchema(t *testing.T) {
	if testdb.Driver(t) != "postgres" {
		t.Skip("covered by the sqlite migration tests on the sqlite leg")
	}

	db := testdb.Open(t)

	for _, table := range []string{
		"tickets", "replies", "ticket_activities", "ticket_followers",
		"departments", "tags", "skills", "agent_skills", "contacts",
		"newsletters", "newsletter_lists", "articles", "webhooks", "workflows",
	} {
		var exists bool

		err := db.QueryRow(
			`SELECT EXISTS (
				SELECT 1 FROM information_schema.tables
				WHERE table_schema = current_schema() AND table_name = $1
			)`,
			testdb.Prefix+table,
		).Scan(&exists)
		if err != nil {
			t.Fatalf("checking %s: %v", table, err)
		}

		if !exists {
			t.Errorf("%s%s was not created", testdb.Prefix, table)
		}
	}
}

func TestPostgresForeignKeysPointAtTables(t *testing.T) {
	if testdb.Driver(t) != "postgres" {
		t.Skip("sqlite does not enforce these")
	}

	db := testdb.Open(t)

	// The exact shape the swapped arguments got wrong: a foreign key that names
	// a *type* rather than a table cannot exist, so finding it here is proof
	// the statement was built correctly.
	for _, c := range []struct{ table, column, references string }{
		{"replies", "ticket_id", "tickets"},
		{"ticket_activities", "ticket_id", "tickets"},
		{"ticket_followers", "ticket_id", "tickets"},
	} {
		var target string

		err := db.QueryRow(
			`SELECT ccu.table_name
			 FROM information_schema.table_constraints tc
			 JOIN information_schema.key_column_usage kcu
			   ON tc.constraint_name = kcu.constraint_name
			 JOIN information_schema.constraint_column_usage ccu
			   ON tc.constraint_name = ccu.constraint_name
			 WHERE tc.constraint_type = 'FOREIGN KEY'
			   AND tc.table_schema = current_schema()
			   AND tc.table_name = $1
			   AND kcu.column_name = $2
			 LIMIT 1`,
			testdb.Prefix+c.table, c.column,
		).Scan(&target)
		if err != nil {
			t.Errorf("%s.%s has no foreign key: %v", c.table, c.column, err)

			continue
		}

		if want := testdb.Prefix + c.references; target != want {
			t.Errorf("%s.%s references %s, want %s", c.table, c.column, target, want)
		}
	}
}

// The column the swap mistyped. It holds a host user id, so it must be the
// configured user-key type -- not a table name.
func TestPostgresUserColumnsAreTheUserKeyType(t *testing.T) {
	if testdb.Driver(t) != "postgres" {
		t.Skip("sqlite has no static column types to check")
	}

	db := testdb.Open(t)

	for _, c := range []struct{ table, column string }{
		{"replies", "author_id"},
		{"ticket_activities", "causer_id"},
		{"tickets", "assigned_to"},
	} {
		var dataType string

		err := db.QueryRow(
			`SELECT data_type FROM information_schema.columns
			 WHERE table_schema = current_schema() AND table_name = $1 AND column_name = $2`,
			testdb.Prefix+c.table, c.column,
		).Scan(&dataType)
		if err != nil {
			t.Errorf("%s.%s: %v", c.table, c.column, err)

			continue
		}

		if dataType != "bigint" {
			t.Errorf("%s.%s is %s, want bigint", c.table, c.column, dataType)
		}
	}
}
