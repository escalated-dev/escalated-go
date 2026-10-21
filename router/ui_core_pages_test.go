package router_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	escalated "github.com/escalated-dev/escalated-go"
	"github.com/escalated-dev/escalated-go/handlers"
	"github.com/escalated-dev/escalated-go/internal/sqldialect"
	"github.com/escalated-dev/escalated-go/internal/testdb"
	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/router"
	"github.com/escalated-dev/escalated-go/services"
)

// The shared Vue pages read escalated-laravel's prop shapes: a paginator under
// tickets.data, status and priority as slugs, requester/assignee/department as
// nested objects, and the ticket screen's panels inside props.ticket. These
// tests assert every key the pages read, through the real routers and the
// Inertia renderer.

const uiAgent = "900"

type uiFixture struct {
	esc       *escalated.Escalated
	ticket    *models.Ticket
	other     *models.Ticket
	slaPolicy int64
}

func newUIFixture(t *testing.T, configure ...func(*escalated.Config)) *uiFixture {
	t.Helper()
	db := testdb.Open(t)

	cfg := escalated.DefaultConfig()
	cfg.DB = db
	cfg.UIEnabled = true
	cfg.AgentCheck = func(r *http.Request) bool {
		role := r.Header.Get(headerRole)
		return role == "agent" || role == "admin"
	}
	cfg.AdminCheck = func(r *http.Request) bool { return r.Header.Get(headerRole) == "admin" }
	cfg.UserIDFunc = func(r *http.Request) models.UserID { return models.UserID(r.Header.Get(headerUser)) }
	cfg.UserName = func(_ context.Context, id models.UserID) string {
		return map[models.UserID]string{customerB: "Bea Customer", uiAgent: "Ada Agent"}[id]
	}
	cfg.UserEmail = func(_ context.Context, id models.UserID) (string, error) {
		return map[models.UserID]string{customerB: "bea@example.com", uiAgent: "ada@example.com"}[id], nil
	}

	cfg.SkillAgentDirectory = uiAgents{}
	for _, c := range configure {
		c(&cfg)
	}

	esc, err := escalated.New(cfg)
	if err != nil {
		t.Fatalf("new escalated: %v", err)
	}
	ctx := context.Background()
	s := esc.Store

	dept := &models.Department{Name: "Billing", Slug: "billing", IsActive: true}
	if err := s.CreateDepartment(ctx, dept); err != nil {
		t.Fatalf("department: %v", err)
	}
	if err := s.CreateDepartment(ctx, &models.Department{Name: "Archived", Slug: "archived"}); err != nil {
		t.Fatalf("department: %v", err)
	}
	color := "#ff0000"
	tag := &models.Tag{Name: "vip", Slug: "vip", Color: &color}
	if err := s.CreateTag(ctx, tag); err != nil {
		t.Fatalf("tag: %v", err)
	}
	policy := &models.SLAPolicy{
		Name:               "Standard",
		IsActive:           true,
		IsDefault:          true,
		FirstResponseHours: models.JSONText(`{"high":4}`),
		ResolutionHours:    models.JSONText(`{"high":24}`),
	}
	if err := s.CreateSLAPolicy(ctx, policy); err != nil {
		t.Fatalf("sla policy: %v", err)
	}

	tickets := services.NewTicketService(s)
	userType := "User"
	requester := models.UserID(customerB)
	ticket, err := tickets.Create(ctx, services.CreateTicketInput{
		Subject:       "Card declined",
		Description:   "My card was declined twice",
		Priority:      models.PriorityHigh,
		TicketType:    "problem",
		RequesterType: &userType,
		RequesterID:   &requester,
		DepartmentID:  &dept.ID,
	})
	if err != nil {
		t.Fatalf("create ticket: %v", err)
	}
	other, err := tickets.Create(ctx, services.CreateTicketInput{Subject: "Someone else's", Description: "x", GuestName: ptr("Gus"), GuestEmail: ptr("gus@example.com")})
	if err != nil {
		t.Fatalf("create other ticket: %v", err)
	}

	agent := models.UserID(uiAgent)
	ticket.AssignedTo = &agent
	ticket.SLAPolicyID = &policy.ID
	if err := s.UpdateTicket(ctx, ticket); err != nil {
		t.Fatalf("assign ticket: %v", err)
	}
	if err := s.AddTagToTicket(ctx, ticket.ID, tag.ID); err != nil {
		t.Fatalf("tag ticket: %v", err)
	}
	if err := tickets.ChangePriority(ctx, ticket.ID, models.PriorityUrgent, &agent); err != nil {
		t.Fatalf("change priority: %v", err)
	}

	if _, err := tickets.AddReply(ctx, ticket.ID, "Here is a screenshot", &userType, &requester, false); err != nil {
		t.Fatalf("customer reply: %v", err)
	}
	if _, err := tickets.AddReply(ctx, ticket.ID, "We are looking into it", &userType, &agent, false); err != nil {
		t.Fatalf("agent reply: %v", err)
	}
	note, err := tickets.AddReply(ctx, ticket.ID, "Fraud check pending", &userType, &agent, true)
	if err != nil {
		t.Fatalf("internal note: %v", err)
	}
	note.IsPinned = true
	if err := s.UpdateReply(ctx, note); err != nil {
		t.Fatalf("pin note: %v", err)
	}
	if err := s.CreateAttachment(ctx, &models.Attachment{TicketID: ticket.ID, OriginalFilename: "receipt.pdf", MimeType: "application/pdf", Size: 42, StoragePath: "x"}); err != nil {
		t.Fatalf("attachment: %v", err)
	}

	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(sqldialect.Rebind(db, q), args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec(`INSERT INTO escalated_ticket_followers (ticket_id, user_id) VALUES (?, ?)`, ticket.ID, uiAgent)
	exec(`INSERT INTO escalated_ticket_links (parent_ticket_id, child_ticket_id, link_type) VALUES (?, ?, ?)`, ticket.ID, other.ID, "related")
	exec(`INSERT INTO escalated_satisfaction_ratings (ticket_id, rating, comment) VALUES (?, ?, ?)`, ticket.ID, 4, "Quick help")

	ticket, _ = s.GetTicket(ctx, ticket.ID)
	return &uiFixture{esc: esc, ticket: ticket, other: other, slaPolicy: policy.ID}
}

func ptr(s string) *string { return &s }

// uiAgents is the host's agent list for the assignee controls.
type uiAgents struct{}

func (uiAgents) ListAgentsForSkillForm(context.Context) ([]handlers.SkillFormAgent, error) {
	return []handlers.SkillFormAgent{{ID: 900, Name: "Ada Agent", Email: "ada@example.com"}}, nil
}

func (f *uiFixture) mounts() map[string]http.Handler {
	r := chi.NewRouter()
	router.MountChi(r, f.esc)
	mux := http.NewServeMux()
	router.MountStdlib(mux, f.esc)
	return map[string]http.Handler{"chi": r, "stdlib": mux}
}

type inertiaPage struct {
	Component string         `json:"component"`
	Props     map[string]any `json:"props"`
}

func visit(t *testing.T, h http.Handler, path, user, role string) inertiaPage {
	t.Helper()
	rec := send(h, http.MethodGet, path, user, role, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: %d %s", path, rec.Code, rec.Body.String())
	}
	var page inertiaPage
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("GET %s: not an Inertia page: %v: %s", path, err, rec.Body.String())
	}
	return page
}

