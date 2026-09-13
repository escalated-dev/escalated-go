package store_test

import (
	"context"
	"testing"

	"github.com/escalated-dev/escalated-go/internal/testdb"
	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/store"
)

// sequence returns each reference in turn, then repeats the last one, and
// counts the calls.
func sequence(refs ...string) (func() string, *int) {
	calls := 0
	return func() string {
		i := calls
		if i >= len(refs) {
			i = len(refs) - 1
		}
		calls++
		return refs[i]
	}, &calls
}

func newTicket(subject string) *models.Ticket {
	return &models.Ticket{
		Subject:     subject,
		Description: "body",
		Status:      models.StatusOpen,
		Priority:    models.PriorityMedium,
		TicketType:  "question",
	}
}

// The reference column is unique and the reference is random. Before the retry
// a draw that matched a stored reference failed the insert and the ticket was
// lost.
func TestCreateTicketRetriesUnderAFreshReferenceWhenTheFirstIsTaken(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t)
	s := testdb.Store(t, db)

	gen, _ := sequence("ESC-2609-TAKEN000")
	store.SetTicketReferenceGenerator(t, gen)
	first := newTicket("first")
	if err := s.CreateTicket(ctx, first); err != nil {
		t.Fatalf("creating the first ticket: %v", err)
	}

	gen, calls := sequence("ESC-2609-TAKEN000", "ESC-2609-FRESH001")
	store.SetTicketReferenceGenerator(t, gen)
	second := newTicket("second")
	if err := s.CreateTicket(ctx, second); err != nil {
		t.Fatalf("creating a ticket whose first reference is taken: %v", err)
	}

	if *calls != 2 {
		t.Errorf("generator called %d times, want 2", *calls)
	}
	if second.Reference != "ESC-2609-FRESH001" {
		t.Errorf("second.Reference = %q, want ESC-2609-FRESH001", second.Reference)
	}
	if second.ID == 0 || second.ID == first.ID {
		t.Errorf("second.ID = %d, want a new id (first is %d)", second.ID, first.ID)
	}

	// The connection is still usable after the failed statement, and both
	// tickets are stored under their own references.
	for ref, subject := range map[string]string{"ESC-2609-TAKEN000": "first", "ESC-2609-FRESH001": "second"} {
		got, err := s.GetTicketByReference(ctx, ref)
		if err != nil {
			t.Fatalf("GetTicketByReference(%q): %v", ref, err)
		}
		if got == nil || got.Subject != subject {
			t.Fatalf("GetTicketByReference(%q) = %+v, want the %q ticket", ref, got, subject)
		}
	}
}

// A generator that only ever returns a taken reference must not loop.
func TestCreateTicketStopsAfterThreeTakenReferences(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t)
	s := testdb.Store(t, db)

	gen, _ := sequence("ESC-2609-TAKEN000")
	store.SetTicketReferenceGenerator(t, gen)
	if err := s.CreateTicket(ctx, newTicket("first")); err != nil {
		t.Fatalf("creating the first ticket: %v", err)
	}

	gen, calls := sequence("ESC-2609-TAKEN000")
	store.SetTicketReferenceGenerator(t, gen)
	err := s.CreateTicket(ctx, newTicket("second"))
	if err == nil {
		t.Fatal("CreateTicket succeeded with a generator that only returns a taken reference")
	}
	if *calls != store.TicketReferenceAttempts || store.TicketReferenceAttempts != 3 {
		t.Errorf("generator called %d times (attempts %d), want 3", *calls, store.TicketReferenceAttempts)
	}
}

// Only a taken reference is retried. Any other unique violation, here the guest
// token, fails at once.
func TestCreateTicketDoesNotRetryOtherUniqueViolations(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t)
	s := testdb.Store(t, db)

	token := "GT-shared-token"
	first := newTicket("first")
	first.GuestToken = &token
	if err := s.CreateTicket(ctx, first); err != nil {
		t.Fatalf("creating the first ticket: %v", err)
	}

	gen, calls := sequence("ESC-2609-FRESH001", "ESC-2609-FRESH002", "ESC-2609-FRESH003")
	store.SetTicketReferenceGenerator(t, gen)
	second := newTicket("second")
	second.GuestToken = &token
	if err := s.CreateTicket(ctx, second); err == nil {
		t.Fatal("CreateTicket succeeded with a duplicate guest token")
	}
	if *calls != 1 {
		t.Errorf("generator called %d times, want 1: a guest token collision is not a reference collision", *calls)
	}
}

// A reference the caller chose is the caller's: a collision on it is an error,
// not a reason to store the ticket under a different reference.
func TestCreateTicketKeepsACallerChosenReference(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t)
	s := testdb.Store(t, db)

	first := newTicket("first")
	first.Reference = "ESC-2609-CHOSEN00"
	if err := s.CreateTicket(ctx, first); err != nil {
		t.Fatalf("creating the first ticket: %v", err)
	}

	gen, calls := sequence("ESC-2609-FRESH001")
	store.SetTicketReferenceGenerator(t, gen)
	second := newTicket("second")
	second.Reference = "ESC-2609-CHOSEN00"
	if err := s.CreateTicket(ctx, second); err == nil {
		t.Fatal("CreateTicket succeeded with a duplicate caller-chosen reference")
	}
	if *calls != 0 {
		t.Errorf("generator called %d times, want 0", *calls)
	}
}

// References written before the change have six hex characters. They are
// stored as they are and still resolve.
func TestOlderSixHexReferencesStillResolve(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t)
	s := testdb.Store(t, db)

	old := newTicket("older ticket")
	old.Reference = "ESC-2604-A1B2C3"
	if err := s.CreateTicket(ctx, old); err != nil {
		t.Fatalf("creating a ticket with an older reference: %v", err)
	}

	fresh := newTicket("newer ticket")
	if err := s.CreateTicket(ctx, fresh); err != nil {
		t.Fatalf("creating a ticket with a generated reference: %v", err)
	}

	got, err := s.GetTicketByReference(ctx, "ESC-2604-A1B2C3")
	if err != nil {
		t.Fatalf("GetTicketByReference: %v", err)
	}
	if got == nil || got.ID != old.ID {
		t.Fatalf("GetTicketByReference(ESC-2604-A1B2C3) = %+v, want ticket %d", got, old.ID)
	}
}
