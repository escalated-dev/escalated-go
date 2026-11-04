package router_test

import (
	"context"
	"net/http"
	"net/url"
	"reflect"
	"testing"
	"time"

	escalated "github.com/escalated-dev/escalated-go"
	"github.com/escalated-dev/escalated-go/access"
	"github.com/escalated-dev/escalated-go/handlers"
	"github.com/escalated-dev/escalated-go/internal/sqldialect"
	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/ticketaccess"
)

// What the agent ticket page needs beyond the ticket itself: the agents it can
// be reassigned to, the requester's other tickets, and snoozing from the agent
// panel (escalated-laravel's AgentWorkspaceTest).

const (
	wsAdmin = "1"
	wsOther = "901"
)

// workspaceAgents is a host agent directory with an admin and a second agent.
type workspaceAgents struct{}

func (workspaceAgents) ListAgentsForSkillForm(context.Context) ([]handlers.SkillFormAgent, error) {
	return []handlers.SkillFormAgent{
		{ID: 900, Name: "Ada Agent", Email: "ada@example.com"},
		{ID: 901, Name: "Otto Other", Email: "otto@example.com"},
		{ID: 1, Name: "Al Admin", Email: "al@example.com"},
	}, nil
}

// departmentVisibility turns on department visibility, with 900 and 901 as
// agents and 1 as an admin.
func departmentVisibility(cfg *escalated.Config) {
	cfg.Visibility = "department"
	cfg.TicketAccessUserResolver = func(_ context.Context, id models.UserID) (*ticketaccess.User, error) {
		switch id {
		case uiAgent, wsOther:
			return &ticketaccess.User{ID: id, Agent: true}, nil
		case wsAdmin:
			return &ticketaccess.User{ID: id, Admin: true}, nil
		}
		return &ticketaccess.User{ID: id}, nil
	}
}

func workspaceDirectory(cfg *escalated.Config) { cfg.SkillAgentDirectory = workspaceAgents{} }

// joinDepartment makes the agent a member of the fixture ticket's department.
func (f *uiFixture) joinDepartment(t *testing.T, agent string) {
	t.Helper()
	db := f.esc.Config.DB
	if _, err := db.Exec(sqldialect.Rebind(db, `INSERT INTO escalated_department_agent (department_id, agent_id) VALUES (?, ?)`), *f.ticket.DepartmentID, agent); err != nil {
		t.Fatal(err)
	}
}

func (f *uiFixture) newTicket(t *testing.T, subject string, opts ...ticketOpt) *models.Ticket {
	t.Helper()
	tk := &models.Ticket{Subject: subject, Description: "Body", TicketType: "question", Priority: models.PriorityLow}
	for _, o := range opts {
		o(tk)
	}
	if err := f.esc.Store.CreateTicket(context.Background(), tk); err != nil {
		t.Fatalf("create ticket: %v", err)
	}
	return tk
}

func requestedBy(id string) ticketOpt {
	return func(t *models.Ticket) {
		typ, uid := "User", models.UserID(id)
		t.RequesterType, t.RequesterID = &typ, &uid
	}
}

func guest(email string) ticketOpt {
	return func(t *models.Ticket) { name := "Gus"; t.GuestName, t.GuestEmail = &name, &email }
}

func (f *uiFixture) setCreatedAt(t *testing.T, id int64, at time.Time) {
	t.Helper()
	db := f.esc.Config.DB
	if _, err := db.Exec(sqldialect.Rebind(db, `UPDATE escalated_tickets SET created_at = ? WHERE id = ?`), at.UTC(), id); err != nil {
		t.Fatal(err)
	}
}

func showPath(tk *models.Ticket) string {
	return "/escalated/agent/tickets/" + url.PathEscape(tk.Reference)
}

func requesterTickets(t *testing.T, h http.Handler, tk *models.Ticket) []any {
	t.Helper()
	return list(t, obj(t, visit(t, h, showPath(tk), uiAgent, "agent").Props, "ticket"), "requester_tickets")
}