// obj returns m[key] as an object, failing when it is not one.
func obj(t *testing.T, m map[string]any, key string) map[string]any {
	t.Helper()
	v, ok := m[key].(map[string]any)
	if !ok {
		t.Fatalf("%q is %T (%v), want an object", key, m[key], m[key])
	}
	return v
}

func list(t *testing.T, m map[string]any, key string) []any {
	t.Helper()
	v, ok := m[key].([]any)
	if !ok {
		t.Fatalf("%q is %T (%v), want an array", key, m[key], m[key])
	}
	return v
}

func hasKeys(t *testing.T, where string, m map[string]any, keys ...string) {
	t.Helper()
	for _, k := range keys {
		if _, ok := m[k]; !ok {
			t.Errorf("%s: missing key %q (have %v)", where, k, keysOf(m))
		}
	}
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func first(t *testing.T, items []any, where string) map[string]any {
	t.Helper()
	if len(items) == 0 {
		t.Fatalf("%s: empty", where)
	}
	m, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("%s: item is %T", where, items[0])
	}
	return m
}

func paginatorKeys(t *testing.T, where string, p map[string]any) {
	t.Helper()
	hasKeys(t, where, p, "data", "current_page", "last_page", "per_page", "total", "links", "from", "to",
		"path", "first_page_url", "last_page_url", "next_page_url", "prev_page_url")
}

