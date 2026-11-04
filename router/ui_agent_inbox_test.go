package router_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"sort"
	"testing"
	"time"

	escalated "github.com/escalated-dev/escalated-go"
	"github.com/escalated-dev/escalated-go/internal/sqldialect"
	"github.com/escalated-dev/escalated-go/internal/testdb"
	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/ticketaccess"
)

// The agent panel's optional inbox and queue props, and the list filters its
// rail, views and stat strip link to (shared frontend docs/agent-panel.md,
// after escalated-laravel's AgentInboxPropsTest). Every number must be what
// the agent gets by opening the list it labels.

const (
	inboxAgent = "900"
	inboxOther = "901"
	inboxIndex = "/escalated/agent/tickets"
)

var builtInViewIDs = []string{"urgent", "sla_at_risk", "waiting_on_me", "waiting_on_customer", "recently_updated"}

type inboxFixture struct {
	t   *testing.T
	esc *escalated.Escalated
	h   http.Handler
}

func newInboxFixture(t *testing.T) *inboxFixture {
	t.Helper()
	cfg := escalated.DefaultConfig()
	cfg.DB = testdb.Open(t)
	cfg.UIEnabled = true
	cfg.AgentCheck = func(r *http.Request) bool { return r.Header.Get(headerRole) == "agent" }
	cfg.AdminCheck = func(r *http.Request) bool { return r.Header.Get(headerRole) == "admin" }
	cfg.UserIDFunc = func(r *http.Request) models.UserID { return models.UserID(r.Header.Get(headerUser)) }
	esc, err := escalated.New(cfg)
	if err != nil {
		t.Fatalf("new escalated: %v", err)
	}
	f := &inboxFixture{t: t, esc: esc}
	f.h = f.mounts()["chi"]
	return f
}

func (f *inboxFixture) mounts() map[string]http.Handler {
	return (&uiFixture{esc: f.esc}).mounts()
}

type ticketOpt func(*models.Ticket)

func status(s int) ticketOpt   { return func(t *models.Ticket) { t.Status = s } }
func priority(p int) ticketOpt { return func(t *models.Ticket) { t.Priority = p } }
func assigned(id string) ticketOpt {
	return func(t *models.Ticket) { u := models.UserID(id); t.AssignedTo = &u }
}
func resolvedAt(at time.Time) ticketOpt {
	return func(t *models.Ticket) { t.ResolvedAt = &at }
}
func closedNow() ticketOpt {
	return func(t *models.Ticket) { now := time.Now(); t.Status = models.StatusClosed; t.ClosedAt = &now }
}
func department(id int64) ticketOpt { return func(t *models.Ticket) { t.DepartmentID = &id } }

func (f *inboxFixture) ticket(opts ...ticketOpt) *models.Ticket {
	f.t.Helper()
	tk := &models.Ticket{Subject: "Ticket", Description: "Body", TicketType: "question", Priority: models.PriorityLow}
	for _, o := range opts {
		o(tk)
	}
	if err := f.esc.Store.CreateTicket(context.Background(), tk); err != nil {
		f.t.Fatalf("create ticket: %v", err)
	}
	return tk
}

func (f *inboxFixture) exec(q string, args ...any) {
	f.t.Helper()
	db := f.esc.Config.DB
	if _, err := db.Exec(sqldialect.Rebind(db, q), args...); err != nil {
		f.t.Fatalf("%s: %v", q, err)
	}
}

func (f *inboxFixture) index(query string) inertiaPage {
	f.t.Helper()
	return visit(f.t, f.h, inboxIndex+query, inboxAgent, "agent")
}

func (f *inboxFixture) total(filters map[string]any) int {
	f.t.Helper()
	q := url.Values{}
	for k, v := range filters {
		q.Set(k, fmt.Sprint(v))
	}
	page := f.index("?" + q.Encode())
	return int(obj(f.t, page.Props, "tickets")["total"].(float64))
}

func ticketIDs(t *testing.T, page inertiaPage) []int64 {
	t.Helper()
	var ids []int64
	for _, row := range list(t, obj(t, page.Props, "tickets"), "data") {
		ids = append(ids, int64(row.(map[string]any)["id"].(float64)))
	}
	return ids
}

func asJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func wantJSON(t *testing.T, what string, got any, want string) {
	t.Helper()
	var g, w any
	_ = json.Unmarshal([]byte(asJSON(t, got)), &g)
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatalf("bad want %s: %v", want, err)
	}
	if !reflect.DeepEqual(g, w) {
		t.Errorf("%s = %s, want %s", what, asJSON(t, got), want)
	}
}

