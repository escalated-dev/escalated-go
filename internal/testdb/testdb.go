// Package testdb opens the database the test suite runs against.
//
// SQLite unless ESCALATED_TEST_DRIVER says otherwise, so running the suite
// locally needs nothing installed. CI runs it twice, because this package ships
// two hand-written stores and two hand-written migration sets -- and every test
// opened SQLite, so the PostgreSQL half had never been executed at all. A
// syntax error in it would have shipped.
package testdb

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/escalated-dev/escalated-go/migrations"
	"github.com/escalated-dev/escalated-go/store"

	// Both drivers are registered here rather than in each test file, so a
	// package only has to import this one to run against either.
	_ "github.com/lib/pq"
	_ "modernc.org/sqlite"
)

// Prefix is the table prefix every fixture migrates with. It stays the ordinary
// one on both drivers: PostgreSQL isolation comes from a schema per test, not
// from a prefix per test, so nothing else in a fixture has to change.
const Prefix = "escalated_"

// Driver returns the driver the suite was asked to run against.
//
// Never falls back. A CI leg that silently ran SQLite would report green having
// tested nothing the matrix exists for, and every assertion in the suite would
// still pass.
func Driver(t *testing.T) string {
	t.Helper()

	driver := os.Getenv("ESCALATED_TEST_DRIVER")
	if driver == "" {
		driver = "sqlite"
	}

	switch driver {
	case "sqlite", "postgres":
		return driver
	default:
		t.Fatalf("ESCALATED_TEST_DRIVER must be sqlite or postgres; got %q", driver)

		return ""
	}
}

// schemaCounter gives each PostgreSQL fixture a schema of its own.
var schemaCounter atomic.Uint64

// Open returns a migrated, empty database.
//
// On SQLite that is a fresh in-memory database per call. PostgreSQL is a
// server, not a file, so the database is shared and each call gets its own
// schema instead -- same effect, and the connection is pinned to that schema so
// every unqualified table name in the suite resolves there.
func Open(t *testing.T) *sql.DB {
	t.Helper()

	if Driver(t) == "sqlite" {
		return openSQLite(t)
	}

	return openPostgres(t)
}

func openSQLite(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("opening sqlite: %v", err)
	}

	// An in-memory SQLite database belongs to its connection, so a second one
	// in the pool would be a second, empty database.
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	if err := migrations.MigrateSQLite(db, Prefix); err != nil {
		t.Fatalf("migrating sqlite: %v", err)
	}

	return db
}

func openPostgres(t *testing.T) *sql.DB {
	t.Helper()

	admin, err := sql.Open("postgres", dsn(""))
	if err != nil {
		t.Fatalf("opening postgres: %v", err)
	}
	defer func() { _ = admin.Close() }()

	if err := admin.Ping(); err != nil {
		t.Fatalf("connecting to postgres at %s: %v", redacted(dsn("")), err)
	}

	// Go runs each package's tests in its own process, so a counter alone
	// collides across packages -- two of them would both ask for esc_test_1.
	schema := fmt.Sprintf("esc_test_%d_%d", os.Getpid(), schemaCounter.Add(1))

	if _, err := admin.Exec(`CREATE SCHEMA IF NOT EXISTS ` + quote(schema)); err != nil {
		t.Fatalf("creating schema %s: %v", schema, err)
	}

	db, err := sql.Open("postgres", dsn(schema))
	if err != nil {
		t.Fatalf("opening postgres on %s: %v", schema, err)
	}

	t.Cleanup(func() {
		_ = db.Close()

		cleanup, err := sql.Open("postgres", dsn(""))
		if err != nil {
			return
		}
		defer func() { _ = cleanup.Close() }()

		_, _ = cleanup.Exec(`DROP SCHEMA IF EXISTS ` + quote(schema) + ` CASCADE`)
	})

	if err := migrations.Migrate(db, Prefix); err != nil {
		t.Fatalf("migrating postgres: %v", err)
	}

	return db
}

// dsn returns the connection string, pinned to a schema when one is given.
func dsn(schema string) string {
	base := os.Getenv("ESCALATED_TEST_DSN")
	if base == "" {
		base = "postgres://postgres:postgres@127.0.0.1:5432/escalated_test?sslmode=disable"
	}

	if schema == "" {
		return base
	}

	parsed, err := url.Parse(base)
	if err != nil {
		return base
	}

	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()

	return parsed.String()
}

// quote makes an identifier safe to interpolate. The schema names here are
// generated, not user input, but interpolating an identifier unquoted is a
// habit worth not having.
func quote(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

// redacted keeps a password out of a failure message.
func redacted(connection string) string {
	at := strings.LastIndex(connection, "@")
	scheme := strings.Index(connection, "://")

	if at < 0 || scheme < 0 {
		return connection
	}

	return connection[:scheme+3] + "***" + connection[at:]
}

// Store returns the store implementation for the driver in use.
//
// Fixtures used to name store.NewSQLiteStore directly, which is what kept the
// PostgreSQL store -- a separate, hand-written implementation of the same
// interface -- from ever being executed by a test.
func Store(t *testing.T, db *sql.DB) store.Store {
	t.Helper()

	if Driver(t) == "sqlite" {
		return store.NewSQLiteStore(db, Prefix)
	}

	return store.NewPostgresStore(db, Prefix)
}

// Dialect is the name the newsletter SQL store expects for the driver in use.
func Dialect(t *testing.T) string {
	t.Helper()

	return Driver(t)
}