func subjects(rows []any) []string {
	out := []string{}
	for _, row := range rows {
		out = append(out, row.(map[string]any)["subject"].(string))
	}
	return out
}

// --- Reassigning ------------------------------------------------------------

func TestAgentTicketShowSendsTheAssignableAgents(t *testing.T) {
	f := newUIFixture(t, workspaceDirectory)
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			got := visit(t, h, showPath(f.ticket), uiAgent, "agent").Props["agents"]
			wantJSON(t, "agents", got, `[{"id":900,"name":"Ada Agent"},{"id":901,"name":"Otto Other"},{"id":1,"name":"Al Admin"}]`)
		})
	}
}

func TestAgentTicketShowLeavesOutAgentsWhoCannotSeeTheTicket(t *testing.T) {
	f := newUIFixture(t, workspaceDirectory, departmentVisibility)
	f.joinDepartment(t, uiAgent)
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			got := visit(t, h, showPath(f.ticket), uiAgent, "agent").Props["agents"]
			wantJSON(t, "agents", got, `[{"id":900,"name":"Ada Agent"},{"id":1,"name":"Al Admin"}]`)
		})
	}
}

// --- The requester's other tickets ------------------------------------------

func TestAgentTicketShowSendsTheRequestersOtherTickets(t *testing.T) {
	f := newUIFixture(t)
	now := time.Now()
	var newest *models.Ticket
	for i := 1; i <= 6; i++ {
		tk := f.newTicket(t, "Earlier "+string(rune('0'+i)), requestedBy(customerB))
		f.setCreatedAt(t, tk.ID, now.Add(-time.Duration(10-i)*24*time.Hour))
		newest = tk
	}
	f.newTicket(t, "Someone else", requestedBy(customerA))
	f.setCreatedAt(t, f.ticket.ID, now.Add(time.Minute))

	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			recent := requesterTickets(t, h, f.ticket)
			if got, want := subjects(recent), []string{"Earlier 6", "Earlier 5", "Earlier 4", "Earlier 3", "Earlier 2"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("subjects %v, want %v", got, want)
			}
			first := recent[0].(map[string]any)
			var keys []string
			for k := range first {
				keys = append(keys, k)
			}
			if got, want := sorted(keys), sorted([]string{"id", "reference", "subject", "status", "created_at", "updated_at"}); !reflect.DeepEqual(got, want) {
				t.Errorf("keys %v, want %v", got, want)
			}
			if first["reference"] != newest.Reference || first["status"] != "open" || first["id"] != float64(newest.ID) {
				t.Errorf("first row %v, want %s open", first, newest.Reference)
			}
			if _, err := time.Parse(time.RFC3339, first["created_at"].(string)); err != nil {
				t.Errorf("created_at %v: %v", first["created_at"], err)
			}
		})
	}
}

func TestAgentTicketShowMatchesAGuestsOtherTicketsByEmail(t *testing.T) {
	f := newUIFixture(t)
	f.newTicket(t, "Earlier guest ticket", guest("guest@example.com"))
	f.newTicket(t, "Another guest", guest("other@example.com"))
	tk := f.newTicket(t, "Now", guest("guest@example.com"))

	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			if got := subjects(requesterTickets(t, h, tk)); !reflect.DeepEqual(got, []string{"Earlier guest ticket"}) {
				t.Errorf("subjects %v", got)
			}
		})
	}
}

func TestAgentTicketShowListsOnlyRequesterTicketsTheAgentMaySee(t *testing.T) {
	f := newUIFixture(t, departmentVisibility)
	f.joinDepartment(t, uiAgent)
	elsewhere := &models.Department{Name: "Elsewhere", Slug: "elsewhere", IsActive: true}
	if err := f.esc.Store.CreateDepartment(context.Background(), elsewhere); err != nil {
		t.Fatal(err)
	}
	f.newTicket(t, "Visible", requestedBy(customerB), department(*f.ticket.DepartmentID))
	f.newTicket(t, "Hidden", requestedBy(customerB), department(elsewhere.ID))

	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			if got := subjects(requesterTickets(t, h, f.ticket)); !reflect.DeepEqual(got, []string{"Visible"}) {
				t.Errorf("subjects %v", got)
			}
		})
	}
}

