package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/escalated-dev/escalated-go/internal/testdb"
	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/store"
)

// Custom ticket statuses (the shared statuses spec): a custom status takes its
// category's built-in anchor through the normal status change.

// payloadRecorder is a webhook endpoint that keeps each event's payload.
type payloadRecorder struct {
	mu       sync.Mutex
	payloads []struct {
		event   string
		payload map[string]any
	}
}

func newPayloadRecorder(t *testing.T, db *sql.DB, events ...string) *payloadRecorder {
	t.Helper()
	rec := &payloadRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var env struct {
			Event   string         `json:"event"`
			Payload map[string]any `json:"payload"`
		}
		_ = json.Unmarshal(body, &env)
		rec.mu.Lock()
		rec.payloads = append(rec.payloads, struct {
			event   string
			payload map[string]any
		}{env.Event, env.Payload})
		rec.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	insertWebhook(t, db, srv.URL, events, nil, true)
	return rec
}

// waitFor returns the payloads of event once there are want of them (or
// after a timeout).
func (r *payloadRecorder) waitFor(event string, want int) []map[string]any {
	deadline := time.Now().Add(3 * time.Second)
	for {
		r.mu.Lock()
		var out []map[string]any
		for _, p := range r.payloads {
			if p.event == event {
				out = append(out, p.payload)
			}
		}
		r.mu.Unlock()
		if len(out) >= want || time.Now().After(deadline) {
			return out
		}
		time.Sleep(10 * time.Millisecond)
	}
}

type statusFixture struct {
	db       *sql.DB
	store    store.Store
	tickets  *TicketService
	statuses *TicketStatusService
}

func newStatusFixture(t *testing.T) *statusFixture {
	t.Helper()
	db := testdb.Open(t)
	s := testdb.Store(t, db)
	ts := NewTicketService(s)
	ts.Statuses = &TicketStatusService{DB: db, Prefix: testdb.Prefix}
	d := NewWebhookDispatcher(db, discardLogger())
	d.Client = &http.Client{Timeout: 5 * time.Second}
	d.RetryBackoff = 0
	ts.Webhooks = d
	return &statusFixture{db: db, store: s, tickets: ts, statuses: ts.Statuses}
}

func (f *statusFixture) custom(t *testing.T, label, category string) *models.TicketStatus {
	t.Helper()
	st := &models.TicketStatus{Label: label, Category: category}
	if err := f.statuses.Create(context.Background(), st); err != nil {
		t.Fatalf("create status %s: %v", label, err)
	}
	return st
}

func (f *statusFixture) ticket(t *testing.T) *models.Ticket {
	t.Helper()
	tk, err := f.tickets.Create(context.Background(), CreateTicketInput{Subject: "S", Description: "D"})
	if err != nil {
		t.Fatal(err)
	}
	return tk
}

func (f *statusFixture) reload(t *testing.T, id int64) *models.Ticket {
	t.Helper()
	tk, err := f.store.GetTicket(context.Background(), id)
	if err != nil || tk == nil {
		t.Fatalf("reload ticket %d: %v", id, err)
	}
	return tk
}

func TestCustomSolvedStatusResolvesThroughTheStatusChange(t *testing.T) {
	ctx := context.Background()
	f := newStatusFixture(t)
	rec := newPayloadRecorder(t, f.db, "ticket.status_changed", "ticket.resolved")
	solved := f.custom(t, "Fixed upstream", models.StatusCategorySolved)
	if solved.Slug != "fixed_upstream" {
		t.Fatalf("slug %q", solved.Slug)
	}

	// Two tickets past their SLA deadlines; one gets the custom solved status.
	past := time.Now().Add(-time.Hour)
	tk, open := f.ticket(t), f.ticket(t)
	for _, x := range []*models.Ticket{tk, open} {
		x.SLAResolutionDueAt = &past
		if err := f.store.UpdateTicket(ctx, x); err != nil {
			t.Fatal(err)
		}
	}

	if err := f.tickets.ChangeStatusValue(ctx, tk.ID, "fixed_upstream", nil); err != nil {
		t.Fatalf("set custom status: %v", err)
	}
	got := f.reload(t, tk.ID)
	if got.Status != models.StatusResolved || got.ResolvedAt == nil || got.TicketStatusID == nil || *got.TicketStatusID != solved.ID {
		t.Fatalf("ticket after custom solved: status %s resolved_at %v custom %v", got.StatusString(), got.ResolvedAt, got.TicketStatusID)
	}

	// The SLA stops as for the built-in resolved: the breach check skips it.
	sla := NewSLAService(f.store)
	if n, err := sla.CheckBreaches(ctx); err != nil || n != 1 {
		t.Fatalf("breaches %d %v, want only the open ticket", n, err)
	}
	if f.reload(t, tk.ID).SLABreached || !f.reload(t, open.ID).SLABreached {
		t.Fatal("the custom solved ticket was breached, or the open one was not")
	}
	// The breach check's save kept the custom status.
	if got := f.reload(t, tk.ID); got.TicketStatusID == nil {
		t.Fatal("custom status lost")
	}

	changed := rec.waitFor("ticket.status_changed", 1)
	if len(changed) != 1 {
		t.Fatalf("status_changed payloads %v", changed)
	}
	p := changed[0]
	if p["new_custom_status"] != "fixed_upstream" || p["old_custom_status"] != nil || p["new_category"] != "solved" || p["old_category"] != "open" {
		t.Errorf("status_changed payload %v", p)
	}
	if ticket, _ := p["ticket"].(map[string]any); ticket["status"] != "resolved" {
		t.Errorf("payload ticket status %v, want the built-in resolved", p["ticket"])
	}
	if n := len(rec.waitFor("ticket.resolved", 1)); n != 1 {
		t.Errorf("ticket.resolved fired %d times", n)
	}
}

