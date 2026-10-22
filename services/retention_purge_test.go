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

// retentionFixture is a migrated database with a RetentionService over it.
type retentionFixture struct {
	t        *testing.T
	ctx      context.Context
	db       *sql.DB
	rs       *RetentionService
	set      func(key, value string)
	workflow int64
	field    int64
}

func newRetentionFixture(t *testing.T) *retentionFixture {
	t.Helper()
	db := testdb.Open(t)
	s := testdb.Store(t, db)
	ctx := context.Background()
	f := &retentionFixture{t: t, ctx: ctx, db: db, rs: NewRetentionService(db, s), set: func(key, value string) {
		t.Helper()
		if err := s.SetSetting(ctx, key, value); err != nil {
			t.Fatal(err)
		}
	}}
	f.workflow = f.insert(`INSERT INTO escalated_workflows (name, trigger_event) VALUES ('wf', 'ticket.created')`)
	f.field = f.insert(`INSERT INTO escalated_custom_fields (name, slug, field_type) VALUES ('Order', 'order', 'text')`)
	return f
}

func (f *retentionFixture) insert(q string, args ...any) int64 {
	f.t.Helper()
	res, err := sqldialect.ExecInsertContext(f.ctx, f.db, q, args...)
	if err != nil {
		f.t.Fatalf("%s: %v", q, err)
	}
	id, _ := res.LastInsertId()
	return id
}

func (f *retentionFixture) exec(q string, args ...any) {
	f.t.Helper()
	if _, err := f.db.ExecContext(f.ctx, sqldialect.Rebind(f.db, q), args...); err != nil {
		f.t.Fatalf("%s: %v", q, err)
	}
}