func TestAgentTicketShowSendsNoRequesterTicketsWithoutARequester(t *testing.T) {
	f := newUIFixture(t)
	f.newTicket(t, "Anonymous one")
	tk := f.newTicket(t, "Anonymous two")
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			if got := requesterTickets(t, h, tk); len(got) != 0 {
				t.Errorf("requester_tickets %v, want []", got)
			}
		})
	}
}

// --- Snoozing from the agent panel ------------------------------------------

func TestAgentSnoozesAndUnsnoozesATicket(t *testing.T) {
	for _, surface := range []string{"agent", "admin"} {
		t.Run(surface, func(t *testing.T) {
			f := newUIFixture(t)
			h := f.mounts()["chi"]
			role := "agent"
			if surface == "admin" {
				role = "admin"
			}
			show := "/escalated/" + surface + "/tickets/" + url.PathEscape(f.ticket.Reference)
			until := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)

			wantBack(t, postPage(h, http.MethodPost, show+"/snooze", uiAgent, role, show,
				url.Values{"snoozed_until": {until.Format("2006-01-02T15:04:05.000Z")}}), "snooze", show)
			got := f.reload(t)
			if got.SnoozedUntil == nil || !got.SnoozedUntil.Equal(until) {
				t.Fatalf("snoozed_until %v, want %v", got.SnoozedUntil, until)
			}
			if got.SnoozedBy == nil || *got.SnoozedBy != uiAgent {
				t.Errorf("snoozed_by %v, want %s", got.SnoozedBy, uiAgent)
			}
			if got.Status != models.StatusSnoozed || got.StatusBeforeSnooze == nil || *got.StatusBeforeSnooze != f.ticket.Status {
				t.Errorf("status %d before %v, want snoozed from %d", got.Status, got.StatusBeforeSnooze, f.ticket.Status)
			}
			page := visit(t, h, show, uiAgent, role)
			if obj(t, page.Props, "ticket")["is_snoozed"] != true {
				t.Errorf("is_snoozed %v, want true", obj(t, page.Props, "ticket")["is_snoozed"])
			}
			if !hasActivity(t, f, models.ActionTicketSnoozed) {
				t.Error("no snoozed activity")
			}

			wantBack(t, postPage(h, http.MethodPost, show+"/unsnooze", uiAgent, role, show, url.Values{}), "unsnooze", show)
			got = f.reload(t)
			if got.SnoozedUntil != nil || got.SnoozedBy != nil || got.StatusBeforeSnooze != nil || got.Status != f.ticket.Status {
				t.Errorf("after unsnooze: until %v by %v before %v status %d, want cleared and %d",
					got.SnoozedUntil, got.SnoozedBy, got.StatusBeforeSnooze, got.Status, f.ticket.Status)
			}
			if !hasActivity(t, f, models.ActionTicketUnsnoozed) {
				t.Error("no unsnoozed activity")
			}
		})
	}
}

func TestAgentUnsnoozeRestoresTheStatusBeforeSnooze(t *testing.T) {
	f := newUIFixture(t)
	h := f.mounts()["stdlib"]
	if err := f.esc.NewTicketService().ChangeStatus(context.Background(), f.ticket.ID, models.StatusInProgress, nil); err != nil {
		t.Fatal(err)
	}
	show := showPath(f.ticket)
	wantBack(t, postPage(h, http.MethodPost, show+"/snooze", uiAgent, "agent", show,
		url.Values{"snoozed_until": {time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}}), "snooze", show)
	wantBack(t, postPage(h, http.MethodPost, show+"/unsnooze", uiAgent, "agent", show, url.Values{}), "unsnooze", show)
	if got := f.reload(t); got.Status != models.StatusInProgress || got.SnoozedUntil != nil {
		t.Errorf("status %d until %v, want in_progress and not snoozed", got.Status, got.SnoozedUntil)
	}
}

