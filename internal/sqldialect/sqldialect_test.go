package sqldialect

import "testing"

func TestToOrdinalsNumbersPlaceholdersInOrder(t *testing.T) {
	got := ToOrdinals(`INSERT INTO t (a, b, c) VALUES (?, ?, ?)`)
	want := `INSERT INTO t (a, b, c) VALUES ($1, $2, $3)`

	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestToOrdinalsLeavesQuestionMarksInsideStringsAlone(t *testing.T) {
	// A `?` in a literal is data. Renumbering it would change what the query
	// means and shift every placeholder after it.
	for _, c := range []struct{ in, want string }{
		{`SELECT * FROM t WHERE body = 'what?' AND id = ?`, `SELECT * FROM t WHERE body = 'what?' AND id = $1`},
		{`SELECT ? , 'a?b' , ?`, `SELECT $1 , 'a?b' , $2`},

		// '' is an escaped quote, so the string has not ended and the ? inside
		// it is still data.
		{`SELECT 'it''s a ?' , ?`, `SELECT 'it''s a ?' , $1`},
	} {
		if got := ToOrdinals(c.in); got != c.want {
			t.Errorf("ToOrdinals(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestToOrdinalsLeavesAQueryWithoutPlaceholdersAlone(t *testing.T) {
	query := `SELECT count(*) FROM t`

	if got := ToOrdinals(query); got != query {
		t.Errorf("got %q, want it unchanged", got)
	}
}

func TestDriverUsesOrdinalsRecognisesThePostgresDrivers(t *testing.T) {
	for _, c := range []struct {
		name string
		want bool
	}{
		{"github.com/lib/pq.Driver", true},
		{"github.com/jackc/pgx/v5/stdlib.Driver", true},

		// Everything else keeps `?`. Leaving a query alone cannot turn a
		// working one into a broken one; rewriting one wrongly can.
		{"modernc.org/sqlite.Driver", false},
		{"github.com/mattn/go-sqlite3.SQLiteDriver", false},
		{"github.com/go-sql-driver/mysql.MySQLDriver", false},
		{"github.com/acme/tracing.Driver", false},
	} {
		if got := nameUsesOrdinals(c.name); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestRebindToleratesANilDatabase(t *testing.T) {
	query := `SELECT ?`

	if got := Rebind(nil, query); got != query {
		t.Errorf("got %q, want it unchanged", got)
	}
}
