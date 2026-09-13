package store

import "testing"

// SetTicketReferenceGenerator replaces the reference generator both stores use
// for the length of a test, so a test can hand CreateTicket a reference that is
// already taken.
func SetTicketReferenceGenerator(t testing.TB, generate func() string) {
	t.Helper()

	previous := newTicketReference
	newTicketReference = generate
	t.Cleanup(func() { newTicketReference = previous })
}

// TicketReferenceAttempts is how many references CreateTicket tries.
const TicketReferenceAttempts = ticketReferenceAttempts