func TestAgentSnoozeRejectsAMissingOrPastTime(t *testing.T) {
	f := newUIFixture(t)
	show := showPath(f.ticket)
	for name, h := range f.mounts() {
		for label, value := range map[string]string{
			"missing": "",
			"garbage": "next tuesday-ish",
			"past":    time.Now().Add(-time.Hour).UTC().Format(time.RFC3339),
		} {
			t.Run(name+"/"+label, func(t *testing.T) {
				rec := postPage(h, http.MethodPost, show+"/snooze", uiAgent, "agent", show, url.Values{"snoozed_until": {value}})
				wantPageErrors(t, rec, label, "Escalated/Agent/TicketShow", "snoozed_until")
				if got := f.reload(t); got.SnoozedUntil != nil || got.Status == models.StatusSnoozed {
					t.Errorf("snoozed anyway: %v", got.SnoozedUntil)
				}
				api := sendAPI(h, http.MethodPost, show+"/snooze", uiAgent, "agent", `{"snoozed_until":"`+value+`"}`)
				if api.Code != http.StatusUnprocessableEntity {
					t.Errorf("API client got %d, want 422", api.Code)
				}
			})
		}
	}
}

func TestCustomerCannotSnoozeThroughTheAgentPanel(t *testing.T) {
	f := newUIFixture(t)
	show := showPath(f.ticket)
	body := url.Values{"snoozed_until": {time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)}}
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			if rec := postPage(h, http.MethodPost, show+"/snooze", customerB, "", show, body); rec.Code != http.StatusForbidden {
				t.Errorf("customer got %d, want 403", rec.Code)
			}
			if got := f.reload(t); got.SnoozedUntil != nil {
				t.Error("customer snoozed the ticket")
			}
		})
	}
}

func TestAgentCannotSnoozeATicketOutsideTheirDepartments(t *testing.T) {
	f := newUIFixture(t, departmentVisibility)
	show := showPath(f.ticket)
	body := url.Values{"snoozed_until": {time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)}}
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			for _, action := range []string{"/snooze", "/unsnooze"} {
				if rec := postPage(h, http.MethodPost, show+action, uiAgent, "agent", show, body); rec.Code != http.StatusNotFound {
					t.Errorf("%s got %d, want 404", action, rec.Code)
				}
			}
			if got := f.reload(t); got.SnoozedUntil != nil {
				t.Error("agent snoozed a ticket outside their departments")
			}
		})
	}
}

func TestSnoozeRoutesNeedTheTicketEditPermission(t *testing.T) {
	for _, path := range []string{
		"/agent/tickets/TK-1/snooze", "/agent/tickets/TK-1/unsnooze",
		"/admin/tickets/TK-1/snooze", "/admin/tickets/TK-1/unsnooze",
	} {
		slugs, ok := access.Mapped(http.MethodPost, path)
		if !ok || !reflect.DeepEqual(slugs, []string{"ticket.edit"}) {
			t.Errorf("%s maps to %v (%v), want ticket.edit", path, slugs, ok)
		}
	}
	routes := escalated.BuiltInRoutes("/escalated")
	for name, uri := range map[string]string{
		"escalated.agent.tickets.snooze":   "escalated/agent/tickets/{ticket}/snooze",
		"escalated.agent.tickets.unsnooze": "escalated/agent/tickets/{ticket}/unsnooze",
		"escalated.admin.tickets.snooze":   "escalated/admin/tickets/{ticket}/snooze",
		"escalated.admin.tickets.unsnooze": "escalated/admin/tickets/{ticket}/unsnooze",
	} {
		if got := routes[name]; got.URI != uri || !reflect.DeepEqual(got.Methods, []string{http.MethodPost}) {
			t.Errorf("%s = %+v, want POST %s", name, got, uri)
		}
	}
}

func hasActivity(t *testing.T, f *uiFixture, action string) bool {
	t.Helper()
	activities, err := f.esc.Store.ListActivities(context.Background(), f.ticket.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range activities {
		if a.Action == action {
			return true
		}
	}
	return false
}