var ticketRowKeys = []string{
	"id", "reference", "subject", "status", "priority", "ticket_type", "channel",
	"requester", "requester_name", "requester_email", "assigned_to", "assignee", "department", "tags",
	"first_response_due_at", "resolution_due_at", "sla_first_response_breached", "sla_resolution_breached",
	"snoozed_until", "latest_reply", "last_reply_at", "last_reply_author", "created_at", "updated_at",
}

func TestAgentTicketIndexProps(t *testing.T) {
	f := newUIFixture(t)
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			page := visit(t, h, "/escalated/agent/tickets?search=declined", uiAgent, "agent")
			if page.Component != "Escalated/Agent/TicketIndex" {
				t.Fatalf("component %q", page.Component)
			}
			hasKeys(t, "props", page.Props, "tickets", "filters", "departments", "tags")

			tickets := obj(t, page.Props, "tickets")
			paginatorKeys(t, "tickets", tickets)
			if tickets["total"].(float64) != 1 || tickets["per_page"].(float64) != 15 {
				t.Errorf("total %v per_page %v, want 1 and 15", tickets["total"], tickets["per_page"])
			}
			row := first(t, list(t, tickets, "data"), "tickets.data")
			hasKeys(t, "ticket row", row, ticketRowKeys...)
			if row["reference"] != f.ticket.Reference || row["status"] != "open" || row["priority"] != "urgent" {
				t.Errorf("row reference/status/priority = %v/%v/%v", row["reference"], row["status"], row["priority"])
			}
			if got := obj(t, row, "requester"); got["name"] != "Bea Customer" || got["email"] != "bea@example.com" {
				t.Errorf("requester %v", got)
			}
			if got := obj(t, row, "assignee"); got["name"] != "Ada Agent" {
				t.Errorf("assignee %v", got)
			}
			if got := obj(t, row, "department"); got["name"] != "Billing" {
				t.Errorf("department %v", got)
			}
			tag := first(t, list(t, row, "tags"), "row.tags")
			hasKeys(t, "row tag", tag, "id", "name", "color")
			latest := obj(t, row, "latest_reply")
			hasKeys(t, "latest_reply", latest, "author", "created_at")

			if got := obj(t, page.Props, "filters"); got["search"] != "declined" {
				t.Errorf("filters %v", got)
			}
			depts := list(t, page.Props, "departments")
			if len(depts) != 1 {
				t.Errorf("departments %v, want the active one only", depts)
			}
			hasKeys(t, "department", first(t, depts, "departments"), "id", "name")
			hasKeys(t, "tag", first(t, list(t, page.Props, "tags"), "tags"), "id", "name", "color")
		})
	}
}

func TestAgentTicketIndexFiltersBySlugAndPaginates(t *testing.T) {
	f := newUIFixture(t)
	h := f.mounts()["chi"]

	urgent := obj(t, visit(t, h, "/escalated/agent/tickets?priority=urgent", uiAgent, "agent").Props, "tickets")
	if urgent["total"].(float64) != 1 {
		t.Errorf("priority=urgent matched %v tickets, want 1", urgent["total"])
	}
	closed := obj(t, visit(t, h, "/escalated/agent/tickets?status=closed", uiAgent, "agent").Props, "tickets")
	if closed["total"].(float64) != 0 {
		t.Errorf("status=closed matched %v tickets, want 0", closed["total"])
	}
	all := obj(t, visit(t, h, "/escalated/agent/tickets", uiAgent, "agent").Props, "tickets")
	for _, item := range list(t, all, "data") {
		row := item.(map[string]any)
		if _, ok := row["guest_token"]; ok {
			t.Errorf("a guest token reached the queue: %v", row["reference"])
		}
	}
	paged := obj(t, visit(t, h, "/escalated/agent/tickets?per_page=1&page=2", uiAgent, "agent").Props, "tickets")
	if paged["current_page"].(float64) != 2 || paged["last_page"].(float64) != 2 || len(list(t, paged, "data")) != 1 {
		t.Errorf("page 2 of 1-per-page: %v", paged)
	}
}

