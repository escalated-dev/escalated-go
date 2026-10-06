package email

import (
	"context"
	"errors"
	"testing"

	"github.com/escalated-dev/escalated-go/models"
)

func requesterReply(ref, from string) InboundMessage {
	return InboundMessage{
		FromEmail: from,
		ToEmail:   "support@example.com",
		Subject:   "RE: [" + ref + "] Your order",
		BodyText:  "Still broken, please look again.",
	}
}

func TestInboundService_GuestReplyReopensResolvedOrClosedTicket(t *testing.T) {
	for _, status := range []int{models.StatusResolved, models.StatusClosed} {
		t.Run(models.StatusName[status], func(t *testing.T) {
			ticket := guestTicket(7101, "ESC-07101", "owner@example.com", status)
			svc, lookup, writer := newInboundSvc(t, "", nil)
			lookup.ticketsByRef["ESC-07101"] = ticket

			result, err := svc.Process(context.Background(), requesterReply("ESC-07101", "Owner@Example.com"))
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if result.Outcome != OutcomeRepliedToExisting {
				t.Fatalf("Outcome = %q, want replied_to_existing", result.Outcome)
			}
			if len(writer.statusCalls) != 1 {
				t.Fatalf("ChangeStatus called %d times, want 1", len(writer.statusCalls))
			}
			call := writer.statusCalls[0]
			if call.ticketID != ticket.ID || call.newStatus != models.StatusReopened {
				t.Fatalf("ChangeStatus(%d, %d), want (%d, reopened)", call.ticketID, call.newStatus, ticket.ID)
			}
			if call.causerID != nil {
				t.Fatalf("guest reopen causer = %v, want nil", *call.causerID)
			}
		})
	}
}

func TestInboundService_UserRequesterReplyReopensResolvedTicket(t *testing.T) {
	ticket := userTicket(7102, "u-7")
	ticket.Status = models.StatusResolved
	svc, lookup, writer := newInboundSvc(t, "", nil)
	svc.WithRequesterEmailResolver(fakeRequesterEmails{"u-7": "user7@example.com"})
	lookup.ticketsByRef["ESC-07001"] = ticket

	if _, err := svc.Process(context.Background(), requesterReply("ESC-07001", "user7@example.com")); err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(writer.statusCalls) != 1 || writer.statusCalls[0].newStatus != models.StatusReopened {
		t.Fatalf("statusCalls = %+v, want one reopen", writer.statusCalls)
	}
	if got := writer.statusCalls[0].causerID; got == nil || *got != "u-7" {
		t.Fatalf("reopen causer = %v, want u-7", got)
	}
}

func TestInboundService_RequesterReplyLeavesActiveTicketStatusAlone(t *testing.T) {
	ticket := guestTicket(7103, "ESC-07103", "owner@example.com", models.StatusWaitingOnCustomer)
	svc, lookup, writer := newInboundSvc(t, "", nil)
	lookup.ticketsByRef["ESC-07103"] = ticket

	if _, err := svc.Process(context.Background(), requesterReply("ESC-07103", "owner@example.com")); err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(writer.statusCalls) != 0 {
		t.Fatalf("ChangeStatus called %d times, want 0", len(writer.statusCalls))
	}
}

func TestInboundService_StrangerReplyDoesNotReopenClosedTicket(t *testing.T) {
	ticket := guestTicket(7104, "ESC-07104", "owner@example.com", models.StatusClosed)
	svc, lookup, writer := newInboundSvc(t, "", nil)
	lookup.ticketsByRef["ESC-07104"] = ticket

	result, err := svc.Process(context.Background(), requesterReply("ESC-07104", "stranger@example.net"))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	assertCreatedForSender(t, result, writer, "stranger@example.net")
	if len(writer.statusCalls) != 0 {
		t.Fatalf("ChangeStatus called %d times, want 0", len(writer.statusCalls))
	}
}

func TestInboundService_ReopenFailureKeepsTheReply(t *testing.T) {
	ticket := guestTicket(7105, "ESC-07105", "owner@example.com", models.StatusResolved)
	svc, lookup, writer := newInboundSvc(t, "", nil)
	writer.statusErr = errors.New("transition refused")
	lookup.ticketsByRef["ESC-07105"] = ticket

	result, err := svc.Process(context.Background(), requesterReply("ESC-07105", "owner@example.com"))
	if err != nil {
		t.Fatalf("err = %v, want nil (reopen failure is logged, not fatal)", err)
	}
	if result.Outcome != OutcomeRepliedToExisting || len(writer.replyCalls) != 1 {
		t.Fatalf("Outcome = %q, replies = %d; want the reply kept", result.Outcome, len(writer.replyCalls))
	}
}