func TestAgentInboxPropsOnTheList(t *testing.T) {
	f := newInboxFixture(t)
	f.ticket(assigned(inboxAgent), priority(models.PriorityUrgent))
	f.ticket(assigned(inboxAgent), status(models.StatusWaitingOnCustomer))
	f.ticket(assigned(inboxAgent), status(models.StatusResolved), resolvedAt(time.Now()))
	f.ticket(assigned(inboxAgent), status(models.StatusResolved), resolvedAt(time.Now().Add(-48*time.Hour)))
	f.ticket(assigned(inboxAgent), status(models.StatusSnoozed))
	f.ticket()
	f.ticket(closedNow())
	f.ticket(assigned(inboxOther))

	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			page := visit(t, h, inboxIndex, inboxAgent, "agent")
			inbox := obj(t, page.Props, "inbox")
			if keys := keysOf(inbox); !reflect.DeepEqual(sorted(keys), []string{"counts", "stats", "views"}) {
				t.Errorf("inbox keys %v", keys)
			}
			// Unresolved, unsnoozed tickets in each queue.
			wantJSON(t, "counts", inbox["counts"], `{"mine":2,"unassigned":1,"all":4}`)
			views := list(t, inbox, "views")
			var ids []string
			for _, v := range views {
				ids = append(ids, v.(map[string]any)["id"].(string))
			}
			if !reflect.DeepEqual(ids, builtInViewIDs) {
				t.Errorf("view ids %v", ids)
			}
			wantJSON(t, "urgent view", views[0], `{"id":"urgent","name":"Urgent","count":1,"filters":{"priority":"urgent","open":1}}`)
			byID := map[string]map[string]any{}
			for _, v := range views {
				byID[v.(map[string]any)["id"].(string)] = v.(map[string]any)
			}
			wantJSON(t, "waiting_on_customer", byID["waiting_on_customer"], `{"id":"waiting_on_customer","name":"Waiting on customer","count":1,"filters":{"assigned_to":"900","status":"waiting_on_customer"}}`)
			wantJSON(t, "waiting_on_me filters", byID["waiting_on_me"]["filters"], `{"assigned_to":"900","status":"waiting_on_agent"}`)
			wantJSON(t, "sla_at_risk filters", byID["sla_at_risk"]["filters"], `{"sla_at_risk":1}`)
			wantJSON(t, "recently_updated", byID["recently_updated"], `{"id":"recently_updated","name":"Recently updated","count":4,"filters":{"open":1,"updated_within_hours":24,"sort_by":"updated_at"}}`)
			// The "all" queue: three tickets with status open, one urgent, one resolved today.
			wantJSON(t, "stats", inbox["stats"], `{"open":3,"urgent":1,"sla_at_risk":0,"resolved_today":1}`)
			wantJSON(t, "queue", page.Props["queue"], `{"key":"all","title":"All tickets"}`)
		})
	}
}

func TestAgentInboxQueueFromTheListQuery(t *testing.T) {
	cases := []struct{ name, query, queue, stats string }{
		{"mine", "?assigned_to=900", `{"key":"mine","title":"My inbox"}`, `{"open":1,"urgent":1,"sla_at_risk":0,"resolved_today":1}`},
		{"mine by alias", "?assigned=me", `{"key":"mine","title":"My inbox"}`, `{"open":1,"urgent":1,"sla_at_risk":0,"resolved_today":1}`},
		{"mine by assigned_to=me", "?assigned_to=me", `{"key":"mine","title":"My inbox"}`, `{"open":1,"urgent":1,"sla_at_risk":0,"resolved_today":1}`},
		{"mine with a stat tile on", "?assigned_to=900&priority=urgent", `{"key":"mine","title":"My inbox"}`, `{"open":1,"urgent":1,"sla_at_risk":0,"resolved_today":1}`},
		{"unassigned", "?unassigned=1", `{"key":"unassigned","title":"Unassigned"}`, `{"open":2,"urgent":1,"sla_at_risk":0,"resolved_today":1}`},
		{"all", "", `{"key":"all","title":"All tickets"}`, `{"open":3,"urgent":2,"sla_at_risk":0,"resolved_today":2}`},
		{"a built-in view by its filters", "?priority=urgent&open=1", `{"key":"view:urgent","title":"Urgent"}`, `{"open":2,"urgent":2,"sla_at_risk":0,"resolved_today":0}`},
		{"a built-in view by id", "?view=urgent", `{"key":"view:urgent","title":"Urgent"}`, `{"open":2,"urgent":2,"sla_at_risk":0,"resolved_today":0}`},
		{"an unknown view", "?view=nope", `{"key":"all","title":"All tickets"}`, `{"open":3,"urgent":2,"sla_at_risk":0,"resolved_today":2}`},
	}
	f := newInboxFixture(t)
	f.ticket(assigned(inboxAgent), priority(models.PriorityUrgent))
	f.ticket(assigned(inboxAgent), status(models.StatusResolved), resolvedAt(time.Now()))
	f.ticket()
	f.ticket(status(models.StatusResolved), resolvedAt(time.Now()))
	f.ticket(priority(models.PriorityUrgent))

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			page := f.index(c.query)
			wantJSON(t, "queue", page.Props["queue"], c.queue)
			wantJSON(t, "stats", obj(t, page.Props, "inbox")["stats"], c.stats)
		})
	}
}

