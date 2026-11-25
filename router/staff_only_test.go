package router_test

import (
	"net/http"
	"net/url"
	"reflect"
	"testing"

	escalated "github.com/escalated-dev/escalated-go"
	"github.com/escalated-dev/escalated-go/middleware"
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