func TestAgentTicketShowProps(t *testing.T) {
	f := newUIFixture(t)
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			// The pages link to tickets by reference; the id keeps working.
			for _, key := range []string{f.ticket.Reference, fmt.Sprint(f.ticket.ID)} {
				page := visit(t, h, "/escalated/agent/tickets/"+url.PathEscape(key), uiAgent, "agent")
				if page.Component != "Escalated/Agent/TicketShow" {
					t.Fatalf("component %q", page.Component)
				}
				hasKeys(t, "props", page.Props, "ticket", "departments", "tags", "cannedResponses", "macros",
					"is_following", "followers_count", "customActions")
				if page.Props["is_following"] != true || page.Props["followers_count"].(float64) != 1 {
					t.Errorf("is_following %v followers_count %v", page.Props["is_following"], page.Props["followers_count"])
				}

				ticket := obj(t, page.Props, "ticket")
				hasKeys(t, "ticket", ticket, append(ticketRowKeys,
					"description", "replies", "attachments", "activities", "sla_policy", "satisfaction_rating",
					"pinned_notes", "chat_session_id", "chat_started_at", "chat_messages", "chat_metadata",
					"requester_ticket_count", "related_tickets", "subjects")...)
				if ticket["status"] != "open" || ticket["priority"] != "urgent" || ticket["ticket_type"] != "problem" {
					t.Errorf("status/priority/type = %v/%v/%v", ticket["status"], ticket["priority"], ticket["ticket_type"])
				}

				replies := list(t, ticket, "replies")
				if len(replies) != 3 {
					t.Fatalf("replies %d, want 3 (agents see internal notes)", len(replies))
				}
				newest := first(t, replies, "replies")
				hasKeys(t, "reply", newest, "id", "body", "is_internal_note", "is_pinned", "author", "attachments", "created_at")
				if newest["is_internal_note"] != true || obj(t, newest, "author")["name"] != "Ada Agent" {
					t.Errorf("newest reply %v", newest)
				}

				att := first(t, list(t, ticket, "attachments"), "attachments")
				hasKeys(t, "attachment", att, "id", "original_filename", "mime_type", "size", "url")
				if !strings.HasPrefix(att["url"].(string), "/escalated/attachments/") {
					t.Errorf("attachment url %v", att["url"])
				}

				act := first(t, list(t, ticket, "activities"), "activities")
				hasKeys(t, "activity", act, "id", "type", "description", "properties", "causer", "created_at", "created_at_human")

				if got := obj(t, ticket, "sla_policy"); got["name"] != "Standard" {
					t.Errorf("sla_policy %v", got)
				}
				if got := obj(t, ticket, "satisfaction_rating"); got["rating"].(float64) != 4 || got["comment"] != "Quick help" {
					t.Errorf("satisfaction_rating %v", got)
				}
				pinned := first(t, list(t, ticket, "pinned_notes"), "pinned_notes")
				hasKeys(t, "pinned note", pinned, "id", "body", "author", "created_at")
				related := first(t, list(t, ticket, "related_tickets"), "related_tickets")
				hasKeys(t, "related ticket", related, "id", "reference", "subject", "status")
				if related["reference"] != f.other.Reference {
					t.Errorf("related_tickets %v", related)
				}
				if ticket["requester_ticket_count"].(float64) != 1 {
					t.Errorf("requester_ticket_count %v", ticket["requester_ticket_count"])
				}
				if _, ok := ticket["guest_token"]; ok {
					t.Error("the guest token reached the page")
				}
			}
		})
	}
}