func TestAgentInboxViewByIDFiltersTheList(t *testing.T) {
	f := newInboxFixture(t)
	urgent := f.ticket(priority(models.PriorityUrgent))
	f.ticket(priority(models.PriorityUrgent), closedNow())
	f.ticket()

	page := f.index("?view=urgent")
	if ids := ticketIDs(t, page); !reflect.DeepEqual(ids, []int64{urgent.ID}) {
		t.Errorf("ids %v, want [%d]", ids, urgent.ID)
	}
}

func TestAgentInboxViewCountsMatchTheirLists(t *testing.T) {
	f := newInboxFixture(t)
	now := time.Now()
	f.ticket(assigned(inboxAgent), priority(models.PriorityUrgent))
	f.ticket(priority(models.PriorityUrgent), closedNow())
	f.ticket(assigned(inboxAgent), status(models.StatusWaitingOnAgent))
	f.ticket(assigned(inboxAgent), status(models.StatusWaitingOnCustomer))
	f.ticket(func(t *models.Ticket) { t.SLABreached = true })
	f.ticket(func(t *models.Ticket) { due := now.Add(30 * time.Minute); t.SLAResolutionDueAt = &due })
	stale := f.ticket()
	f.exec(`UPDATE escalated_tickets SET updated_at = ? WHERE id = ?`, now.Add(-72*time.Hour), stale.ID)

	views := list(t, obj(t, f.index("").Props, "inbox"), "views")
	got := map[string]int{}
	for _, v := range views {
		view := v.(map[string]any)
		count := int(view["count"].(float64))
		got[view["id"].(string)] = count
		if shown := f.total(view["filters"].(map[string]any)); shown != count {
			t.Errorf("view %v counts %d but its list shows %d", view["id"], count, shown)
		}
	}
	want := map[string]int{"urgent": 1, "sla_at_risk": 2, "waiting_on_me": 1, "waiting_on_customer": 1, "recently_updated": 5}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("view counts %v, want %v", got, want)
	}
}

func TestAgentInboxCountsOnlyVisibleTickets(t *testing.T) {
	f := newInboxFixture(t)
	db := f.esc.Config.DB
	access, err := ticketaccess.New(db, "escalated_", ticketaccess.Options{Visibility: "department"})
	if err != nil {
		t.Fatal(err)
	}
	f.esc.Access = access
	f.esc.Store.(interface{ ConfigureTicketAccess(*ticketaccess.Access) }).ConfigureTicketAccess(access)
	ctx := context.Background()
	support := &models.Department{Name: "Support", Slug: "support", IsActive: true}
	billing := &models.Department{Name: "Billing", Slug: "billing", IsActive: true}
	for _, d := range []*models.Department{support, billing} {
		if err := f.esc.Store.CreateDepartment(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	f.exec(`CREATE TABLE IF NOT EXISTS escalated_department_agent (department_id BIGINT NOT NULL, agent_id VARCHAR(255) NOT NULL, PRIMARY KEY(department_id,agent_id))`)
	f.exec(`INSERT INTO escalated_department_agent (department_id,agent_id) VALUES (?,?)`, support.ID, inboxAgent)

	f.ticket(department(support.ID), assigned(inboxAgent), priority(models.PriorityUrgent))
	f.ticket(department(support.ID))
	f.ticket(department(billing.ID))
	f.ticket(department(billing.ID), priority(models.PriorityUrgent))
	f.ticket(department(billing.ID), status(models.StatusResolved), resolvedAt(time.Now()))

	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			inbox := obj(t, visit(t, h, inboxIndex, inboxAgent, "agent").Props, "inbox")
			wantJSON(t, "counts", inbox["counts"], `{"mine":1,"unassigned":1,"all":2}`)
			wantJSON(t, "stats", inbox["stats"], `{"open":2,"urgent":1,"sla_at_risk":0,"resolved_today":0}`)
			if c := list(t, inbox, "views")[0].(map[string]any)["count"]; c != float64(1) {
				t.Errorf("urgent view count %v, want 1", c)
			}
		})
	}
}

