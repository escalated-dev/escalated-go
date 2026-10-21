package router_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	escalated "github.com/escalated-dev/escalated-go"
	"github.com/escalated-dev/escalated-go/internal/sqldialect"
	"github.com/escalated-dev/escalated-go/internal/testdb"
	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/router"
	"github.com/escalated-dev/escalated-go/services"
)

// Ticket notification emails, with the recipients escalated-laravel's
// notification listeners pick. Requests go through the mounted routes; the
// host's users are a map, as a host's UserEmail callback would look them up.

var notifyUsers = map[models.UserID]string{
	"1": "lead@example.com",    // agent who assigns and changes status
	"2": "agent@example.com",   // the assignee
	"3": "watcher@example.com", // a follower
	"7": "customer@example.com",
}

type notifyFixture struct {
	esc    *escalated.Escalated
	mailer *codeMailer
	h      http.Handler
}

func newNotifyFixture(t *testing.T, mount string, tweak func(*escalated.Config)) notifyFixture {
	t.Helper()
	cfg := escalated.DefaultConfig()
	cfg.DB = testdb.Open(t)
	cfg.UIEnabled = true
	cfg.EnableWidget = true
	cfg.Widget.Enabled = true
	cfg.GuestRateLimit.Disabled = true
	mailer := newCodeMailer()
	cfg.Mailer = mailer
	cfg.MailFrom = "support@example.com"
	cfg.Notifications.Sync = true
	cfg.Notifications.Domain = "support.example.com"
	cfg.Notifications.BaseURL = "https://support.example.com"
	cfg.UserIDFunc = func(r *http.Request) models.UserID { return models.UserID(r.Header.Get("X-User")) }
	cfg.AgentCheck = func(r *http.Request) bool { u := r.Header.Get("X-User"); return u == "1" || u == "2" || u == "3" }
	cfg.UserEmail = func(_ context.Context, id models.UserID) (string, error) { return notifyUsers[id], nil }
	if tweak != nil {
		tweak(&cfg)
	}
	esc, err := escalated.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var h http.Handler
	if mount == "chi" {
		r := chi.NewRouter()
		router.MountChi(r, esc)
		h = r
	} else {
		mux := http.NewServeMux()
		router.MountStdlib(mux, esc)
		h = mux
	}
	return notifyFixture{esc: esc, mailer: mailer, h: h}
}

func (f notifyFixture) do(t *testing.T, user, method, path string, body any) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, "/escalated"+path, &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if user != "" {
		req.Header.Set("X-User", user)
	}
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, req)
	if rec.Code/100 != 2 {
		t.Fatalf("%s %s: %d %s", method, path, rec.Code, rec.Body.String())
	}
	out := map[string]any{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return out
}

// take returns the notifications sent since the last call (verification code
// emails left out), as "to: subject" lines in order, plus the messages.
func (f notifyFixture) take(from *int) ([]string, []services.Mail) {
	f.mailer.mu.Lock()
	defer f.mailer.mu.Unlock()
	var lines []string
	var mails []services.Mail
	for _, m := range f.mailer.sent[*from:] {
		if strings.Contains(m.Subject, "verification code") {
			continue
		}
		lines = append(lines, m.To+": "+m.Subject)
		mails = append(mails, m)
	}
	*from = len(f.mailer.sent)
	sort.Strings(lines)
	return lines, mails
}

func expectSent(t *testing.T, step string, got []string, want ...string) {
	t.Helper()
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("%s:\n got  %q\n want %q", step, got, want)
	}
}

