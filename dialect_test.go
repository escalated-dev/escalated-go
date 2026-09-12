package escalated

import (
	"database/sql"
	"database/sql/driver"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func openSQLite(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("opening sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := db.Ping(); err != nil {
		t.Fatalf("pinging sqlite: %v", err)
	}

	return db
}

func TestDetectDialectRecognisesSQLite(t *testing.T) {
	dialect, err := DetectDialect(openSQLite(t))
	if err != nil {
		t.Fatalf("detecting: %v", err)
	}

	if dialect != DialectSQLite {
		t.Errorf("got %q, want %q", dialect, DialectSQLite)
	}
}

func TestNewChoosesTheSQLiteStoreForASQLiteConnection(t *testing.T) {
	// The whole point. New used to assume PostgreSQL, so a host that opened a
	// SQLite connection got PostgreSQL SQL -- with no error at startup, because
	// nothing checked until a query happened to differ.
	cfg := DefaultConfig()
	cfg.DB = openSQLite(t)

	esc, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if esc.Config.DatabaseDialect != DialectSQLite {
		t.Errorf("dialect: got %q, want %q", esc.Config.DatabaseDialect, DialectSQLite)
	}

	// And the store itself, not just the label on it.
	if name := typeName(esc.Store); !strings.Contains(strings.ToLower(name), "sqlite") {
		t.Errorf("store: got %s, want a SQLite store", name)
	}
}

func TestAnExplicitDialectIsHonouredWithoutDetection(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DB = openSQLite(t)
	cfg.DatabaseDialect = DialectPostgres

	esc, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if esc.Config.DatabaseDialect != DialectPostgres {
		t.Errorf("an explicit dialect must win over detection, got %q", esc.Config.DatabaseDialect)
	}
}

func TestAnUnsupportedExplicitDialectIsRefused(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DB = openSQLite(t)
	cfg.DatabaseDialect = "mysql"

	_, err := New(cfg)
	if err == nil {
		t.Fatal("expected an error naming the unsupported dialect")
	}

	if !strings.Contains(err.Error(), "mysql") {
		t.Errorf("the error should name what was asked for, got: %v", err)
	}
}

func TestDetectDialectRefusesANilDatabase(t *testing.T) {
	if _, err := DetectDialect(nil); err == nil {
		t.Fatal("expected an error for a nil *sql.DB")
	}
}

// A driver Escalated has never heard of, wrapping one it has. Instrumentation
// and tracing layers look exactly like this, and the type name says nothing --
// which is why detection falls through to asking the connection.
type wrappedDriver struct{ driver.Driver }

func TestDetectDialectFallsBackToTheConnectionForAnUnknownDriver(t *testing.T) {
	if got := dialectFromDriver(wrappedDriver{}); got != "" {
		t.Fatalf("an unrecognised driver type must not be guessed at, got %q", got)
	}

	// The probe is what answers in that case, and it answers correctly.
	dialect, err := dialectFromProbe(openSQLite(t))
	if err != nil {
		t.Fatalf("probing: %v", err)
	}

	if dialect != DialectSQLite {
		t.Errorf("got %q, want %q", dialect, DialectSQLite)
	}
}

func TestDialectFromDriverNamesTheCommonDrivers(t *testing.T) {
	// Named by type, so this is a table of what those types look like rather
	// than of real drivers -- importing five database drivers to assert their
	// package names is not worth the dependency.
	for _, c := range []struct {
		name string
		want string
	}{
		{"github.com/lib/pq.Driver", DialectPostgres},
		{"github.com/jackc/pgx/v5/stdlib.Driver", DialectPostgres},
		{"github.com/mattn/go-sqlite3.SQLiteDriver", DialectSQLite},
		{"modernc.org/sqlite.Driver", DialectSQLite},
		{"github.com/glebarez/go-sqlite.Driver", DialectSQLite},

		// MySQL is recognisably not one of the two, and must not be guessed
		// into either -- Escalated has no MySQL store.
		{"github.com/go-sql-driver/mysql.MySQLDriver", ""},
		{"github.com/acme/tracing.Driver", ""},
	} {
		if got := dialectFromDriverName(c.name); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