func TestCustomPendingStatusAnchorsAtWaitingOnCustomer(t *testing.T) {
	ctx := context.Background()
	f := newStatusFixture(t)
	f.custom(t, "Waiting on vendor", models.StatusCategoryPending)
	tk := f.ticket(t)
	if err := f.tickets.ChangeStatusValue(ctx, tk.ID, "waiting_on_vendor", nil); err != nil {
		t.Fatal(err)
	}
	if got := f.reload(t, tk.ID); got.Status != models.StatusWaitingOnCustomer || got.TicketStatusID == nil {
		t.Fatalf("status %s custom %v", got.StatusString(), got.TicketStatusID)
	}

	for category, anchor := range map[string]int{"new": models.StatusOpen, "open": models.StatusOpen, "on_hold": models.StatusInProgress} {
		st := f.custom(t, "Custom "+category, category)
		if err := f.tickets.SetCustomStatus(ctx, tk.ID, st, nil); err != nil {
			t.Fatal(err)
		}
		if got := f.reload(t, tk.ID); got.Status != anchor || *got.TicketStatusID != st.ID {
			t.Errorf("%s: status %s custom %v", category, got.StatusString(), got.TicketStatusID)
		}
	}
}

func TestBuiltInStatusClearsTheCustomStatus(t *testing.T) {
	ctx := context.Background()
	f := newStatusFixture(t)
	f.custom(t, "Waiting on vendor", models.StatusCategoryPending)
	newCat := f.custom(t, "Triage", models.StatusCategoryNew)
	tk := f.ticket(t)

	set := func(value string) {
		t.Helper()
		if err := f.tickets.ChangeStatusValue(ctx, tk.ID, value, nil); err != nil {
			t.Fatalf("set %s: %v", value, err)
		}
		if f.reload(t, tk.ID).TicketStatusID == nil {
			t.Fatalf("%s not set", value)
		}
	}

	// The status endpoint path, by slug and by integer.
	set("waiting_on_vendor")
	if err := f.tickets.ChangeStatusValue(ctx, tk.ID, "escalated", nil); err != nil {
		t.Fatal(err)
	}
	if got := f.reload(t, tk.ID); got.Status != models.StatusEscalated || got.TicketStatusID != nil {
		t.Fatalf("after built-in slug: %s %v", got.StatusString(), got.TicketStatusID)
	}
	set("waiting_on_vendor")
	if err := f.tickets.ChangeStatusValue(ctx, tk.ID, float64(models.StatusOpen), nil); err != nil {
		t.Fatal(err)
	}
	if got := f.reload(t, tk.ID); got.Status != models.StatusOpen || got.TicketStatusID != nil {
		t.Fatalf("after built-in integer: %s %v", got.StatusString(), got.TicketStatusID)
	}

	// Snooze.
	set("waiting_on_vendor")
	snooze := NewSnoozeService(f.store)
	if err := snooze.SnoozeTicket(ctx, tk.ID, time.Now().Add(time.Hour), nil); err != nil {
		t.Fatal(err)
	}
	if got := f.reload(t, tk.ID); got.Status != models.StatusSnoozed || got.TicketStatusID != nil {
		t.Fatalf("after snooze: %s %v", got.StatusString(), got.TicketStatusID)
	}
	if err := snooze.UnsnoozeTicket(ctx, tk.ID, nil); err != nil {
		t.Fatal(err)
	}

	// Assigning an open ticket moves it to in_progress, a built-in status.
	if err := f.tickets.SetCustomStatus(ctx, tk.ID, newCat, nil); err != nil {
		t.Fatal(err)
	}
	if err := f.tickets.Assign(ctx, tk.ID, "7", nil); err != nil {
		t.Fatal(err)
	}
	if got := f.reload(t, tk.ID); got.Status != models.StatusInProgress || got.TicketStatusID != nil {
		t.Fatalf("after assign: %s %v", got.StatusString(), got.TicketStatusID)
	}

	// Merging closes the source ticket.
	set("waiting_on_vendor")
	target := f.ticket(t)
	merges := &TicketMergeService{DB: f.db, Prefix: testdb.Prefix, Tickets: f.tickets}
	if _, _, err := merges.Merge(ctx, tk.ID, target.Reference, nil); err != nil {
		t.Fatal(err)
	}
	if got := f.reload(t, tk.ID); got.Status != models.StatusClosed || got.TicketStatusID != nil {
		t.Fatalf("after merge: %s %v", got.StatusString(), got.TicketStatusID)
	}
}