func TestAgentListSLAAtRiskFilter(t *testing.T) {
	f := newInboxFixture(t)
	now := time.Now()
	at := func(d time.Duration) *time.Time { v := now.Add(d); return &v }
	breached := f.ticket(func(t *models.Ticket) { t.SLABreached = true })
	responseDue := f.ticket(func(t *models.Ticket) { t.SLAFirstResponseDueAt = at(20 * time.Minute) })
	resolutionDue := f.ticket(func(t *models.Ticket) { t.SLAResolutionDueAt = at(50 * time.Minute) })
	f.ticket(func(t *models.Ticket) {
		t.SLAFirstResponseDueAt = at(20 * time.Minute)
		t.FirstResponseAt = at(-time.Minute)
	})
	f.ticket(func(t *models.Ticket) { t.SLAResolutionDueAt = at(3 * time.Hour) })
	f.ticket(func(t *models.Ticket) { t.SLABreached = true }, status(models.StatusResolved), resolvedAt(now))
	f.ticket()

	page := f.index("?sla_at_risk=1")
	ids := ticketIDs(t, page)
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	if want := []int64{breached.ID, responseDue.ID, resolutionDue.ID}; !reflect.DeepEqual(ids, want) {
		t.Errorf("ids %v, want %v", ids, want)
	}
	if got := obj(t, obj(t, page.Props, "inbox"), "stats")["sla_at_risk"]; got != float64(3) {
		t.Errorf("sla_at_risk stat %v, want 3", got)
	}
}

func TestAgentListResolvedTodayOpenAndRecentFilters(t *testing.T) {
	f := newInboxFixture(t)
	now := time.Now()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	today := f.ticket(status(models.StatusResolved), resolvedAt(start.Add(time.Minute)))
	f.ticket(status(models.StatusResolved), resolvedAt(start.Add(-24*time.Hour)))
	fresh := f.ticket()
	stale := f.ticket()
	f.exec(`UPDATE escalated_tickets SET updated_at = ? WHERE id = ?`, now.Add(-72*time.Hour), stale.ID)
	f.ticket(closedNow())
	f.ticket(status(models.StatusSnoozed))

	if ids := ticketIDs(t, f.index("?status=resolved&resolved_today=1")); !reflect.DeepEqual(ids, []int64{today.ID}) {
		t.Errorf("resolved today %v, want [%d]", ids, today.ID)
	}
	if n := f.total(map[string]any{"open": 1}); n != 2 {
		t.Errorf("open total %d, want 2", n)
	}
	if ids := ticketIDs(t, f.index("?open=1&updated_within_hours=24")); !reflect.DeepEqual(ids, []int64{fresh.ID}) {
		t.Errorf("recently updated %v, want [%d]", ids, fresh.ID)
	}
}

func TestAgentListRejectsMalformedInboxFilters(t *testing.T) {
	f := newInboxFixture(t)
	for _, c := range []struct{ query, field string }{
		{"sla_at_risk=soon", "sla_at_risk"},
		{"resolved_today=maybe", "resolved_today"},
		{"open=perhaps", "open"},
		{"updated_within_hours=0", "updated_within_hours"},
		{"updated_within_hours=a+day", "updated_within_hours"},
		{"updated_within_hours=8761", "updated_within_hours"},
	} {
		t.Run(c.query, func(t *testing.T) {
			rec := sendAPI(f.h, http.MethodGet, inboxIndex+"?"+c.query, inboxAgent, "agent", "")
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status %d, want 422: %s", rec.Code, rec.Body)
			}
			var body struct{ Errors map[string]any }
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Errors[c.field] == nil {
				t.Errorf("errors %s, want one for %s", rec.Body, c.field)
			}

			page := f.index("?" + c.query)
			if obj(t, page.Props, "errors")[c.field] == nil {
				t.Errorf("Inertia visit: errors %v, want one for %s", page.Props["errors"], c.field)
			}
		})
	}
}

