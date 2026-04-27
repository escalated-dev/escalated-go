package email

import (
	"context"
	"errors"
	"testing"

	"github.com/escalated-dev/escalated-go/models"
)

// fakeTicketWriter records calls + stages return values.
type fakeTicketWriter struct {
	createReturn *models.Ticket
	createErr    error
	replyReturn  *models.Reply
	replyErr     error

	createCalls []CreateTicketInputShim
	replyCalls  []struct {
		ticketID   int64
		body       string
		authorType *string
		internal   bool
	}
}

func (f *fakeTicketWriter) Create(_ context.Context, in CreateTicketInputShim) (*models.Ticket, error) {
	f.createCalls = append(f.createCalls, in)
	if f.createErr != nil {
		return nil, f.createErr
	}
	if f.createReturn == nil {
		return &models.Ticket{ID: 101}, nil
	}
	return f.createReturn, nil
}

func (f *fakeTicketWriter) AddReply(_ context.Context, ticketID int64, body string, authorType *string, _ *int64, internal bool) (*models.Reply, error) {
	f.replyCalls = append(f.replyCalls, struct {
		ticketID   int64
		body       string
		authorType *string
		internal   bool
	}{ticketID, body, authorType, internal})
	if f.replyErr != nil {
		return nil, f.replyErr
	}
	if f.replyReturn == nil {
		return &models.Reply{ID: 202}, nil
	}
	return f.replyReturn, nil
}

func newInboundSvc(t *testing.T, secret string, ticket *models.Ticket) (*InboundEmailService, *fakeTicketLookup, *fakeTicketWriter) {
	t.Helper()
	lookup := &fakeTicketLookup{
		ticketsByID:  map[int64]*models.Ticket{},
		ticketsByRef: map[string]*models.Ticket{},
	}
	if ticket != nil {
		lookup.ticketsByID[ticket.ID] = ticket
	}
	router := NewInboundRouter(lookup, "support.example.com", secret)
	writer := &fakeTicketWriter{}
	return NewInboundEmailService(router, writer), lookup, writer
}

func TestInboundService_ExistingTicketMatched_AddsReply(t *testing.T) {
	ticket := &models.Ticket{ID: 42}
	svc, _, writer := newInboundSvc(t, "", ticket)

	msg := InboundMessage{
		FromEmail: "customer@example.com",
		ToEmail:   "support@example.com",
		Subject:   "reply",
		BodyText:  "Follow-up",
		InReplyTo: "<ticket-42@support.example.com>",
	}

	result, err := svc.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("err = %v", err)
	}

	if result.Outcome != OutcomeRepliedToExisting {
		t.Errorf("Outcome = %q, want replied_to_existing", result.Outcome)
	}
	if result.TicketID != 42 {
		t.Errorf("TicketID = %d, want 42", result.TicketID)
	}
	if result.ReplyID == 0 {
		t.Errorf("ReplyID = 0, want non-zero")
	}
	if len(writer.replyCalls) != 1 {
		t.Fatalf("len(replyCalls) = %d, want 1", len(writer.replyCalls))
	}
	if writer.replyCalls[0].ticketID != 42 {
		t.Errorf("reply ticketID = %d, want 42", writer.replyCalls[0].ticketID)
	}
	if writer.replyCalls[0].body != "Follow-up" {
		t.Errorf("reply body = %q", writer.replyCalls[0].body)
	}
	if writer.replyCalls[0].authorType == nil || *writer.replyCalls[0].authorType != "inbound_email" {
		t.Errorf("authorType = %v, want inbound_email", writer.replyCalls[0].authorType)
	}
	if len(writer.createCalls) != 0 {
		t.Errorf("create should not have been called")
	}
}

func TestInboundService_NoMatchRealContent_CreatesNewTicket(t *testing.T) {
	svc, _, writer := newInboundSvc(t, "", nil)

	msg := InboundMessage{
		FromEmail: "newcustomer@example.com",
		FromName:  "New Customer",
		ToEmail:   "support@example.com",
		Subject:   "New issue",
		BodyText:  "Something broken",
	}

	result, err := svc.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("err = %v", err)
	}

	if result.Outcome != OutcomeCreatedNew {
		t.Errorf("Outcome = %q, want created_new", result.Outcome)
	}
	if result.TicketID == 0 {
		t.Errorf("TicketID = 0, want non-zero")
	}
	if len(writer.createCalls) != 1 {
		t.Fatalf("len(createCalls) = %d, want 1", len(writer.createCalls))
	}
	call := writer.createCalls[0]
	if call.Subject != "New issue" {
		t.Errorf("Subject = %q", call.Subject)
	}
	if call.Description != "Something broken" {
		t.Errorf("Description = %q", call.Description)
	}
	if call.GuestEmail == nil || *call.GuestEmail != "newcustomer@example.com" {
		t.Errorf("GuestEmail = %v", call.GuestEmail)
	}
	if call.GuestName == nil || *call.GuestName != "New Customer" {
		t.Errorf("GuestName = %v", call.GuestName)
	}
}