func (f *retentionFixture) count(table, where string, args ...any) int {
	f.t.Helper()
	var n int
	if err := f.db.QueryRowContext(f.ctx, sqldialect.Rebind(f.db, "SELECT COUNT(1) FROM escalated_"+table+" WHERE "+where), args...).Scan(&n); err != nil {
		f.t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// ticket inserts a ticket with the given status and closed_at, plus a row in
// every table that points at a ticket.
func (f *retentionFixture) ticket(ref string, status int, closedAt *time.Time) int64 {
	f.t.Helper()
	now := time.Now()
	id := f.insert(`INSERT INTO escalated_tickets (reference, subject, description, status, priority, closed_at, created_at, updated_at)
		 VALUES (?, 'Retention', 'body', ?, 2, ?, ?, ?)`, ref, status, closedAt, now, now)

	f.exec(`INSERT INTO escalated_replies (ticket_id, body, is_internal, is_system, is_pinned, created_at, updated_at) VALUES (?, 'hi', false, false, false, ?, ?)`, id, now, now)
	f.exec(`INSERT INTO escalated_attachments (ticket_id, original_filename, mime_type, storage_path, created_at) VALUES (?, 'f', 'text/plain', '/p', ?)`, id, now)
	f.exec(`INSERT INTO escalated_ticket_followers (ticket_id, user_id) VALUES (?, '7')`, id)
	f.exec(`INSERT INTO escalated_satisfaction_ratings (ticket_id, rating) VALUES (?, 5)`, id)
	f.exec(`INSERT INTO escalated_workflow_logs (workflow_id, ticket_id, trigger_event, status) VALUES (?, ?, 'ticket.created', 'success')`, f.workflow, id)
	f.exec(`INSERT INTO escalated_delayed_actions (workflow_id, ticket_id, execute_at) VALUES (?, ?, ?)`, f.workflow, id, now)
	f.exec(`INSERT INTO escalated_chat_sessions (ticket_id) VALUES (?)`, id)
	f.exec(`INSERT INTO escalated_chat_messages (chat_session_id, sender_type, body) SELECT id, 'visitor', 'hello' FROM escalated_chat_sessions WHERE ticket_id = ?`, id)
	f.exec(`INSERT INTO escalated_side_conversations (ticket_id, subject) VALUES (?, 'aside')`, id)
	f.exec(`INSERT INTO escalated_side_conversation_replies (side_conversation_id, body) SELECT id, 'reply' FROM escalated_side_conversations WHERE ticket_id = ?`, id)
	f.exec(`INSERT INTO escalated_custom_field_values (custom_field_id, entity_type, entity_id, value) VALUES (?, 'ticket', ?, 'v')`, f.field, id)
	return id
}

func TestPurgeExpiredNeverLeavesTicketsAlone(t *testing.T) {
	f := newRetentionFixture(t)
	old := time.Now().AddDate(-6, 0, 0)
	id := f.ticket("RET-OLD", models.StatusClosed, &old)

	rep, err := f.rs.PurgeExpired(f.ctx, time.Now(), false)
	if err != nil {
		t.Fatal(err)
	}
	if rep.ClosedTicketsDeleted != 0 || f.count("tickets", "id = ?", id) != 1 || f.count("attachments", "ticket_id = ?", id) != 1 {
		t.Fatalf("never purged something: %+v", rep)
	}
}

func TestPurgeExpiredDeletesOldClosedTicketsWithTheirRows(t *testing.T) {
	f := newRetentionFixture(t)
	f.set("retention_closed_tickets", "1_year")
	old := time.Now().AddDate(-2, 0, 0)
	recent := time.Now().AddDate(0, -1, 0)
	expired := f.ticket("RET-OLD", models.StatusClosed, &old)    // closed two years ago
	kept := f.ticket("RET-NEW", models.StatusClosed, &recent)    // closed last month
	open := f.ticket("RET-OPEN", models.StatusOpen, &old)        // open, whatever closed_at says
	resolved := f.ticket("RET-RES", models.StatusResolved, &old) // resolved, not closed
	f.exec(`INSERT INTO escalated_ticket_links (parent_ticket_id, child_ticket_id, link_type) VALUES (?, ?, 'related')`, kept, expired)
	f.exec(`UPDATE escalated_tickets SET merged_into_id = ? WHERE id = ?`, expired, open)

	// A dry run deletes nothing but reports the candidate.
	rep, err := f.rs.PurgeExpired(f.ctx, time.Now(), true)
	if err != nil {
		t.Fatal(err)
	}
	if rep.ClosedTicketsDeleted != 1 || f.count("tickets", "id = ?", expired) != 1 || f.count("replies", "ticket_id = ?", expired) != 1 {
		t.Fatalf("dry run: %+v", rep)
	}

	rep, err = f.rs.PurgeExpired(f.ctx, time.Now(), false)
	if err != nil {
		t.Fatal(err)
	}
	if rep.ClosedTicketsDeleted != 1 || rep.ClosedTicketCandidates != 1 {
		t.Fatalf("report %+v", rep)
	}
	if f.count("tickets", "id = ?", expired) != 0 {
		t.Fatal("the expired ticket is still there")
	}
	for _, id := range []int64{kept, open, resolved} {
		if f.count("tickets", "id = ?", id) != 1 || f.count("replies", "ticket_id = ?", id) != 1 {
			t.Errorf("ticket %d or its reply was purged", id)
		}
	}
	for _, table := range []string{"replies", "attachments", "ticket_followers", "satisfaction_ratings", "workflow_logs", "delayed_actions", "chat_sessions", "side_conversations"} {
		if n := f.count(table, "ticket_id = ?", expired); n != 0 {
			t.Errorf("%s keeps %d rows of the purged ticket", table, n)
		}
	}
	if n := f.count("custom_field_values", "entity_type = 'ticket' AND entity_id = ?", expired); n != 0 {
		t.Errorf("custom field values left: %d", n)
	}
	if n := f.count("ticket_links", "child_ticket_id = ?", expired); n != 0 {
		t.Errorf("links left: %d", n)
	}
	if n := f.count("chat_messages", "1 = 1"); n != 3 {
		t.Errorf("chat messages: %d, want the kept tickets' 3", n)
	}
	if n := f.count("side_conversation_replies", "1 = 1"); n != 3 {
		t.Errorf("side conversation replies: %d, want the kept tickets' 3", n)
	}
	if n := f.count("tickets", "id = ? AND merged_into_id IS NULL", open); n != 1 {
		t.Error("a ticket merged into the purged one still points at it")
	}
}

func TestPurgeExpiredAuditLogsAndGuestVerifications(t *testing.T) {
	f := newRetentionFixture(t)
	f.set("retention_audit_logs", "90_days")
	f.exec(`INSERT INTO escalated_audit_logs (action, entity_type, created_at) VALUES ('updated', 'ticket', ?)`, time.Now().AddDate(0, 0, -100))
	f.exec(`INSERT INTO escalated_audit_logs (action, entity_type, created_at) VALUES ('updated', 'ticket', ?)`, time.Now().AddDate(0, 0, -10))
	f.exec(`INSERT INTO escalated_guest_verifications (id, email, purpose, code_hash, expires_at, created_at) VALUES ('a', 'g@example.com', 'create', 'h', ?, ?)`, time.Now().Add(-time.Hour), time.Now())
	f.exec(`INSERT INTO escalated_guest_verifications (id, email, purpose, code_hash, expires_at, created_at) VALUES ('b', 'g@example.com', 'create', 'h', ?, ?)`, time.Now().Add(time.Hour), time.Now())

	rep, err := f.rs.PurgeExpired(f.ctx, time.Now(), true)
	if err != nil || rep.AuditLogsDeleted != 1 || rep.GuestVerificationsDeleted != 1 || f.count("audit_logs", "1 = 1") != 2 || f.count("guest_verifications", "1 = 1") != 2 {
		t.Fatalf("dry run %+v %v", rep, err)
	}
	rep, err = f.rs.PurgeExpired(f.ctx, time.Now(), false)
	if err != nil {
		t.Fatal(err)
	}
	if rep.AuditLogsDeleted != 1 || f.count("audit_logs", "1 = 1") != 1 {
		t.Errorf("audit logs %+v", rep)
	}
	if rep.GuestVerificationsDeleted != 1 || f.count("guest_verifications", "id = 'b'") != 1 || f.count("guest_verifications", "1 = 1") != 1 {
		t.Errorf("guest verifications %+v", rep)
	}
}

func TestRetentionPreview(t *testing.T) {
	f := newRetentionFixture(t)
	old := time.Now().AddDate(-2, 0, 0)
	f.ticket("RET-OLD", models.StatusClosed, &old)
	if p := f.rs.Preview(f.ctx, time.Now()); p.Tickets != 0 || p.Attachments != 0 || p.AuditLogs != 0 {
		t.Fatalf("never previews nothing: %+v", p)
	}
	f.set("retention_closed_tickets", "1_year")
	f.set("retention_attachments", "1_year")
	f.exec(`UPDATE escalated_attachments SET created_at = ?`, old)
	if p := f.rs.Preview(f.ctx, time.Now()); p.Tickets != 1 || p.Attachments != 1 || p.AuditLogs != 0 {
		t.Fatalf("preview %+v", p)
	}
}