func TestAgentListPreviewsTheLatestPublicReply(t *testing.T) {
	f := newInboxFixture(t)
	tk := f.ticket()
	agent := models.UserID(inboxAgent)
	ctx := context.Background()
	public := &models.Reply{TicketID: tk.ID, Body: "Customer-visible answer", AuthorID: &agent}
	if err := f.esc.Store.CreateReply(ctx, public); err != nil {
		t.Fatal(err)
	}
	note := &models.Reply{TicketID: tk.ID, Body: "Secret internal note", AuthorID: &agent, IsInternal: true}
	if err := f.esc.Store.CreateReply(ctx, note); err != nil {
		t.Fatal(err)
	}
	f.exec(`UPDATE escalated_replies SET created_at = ? WHERE id = ?`, time.Now().Add(-5*time.Minute), public.ID)

	row := list(t, obj(t, f.index("").Props, "tickets"), "data")[0].(map[string]any)
	latest := obj(t, row, "latest_reply")
	if latest["body"] != "Customer-visible answer" || latest["id"] != float64(public.ID) {
		t.Errorf("latest_reply %v, want the public reply", latest)
	}
	// Escalated tracks no per-agent read state or customer presence.
	for _, k := range []string{"is_unread", "requester_online"} {
		if _, ok := row[k]; ok {
			t.Errorf("row has %q", k)
		}
	}
}

func TestAgentTicketShowGetsInboxAndTheLastQueue(t *testing.T) {
	f := newInboxFixture(t)
	tk := f.ticket(assigned(inboxAgent))
	f.ticket()
	show := "/escalated/agent/tickets/" + url.PathEscape(tk.Reference)

	page := visit(t, f.h, show, inboxAgent, "agent")
	wantJSON(t, "queue", page.Props["queue"], `{"key":"all","title":"All tickets"}`)
	wantJSON(t, "counts", obj(t, page.Props, "inbox")["counts"], `{"mine":1,"unassigned":1,"all":2}`)

	listed := send(f.h, http.MethodGet, inboxIndex+"?assigned_to="+inboxAgent, inboxAgent, "agent", "")
	if listed.Code != http.StatusOK {
		t.Fatalf("list: %d", listed.Code)
	}
	r := httptest.NewRequest(http.MethodGet, show, nil)
	r.Header.Set("X-Inertia", "true")
	r.Header.Set(headerUser, inboxAgent)
	r.Header.Set(headerRole, "agent")
	for _, c := range listed.Result().Cookies() {
		r.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("show: %d %s", rec.Code, rec.Body)
	}
	var again inertiaPage
	if err := json.Unmarshal(rec.Body.Bytes(), &again); err != nil {
		t.Fatal(err)
	}
	wantJSON(t, "queue", again.Props["queue"], `{"key":"mine","title":"My inbox"}`)
	wantJSON(t, "stats", obj(t, again.Props, "inbox")["stats"], `{"open":1,"urgent":0,"sla_at_risk":0,"resolved_today":0}`)

	// A forged cookie selects nothing.
	r = httptest.NewRequest(http.MethodGet, show, nil)
	r.Header.Set("X-Inertia", "true")
	r.Header.Set(headerUser, inboxAgent)
	r.Header.Set(headerRole, "agent")
	r.AddCookie(&http.Cookie{Name: "escalated_agent_queue", Value: "view:bogus"})
	rec = httptest.NewRecorder()
	f.h.ServeHTTP(rec, r)
	_ = json.Unmarshal(rec.Body.Bytes(), &again)
	wantJSON(t, "queue", again.Props["queue"], `{"key":"all","title":"All tickets"}`)
}

// The agent ticket pages send the props the shared frontend's components
// declare (testdata/escalated-pages.json), so nothing a page reads is missing
// and nothing sent is dropped on the root element.
func TestAgentTicketPagesSendTheDeclaredProps(t *testing.T) {
	raw, err := os.ReadFile("../testdata/escalated-pages.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Props map[string]struct{ Props []string } `json:"props"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	f := newInboxFixture(t)
	tk := f.ticket()
	shared := map[string]bool{"errors": true, "auth": true, "flash": true, "escalated": true, "ziggy": true}
	// TicketShow also carries the ticket's custom field values, which this
	// package has sent there since custom fields shipped.
	extra := map[string][]string{"Escalated/Agent/TicketShow": {"custom_fields"}}

	for page, path := range map[string]string{
		"Escalated/Agent/TicketIndex": inboxIndex,
		"Escalated/Agent/TicketShow":  "/escalated/agent/tickets/" + url.PathEscape(tk.Reference),
	} {
		t.Run(page, func(t *testing.T) {
			got := visit(t, f.h, path, inboxAgent, "agent")
			var sent []string
			for k := range got.Props {
				if !shared[k] {
					sent = append(sent, k)
				}
			}
			want := append(append([]string{}, manifest.Props[page].Props...), extra[page]...)
			if !reflect.DeepEqual(sorted(sent), sorted(want)) {
				t.Errorf("sent %v, want %v", sorted(sent), sorted(want))
			}
		})
	}
}

func sorted(in []string) []string {
	out := append([]string{}, in...)
	sort.Strings(out)
	return out
}