func TestSameCategoryMoveStillAnnouncesTheChange(t *testing.T) {
	ctx := context.Background()
	f := newStatusFixture(t)
	rec := newPayloadRecorder(t, f.db, "ticket.status_changed")
	vendor := f.custom(t, "Waiting on vendor", models.StatusCategoryPending)
	f.custom(t, "Waiting on legal", models.StatusCategoryPending)
	tk := f.ticket(t)

	if err := f.tickets.ChangeStatusValue(ctx, tk.ID, "waiting_on_vendor", nil); err != nil {
		t.Fatal(err)
	}
	if err := f.tickets.ChangeStatusValue(ctx, tk.ID, "waiting_on_legal", nil); err != nil {
		t.Fatal(err)
	}
	got := f.reload(t, tk.ID)
	if got.Status != models.StatusWaitingOnCustomer || got.TicketStatusID == nil || *got.TicketStatusID == vendor.ID {
		t.Fatalf("after sub-status move: %s %v", got.StatusString(), got.TicketStatusID)
	}
	payloads := rec.waitFor("ticket.status_changed", 2)
	if len(payloads) != 2 {
		t.Fatalf("status_changed fired %d times, want 2", len(payloads))
	}
	var move map[string]any
	for _, p := range payloads {
		if p["new_custom_status"] == "waiting_on_legal" {
			move = p
		}
	}
	if move == nil || move["old_custom_status"] != "waiting_on_vendor" || move["old_category"] != "pending" || move["new_category"] != "pending" {
		t.Errorf("sub-status move payload %v", payloads)
	}
}

func TestDeletingACustomStatusKeepsTheTicketsStatus(t *testing.T) {
	ctx := context.Background()
	f := newStatusFixture(t)
	solved := f.custom(t, "Fixed upstream", models.StatusCategorySolved)
	tk := f.ticket(t)
	if err := f.tickets.SetCustomStatus(ctx, tk.ID, solved, nil); err != nil {
		t.Fatal(err)
	}
	if err := f.statuses.Delete(ctx, solved); err != nil {
		t.Fatal(err)
	}
	if got := f.reload(t, tk.ID); got.Status != models.StatusResolved || got.TicketStatusID != nil {
		t.Fatalf("after delete: %s %v", got.StatusString(), got.TicketStatusID)
	}
	if st, _ := f.statuses.Get(ctx, solved.ID); st != nil {
		t.Fatal("status not deleted")
	}
}

