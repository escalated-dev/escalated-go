// Package sqldialect adapts a query written with `?` placeholders to the
// database it is about to run against.
//
// The store interface has a hand-written implementation per database, so it
// writes each dialect directly. Everything outside it -- handlers, services,
// the newsletter and workflow runners -- builds SQL inline with `?`, which is
// SQLite's and MySQL's spelling. PostgreSQL uses $1, $2 and rejects `?` as a
// syntax error, so those code paths failed on the database this package
// documents as its default.
package sqldialect

import (
	"context"
	"database/sql"
	"reflect"
	"strconv"
	"strings"
	"sync"
)

// dialects memoizes the answer per *sql.DB. Working it out means reflecting on
// the driver, and these functions are called once per query.
var dialects sync.Map

// Rebind returns query with its placeholders in the form db's driver expects.
//
// A query with no `?` is returned untouched, as is any query for a driver that
// already takes `?`. A nil db is treated the same way: callers that reach one
// have a worse problem than placeholder style, and the driver error says so
// more clearly than a panic here would.
func Rebind(db *sql.DB, query string) string {
	if db == nil || !usesOrdinals(db) {
		return query
	}

	return ToOrdinals(query)
}

// ToOrdinals rewrites `?` placeholders as $1, $2, ... leaving anything inside a
// string literal alone -- a `?` in 'what?' is data, not a placeholder.
func ToOrdinals(query string) string {
	var (
		out      strings.Builder
		n        int
		inString bool
	)

	out.Grow(len(query) + 8)

	for i := 0; i < len(query); i++ {
		c := query[i]

		switch {
		case c == '\'':
			// '' inside a string is an escaped quote, not the end of one.
			if inString && i+1 < len(query) && query[i+1] == '\'' {
				out.WriteString("''")
				i++

				continue
			}

			inString = !inString
			out.WriteByte(c)

		case c == '?' && !inString:
			n++
			out.WriteByte('$')
			out.WriteString(strconv.Itoa(n))

		default:
			out.WriteByte(c)
		}
	}

	return out.String()
}

// usesOrdinals reports whether db's driver wants $1 rather than ?.
func usesOrdinals(db *sql.DB) bool {
	if cached, ok := dialects.Load(db); ok {
		return cached.(bool)
	}

	ordinals := driverUsesOrdinals(db.Driver())
	dialects.Store(db, ordinals)

	return ordinals
}

func driverUsesOrdinals(driver any) bool {
	if driver == nil {
		return false
	}

	t := reflect.TypeOf(driver)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	return nameUsesOrdinals(t.PkgPath() + "." + t.Name())
}

// nameUsesOrdinals decides from a driver's import path and type.
//
// lib/pq, jackc/pgx (stdlib shim and pgxpool), cockroachdb. Anything else --
// SQLite, MySQL, a driver we have never heard of -- keeps `?`, which is the
// safe assumption: leaving a query alone cannot turn a working one into a
// broken one, and rewriting one wrongly can.
func nameUsesOrdinals(name string) bool {
	name = strings.ToLower(name)

	return strings.Contains(name, "lib/pq") ||
		strings.Contains(name, "pq.driver") ||
		strings.Contains(name, "pgx") ||
		strings.Contains(name, "postgres")
}

// ExecInsert runs an INSERT and returns a result whose LastInsertId works on
// every driver.
//
// PostgreSQL's driver does not implement LastInsertId at all -- it returns
// "LastInsertId is not supported by this driver" -- because the value comes
// back from a RETURNING clause instead. Every call site here already reads the
// new id that way, so the statement gains one and the id is carried back in a
// result of the same shape. Nothing at the call site has to change but the
// call itself.
func ExecInsert(db *sql.DB, query string, args ...any) (sql.Result, error) {
	query = Rebind(db, query)

	if db == nil || !usesOrdinals(db) {
		return db.Exec(query, args...)
	}

	var id int64

	if err := db.QueryRow(query+" RETURNING id", args...).Scan(&id); err != nil {
		return nil, err
	}

	return insertResult(id), nil
}

// ExecInsertContext is ExecInsert with a context.
func ExecInsertContext(ctx context.Context, db *sql.DB, query string, args ...any) (sql.Result, error) {
	query = Rebind(db, query)

	if db == nil || !usesOrdinals(db) {
		return db.ExecContext(ctx, query, args...)
	}

	var id int64

	if err := db.QueryRowContext(ctx, query+" RETURNING id", args...).Scan(&id); err != nil {
		return nil, err
	}

	return insertResult(id), nil
}

// insertResult carries an id back in the shape database/sql callers expect.
type insertResult int64

func (r insertResult) LastInsertId() (int64, error) { return int64(r), nil }

// One row, because that is what an INSERT ... RETURNING id returned.
func (r insertResult) RowsAffected() (int64, error) { return 1, nil }

// Execer is anything that can run a statement: *sql.DB, *sql.Tx, *sql.Conn.
type Execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// ExecInsertOn is ExecInsertContext against something other than the database
// itself -- a transaction, usually.
//
// The dialect belongs to the connection, so db is still what decides how the
// statement is written; on is what runs it. Running a transaction's statement
// against the database instead would be a deadlock, not merely a detour: an
// in-memory SQLite pool holds one connection, and the transaction has it.
func ExecInsertOn(
	ctx context.Context,
	db *sql.DB,
	on Execer,
	query string,
	args ...any,
) (sql.Result, error) {
	query = Rebind(db, query)

	if db == nil || !usesOrdinals(db) {
		return on.ExecContext(ctx, query, args...)
	}

	var id int64

	if err := on.QueryRowContext(ctx, query+" RETURNING id", args...).Scan(&id); err != nil {
		return nil, err
	}

	return insertResult(id), nil
}
