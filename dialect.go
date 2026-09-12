package escalated

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"time"
)

// Dialects Escalated has a store for.
const (
	DialectPostgres = "postgres"
	DialectSQLite   = "sqlite"
)

// DetectDialect works out which database a *sql.DB is connected to.
//
// The host chooses the driver, opens the connection and hands it over; until
// now it also had to remember which constructor matched. New() assumed
// PostgreSQL and NewSQLite() assumed SQLite, and picking the wrong one is not
// an error anyone sees at startup -- the wrong SQL reaches the database on the
// first query that happens to differ, which on a good day is a 500 and on a bad
// one is a query that parses and means something else.
//
// The driver's own type is the answer where it is available, because it costs
// nothing and cannot be wrong: no PostgreSQL driver registers itself as
// something else. Where a driver is wrapped -- instrumentation, tracing, a
// connection proxy -- the name no longer says anything, so the connection is
// asked directly.
func DetectDialect(db *sql.DB) (string, error) {
	if db == nil {
		return "", fmt.Errorf("escalated: cannot detect the database dialect from a nil *sql.DB")
	}

	if dialect := dialectFromDriver(db.Driver()); dialect != "" {
		return dialect, nil
	}

	return dialectFromProbe(db)
}

// dialectFromDriver reads the driver's Go type. Returns "" when it recognises
// nothing, which is the signal to ask the database itself.
func dialectFromDriver(d any) string {
	if d == nil {
		return ""
	}

	return dialectFromDriverName(typeName(d))
}

// typeName is the driver's import path and type together -- pgx registers its
// database/sql driver as `*stdlib.Driver`, which says nothing on its own, while
// its import path says `github.com/jackc/pgx/v5/stdlib`.
func typeName(v any) string {
	t := reflect.TypeOf(v)
	if t == nil {
		return ""
	}

	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	return t.PkgPath() + "." + t.Name()
}

func dialectFromDriverName(name string) string {
	name = strings.ToLower(name)

	switch {
	// lib/pq, jackc/pgx (stdlib shim and pgxpool), cockroachdb.
	case strings.Contains(name, "lib/pq"),
		strings.Contains(name, "pq.driver"),
		strings.Contains(name, "pgx"),
		strings.Contains(name, "postgres"):
		return DialectPostgres

	// mattn/go-sqlite3, modernc.org/sqlite, glebarez/sqlite.
	case strings.Contains(name, "sqlite"):
		return DialectSQLite
	}

	return ""
}

// dialectFromProbe asks the connection what it is.
//
// `select sqlite_version()` is the discriminator rather than `select version()`:
// PostgreSQL has no sqlite_version(), and SQLite's version() would need an
// extension. One statement, one answer, no parsing of vendor strings.
func dialectFromProbe(db *sql.DB) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var version string

	if err := db.QueryRowContext(ctx, "select sqlite_version()").Scan(&version); err == nil {
		return DialectSQLite, nil
	}

	// A failed probe leaves some drivers' connections in a bad state, and on
	// PostgreSQL it aborts an open transaction. Ping forces a usable one.
	if err := db.PingContext(ctx); err != nil {
		return "", fmt.Errorf("escalated: cannot detect the database dialect: %w", err)
	}

	if err := db.QueryRowContext(ctx, "select version()").Scan(&version); err != nil {
		return "", fmt.Errorf("escalated: cannot detect the database dialect: %w", err)
	}

	if strings.Contains(strings.ToLower(version), "postgresql") {
		return DialectPostgres, nil
	}

	// Naming what was found matters more than guessing. MySQL reaches here, and
	// Escalated has no MySQL store: silently handing it the PostgreSQL one
	// would fail later, somewhere less obvious.
	return "", fmt.Errorf(
		"escalated: unsupported database %q; Escalated has stores for PostgreSQL and SQLite",
		strings.TrimSpace(version),
	)
}
