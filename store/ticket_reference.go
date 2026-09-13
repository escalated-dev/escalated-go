package store

import (
	"errors"
	"strings"

	"github.com/escalated-dev/escalated-go/models"
)

// ticketReferenceAttempts bounds the references CreateTicket tries. A 40-bit
// reference matches a given stored one about once in 2^40 draws, so two
// collisions in a row will not happen on a real site; the bound only stops a
// broken generator from looping.
const ticketReferenceAttempts = 3

// newTicketReference is a variable so a test can force a collision. It is not a
// host setting.
var newTicketReference = func() string { return models.GenerateReference("") }

// insertTicket runs insert. When the store generated the ticket's reference and
// the insert failed because that reference is already stored, it runs insert
// again under a fresh reference, up to ticketReferenceAttempts in all. A
// reference the caller set is never replaced, and any other error is returned
// at once.
//
// Both stores insert on their *sql.DB, never inside a transaction a caller
// opened, so every attempt is a statement of its own. That is what makes the
// retry safe on PostgreSQL: a failed statement aborts the transaction around
// it, and a retry inside a caller's transaction would fail with "current
// transaction is aborted" whatever reference it tried. An insert that one day
// runs on a *sql.Tx needs a savepoint around each attempt.
func insertTicket(t *models.Ticket, prefix string, insert func() error) error {
	if t.Reference != "" {
		return insert()
	}

	var err error
	for attempt := 0; attempt < ticketReferenceAttempts; attempt++ {
		t.Reference = newTicketReference()
		if err = insert(); err == nil {
			return nil
		}
		if !referenceTaken(err, prefix) {
			break
		}
	}

	// Nothing was stored, so the ticket keeps no reference: a caller that tries
	// again gets a fresh one instead of the one that failed.
	t.Reference = ""
	return err
}

// referenceTaken reports whether err is a unique violation on the tickets
// reference, and not on any other unique index such as the guest token.
//
// The stores don't import a driver, so this reads what the drivers return.
// PostgreSQL drivers (lib/pq, pgx) report SQLSTATE 23505 through a SQLState
// method and quote the index name in the message. SQLite drivers
// (modernc.org/sqlite, mattn/go-sqlite3) pass on SQLite's own message, which
// names the table and column.
func referenceTaken(err error, prefix string) bool {
	if err == nil {
		return false
	}

	var coded interface{ SQLState() string }
	if errors.As(err, &coded) {
		return coded.SQLState() == "23505" &&
			strings.Contains(err.Error(), `"idx_`+prefix+`tkt_ref"`)
	}

	return strings.Contains(err.Error(), "UNIQUE constraint failed: "+prefix+"tickets.reference")
}
