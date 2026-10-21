package services

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/escalated-dev/escalated-go/internal/sqldialect"
	"github.com/escalated-dev/escalated-go/internal/testdb"
	"github.com/escalated-dev/escalated-go/models"
)

// eventRecorder is a webhook endpoint that remembers every event name it got.
type eventRecorder struct {
	mu     sync.Mutex
	events []string
	srv    *httptest.Server
}

func newEventRecorder(t *testing.T) *eventRecorder {
	t.Helper()
	rec := &eventRecorder{}
	rec.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var env struct {
			Event string `json:"event"`
		}
		_ = json.Unmarshal(body, &env)
		rec.mu.Lock()
		rec.events = append(rec.events, env.Event)
		rec.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(rec.srv.Close)
	return rec
}

func (r *eventRecorder) waitFor(t *testing.T, event string, want int) int {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		r.mu.Lock()
		n := 0
		for _, e := range r.events {
			if e == event {
				n++
			}
		}
		r.mu.Unlock()
		if n >= want || time.Now().After(deadline) {
			return n
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func vocabularyTicketService(t *testing.T) (*TicketService, *eventRecorder, func() *models.Ticket) {
	t.Helper()
	db := newWorkflowTestDB(t)
	s := testdb.Store(t, db)
	ts := NewTicketService(s)
	ts.Workflows = NewWorkflowRunner(db, discardLogger())
	ts.Workflows.Tickets = ts
	d := NewWebhookDispatcher(db, discardLogger())
	d.Client = &http.Client{Timeout: 5 * time.Second}
	d.RetryBackoff = 0
	ts.Webhooks = d
	rec := newEventRecorder(t)
	insertWebhook(t, db, rec.srv.URL, []string{
		"ticket.tagged", "ticket.untagged", "internal_note.added", "ticket.replied",
		"sla.breached", "sla.warning",
	}, nil, true)
	mk := func() *models.Ticket {
		tk, err := ts.Create(context.Background(), CreateTicketInput{Subject: "S", Description: "D"})
		if err != nil {
			t.Fatal(err)
		}
		return tk
	}
	return ts, rec, mk
}

// Tag changes are announced as ticket.tag_added / ticket.tag_removed, and a
// workflow or webhook saved under a legacy name (ticket.tagged,
// ticket.untagged) still receives them. Re-adding a tag the ticket already has
// announces nothing.
func TestTagChangesEmitCanonicalEventsAndLegacySubscriptionsStillMatch(t *testing.T) {
	ts, rec, mk := vocabularyTicketService(t)
	db := ts.Workflows.DB
	ctx := context.Background()
	tk := mk()
	tagID := insertTagRow(t, db, "vip")

	addedWF := insertWorkflow(t, db, "ticket.tag_added", `{}`, `[]`, true, false, 0)
	legacyWF := insertWorkflow(t, db, "ticket.tagged", `{}`, `[]`, true, false, 1)
	removedWF := insertWorkflow(t, db, "ticket.tag_removed", `{}`, `[]`, true, false, 2)

	if err := ts.AddTag(ctx, tk.ID, tagID, nil); err != nil {
		t.Fatalf("AddTag: %v", err)
	}
	if err := ts.AddTag(ctx, tk.ID, tagID, nil); err != nil {
		t.Fatalf("AddTag again: %v", err)
	}
	if err := ts.RemoveTag(ctx, tk.ID, tagID, nil); err != nil {
		t.Fatalf("RemoveTag: %v", err)
	}

	for name, c := range map[string]struct{ id int64 }{"tag_added": {addedWF}, "legacy ticket.tagged": {legacyWF}, "tag_removed": {removedWF}} {
		if n := waitWorkflowLogs(t, db, c.id, 1); n != 1 {
			t.Errorf("%s workflow ran %d time(s), want 1", name, n)
		}
	}
	if n := rec.waitFor(t, "ticket.tag_added", 1); n != 1 {
		t.Errorf("webhook got ticket.tag_added %d time(s), want 1", n)
	}
	if n := rec.waitFor(t, "ticket.tag_removed", 1); n != 1 {
		t.Errorf("webhook got ticket.tag_removed %d time(s), want 1", n)
	}
	time.Sleep(50 * time.Millisecond)
	if n := rec.waitFor(t, "ticket.tag_added", 2); n != 1 {
		t.Errorf("re-adding an attached tag announced ticket.tag_added again (%d)", n)
	}

	var acts int
	if err := db.QueryRow(sqldialect.Rebind(db, `SELECT COUNT(*) FROM escalated_ticket_activities WHERE ticket_id = ? AND action IN (?, ?)`),
		tk.ID, models.ActionTagsAdded, models.ActionTagsRemoved).Scan(&acts); err != nil {
		t.Fatal(err)
	}
	if acts != 2 {
		t.Errorf("tag activities = %d, want 2", acts)
	}
}

// Workflow add_tag / remove_tag actions go through the ticket service, so they
// announce the change like any other tag edit.
func TestWorkflowTagActionsEmitTagEvents(t *testing.T) {
	ts, rec, mk := vocabularyTicketService(t)
	db := ts.Workflows.DB
	tk := mk()
	insertTagRow(t, db, "urgent")

	tagged := insertWorkflow(t, db, "ticket.tag_added", `{}`, `[]`, true, false, 5)
	insertWorkflow(t, db, "ticket.priority_changed", `{}`, `[{"type":"add_tag","value":"urgent"}]`, true, false, 0)

	if err := ts.ChangePriority(context.Background(), tk.ID, models.PriorityUrgent, nil); err != nil {
		t.Fatal(err)
	}
	if n := waitWorkflowLogs(t, db, tagged, 1); n != 1 {
		t.Errorf("tag_added workflow ran %d time(s) after a workflow add_tag, want 1", n)
	}
	if n := rec.waitFor(t, "ticket.tag_added", 1); n != 1 {
		t.Errorf("webhook got ticket.tag_added %d time(s), want 1", n)
	}
}

// Workflows and webhooks saved under the legacy reply and note names still run.
func TestLegacyReplyAndNoteNamesStillMatch(t *testing.T) {
	ts, rec, mk := vocabularyTicketService(t)
	db := ts.Workflows.DB
	tk := mk()
	replied := insertWorkflow(t, db, "ticket.replied", `{}`, `[]`, true, false, 0)
	noted := insertWorkflow(t, db, "internal_note.added", `{}`, `[]`, true, false, 1)
	agent := "User"
	uid := models.UserID("5")
	if _, err := ts.AddReply(context.Background(), tk.ID, "hi", &agent, &uid, false); err != nil {
		t.Fatal(err)
	}
	if _, err := ts.AddReply(context.Background(), tk.ID, "note", &agent, &uid, true); err != nil {
		t.Fatal(err)
	}
	if n := waitWorkflowLogs(t, db, replied, 1); n != 1 {
		t.Errorf("ticket.replied workflow ran %d time(s), want 1", n)
	}
	if n := waitWorkflowLogs(t, db, noted, 1); n != 1 {
		t.Errorf("internal_note.added workflow ran %d time(s), want 1", n)
	}
	if n := rec.waitFor(t, "reply.created", 1); n != 1 {
		t.Errorf("webhook subscribed to ticket.replied got reply.created %d time(s)", n)
	}
	if n := rec.waitFor(t, "note.created", 1); n != 1 {
		t.Errorf("webhook subscribed to internal_note.added got note.created %d time(s)", n)
	}
}

// The SLA check marks breaches and announces sla.breached, and warns about
// deadlines inside the warning window with sla.warning. It used to mark the
// breach silently, and nothing ever called it.
func TestSLAChecksEmitBreachedAndWarning(t *testing.T) {
	ts, rec, mk := vocabularyTicketService(t)
	db := ts.Workflows.DB
	ctx := context.Background()
	late, soon, fine := mk(), mk(), mk()

	past := time.Now().Add(-time.Hour)
	inTen := time.Now().Add(10 * time.Minute)
	inDay := time.Now().Add(24 * time.Hour)
	set := func(tk *models.Ticket, due time.Time) {
		got, _ := ts.store.GetTicket(ctx, tk.ID)
		got.SLAFirstResponseDueAt = &due
		if err := ts.store.UpdateTicket(ctx, got); err != nil {
			t.Fatal(err)
		}
	}
	set(late, past)
	set(soon, inTen)
	set(fine, inDay)

	breachedWF := insertWorkflow(t, db, "sla.breached", `{}`, `[]`, true, false, 0)
	warningWF := insertWorkflow(t, db, "sla.warning", `{}`, `[]`, true, false, 1)

	sla := NewSLAService(ts.store)
	sla.Tickets = ts
	breaches, warnings, err := sla.Check(ctx, 30)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if breaches != 1 || warnings != 1 {
		t.Fatalf("breaches/warnings = %d/%d, want 1/1", breaches, warnings)
	}
	got, _ := ts.store.GetTicket(ctx, late.ID)
	if !got.SLABreached {
		t.Errorf("late ticket not marked breached")
	}
	if n := waitWorkflowLogs(t, db, breachedWF, 1); n != 1 {
		t.Errorf("sla.breached workflow ran %d time(s), want 1", n)
	}
	if n := waitWorkflowLogs(t, db, warningWF, 1); n != 1 {
		t.Errorf("sla.warning workflow ran %d time(s), want 1", n)
	}
	if n := rec.waitFor(t, "sla.breached", 1); n != 1 {
		t.Errorf("webhook got sla.breached %d time(s)", n)
	}
	if n := rec.waitFor(t, "sla.warning", 1); n != 1 {
		t.Errorf("webhook got sla.warning %d time(s)", n)
	}

	// A breach is announced once.
	if b, _, err := sla.Check(ctx, 30); err != nil || b != 0 {
		t.Errorf("second check breaches = %d (%v), want 0", b, err)
	}
}

// The builder and the webhook form offer canonical names only.
func TestOfferedEventListsAreCanonical(t *testing.T) {
	for _, list := range [][]string{TriggerEvents, SupportedWebhookEvents} {
		seen := map[string]bool{}
		for _, e := range list {
			if models.CanonicalEventName(e) != e {
				t.Errorf("legacy name %q offered", e)
			}
			seen[e] = true
		}
		for _, want := range []string{"ticket.tag_added", "ticket.tag_removed", "sla.breached", "sla.warning", "reply.created", "note.created"} {
			if !seen[want] {
				t.Errorf("%q not offered", want)
			}
		}
	}
	for _, e := range TriggerEvents {
		if e == "reply.agent_reply" {
			t.Errorf("reply.agent_reply is offered but never emitted")
		}
	}
}