func TestTicketStatusRules(t *testing.T) {
	ctx := context.Background()
	f := newStatusFixture(t)

	// Unknown values are rejected; a system row's slug is its built-in.
	tk := f.ticket(t)
	if err := f.tickets.ChangeStatusValue(ctx, tk.ID, "nope", nil); !errors.Is(err, ErrInvalidStatus) {
		t.Errorf("unknown slug: %v", err)
	}
	if err := f.statuses.EnsureSystem(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.statuses.EnsureSystem(ctx); err != nil {
		t.Fatalf("second ensure: %v", err)
	}
	all, err := f.statuses.List(ctx)
	if err != nil || len(all) != len(models.StatusName) {
		t.Fatalf("system rows %d %v", len(all), err)
	}
	closed, _ := f.statuses.BySlug(ctx, "closed")
	if closed == nil || !closed.IsSystem {
		t.Fatalf("closed system row %v", closed)
	}
	if err := f.statuses.Delete(ctx, closed); !errors.Is(err, ErrSystemStatus) {
		t.Errorf("deleting a system row: %v", err)
	}
	if err := f.tickets.SetCustomStatus(ctx, tk.ID, closed, nil); !errors.Is(err, ErrInvalidStatus) {
		t.Errorf("a system row as a custom status: %v", err)
	}

	// Slugs: generated, de-duplicated, never a built-in's name.
	a, b := f.custom(t, "On vendor", "on_hold"), f.custom(t, "On vendor", "on_hold")
	if a.Slug != "on_vendor" || b.Slug != "on_vendor_2" {
		t.Errorf("slugs %s %s", a.Slug, b.Slug)
	}
	if c := f.custom(t, "Resolved", "solved"); c.Slug != "resolved_2" {
		t.Errorf("a label equal to a built-in got slug %s", c.Slug)
	}

	// is_default is exclusive within a category.
	first := &models.TicketStatus{Label: "First", Category: "pending", IsDefault: true}
	second := &models.TicketStatus{Label: "Second", Category: "pending", IsDefault: true}
	other := &models.TicketStatus{Label: "Other", Category: "open", IsDefault: true}
	for _, st := range []*models.TicketStatus{first, other, second} {
		if err := f.statuses.Create(ctx, st); err != nil {
			t.Fatal(err)
		}
	}
	reload := func(id int64) *models.TicketStatus {
		st, err := f.statuses.Get(ctx, id)
		if err != nil || st == nil {
			t.Fatalf("get %d: %v", id, err)
		}
		return st
	}
	if reload(first.ID).IsDefault || !reload(second.ID).IsDefault || !reload(other.ID).IsDefault {
		t.Error("is_default not exclusive per category on create")
	}
	first.IsDefault = true
	if err := f.statuses.Update(ctx, first); err != nil {
		t.Fatal(err)
	}
	if !reload(first.ID).IsDefault || reload(second.ID).IsDefault {
		t.Error("is_default not exclusive per category on update")
	}
	if c := reload(first.ID); c.Color != models.DefaultStatusColor {
		t.Errorf("default colour %q", c.Color)
	}

	// The list runs category by category in the categories' order.
	all, _ = f.statuses.List(ctx)
	last := -1
	for _, st := range all {
		idx := -1
		for i, c := range models.StatusCategories {
			if c == st.Category {
				idx = i
			}
		}
		if idx < last {
			t.Fatalf("list out of category order at %s (%s)", st.Slug, st.Category)
		}
		last = idx
	}
}

// Workflows set a custom status directly, as they set a built-in one, so the
// change does not run workflows again.
func TestWorkflowChangeStatusTakesACustomSlug(t *testing.T) {
	f := newStatusFixture(t)
	vendor := f.custom(t, "Waiting on vendor", models.StatusCategoryPending)
	tk := f.ticket(t)
	runner := NewWorkflowRunner(f.db, discardLogger())

	if err := runner.runAction(models.Workflow{}, tk, TicketData{}, WorkflowAction{Type: "change_status", Value: "waiting_on_vendor"}); err != nil {
		t.Fatal(err)
	}
	if got := f.reload(t, tk.ID); got.Status != models.StatusWaitingOnCustomer || got.TicketStatusID == nil || *got.TicketStatusID != vendor.ID {
		t.Fatalf("workflow custom status: %s %v", got.StatusString(), got.TicketStatusID)
	}
	if err := runner.runAction(models.Workflow{}, tk, TicketData{}, WorkflowAction{Type: "change_status", Value: "closed"}); err != nil {
		t.Fatal(err)
	}
	if got := f.reload(t, tk.ID); got.Status != models.StatusClosed || got.TicketStatusID != nil {
		t.Fatalf("workflow built-in status: %s %v", got.StatusString(), got.TicketStatusID)
	}
	if err := runner.runAction(models.Workflow{}, tk, TicketData{}, WorkflowAction{Type: "change_status", Value: "nope"}); !errors.Is(err, ErrInvalidStatus) {
		t.Errorf("unknown slug: %v", err)
	}

	autos := NewAutomationRunner(f.db, discardLogger())
	if err := autos.runAction(models.Automation{}, *tk, models.AutomationAction{Type: "change_status", Value: "waiting_on_vendor"}); err != nil {
		t.Fatal(err)
	}
	if got := f.reload(t, tk.ID); got.Status != models.StatusWaitingOnCustomer || got.TicketStatusID == nil {
		t.Fatalf("automation custom status: %s %v", got.StatusString(), got.TicketStatusID)
	}

	macros := NewMacroService(f.db, discardLogger())
	macros.Tickets = f.tickets
	if err := macros.runAction(models.MacroAction{Type: "set_status", Value: "resolved"}, tk.ID, "7"); err != nil {
		t.Fatal(err)
	}
	if got := f.reload(t, tk.ID); got.Status != models.StatusResolved || got.TicketStatusID != nil {
		t.Fatalf("macro built-in status: %s %v", got.StatusString(), got.TicketStatusID)
	}
	if err := macros.runAction(models.MacroAction{Type: "set_status", Value: "waiting_on_vendor"}, tk.ID, "7"); err != nil {
		t.Fatal(err)
	}
	if got := f.reload(t, tk.ID); got.TicketStatusID == nil || *got.TicketStatusID != vendor.ID {
		t.Fatalf("macro custom status: %v", got.TicketStatusID)
	}
}