func TestNotificationEmailsFollowLaravelRecipients(t *testing.T) {
	for _, mount := range []string{"chi", "stdlib"} {
		t.Run(mount, func(t *testing.T) {
			f := newNotifyFixture(t, mount, nil)
			seen := 0

			// A signed-in customer opens a ticket: they get the thread anchor.
			out := f.do(t, "7", http.MethodPost, "/tickets", map[string]any{"subject": "Printer", "description": "It is on fire"})
			tk := out["ticket"].(map[string]any)
			id := int64(tk["id"].(float64))
			ref := tk["reference"].(string)
			got, mails := f.take(&seen)
			expectSent(t, "created", got, "customer@example.com: ["+ref+"] New Ticket: Printer")
			root := fmt.Sprintf("<ticket-%d@support.example.com>", id)
			if len(mails) == 1 {
				if mails[0].From != "support@example.com" || !strings.Contains(mails[0].HTML, "Printer") {
					t.Errorf("anchor mail = %+v", mails[0])
				}
				if mails[0].Headers["Message-ID"] != root {
					t.Errorf("anchor Message-ID = %q, want %q", mails[0].Headers["Message-ID"], root)
				}
				if !strings.Contains(mails[0].Text, fmt.Sprintf("https://support.example.com/escalated/tickets/%d", id)) {
					t.Errorf("customer link missing: %q", mails[0].Text)
				}
			}

			// A follower, then the lead assigns agent 2: only agent 2 hears.
			if err := models.AddFollower(f.esc.Config.DB, id, "3"); err != nil {
				t.Fatal(err)
			}
			f.do(t, "1", http.MethodPost, fmt.Sprintf("/agent/tickets/%d/assign", id), map[string]any{"agent_id": "2"})
			got, mails = f.take(&seen)
			expectSent(t, "assigned", got, "agent@example.com: ["+ref+"] Ticket Assigned to You")
			if len(mails) == 1 && (mails[0].Headers["In-Reply-To"] != root || mails[0].Headers["References"] != root) {
				t.Errorf("assignment not threaded: %v", mails[0].Headers)
			}
			// The assignment moved the ticket to in_progress without a status
			// change event, so nothing else was sent.

			// Agent 2 replies: the customer and the follower, not the author.
			out = f.do(t, "2", http.MethodPost, fmt.Sprintf("/agent/tickets/%d/replies", id), map[string]any{"body": "Unplug it"})
			replyID := int64(out["reply"].(map[string]any)["id"].(float64))
			got, mails = f.take(&seen)
			expectSent(t, "agent reply", got,
				"customer@example.com: Re: ["+ref+"] Printer",
				"watcher@example.com: Re: ["+ref+"] Printer")
			for _, m := range mails {
				if m.Headers["Message-ID"] != fmt.Sprintf("<ticket-%d-reply-%d@support.example.com>", id, replyID) || m.Headers["In-Reply-To"] != root {
					t.Errorf("reply headers = %v", m.Headers)
				}
				if !strings.Contains(m.Text, "Unplug it") {
					t.Errorf("reply body missing: %q", m.Text)
				}
			}

			// An internal note emails nobody.
			f.do(t, "2", http.MethodPost, fmt.Sprintf("/agent/tickets/%d/replies", id), map[string]any{"body": "secret", "is_internal": true})
			got, _ = f.take(&seen)
			expectSent(t, "internal note", got)

			// The customer replies: the assignee and the follower.
			f.do(t, "7", http.MethodPost, fmt.Sprintf("/tickets/%d/replies", id), map[string]any{"body": "Still smoking"})
			got, _ = f.take(&seen)
			expectSent(t, "customer reply", got,
				"agent@example.com: Re: ["+ref+"] Printer",
				"watcher@example.com: Re: ["+ref+"] Printer")

			// The follower changes the status: the customer only.
			f.do(t, "3", http.MethodPost, fmt.Sprintf("/agent/tickets/%d/status", id), map[string]any{"status": models.StatusWaitingOnCustomer})
			got, _ = f.take(&seen)
			expectSent(t, "status by follower", got, "customer@example.com: ["+ref+"] Status Updated: Waiting On Customer")

			// The lead escalates: customer + follower for the status, assignee
			// for the escalation.
			f.do(t, "1", http.MethodPost, fmt.Sprintf("/agent/tickets/%d/status", id), map[string]any{"status": models.StatusEscalated})
			got, _ = f.take(&seen)
			expectSent(t, "escalated", got,
				"customer@example.com: ["+ref+"] Status Updated: Escalated",
				"watcher@example.com: ["+ref+"] Status Updated: Escalated",
				"agent@example.com: [Escalated] ["+ref+"] Printer")

			// SLA breach: the assignee.
			past := time.Now().Add(-time.Hour)
			if _, err := f.esc.Config.DB.Exec(sqldialect.Rebind(f.esc.Config.DB, `UPDATE escalated_tickets SET sla_resolution_due_at = ? WHERE id = ?`), past, id); err != nil {
				t.Fatal(err)
			}
			if _, _, err := f.esc.CheckSLA(context.Background(), 0); err != nil {
				t.Fatal(err)
			}
			got, _ = f.take(&seen)
			expectSent(t, "sla breach", got, "agent@example.com: [SLA Breach] ["+ref+"] Resolution SLA Breached")
		})
	}
}

// Guests are emailed at their verified address, and are not told about their
// own replies.
func TestNotificationEmailsForGuests(t *testing.T) {
	f := newNotifyFixture(t, "chi", func(c *escalated.Config) { c.Notifications.InboundSecret = "inbound" })
	seen := 0

	body := withProof(t, f.esc, "ticket", map[string]any{"name": "Gus", "email": "gus@example.com", "subject": "Lost order", "description": "Where is it?"})
	out := f.do(t, "", http.MethodPost, "/widget/tickets", body)
	ref := out["reference"].(string)
	grant := out["guest_access_token"].(string)
	got, mails := f.take(&seen)
	expectSent(t, "guest created", got, "gus@example.com: ["+ref+"] New Ticket: Lost order")
	if len(mails) == 1 && !strings.HasPrefix(mails[0].ReplyTo, "reply+") {
		t.Errorf("signed Reply-To missing: %q", mails[0].ReplyTo)
	}

	tk, _ := f.esc.Store.GetTicketByReference(context.Background(), ref)
	f.do(t, "1", http.MethodPost, fmt.Sprintf("/agent/tickets/%d/assign", tk.ID), map[string]any{"agent_id": "2"})
	f.take(&seen)

	f.do(t, "", http.MethodPost, "/api/guest/tickets/"+grant+"/replies", map[string]any{"body": "Any news?", "email": "gus@example.com"})
	got, _ = f.take(&seen)
	expectSent(t, "guest reply", got, "agent@example.com: Re: ["+ref+"] Lost order")

	f.do(t, "2", http.MethodPost, fmt.Sprintf("/agent/tickets/%d/replies", tk.ID), map[string]any{"body": "Shipped today"})
	got, _ = f.take(&seen)
	expectSent(t, "agent reply to guest", got, "gus@example.com: Re: ["+ref+"] Lost order")
}

// Nothing is emailed without a Mailer, or with notifications disabled.
func TestNotificationEmailsOffByDefault(t *testing.T) {
	f := newNotifyFixture(t, "chi", func(c *escalated.Config) { c.Mailer = nil })
	if f.esc.Notifier != nil {
		t.Fatalf("a notifier was built without a Mailer")
	}
	f.do(t, "7", http.MethodPost, "/tickets", map[string]any{"subject": "S", "description": "D"})

	off := newNotifyFixture(t, "chi", func(c *escalated.Config) { c.Notifications.Disabled = true })
	off.do(t, "7", http.MethodPost, "/tickets", map[string]any{"subject": "S", "description": "D"})
	seen := 0
	if got, _ := off.take(&seen); len(got) != 0 {
		t.Errorf("sent with notifications disabled: %v", got)
	}
}