func TestAgentDashboardProps(t *testing.T) {
	f := newUIFixture(t)
	page := visit(t, f.mounts()["chi"], "/escalated/agent/", uiAgent, "agent")
	if page.Component != "Escalated/Agent/Dashboard" {
		t.Fatalf("component %q", page.Component)
	}
	hasKeys(t, "props", page.Props, "stats", "recentTickets", "needsAttention", "myPerformance")
	stats := obj(t, page.Props, "stats")
	hasKeys(t, "stats", stats, "open", "my_assigned", "unassigned", "sla_breached", "resolved_today")
	if stats["open"].(float64) != 2 || stats["my_assigned"].(float64) != 1 || stats["unassigned"].(float64) != 1 {
		t.Errorf("stats %v", stats)
	}
	recent := list(t, page.Props, "recentTickets")
	if len(recent) != 2 {
		t.Errorf("recentTickets %d, want 2", len(recent))
	}
	hasKeys(t, "recent ticket", first(t, recent, "recentTickets"), "id", "reference", "subject", "status", "priority", "requester", "assignee", "latest_reply")
	hasKeys(t, "needsAttention", obj(t, page.Props, "needsAttention"), "sla_breaching", "unassigned_urgent")
	hasKeys(t, "myPerformance", obj(t, page.Props, "myPerformance"), "resolved_this_week")
}

func TestCustomerIndexProps(t *testing.T) {
	f := newUIFixture(t)
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			page := visit(t, h, "/escalated/tickets?status=open&bogus=1", customerB, "")
			if page.Component != "Escalated/Customer/Index" {
				t.Fatalf("component %q", page.Component)
			}
			hasKeys(t, "props", page.Props, "tickets", "filters")
			tickets := obj(t, page.Props, "tickets")
			paginatorKeys(t, "tickets", tickets)
			data := list(t, tickets, "data")
			if len(data) != 1 {
				t.Fatalf("customer sees %d tickets, want only their own", len(data))
			}
			row := first(t, data, "tickets.data")
			hasKeys(t, "row", row, "id", "reference", "subject", "status", "priority", "requester", "assignee", "department", "created_at", "updated_at", "last_reply_at")
			if row["status"] != "open" || obj(t, row, "assignee")["name"] != "Ada Agent" || obj(t, row, "assignee")["email"] != "" {
				t.Errorf("row %v", row)
			}
			filters := obj(t, page.Props, "filters")
			if filters["status"] != "open" {
				t.Errorf("filters %v", filters)
			}
			if _, ok := filters["bogus"]; ok {
				t.Errorf("filters echo an unknown key: %v", filters)
			}
		})
	}
}

func TestCustomerCreatePageProps(t *testing.T) {
	f := newUIFixture(t)
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			page := visit(t, h, "/escalated/tickets/create", customerB, "")
			if page.Component != "Escalated/Customer/Create" {
				t.Fatalf("component %q", page.Component)
			}
			hasKeys(t, "props", page.Props, "departments", "priorities")
			hasKeys(t, "department", first(t, list(t, page.Props, "departments"), "departments"), "id", "name")
			got := fmt.Sprint(list(t, page.Props, "priorities"))
			if got != "[low medium high urgent critical]" {
				t.Errorf("priorities %s", got)
			}
		})
	}
}

