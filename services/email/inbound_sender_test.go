package email

import (
	"context"
	"testing"

	"github.com/escalated-dev/escalated-go/models"
)

// fakeRequesterEmails maps requester ids to the email the host app
// knows them by.
type fakeRequesterEmails map[models.UserID]string

func (f fakeRequesterEmails) RequesterEmail(_ context.Context, ticket *models.Ticket) (string, error) {
	if ticket.RequesterID == nil {
		return "", nil
	}
	return f[*ticket.RequesterID], nil
}

func strPtr(s string) *string { return &s }

func userTicket(id int64, userID string) *models.Ticket {
	uid := models.UserID(userID)
	return &models.Ticket{
		ID:            id,
		Reference:     "ESC-07001",
		Status:        models.StatusOpen,
		RequesterType: strPtr("User"),
		RequesterID:   &uid,
	}
}

func guestTicket(id int64, ref, guestEmail string, status int) *models.Ticket {
	return &models.Ticket{
		ID:         id,
		Reference:  ref,
		Status:     status,
		GuestEmail: strPtr(guestEmail),
	}
}

func assertCreatedForSender(t *testing.T, result ProcessResult, writer *fakeTicketWriter, sender string) {
	t.Helper()
	if result.Outcome != OutcomeCreatedNew {
		t.Fatalf("Outcome = %q, want created_new", result.Outcome)
	}
	if len(writer.replyCalls) != 0 {
		t.Fatalf("AddReply called %d times, want 0", len(writer.replyCalls))
	}
	if len(writer.createCalls) != 1 {
		t.Fatalf("Create called %d times, want 1", len(writer.createCalls))
	}
	if got := writer.createCalls[0].GuestEmail; got == nil || *got != sender {
		t.Fatalf("new ticket GuestEmail = %v, want %s", got, sender)
	}
}