func TestInboundService_EmptySubjectFallsBackToPlaceholder(t *testing.T) {
	svc, _, writer := newInboundSvc(t, "", nil)

	msg := InboundMessage{
		FromEmail: "customer@example.com",
		ToEmail:   "support@example.com",
		Subject:   "",
		BodyText:  "Has content",
	}

	_, _ = svc.Process(context.Background(), msg)

	if len(writer.createCalls) != 1 {
		t.Fatalf("len(createCalls) = %d", len(writer.createCalls))
	}
	if writer.createCalls[0].Subject != "(no subject)" {
		t.Errorf("Subject = %q, want (no subject)", writer.createCalls[0].Subject)
	}
}

func TestInboundService_SkipsSnsConfirmation(t *testing.T) {
	svc, _, writer := newInboundSvc(t, "", nil)

	msg := InboundMessage{
		FromEmail: "no-reply@sns.amazonaws.com",
		ToEmail:   "support@example.com",
		Subject:   "SubscriptionConfirmation",
	}

	result, err := svc.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if result.Outcome != OutcomeSkipped {
		t.Errorf("Outcome = %q, want skipped", result.Outcome)
	}
	if len(writer.createCalls) != 0 || len(writer.replyCalls) != 0 {
		t.Errorf("create/reply should not be called for SNS confirmation")
	}
}

func TestInboundService_SkipsEmptyBodyAndSubject(t *testing.T) {
	svc, _, writer := newInboundSvc(t, "", nil)

	msg := InboundMessage{
		FromEmail: "customer@example.com",
		ToEmail:   "support@example.com",
		Subject:   "",
	}

	result, _ := svc.Process(context.Background(), msg)
	if result.Outcome != OutcomeSkipped {
		t.Errorf("Outcome = %q, want skipped", result.Outcome)
	}
	if len(writer.createCalls) != 0 {
		t.Errorf("create should not be called")
	}
}

func TestInboundService_PropagatesRouterErrors(t *testing.T) {
	lookup := &fakeTicketLookup{errByID: errors.New("db offline")}
	router := NewInboundRouter(lookup, "support.example.com", "")
	svc := NewInboundEmailService(router, &fakeTicketWriter{})

	msg := InboundMessage{
		FromEmail: "customer@example.com",
		ToEmail:   "support@example.com",
		Subject:   "hi",
		BodyText:  "hello",
		InReplyTo: "<ticket-42@support.example.com>",
	}

	_, err := svc.Process(context.Background(), msg)
	if err == nil || err.Error() != "db offline" {
		t.Fatalf("err = %v, want db offline", err)
	}
}

func TestInboundService_SurfacesProviderHostedAttachments(t *testing.T) {
	svc, _, _ := newInboundSvc(t, "", nil)

	msg := InboundMessage{
		FromEmail: "customer@example.com",
		ToEmail:   "support@example.com",
		Subject:   "With attachments",
		BodyText:  "See attached",
		Attachments: []InboundAttachment{
			{
				Name:        "large.pdf",
				ContentType: "application/pdf",
				SizeBytes:   10_000_000,
				DownloadURL: "https://mailgun.example/att/large",
			},
			{
				Name:        "inline.txt",
				ContentType: "text/plain",
				Content:     []byte("hello"),
			},
		},
	}

	result, err := svc.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(result.PendingAttachmentDownloads) != 1 {
		t.Fatalf("PendingAttachmentDownloads count = %d, want 1", len(result.PendingAttachmentDownloads))
	}
	pending := result.PendingAttachmentDownloads[0]
	if pending.Name != "large.pdf" || pending.DownloadURL != "https://mailgun.example/att/large" {
		t.Errorf("pending = %+v", pending)
	}
}

func TestIsNoiseEmail_Matrix(t *testing.T) {
	cases := []struct {
		name    string
		message InboundMessage
		want    bool
	}{
		{"sns confirmation", InboundMessage{FromEmail: "no-reply@sns.amazonaws.com", Subject: "SubscriptionConfirmation"}, true},
		{"empty body+subject", InboundMessage{FromEmail: "c@x.com"}, true},
		{"real email", InboundMessage{FromEmail: "c@x.com", Subject: "real", BodyText: "content"}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsNoiseEmail(tc.message); got != tc.want {
				t.Errorf("IsNoiseEmail = %v, want %v", got, tc.want)
			}
		})
	}
}
