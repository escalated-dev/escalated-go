package services

import (
	"context"
	"database/sql"
	"testing"

	"github.com/escalated-dev/escalated-go/internal/testdb"
	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/store"
)

// A reopened ticket's resolution clock runs again: every path that moves a
// ticket from resolved or closed to another status clears resolved_at and
// closed_at, as escalated-laravel's markReopened does.

func finishedTicket(t *testing.T, ctx context.Context, s store.Store, ts *TicketService) *models.Ticket {
	t.Helper()
	tk, err := ts.Create(ctx, CreateTicketInput{Subject: "Refund", Description: "Please"})
	if err != nil {
		t.Fatal(err)
	}
	if err := ts.ChangeStatus(ctx, tk.ID, models.StatusResolved, nil); err != nil {
		t.Fatal(err)
	}
	if err := ts.ChangeStatus(ctx, tk.ID, models.StatusClosed, nil); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetTicket(ctx, tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ResolvedAt == nil || got.ClosedAt == nil {
		t.Fatalf("resolved then closed: resolved_at %v closed_at %v", got.ResolvedAt, got.ClosedAt)
	}
	return got
}

func assertReopened(t *testing.T, ctx context.Context, s store.Store, id int64, wantStatus int) {
	t.Helper()
	got, err := s.GetTicket(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != wantStatus {
		t.Fatalf("status %d, want %d", got.Status, wantStatus)
	}
	if got.ResolvedAt != nil || got.ClosedAt != nil {
		t.Errorf("reopened ticket kept resolved_at %v closed_at %v", got.ResolvedAt, got.ClosedAt)
	}
}

func TestChangeStatusReopeningClearsTheResolutionTimestamps(t *testing.T) {
	ctx := context.Background()
	for _, status := range []int{models.StatusReopened, models.StatusOpen, models.StatusInProgress, models.StatusEscalated} {
		t.Run(models.StatusName[status], func(t *testing.T) {
			s := testdb.Store(t, testdb.Open(t))
			ts := NewTicketService(s)
			tk := finishedTicket(t, ctx, s, ts)
			if err := ts.ChangeStatus(ctx, tk.ID, status, nil); err != nil {
				t.Fatal(err)
			}
			assertReopened(t, ctx, s, tk.ID, status)
		})
	}
}

func TestChangeStatusBetweenResolvedAndClosedKeepsTheTimestamps(t *testing.T) {
	ctx := context.Background()
	s := testdb.Store(t, testdb.Open(t))
	ts := NewTicketService(s)
	tk := finishedTicket(t, ctx, s, ts)
	if err := ts.ChangeStatus(ctx, tk.ID, models.StatusResolved, nil); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetTicket(ctx, tk.ID)
	if got.ResolvedAt == nil || got.ClosedAt == nil {
		t.Errorf("closed to resolved cleared resolved_at %v closed_at %v", got.ResolvedAt, got.ClosedAt)
	}
}

// directRun is a ticket for an action that writes the status row directly.
type directRun struct {
	db     *sql.DB
	ticket *models.Ticket
}

func TestDirectStatusActionsReopeningClearTheResolutionTimestamps(t *testing.T) {
	ctx := context.Background()
	cases := map[string]func(run directRun) error{
		"macro": func(run directRun) error {
			return NewMacroService(run.db, nil).runActionContext(ctx, models.MacroAction{Type: "change_status", Value: "open"}, run.ticket.ID, "")
		},
		"automation": func(run directRun) error {
			return NewAutomationRunner(run.db, nil).runAction(models.Automation{}, *run.ticket, models.AutomationAction{Type: "change_status", Value: "open"})
		},
		"workflow": func(run directRun) error {
			return NewWorkflowRunner(run.db, discardLogger()).runAction(models.Workflow{}, run.ticket, TicketData{}, WorkflowAction{Type: "change_status", Value: "open"})
		},
		"escalation": func(run directRun) error {
			return NewEscalationService(run.db, nil).runAction(models.EscalationAction{Type: "escalate"}, run.ticket.ID)
		},
	}
	for name, run := range cases {
		t.Run(name, func(t *testing.T) {
			db := testdb.Open(t)
			s := testdb.Store(t, db)
			tk := finishedTicket(t, ctx, s, NewTicketService(s))
			if err := run(directRun{db: db, ticket: tk}); err != nil {
				t.Fatal(err)
			}
			want := models.StatusOpen
			if name == "escalation" {
				want = models.StatusEscalated
			}
			assertReopened(t, ctx, s, tk.ID, want)
		})
	}
}

func TestDirectStatusActionToClosedKeepsTheTimestamps(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t)
	s := testdb.Store(t, db)
	tk := finishedTicket(t, ctx, s, NewTicketService(s))
	if err := NewMacroService(db, nil).runActionContext(ctx, models.MacroAction{Type: "change_status", Value: "resolved"}, tk.ID, ""); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetTicket(ctx, tk.ID)
	if got.ResolvedAt == nil || got.ClosedAt == nil {
		t.Errorf("closed to resolved cleared resolved_at %v closed_at %v", got.ResolvedAt, got.ClosedAt)
	}
}

// Ending a chat moves its ticket to open; one an agent resolved during the
// chat is reopened.
func TestEndingAChatReopensAResolvedChatTicket(t *testing.T) {
	ctx := context.Background()
	ms := newMockStore()
	svc := NewChatSessionService(ms, NewChatRoutingService(ms), NewBroadcaster(BroadcastConfig{Enabled: false}, nil))
	ticket, session, err := svc.StartSession(ctx, StartSessionInput{GuestName: "Test", GuestEmail: "test@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	stored, _ := ms.GetTicket(ctx, ticket.ID)
	resolved := stored.CreatedAt
	stored.Status = models.StatusResolved
	stored.ResolvedAt = &resolved
	if err := ms.UpdateTicket(ctx, stored); err != nil {
		t.Fatal(err)
	}
	if err := svc.EndSession(ctx, session, nil); err != nil {
		t.Fatal(err)
	}
	got, _ := ms.GetTicket(ctx, ticket.ID)
	if got.Status != models.StatusOpen || got.ResolvedAt != nil || got.ClosedAt != nil {
		t.Errorf("after the chat ended: status %d resolved_at %v closed_at %v", got.Status, got.ResolvedAt, got.ClosedAt)
	}
}