func TestCustomerCreateFormPostRedirectsToTheTicket(t *testing.T) {
	f := newUIFixture(t)
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			form := url.Values{"subject": {"Refund " + name}, "description": {"Please refund"}, "priority": {"high"}, "department_id": {""}, "ticket_type": {"question"}}
			r := httptest.NewRequest(http.MethodPost, "/escalated/tickets", strings.NewReader(form.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r.Header.Set("X-Inertia", "true")
			r.Header.Set(headerUser, customerB)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, r)
			if rec.Code != http.StatusSeeOther {
				t.Fatalf("got %d %s, want 303", rec.Code, rec.Body.String())
			}
			loc := rec.Header().Get("Location")
			ref := strings.TrimPrefix(loc, "/escalated/tickets/")
			created, err := f.esc.Store.GetTicketByReference(context.Background(), ref)
			if err != nil || created == nil {
				t.Fatalf("redirected to %q, which names no ticket", loc)
			}
			if created.Priority != models.PriorityHigh || created.Subject != "Refund "+name {
				t.Errorf("created %+v", created)
			}

			// A missing subject shows the form again with the error useForm reads.
			bad := url.Values{"description": {"no subject"}}
			r = httptest.NewRequest(http.MethodPost, "/escalated/tickets", strings.NewReader(bad.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r.Header.Set("X-Inertia", "true")
			r.Header.Set(headerUser, customerB)
			rec = httptest.NewRecorder()
			h.ServeHTTP(rec, r)
			var page inertiaPage
			if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil || page.Component != "Escalated/Customer/Create" {
				t.Fatalf("invalid post: %d %s", rec.Code, rec.Body.String())
			}
			if _, ok := obj(t, page.Props, "errors")["subject"]; !ok {
				t.Errorf("errors %v", page.Props["errors"])
			}

			// An API client gets 422 with Laravel's error shape.
			r = httptest.NewRequest(http.MethodPost, "/escalated/tickets", strings.NewReader(`{"subject":"","description":"x"}`))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set(headerUser, customerB)
			rec = httptest.NewRecorder()
			h.ServeHTTP(rec, r)
			if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), `"subject"`) {
				t.Errorf("json invalid post: %d %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestCustomerShowProps(t *testing.T) {
	f := newUIFixture(t)
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			page := visit(t, h, "/escalated/tickets/"+url.PathEscape(f.ticket.Reference), customerB, "")
			if page.Component != "Escalated/Customer/Show" {
				t.Fatalf("component %q", page.Component)
			}
			ticket := obj(t, page.Props, "ticket")
			hasKeys(t, "ticket", ticket, "reference", "subject", "description", "status", "priority", "channel",
				"department", "attachments", "replies", "satisfaction_rating", "resolved_at", "closed_at", "created_at", "updated_at")
			if obj(t, ticket, "department")["name"] != "Billing" {
				t.Errorf("department %v", ticket["department"])
			}
			replies := list(t, ticket, "replies")
			if len(replies) != 2 {
				t.Fatalf("customer sees %d replies, want the 2 public ones", len(replies))
			}
			for _, item := range replies {
				reply := item.(map[string]any)
				hasKeys(t, "reply", reply, "id", "body", "is_internal_note", "is_pinned", "author", "attachments", "created_at")
				author := obj(t, reply, "author")
				if _, ok := author["email"]; ok {
					t.Errorf("a reply author's email reached the customer: %v", author)
				}
			}
			if obj(t, first(t, replies, "replies"), "author")["name"] != "Ada Agent" {
				t.Errorf("newest reply author %v", first(t, replies, "replies")["author"])
			}
			if got := obj(t, ticket, "satisfaction_rating"); got["rating"].(float64) != 4 {
				t.Errorf("satisfaction_rating %v", got)
			}

			if rec := send(h, http.MethodGet, "/escalated/tickets/"+url.PathEscape(f.ticket.Reference), customerA, "", ""); rec.Code != http.StatusForbidden {
				t.Errorf("another customer: got %d, want 403", rec.Code)
			}
		})
	}
}

func TestAdminSlaPoliciesIndexProps(t *testing.T) {
	f := newUIFixture(t)
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			page := visit(t, h, "/escalated/admin/sla-policies", uiAgent, "admin")
			if page.Component != "Escalated/Admin/SlaPolicies/Index" {
				t.Fatalf("component %q", page.Component)
			}
			policy := first(t, list(t, page.Props, "policies"), "policies")
			hasKeys(t, "policy", policy, "id", "name", "is_default", "business_hours_only", "tickets_count", "is_active")
			// The default policy applies to both fixture tickets.
			if policy["tickets_count"].(float64) != 2 || policy["is_default"] != true || policy["business_hours_only"] != false {
				t.Errorf("policy %v", policy)
			}
		})
	}
}
