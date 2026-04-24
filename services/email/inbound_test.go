package email

import (
	"context"
	"errors"
	"testing"

	"github.com/escalated-dev/escalated-go/models"
)

const (
	inboundDomain = "support.example.com"
	inboundSecret = "test-secret-for-hmac"
)

// fakeTicketLookup is a minimal TicketLookup for tests. Fields let
// a test stage the expected result for each method.
type fakeTicketLookup struct {
	ticketsByID  map[int64]*models.Ticket
	ticketsByRef map[string]*models.Ticket
	errByID      error
	errByRef     error
	calls        struct {
		byID  []int64
		byRef []string
	}
}

func (f *fakeTicketLookup) GetTicket(_ context.Context, id int64) (*models.Ticket, error) {
	f.calls.byID = append(f.calls.byID, id)
	if f.errByID != nil {
		return nil, f.errByID
	}
	return f.ticketsByID[id], nil
}

func (f *fakeTicketLookup) GetTicketByReference(_ context.Context, ref string) (*models.Ticket, error) {
	f.calls.byRef = append(f.calls.byRef, ref)
	if f.errByRef != nil {
		return nil, f.errByRef
	}
	return f.ticketsByRef[ref], nil
}

func newRouter(store TicketLookup, secret string) *InboundRouter {
	return NewInboundRouter(store, inboundDomain, secret)
}

func TestInboundRouter_MatchesCanonicalInReplyTo(t *testing.T) {
	ticket := &models.Ticket{ID: 42, Reference: "ESC-00042"}
	store := &fakeTicketLookup{ticketsByID: map[int64]*models.Ticket{42: ticket}}

	msg := InboundMessage{
		InReplyTo: "<ticket-42@support.example.com>",
		ToEmail:   "support@example.com",
	}

	got, err := newRouter(store, "").ResolveTicket(context.Background(), msg)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got == nil || got.ID != 42 {
		t.Fatalf("got = %v, want ticket #42", got)
	}
}

func TestInboundRouter_MatchesReferencesHeader(t *testing.T) {
	ticket := &models.Ticket{ID: 42}
	store := &fakeTicketLookup{ticketsByID: map[int64]*models.Ticket{42: ticket}}

	msg := InboundMessage{
		References: "<unrelated@mail.com> <ticket-42@support.example.com>",
	}

	got, err := newRouter(store, "").ResolveTicket(context.Background(), msg)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got == nil || got.ID != 42 {
		t.Fatalf("got = %v, want ticket #42", got)
	}
}

func TestInboundRouter_VerifiesSignedReplyToWhenSecretConfigured(t *testing.T) {
	ticket := &models.Ticket{ID: 42}
	store := &fakeTicketLookup{ticketsByID: map[int64]*models.Ticket{42: ticket}}

	to := BuildReplyTo(42, inboundSecret, inboundDomain)
	msg := InboundMessage{ToEmail: to}

	got, err := newRouter(store, inboundSecret).ResolveTicket(context.Background(), msg)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got == nil || got.ID != 42 {
		t.Fatalf("got = %v, want ticket #42", got)
	}
}

func TestInboundRouter_RejectsForgedReplyToSignature(t *testing.T) {
	store := &fakeTicketLookup{ticketsByID: map[int64]*models.Ticket{
		42: {ID: 42},
	}}
	forged := BuildReplyTo(42, "wrong-secret", inboundDomain)
	msg := InboundMessage{ToEmail: forged}

	got, _ := newRouter(store, "real-secret").ResolveTicket(context.Background(), msg)
	if got != nil {
		t.Fatalf("forged signature produced %v, want nil", got)
	}
}

func TestInboundRouter_IgnoresSignedReplyToWhenSecretBlank(t *testing.T) {
	store := &fakeTicketLookup{ticketsByID: map[int64]*models.Ticket{
		42: {ID: 42},
	}}
	to := BuildReplyTo(42, inboundSecret, inboundDomain)
	msg := InboundMessage{ToEmail: to}

	got, _ := newRouter(store, "").ResolveTicket(context.Background(), msg)
	if got != nil {
		t.Fatalf("got = %v, want nil when secret is blank", got)
	}
}

func TestInboundRouter_MatchesSubjectReferenceTag(t *testing.T) {
	ticket := &models.Ticket{ID: 99, Reference: "ESC-00099"}
	store := &fakeTicketLookup{ticketsByRef: map[string]*models.Ticket{"ESC-00099": ticket}}

	msg := InboundMessage{Subject: "RE: [ESC-00099] help", ToEmail: "support@example.com"}

	got, err := newRouter(store, "").ResolveTicket(context.Background(), msg)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got == nil || got.ID != 99 {
		t.Fatalf("got = %v, want ticket #99", got)
	}
}

func TestInboundRouter_ReturnsNilWhenNothingMatches(t *testing.T) {
	store := &fakeTicketLookup{}
	msg := InboundMessage{Subject: "No match here"}

	got, _ := newRouter(store, "").ResolveTicket(context.Background(), msg)
	if got != nil {
		t.Fatalf("got = %v, want nil", got)
	}
}

func TestInboundRouter_PropagatesStoreErrors(t *testing.T) {
	store := &fakeTicketLookup{errByID: errors.New("db offline")}
	msg := InboundMessage{InReplyTo: "<ticket-42@support.example.com>"}

	_, err := newRouter(store, "").ResolveTicket(context.Background(), msg)
	if err == nil || err.Error() != "db offline" {
		t.Fatalf("err = %v, want db offline", err)
	}
}

func TestCandidateHeaderMessageIDs_InReplyToFirstThenReferences(t *testing.T) {
	msg := InboundMessage{
		InReplyTo:  "<primary@mail>",
		References: "<a@mail> <b@mail>",
	}

	ids := CandidateHeaderMessageIDs(msg)
	want := []string{"<primary@mail>", "<a@mail>", "<b@mail>"}

	if len(ids) != len(want) {
		t.Fatalf("got %d ids, want %d", len(ids), len(want))
	}
	for i, id := range ids {
		if id != want[i] {
			t.Errorf("ids[%d] = %q, want %q", i, id, want[i])
		}
	}
}

func TestCandidateHeaderMessageIDs_EmptyHeadersYieldsNone(t *testing.T) {
	msg := InboundMessage{}

	if got := CandidateHeaderMessageIDs(msg); len(got) != 0 {
		t.Fatalf("got %v, want empty", got)
	}
}

func TestInboundMessage_BodyPrefersTextOverHTML(t *testing.T) {
	msg := InboundMessage{BodyText: "plain", BodyHTML: "<p>html</p>"}
	if got := msg.Body(); got != "plain" {
		t.Fatalf("Body() = %q, want plain", got)
	}

	// Falls back to HTML when plain is missing.
	msgHTMLOnly := InboundMessage{BodyHTML: "<p>html</p>"}
	if got := msgHTMLOnly.Body(); got != "<p>html</p>" {
		t.Fatalf("Body() = %q, want html", got)
	}
}
