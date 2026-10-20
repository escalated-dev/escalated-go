package services

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/escalated-dev/escalated-go/internal/sqldialect"
	"github.com/escalated-dev/escalated-go/internal/testdb"
	"github.com/escalated-dev/escalated-go/models"
)

func waitWorkflowLogs(t *testing.T, db *sql.DB, workflowID int64, want int) int {
	t.Helper()
	var n int
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		_ = db.QueryRow(sqldialect.Rebind(db, `SELECT COUNT(*) FROM escalated_workflow_logs WHERE workflow_id = ?`), workflowID).Scan(&n)
		if n >= want {
			return n
		}
		time.Sleep(10 * time.Millisecond)
	}
	return n
}

// ticket.department_changed, ticket.priority_changed and ticket.updated were
// offered as workflow triggers (and unassigned as a webhook event) but nothing
// emitted them, so workflows on those triggers never ran.
func TestTicketMutationsFireAdvertisedEvents(t *testing.T) {
	db := newWorkflowTestDB(t)
	s := testdb.Store(t, db)
	ts := NewTicketService(s)
	ts.Workflows = NewWorkflowRunner(db, discardLogger())
	ctx := context.Background()

	dept := &models.Department{Name: "Billing", Slug: "billing", IsActive: true}
	if err := s.CreateDepartment(ctx, dept); err != nil {
		t.Fatal(err)
	}
	tk, err := ts.Create(ctx, CreateTicketInput{Subject: "S", Description: "D"})
	if err != nil {
		t.Fatal(err)
	}

	deptWF := insertWorkflow(t, db, "ticket.department_changed", `{}`, `[{"type":"add_note","value":"dept"}]`, true, false, 0)
	prioWF := insertWorkflow(t, db, "ticket.priority_changed", `{}`, `[{"type":"add_note","value":"prio"}]`, true, false, 1)
	unassignWF := insertWorkflow(t, db, "ticket.unassigned", `{}`, `[{"type":"add_note","value":"unassigned"}]`, true, false, 2)
	updatedWF := insertWorkflow(t, db, "ticket.updated", `{}`, `[{"type":"add_note","value":"updated"}]`, true, false, 3)

	if err := ts.ChangeDepartment(ctx, tk.ID, dept.ID, nil); err != nil {
		t.Fatalf("ChangeDepartment: %v", err)
	}
	if err := ts.ChangePriority(ctx, tk.ID, models.PriorityUrgent, nil); err != nil {
		t.Fatalf("ChangePriority: %v", err)
	}
	if err := ts.Assign(ctx, tk.ID, "7", nil); err != nil {
		t.Fatalf("Assign: %v", err)
	}
	if err := ts.Unassign(ctx, tk.ID, nil); err != nil {
		t.Fatalf("Unassign: %v", err)
	}

	for name, c := range map[string]struct {
		id   int64
		want int
	}{
		"department_changed": {deptWF, 1},
		"priority_changed":   {prioWF, 1},
		"unassigned":         {unassignWF, 1},
		"updated":            {updatedWF, 4},
	} {
		if n := waitWorkflowLogs(t, db, c.id, c.want); n < c.want {
			t.Errorf("ticket.%s workflow ran %d time(s), want %d", name, n, c.want)
		}
	}

	got, _ := s.GetTicket(ctx, tk.ID)
	if got.DepartmentID == nil || *got.DepartmentID != dept.ID || got.Priority != models.PriorityUrgent || got.AssignedTo != nil {
		t.Fatalf("ticket not updated: %+v", got)
	}
	var acts int
	if err := db.QueryRow(sqldialect.Rebind(db, `SELECT COUNT(*) FROM escalated_ticket_activities WHERE ticket_id = ? AND action IN (?, ?, ?)`),
		tk.ID, models.ActionDeptChanged, models.ActionPriorityChanged, models.ActionTicketUnassigned).Scan(&acts); err != nil {
		t.Fatal(err)
	}
	if acts != 3 {
		t.Errorf("activities = %d, want 3", acts)
	}

	if err := ts.ChangePriority(ctx, tk.ID, 42, nil); err != ErrInvalidPriority {
		t.Errorf("ChangePriority(42) = %v, want ErrInvalidPriority", err)
	}
	if err := ts.ChangeDepartment(ctx, tk.ID, 99999, nil); err != ErrInvalidDepartment {
		t.Errorf("ChangeDepartment(99999) = %v, want ErrInvalidDepartment", err)
	}
}

// Macros and escalation rules carry their changes through the ticket service
// when it is wired, so they fire events, and a slug value no longer scans as
// 0 (which reopened tickets / set the lowest priority).
func TestMacroActionsGoThroughTheTicketService(t *testing.T) {
	db := newWorkflowTestDB(t)
	s := testdb.Store(t, db)
	ts := NewTicketService(s)
	ts.Workflows = NewWorkflowRunner(db, discardLogger())
	ctx := context.Background()
	tk, err := ts.Create(ctx, CreateTicketInput{Subject: "S", Description: "D"})
	if err != nil {
		t.Fatal(err)
	}
	prioWF := insertWorkflow(t, db, "ticket.priority_changed", `{}`, `[{"type":"add_note","value":"prio"}]`, true, false, 0)

	ms := NewMacroService(db, discardLogger())
	ms.Tickets = ts
	if err := ms.runActionContext(ctx, models.MacroAction{Type: "change_priority", Value: "high"}, tk.ID, "9"); err != nil {
		t.Fatalf("change_priority: %v", err)
	}
	if err := ms.runActionContext(ctx, models.MacroAction{Type: "change_status", Value: "resolved"}, tk.ID, "9"); err != nil {
		t.Fatalf("change_status: %v", err)
	}
	if err := ms.runActionContext(ctx, models.MacroAction{Type: "change_status", Value: "bogus"}, tk.ID, "9"); err == nil {
		t.Errorf("unknown status slug was accepted")
	}

	got, _ := s.GetTicket(ctx, tk.ID)
	if got.Priority != models.PriorityHigh || got.Status != models.StatusResolved || got.ResolvedAt == nil {
		t.Fatalf("priority/status = %d/%d resolved_at=%v, want high/resolved", got.Priority, got.Status, got.ResolvedAt)
	}
	if n := waitWorkflowLogs(t, db, prioWF, 1); n != 1 {
		t.Errorf("priority_changed workflow ran %d time(s), want 1", n)
	}
}

func TestStatusAndPriorityFromValue(t *testing.T) {
	for _, c := range []struct {
		in   any
		want int
		ok   bool
	}{
		{"resolved", models.StatusResolved, true},
		{"5", models.StatusResolved, true},
		{float64(6), models.StatusClosed, true},
		{"nope", 0, false},
		{float64(99), 0, false},
		{float64(1.5), 0, false},
	} {
		got, ok := StatusFromValue(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("StatusFromValue(%v) = %d,%v want %d,%v", c.in, got, ok, c.want, c.ok)
		}
	}
	if p, ok := PriorityFromValue("Urgent"); !ok || p != models.PriorityUrgent {
		t.Errorf("PriorityFromValue(Urgent) = %d,%v", p, ok)
	}
}
