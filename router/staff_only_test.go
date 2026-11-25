package router_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"testing"

	escalated "github.com/escalated-dev/escalated-go"
	"github.com/escalated-dev/escalated-go/middleware"
	"github.com/escalated-dev/escalated-go/services"
)

// Config.CustomerMiddleware: extra guards on the customer and knowledge base
// reader pages, the way a host makes the portal staff-only.

func customerAndKBPages(f *uiFixture) []string {
	return []string{
		"/escalated/tickets",
		"/escalated/tickets/create",
		"/escalated/tickets/" + url.PathEscape(f.ticket.Reference),
		"/escalated/kb",
	}
}

func TestCustomerMiddlewareRefusesUsersWhoFailTheHostGuard(t *testing.T) {
	f := newUIFixture(t, func(c *escalated.Config) {
		c.CustomerMiddleware = []func(http.Handler) http.Handler{
			middleware.RequireAgentOrAdmin(c.AgentCheck, c.AdminCheck),
		}
	})
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			for _, path := range customerAndKBPages(f) {
				if rec := send(h, http.MethodGet, path, customerB, "", ""); rec.Code != http.StatusForbidden {
					t.Errorf("customer GET %s: %d, want 403", path, rec.Code)
				}
			}
			// Staff pass the host's guard. (The ticket page itself is the
			// requester's, so it is not asked for here.)
			for _, path := range []string{"/escalated/tickets", "/escalated/tickets/create", "/escalated/kb"} {
				if rec := send(h, http.MethodGet, path, uiAgent, "agent", ""); rec.Code != http.StatusOK {
					t.Errorf("agent GET %s: %d, want 200: %s", path, rec.Code, rec.Body.String())
				}
			}
			for _, path := range []string{"/escalated/tickets", "/escalated/tickets/" + url.PathEscape(f.ticket.Reference) + "/reply"} {
				if rec := send(h, http.MethodPost, path, customerB, "", `{"subject":"x","description":"y","body":"z"}`); rec.Code != http.StatusForbidden {
					t.Errorf("customer POST %s: %d, want 403", path, rec.Code)
				}
			}
		})
	}
}

func TestCustomerMiddlewareDefaultLeavesThePortalOpen(t *testing.T) {
	f := newUIFixture(t)
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			for _, path := range customerAndKBPages(f) {
				if rec := send(h, http.MethodGet, path, customerB, "", ""); rec.Code != http.StatusOK {
					t.Errorf("customer GET %s: %d, want 200: %s", path, rec.Code, rec.Body.String())
				}
			}
		})
	}
}

func TestCustomerMiddlewareRunsAfterThePackagesOwn(t *testing.T) {
	var order []string
	f := newUIFixture(t, func(c *escalated.Config) {
		mark := func(name string) func(http.Handler) http.Handler {
			return func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					order = append(order, name)
					next.ServeHTTP(w, r)
				})
			}
		}
		c.CustomerMiddleware = []func(http.Handler) http.Handler{mark("first"), mark("second")}
	})
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			// Signed out: the package's sign-in check answers before the host's.
			order = nil
			if rec := send(h, http.MethodGet, "/escalated/tickets", "", "", ""); rec.Code == http.StatusOK || len(order) != 0 {
				t.Errorf("signed out: %d, host middleware ran %v", rec.Code, order)
			}
			order = nil
			send(h, http.MethodGet, "/escalated/tickets", customerB, "", "")
			if !reflect.DeepEqual(order, []string{"first", "second"}) {
				t.Errorf("order %v, want [first second]", order)
			}
		})
	}
}

// The `scheduler` prop: the admin pages warn while the recurring jobs are
// not running.
func TestAdminPagesShareTheSchedulerStatus(t *testing.T) {
	f := newUIFixture(t)
	pages := []string{"/escalated/admin/settings", "/escalated/admin/reports", "/escalated/admin/reports/dashboard"}
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			if err := f.esc.Store.SetSetting(context.Background(), services.SchedulerHeartbeatSetting, ""); err != nil {
				t.Fatal(err)
			}
			for _, path := range pages {
				status := obj(t, visit(t, h, path, uiAgent, "admin").Props, "scheduler")
				hasKeys(t, "scheduler", status, "healthy", "last_run_at", "message", "commands")
				if status["healthy"] != false || status["last_run_at"] != nil {
					t.Errorf("%s with no heartbeat: %v", path, status)
				}
				commands := list(t, status, "commands")
				if len(commands) == 0 {
					t.Fatalf("%s: no commands to schedule", path)
				}
				first, _ := commands[0].(map[string]any)
				if first["command"] == "" || first["interval"] == "" {
					t.Errorf("%s: command %v", path, first)
				}
			}

			if _, _, err := f.esc.CheckSLA(context.Background(), 0); err != nil {
				t.Fatalf("check sla: %v", err)
			}
			for _, path := range pages {
				status := obj(t, visit(t, h, path, uiAgent, "admin").Props, "scheduler")
				if status["healthy"] != true || status["last_run_at"] == nil {
					t.Errorf("%s after an SLA check: %v", path, status)
				}
			}
		})
	}
}

// Ticket subjects on the agent and admin ticket pages, serialised as the
// JSON API serialises them.
func TestTicketPagesCarryTheSubjectsTheAPIReturns(t *testing.T) {
	f := newUIFixture(t)
	ctx := context.Background()
	role := "primary"
	subjects := services.NewTicketSubjectService(f.esc.Store, nil, nil)
	if _, err := subjects.AttachSubject(ctx, f.ticket.ID, "Order", "1001", &role, nil, false); err != nil {
		t.Fatalf("attach: %v", err)
	}
	if _, err := subjects.AttachSubject(ctx, f.ticket.ID, "Customer", "77", nil, nil, false); err != nil {
		t.Fatalf("attach: %v", err)
	}

	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			rec := sendAPI(h, http.MethodGet, fmt.Sprintf("/escalated/api/tickets/%d", f.ticket.ID), uiAgent, "agent", "")
			if rec.Code != http.StatusOK {
				t.Fatalf("api: %d %s", rec.Code, rec.Body.String())
			}
			var api struct {
				Ticket map[string]any `json:"ticket"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &api); err != nil {
				t.Fatal(err)
			}
			want := list(t, api.Ticket, "subjects")
			if len(want) != 2 {
				t.Fatalf("api subjects %v", want)
			}

			for path, who := range map[string]string{
				"/escalated/agent/tickets/" + url.PathEscape(f.ticket.Reference): "agent",
				"/escalated/admin/tickets/" + url.PathEscape(f.ticket.Reference): "admin",
			} {
				ticket := obj(t, visit(t, h, path, uiAgent, who).Props, "ticket")
				if got := list(t, ticket, "subjects"); !reflect.DeepEqual(got, want) {
					t.Errorf("%s subjects %v, want the API's %v", path, got, want)
				}
			}
		})
	}
}