func TestInboundService_StrangerQuotingSubjectReference_OpensNewTicket(t *testing.T) {
	ticket := guestTicket(7001, "ESC-07001", "owner@example.com", models.StatusOpen)
	svc, lookup, writer := newInboundSvc(t, "", nil)
	lookup.ticketsByRef["ESC-07001"] = ticket

	result, err := svc.Process(context.Background(), InboundMessage{
		FromEmail: "stranger@example.net",
		ToEmail:   "support@example.com",
		Subject:   "RE: [ESC-07001] Your order",
		BodyText:  "Injected reply.",
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}

	assertCreatedForSender(t, result, writer, "stranger@example.net")
	if result.TicketID == ticket.ID {
		t.Fatalf("stranger mail landed on the matched ticket")
	}
}

func TestInboundService_StrangerThreadingOntoClosedTicket_DoesNotTouchIt(t *testing.T) {
	ticket := guestTicket(7002, "ESC-07002", "owner@example.com", models.StatusClosed)
	svc, _, writer := newInboundSvc(t, "", ticket)

	result, err := svc.Process(context.Background(), InboundMessage{
		FromEmail: "stranger@example.net",
		ToEmail:   "support@example.com",
		Subject:   "RE: [ESC-07002] Closed",
		BodyText:  "Reopen this.",
		InReplyTo: "<ticket-7002@support.example.com>",
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}

	assertCreatedForSender(t, result, writer, "stranger@example.net")
	if ticket.Status != models.StatusClosed {
		t.Fatalf("closed ticket status changed to %d", ticket.Status)
	}
}

func TestInboundService_SpoofedAgentFrom_IsNotPostedAsAgent(t *testing.T) {
	ticket := userTicket(7003, "5")
	svc, _, writer := newInboundSvc(t, inboundSecret, ticket)
	svc.WithRequesterEmailResolver(fakeRequesterEmails{"5": "owner@example.com", "9": "agent@example.com"})

	result, err := svc.Process(context.Background(), InboundMessage{
		FromEmail: "agent@example.com",
		FromName:  "Agent",
		ToEmail:   BuildReplyTo(7003, inboundSecret, inboundDomain),
		Subject:   "RE: [ESC-07001] Update",
		BodyText:  "Refund approved.",
		InReplyTo: "<ticket-7003@support.example.com>",
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}

	assertCreatedForSender(t, result, writer, "agent@example.com")
}

func TestInboundService_SignedReplyFromRequester_PostsAsRequester(t *testing.T) {
	ticket := userTicket(7004, "5")
	svc, _, writer := newInboundSvc(t, inboundSecret, ticket)
	svc.WithRequesterEmailResolver(fakeRequesterEmails{"5": "owner@example.com"})

	result, err := svc.Process(context.Background(), InboundMessage{
		FromEmail: "Owner@Example.com",
		ToEmail:   BuildReplyTo(7004, inboundSecret, inboundDomain),
		Subject:   "RE: Question",
		BodyText:  "Still broken.",
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}

	if result.Outcome != OutcomeRepliedToExisting || result.TicketID != 7004 {
		t.Fatalf("result = %+v, want reply on #7004", result)
	}
	if len(writer.replyCalls) != 1 || len(writer.createCalls) != 0 {
		t.Fatalf("calls: reply=%d create=%d, want 1/0", len(writer.replyCalls), len(writer.createCalls))
	}
	call := writer.replyCalls[0]
	if call.authorType == nil || *call.authorType != "User" {
		t.Errorf("authorType = %v, want User", call.authorType)
	}
	if call.authorID == nil || *call.authorID != "5" {
		t.Errorf("authorID = %v, want requester 5", call.authorID)
	}
	if call.internal {
		t.Errorf("reply posted as internal note")
	}
}

func TestInboundService_SignedReplyFromGuestRequester_IsAccepted(t *testing.T) {
	ticket := guestTicket(7005, "ESC-07005", "Owner@Example.com", models.StatusOpen)
	svc, _, writer := newInboundSvc(t, inboundSecret, ticket)

	result, err := svc.Process(context.Background(), InboundMessage{
		FromEmail: " owner@example.com ",
		ToEmail:   BuildReplyTo(7005, inboundSecret, inboundDomain),
		Subject:   "RE: Question",
		BodyText:  "More detail.",
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}

	if result.Outcome != OutcomeRepliedToExisting || result.TicketID != 7005 {
		t.Fatalf("result = %+v, want reply on #7005", result)
	}
	if call := writer.replyCalls[0]; call.authorID != nil {
		t.Errorf("guest reply authorID = %v, want nil", *call.authorID)
	}
}

func TestInboundService_RequesterWithoutResolver_FallsBackToNewTicket(t *testing.T) {
	ticket := userTicket(7006, "5")
	svc, _, writer := newInboundSvc(t, inboundSecret, ticket)

	result, err := svc.Process(context.Background(), InboundMessage{
		FromEmail: "owner@example.com",
		ToEmail:   BuildReplyTo(7006, inboundSecret, inboundDomain),
		Subject:   "RE: Question",
		BodyText:  "Hello?",
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}

	assertCreatedForSender(t, result, writer, "owner@example.com")
}

func TestInboundService_SecretConfigured_RequiresSignedReplyTo(t *testing.T) {
	ticket := guestTicket(7007, "ESC-07007", "owner@example.com", models.StatusOpen)
	svc, lookup, writer := newInboundSvc(t, inboundSecret, ticket)
	lookup.ticketsByRef["ESC-07007"] = ticket

	result, err := svc.Process(context.Background(), InboundMessage{
		FromEmail:  "owner@example.com",
		ToEmail:    "support@support.example.com",
		Subject:    "RE: [ESC-07007] Question",
		BodyText:   "Unsigned follow-up.",
		InReplyTo:  "<ticket-7007@support.example.com>",
		References: "<ticket-7007@support.example.com>",
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}

	assertCreatedForSender(t, result, writer, "owner@example.com")
}

func TestInboundRouter_SecretConfigured_IgnoresUnsignedPaths(t *testing.T) {
	ticket := &models.Ticket{ID: 42, Reference: "ESC-00042"}
	store := &fakeTicketLookup{
		ticketsByID:  map[int64]*models.Ticket{42: ticket},
		ticketsByRef: map[string]*models.Ticket{"ESC-00042": ticket},
	}

	got, err := newRouter(store, inboundSecret).ResolveTicket(context.Background(), InboundMessage{
		InReplyTo:  "<ticket-42@support.example.com>",
		References: "<ticket-42@support.example.com>",
		Subject:    "RE: [ESC-00042] help",
		ToEmail:    "support@example.com",
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got != nil {
		t.Fatalf("got = %v, want nil: only the signed Reply-To may link mail once a secret is set", got)
	}
	if len(store.calls.byID) != 0 || len(store.calls.byRef) != 0 {
		t.Fatalf("unsigned paths were looked up: %+v", store.calls)
	}
}

func TestInboundService_MissingFrom_NeverMatchesRequester(t *testing.T) {
	// A sender with no From address can never be the requester.
	ticket := guestTicket(7008, "ESC-07008", "owner@example.com", models.StatusOpen)
	svc, _, writer := newInboundSvc(t, "", ticket)

	result, err := svc.Process(context.Background(), InboundMessage{
		ToEmail:   "support@example.com",
		Subject:   "RE: thread",
		BodyText:  "xxx",
		InReplyTo: "<ticket-7008@support.example.com>",
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if result.Outcome != OutcomeCreatedNew || len(writer.replyCalls) != 0 {
		t.Fatalf("result = %+v replies=%d, want new ticket and no reply", result, len(writer.replyCalls))
	}
}
