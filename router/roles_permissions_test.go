package router_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	escalated "github.com/escalated-dev/escalated-go"
	"github.com/escalated-dev/escalated-go/access"
	"github.com/escalated-dev/escalated-go/handlers"
	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/router"
)

// Roles & permissions (ADR 2026-10-29-configurable-access-features, port
// checklist "Switches" and "Roles & permissions"): an install-level switch,
// off by default. Off, the roles routes answer 404 and the gates decide
// everything; on, every admin, agent and staff API route also needs its
// permission from the user's roles.

const (
	staffNoRole = "901" // passes the admin gate, holds no role
	lightAgent  = "902"
)

func rolesOn(on bool) func(*escalated.Config) {
	return func(cfg *escalated.Config) { cfg.Access.RolesPermissions = &on }
}

// newRolesFixture is the UI fixture with the permissions and system roles
// seeded, and uiAgent (900) holding the admin role.
func newRolesFixture(t *testing.T, configure ...func(*escalated.Config)) *uiFixture {
	t.Helper()
	f := newUIFixture(t, configure...)
	ctx := context.Background()
	if err := f.esc.Roles().Seed(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := f.esc.Roles().Assign(ctx, uiAgent, "admin"); err != nil {
		t.Fatalf("assign admin: %v", err)
	}
	return f
}

func (f *uiFixture) assign(t *testing.T, user models.UserID, role string) {
	t.Helper()
	if err := f.esc.Roles().Assign(context.Background(), user, role); err != nil {
		t.Fatalf("assign %s to %s: %v", role, user, err)
	}
}

// customRole makes a role holding exactly the given slugs and gives it to
// the user.
func (f *uiFixture) customRole(t *testing.T, user models.UserID, slugs ...string) {
	t.Helper()
	ctx := context.Background()
	all, err := f.esc.Roles().AllPermissions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{}
	for _, s := range slugs {
		want[s] = true
	}
	var ids []int64
	for _, p := range all {
		if want[p.Slug] {
			ids = append(ids, p.ID)
		}
	}
	if len(ids) != len(slugs) {
		t.Fatalf("unknown slug among %v", slugs)
	}
	id, err := f.esc.Roles().SaveRole(ctx, 0, "Custom "+string(user), nil, ids)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.esc.Roles().ReplaceUserRoles(ctx, user, []int64{id}); err != nil {
		t.Fatal(err)
	}
}

func sharedEscalated(t *testing.T, page inertiaPage) map[string]any {
	t.Helper()
	return obj(t, page.Props, "escalated")
}

func permissionsOf(t *testing.T, page inertiaPage) []string {
	t.Helper()
	var out []string
	for _, v := range list(t, sharedEscalated(t, page), "permissions") {
		out = append(out, v.(string))
	}
	return out
}

func TestRolesPermissionsSwitchOffByDefault(t *testing.T) {
	for _, name := range []string{"chi", "stdlib"} {
		t.Run(name, func(t *testing.T) {
			f := newRolesFixture(t)
			h := f.mounts()[name]
			if f.esc.Features.RolesPermissions {
				t.Fatal("roles & permissions is on by default")
			}

			for _, req := range [][2]string{
				{http.MethodGet, "/escalated/admin/roles"},
				{http.MethodGet, "/escalated/admin/roles/create"},
				{http.MethodPost, "/escalated/admin/roles"},
				{http.MethodGet, "/escalated/admin/roles/1/edit"},
				{http.MethodPut, "/escalated/admin/roles/1"},
				{http.MethodDelete, "/escalated/admin/roles/1"},
				{http.MethodPut, "/escalated/admin/users/900/roles"},
			} {
				if rec := send(h, req[0], req[1], uiAgent, "admin", `{"name":"x","role_ids":[]}`); rec.Code != http.StatusNotFound {
					t.Errorf("%s %s with the switch off: %d, want 404", req[0], req[1], rec.Code)
				}
			}

			// Shared props: the switch is off, staff get "*" and others nothing.
			page := visit(t, h, "/escalated/agent/tickets", uiAgent, "agent")
			features := obj(t, sharedEscalated(t, page), "features")
			for _, k := range []string{"roles_permissions", "two_factor", "sso"} {
				if features[k] != false {
					t.Errorf("features.%s = %v, want false", k, features[k])
				}
			}
			if got := permissionsOf(t, page); len(got) != 1 || got[0] != "*" {
				t.Errorf("staff permissions %v, want [*]", got)
			}
			customer := visit(t, h, "/escalated/tickets", customerB, "")
			if got := list(t, sharedEscalated(t, customer), "permissions"); len(got) != 0 {
				t.Errorf("customer permissions %v, want []", got)
			}

			// The users page has no role column while the switch is off.
			users := visit(t, h, "/escalated/admin/users", uiAgent, "admin")
			if _, has := users.Props["roles"]; has {
				t.Error("users page carries roles with the switch off")
			}
		})
	}
}

func TestRolesPermissionsSwitchOffLetsTheGatesDecide(t *testing.T) {
	for _, name := range []string{"chi", "stdlib"} {
		t.Run(name, func(t *testing.T) {
			f := newRolesFixture(t, rolesOn(false))
			h := f.mounts()[name]
			ref := f.ticket.Reference

			// The same requests that get 403 with the switch on (below) pass
			// the gates alone.
			if rec := send(h, http.MethodGet, "/escalated/admin/departments", staffNoRole, "admin", ""); rec.Code != http.StatusOK {
				t.Errorf("admin read: %d", rec.Code)
			}
			if rec := send(h, http.MethodPost, "/escalated/admin/tags", staffNoRole, "admin", `{"name":"Gold","color":"#ffcc00"}`); rec.Code >= 400 {
				t.Errorf("admin write: %d %s", rec.Code, rec.Body.String())
			}
			if rec := send(h, http.MethodPost, "/escalated/agent/tickets/"+ref+"/priority", staffNoRole, "agent", `{"priority":"low"}`); rec.Code >= 400 {
				t.Errorf("agent ticket action: %d %s", rec.Code, rec.Body.String())
			}
			page := visit(t, h, "/escalated/admin/departments", staffNoRole, "admin")
			if got := permissionsOf(t, page); len(got) != 1 || got[0] != "*" {
				t.Errorf("staff permissions %v, want [*]", got)
			}
		})
	}
}

func TestRolesPermissionsSwitchOnServesTheRolesScreens(t *testing.T) {
	for _, name := range []string{"chi", "stdlib"} {
		t.Run(name, func(t *testing.T) {
			f := newRolesFixture(t, rolesOn(true))
			h := f.mounts()[name]

			index := visit(t, h, "/escalated/admin/roles", uiAgent, "admin")
			if index.Component != "Escalated/Admin/Roles/Index" {
				t.Fatalf("component %q", index.Component)
			}
			roles := list(t, index.Props, "roles")
			if len(roles) != 3 {
				t.Fatalf("roles %v, want the three system roles", roles)
			}
			for _, item := range roles {
				role := item.(map[string]any)
				hasKeys(t, "role", role, "id", "name", "slug", "description", "is_system", "users_count")
				if role["is_system"] != true {
					t.Errorf("system role %v", role)
				}
				if role["slug"] == "admin" && role["users_count"].(float64) != 1 {
					t.Errorf("admin users_count %v", role["users_count"])
				}
			}
			features := obj(t, sharedEscalated(t, index), "features")
			if features["roles_permissions"] != true || features["two_factor"] != false || features["sso"] != false {
				t.Errorf("features %v", features)
			}
			perms := permissionsOf(t, index)
			if len(perms) != len(access.Permissions) {
				t.Errorf("the admin role's permissions: %d slugs, want %d", len(perms), len(access.Permissions))
			}

			form := visit(t, h, "/escalated/admin/roles/create", uiAgent, "admin")
			if form.Component != "Escalated/Admin/Roles/Form" {
				t.Fatalf("create component %q", form.Component)
			}
			grouped := obj(t, form.Props, "permissions")
			tickets := list(t, grouped, "Tickets")
			first := tickets[0].(map[string]any)
			hasKeys(t, "permission", first, "id", "name", "slug", "group", "description")
			viewID := first["id"].(float64)

			rec := pageJSON(h, http.MethodPost, "/escalated/admin/roles", `{"name":"Billing desk","description":"Billing only","permissions":[`+ftoa(viewID)+`]}`)
			wantRedirect(t, rec, "store", "/escalated/admin/roles")
			id := f.id(t, `SELECT id FROM escalated_roles WHERE slug = ?`, "billing-desk")

			edit := visit(t, h, "/escalated/admin/roles/"+itoa(id)+"/edit", uiAgent, "admin")
			role := obj(t, edit.Props, "role")
			if role["name"] != "Billing desk" || len(list(t, role, "permissions")) != 1 {
				t.Errorf("edit role %v", role)
			}

			rec = pageJSON(h, http.MethodPut, "/escalated/admin/roles/"+itoa(id), `{"name":"Billing","permissions":[]}`)
			wantRedirect(t, rec, "update", "/escalated/admin/roles")
			if n := f.count(t, "role_permission", "role_id = ?", id); n != 0 {
				t.Errorf("permissions after update: %d", n)
			}

			// Validation: a name is required, unknown permissions are refused.
			if rec := send(h, http.MethodPost, "/escalated/admin/roles", uiAgent, "admin", `{"name":"","permissions":[99999]}`); rec.Code != http.StatusOK {
				t.Errorf("invalid store: %d", rec.Code)
			} else {
				var page inertiaPage
				_ = json.Unmarshal(rec.Body.Bytes(), &page)
				errs := obj(t, page.Props, "errors")
				hasKeys(t, "errors", errs, "name", "permissions")
			}

			// System roles cannot be deleted; others can.
			adminID := f.id(t, `SELECT id FROM escalated_roles WHERE slug = ?`, "admin")
			if rec := apiJSON(h, http.MethodDelete, "/escalated/admin/roles/"+itoa(adminID), ""); rec.Code != http.StatusUnprocessableEntity {
				t.Errorf("deleting a system role: %d", rec.Code)
			}
			if n := f.count(t, "roles", "id = ?", adminID); n != 1 {
				t.Error("the system role was deleted")
			}
			rec = pageJSON(h, http.MethodDelete, "/escalated/admin/roles/"+itoa(id), "")
			wantRedirect(t, rec, "destroy", "/escalated/admin/roles")
			if n := f.count(t, "roles", "id = ?", id); n != 0 {
				t.Error("the role was not deleted")
			}
		})
	}
}

func TestRolesPermissionsUsersPageEditsRoles(t *testing.T) {
	for _, name := range []string{"chi", "stdlib"} {
		t.Run(name, func(t *testing.T) {
			f := newRolesFixture(t, rolesOn(true), func(cfg *escalated.Config) {
				cfg.UserDirectory = &fakeUserDirectory{users: map[int64]*handlers.AdminUser{
					900: {ID: 900, Name: "Ada", Email: "ada@example.com", IsAdmin: true, IsAgent: true},
					902: {ID: 902, Name: "Lin", Email: "lin@example.com", IsAgent: true},
				}}
			})
			h := f.mounts()[name]
			agentID := f.id(t, `SELECT id FROM escalated_roles WHERE slug = ?`, "agent")

			rec := pageJSON(h, http.MethodPut, "/escalated/admin/users/902/roles", `{"role_ids":[`+itoa(agentID)+`]}`)
			if rec.Code != http.StatusSeeOther {
				t.Fatalf("update roles: %d %s", rec.Code, rec.Body.String())
			}
			page := visit(t, h, "/escalated/admin/users", uiAgent, "admin")
			if len(list(t, page.Props, "roles")) != 3 {
				t.Errorf("roles %v", page.Props["roles"])
			}
			rows := list(t, obj(t, page.Props, "users"), "data")
			found := false
			for _, item := range rows {
				row := item.(map[string]any)
				if row["id"].(float64) == 902 {
					found = true
					if got := list(t, row, "roles"); len(got) != 1 || got[0] != "agent" {
						t.Errorf("902 roles %v", got)
					}
				}
			}
			if !found {
				t.Fatal("user 902 not listed")
			}

			// Taking away your own role management is refused.
			if rec := apiJSON(h, http.MethodPut, "/escalated/admin/users/900/roles", `{"role_ids":[`+itoa(agentID)+`]}`); rec.Code != http.StatusUnprocessableEntity {
				t.Errorf("removing own role management: %d", rec.Code)
			}
			// role_ids must be present and name roles.
			if rec := apiJSON(h, http.MethodPut, "/escalated/admin/users/902/roles", `{}`); rec.Code != http.StatusUnprocessableEntity {
				t.Errorf("missing role_ids: %d", rec.Code)
			}
			if rec := apiJSON(h, http.MethodPut, "/escalated/admin/users/902/roles", `{"role_ids":[99999]}`); rec.Code != http.StatusUnprocessableEntity {
				t.Errorf("unknown role: %d", rec.Code)
			}
		})
	}
}

// A bad role_ids answers as escalated-laravel's validator does: errors keyed
// role_ids or role_ids.N in Laravel's wording, shown on the users page for an
// Inertia visit (Go keeps no session to flash them through a redirect).
func TestRolesPermissionsUsersRolesValidation(t *testing.T) {
	for _, name := range []string{"chi", "stdlib"} {
		t.Run(name, func(t *testing.T) {
			f := newRolesFixture(t, rolesOn(true), func(cfg *escalated.Config) {
				cfg.UserDirectory = &fakeUserDirectory{users: map[int64]*handlers.AdminUser{
					900: {ID: 900, Name: "Ada", Email: "ada@example.com", IsAdmin: true, IsAgent: true},
					902: {ID: 902, Name: "Lin", Email: "lin@example.com", IsAgent: true},
				}}
			})
			h := f.mounts()[name]
			agentID := itoa(f.id(t, `SELECT id FROM escalated_roles WHERE slug = ?`, "agent"))

			cases := []struct {
				body string
				want map[string]any
			}{
				{`{}`, map[string]any{"role_ids": "The role ids field must be present."}},
				{`{"role_ids":null}`, map[string]any{"role_ids": "The role ids field must be an array."}},
				{`{"role_ids":"admin"}`, map[string]any{"role_ids": "The role ids field must be an array."}},
				{`{"role_ids":["x"]}`, map[string]any{"role_ids.0": "The role_ids.0 field must be an integer."}},
				{`{"role_ids":[` + agentID + `,99999]}`, map[string]any{"role_ids.1": "The selected role_ids.1 is invalid."}},
				{`{"role_ids":[1.5,99999]}`, map[string]any{
					"role_ids.0": "The role_ids.0 field must be an integer.",
					"role_ids.1": "The selected role_ids.1 is invalid.",
				}},
			}
			for _, c := range cases {
				rec := pageJSON(h, http.MethodPut, "/escalated/admin/users/902/roles", c.body)
				var page inertiaPage
				if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil || page.Component != "Escalated/Admin/Users/Index" {
					t.Fatalf("%s: got %d %s, want the users page", c.body, rec.Code, rec.Body.String())
				}
				if got := obj(t, page.Props, "errors"); !reflect.DeepEqual(got, c.want) {
					t.Errorf("%s: errors %v, want %v", c.body, got, c.want)
				}
				if len(list(t, page.Props, "roles")) != 3 {
					t.Errorf("%s: the page lost its roles: %v", c.body, page.Props["roles"])
				}

				// A JSON client gets Laravel's 422 with the same keys.
				api := apiJSON(h, http.MethodPut, "/escalated/admin/users/902/roles", c.body)
				var body struct {
					Errors map[string][]string `json:"errors"`
				}
				if api.Code != http.StatusUnprocessableEntity || json.Unmarshal(api.Body.Bytes(), &body) != nil || len(body.Errors) != len(c.want) {
					t.Errorf("%s: api got %d %s", c.body, api.Code, api.Body.String())
				}
				for k := range c.want {
					if _, ok := body.Errors[k]; !ok {
						t.Errorf("%s: api errors %v lack %q", c.body, body.Errors, k)
					}
				}
			}
			if n := f.count(t, "role_user", "user_id = ?", "902"); n != 0 {
				t.Errorf("a rejected change still gave 902 %d roles", n)
			}

			// Taking away your own role management is shown on the page too.
			wantPageErrors(t, pageJSON(h, http.MethodPut, "/escalated/admin/users/900/roles", `{"role_ids":[`+agentID+`]}`),
				"own role management", "Escalated/Admin/Users/Index", "role_ids")

			// Numeric strings are integers, as in Laravel.
			wantRedirect(t, pageJSON(h, http.MethodPut, "/escalated/admin/users/902/roles", `{"role_ids":["`+agentID+`"]}`),
				"numeric string", "/escalated/admin/users")
			if n := f.count(t, "role_user", "user_id = ?", "902"); n != 1 {
				t.Errorf("902 has %d roles, want 1", n)
			}
		})
	}
}

func TestRolesPermissionsSwitchIsConfigOrEnvOnly(t *testing.T) {
	t.Run("env", func(t *testing.T) {
		t.Setenv(access.EnvRolesPermissions, "true")
		f := newRolesFixture(t)
		if !f.esc.Features.RolesPermissions {
			t.Fatal("ESCALATED_ROLES_PERMISSIONS=true did not switch it on")
		}
		if rec := send(f.mounts()["chi"], http.MethodGet, "/escalated/admin/roles", uiAgent, "admin", ""); rec.Code != http.StatusOK {
			t.Errorf("roles index: %d", rec.Code)
		}
	})
	t.Run("config beats env", func(t *testing.T) {
		t.Setenv(access.EnvRolesPermissions, "true")
		f := newRolesFixture(t, rolesOn(false))
		if f.esc.Features.RolesPermissions {
			t.Fatal("Config.Access.RolesPermissions=false was overridden by the env")
		}
	})
	t.Run("a setting with the same name changes nothing", func(t *testing.T) {
		for _, on := range []bool{false, true} {
			f := newRolesFixture(t, rolesOn(on))
			h := f.mounts()["chi"]
			body := `{"roles_permissions":` + map[bool]string{true: "false", false: "true"}[on] + `,"access.roles_permissions":` + map[bool]string{true: "false", false: "true"}[on] + `}`
			if rec := send(h, http.MethodPost, "/escalated/admin/settings", uiAgent, "admin", body); rec.Code >= 500 {
				t.Fatalf("settings post: %d", rec.Code)
			}
			want := map[bool]int{true: http.StatusOK, false: http.StatusNotFound}[on]
			if rec := send(h, http.MethodGet, "/escalated/admin/roles", uiAgent, "admin", ""); rec.Code != want {
				t.Errorf("switch %v after posting the setting: roles index %d, want %d", on, rec.Code, want)
			}
			page := visit(t, h, "/escalated/admin/departments", uiAgent, "admin")
			if obj(t, sharedEscalated(t, page), "features")["roles_permissions"] != on {
				t.Errorf("features.roles_permissions changed by a setting")
			}
		}
	})
}

func TestRolesPermissionsEnforced(t *testing.T) {
	for _, name := range []string{"chi", "stdlib"} {
		t.Run(name, func(t *testing.T) {
			f := newRolesFixture(t, rolesOn(true))
			h := f.mounts()[name]
			ref := f.ticket.Reference

			// Passing the admin gate without a role grants nothing.
			checks := []struct {
				what, method, path, role, body string
			}{
				{"admin read", http.MethodGet, "/escalated/admin/departments", "admin", ""},
				{"admin write", http.MethodPost, "/escalated/admin/tags", "admin", `{"name":"Gold","color":"#ffcc00"}`},
				{"agent ticket action", http.MethodPost, "/escalated/agent/tickets/" + ref + "/priority", "agent", `{"priority":"low"}`},
			}
			for _, c := range checks {
				if rec := send(h, c.method, c.path, staffNoRole, c.role, c.body); rec.Code != http.StatusForbidden {
					t.Errorf("%s without a role: %d, want 403", c.what, rec.Code)
				}
			}
			page := visit(t, h, "/escalated/tickets", staffNoRole, "")
			if got := list(t, sharedEscalated(t, page), "permissions"); len(got) != 0 {
				t.Errorf("permissions without a role %v, want []", got)
			}

			// A role granting each one makes it work.
			f.customRole(t, staffNoRole, "department.view", "tag.manage", "ticket.view", "ticket.edit")
			for _, c := range checks {
				if rec := send(h, c.method, c.path, staffNoRole, c.role, c.body); rec.Code >= 400 {
					t.Errorf("%s with the permission: %d %s", c.what, rec.Code, rec.Body.String())
				}
			}
			page = visit(t, h, "/escalated/admin/departments", staffNoRole, "admin")
			got := permissionsOf(t, page)
			sort.Strings(got)
			if strings.Join(got, ",") != "department.view,tag.manage,ticket.edit,ticket.view" {
				t.Errorf("shared permissions %v", got)
			}

			// The gates still decide who reaches the area: a role is not a gate.
			if rec := send(h, http.MethodGet, "/escalated/admin/departments", staffNoRole, "", ""); rec.Code != http.StatusForbidden {
				t.Errorf("role without the admin gate: %d, want 403", rec.Code)
			}
		})
	}
}

func TestRolesPermissionsLightAgent(t *testing.T) {
	for _, name := range []string{"chi", "stdlib"} {
		t.Run(name, func(t *testing.T) {
			f := newRolesFixture(t, rolesOn(true))
			h := f.mounts()[name]
			f.assign(t, lightAgent, "light_agent")
			base := "/escalated/agent/tickets/" + f.ticket.Reference

			if rec := send(h, http.MethodGet, base, lightAgent, "agent", ""); rec.Code != http.StatusOK {
				t.Errorf("view: %d", rec.Code)
			}
			if rec := send(h, http.MethodPost, base+"/note", lightAgent, "agent", `{"body":"Checked the logs"}`); rec.Code >= 400 {
				t.Errorf("note: %d %s", rec.Code, rec.Body.String())
			}
			for _, c := range []struct{ what, path, body string }{
				{"assign", "/assign", `{"agent_id":900}`},
				{"priority", "/priority", `{"priority":"low"}`},
				{"close", "/status", `{"status":"closed"}`},
				{"reply", "/reply", `{"body":"Hi"}`},
			} {
				rec := send(h, http.MethodPost, base+c.path, lightAgent, "agent", c.body)
				if c.what == "reply" {
					// light_agent holds reply.create.
					if rec.Code >= 400 {
						t.Errorf("reply: %d", rec.Code)
					}
					continue
				}
				if rec.Code != http.StatusForbidden {
					t.Errorf("light agent %s: %d, want 403", c.what, rec.Code)
				}
			}
			if rec := send(h, http.MethodPost, "/escalated/agent/tickets/bulk", lightAgent, "agent", `{"ticket_ids":[`+itoa(f.ticket.ID)+`],"action":"delete"}`); rec.Code != http.StatusForbidden {
				t.Errorf("light agent delete: %d, want 403", rec.Code)
			}
		})
	}
}

func TestRolesPermissionsClosingStatusesNeedTicketClose(t *testing.T) {
	for _, name := range []string{"chi", "stdlib"} {
		t.Run(name, func(t *testing.T) {
			f := newRolesFixture(t, rolesOn(true))
			h := f.mounts()[name]
			f.customRole(t, staffNoRole, "ticket.view", "ticket.edit")
			base := "/escalated/agent/tickets/" + f.ticket.Reference

			for _, status := range []string{`"closed"`, `"resolved"`, `"reopened"`, `6`, `"6"`} {
				if rec := send(h, http.MethodPost, base+"/status", staffNoRole, "agent", `{"status":`+status+`}`); rec.Code != http.StatusForbidden {
					t.Errorf("status %s without ticket.close: %d, want 403", status, rec.Code)
				}
			}
			if rec := send(h, http.MethodPost, base+"/status", staffNoRole, "agent", `{"status":"in_progress"}`); rec.Code >= 400 {
				t.Errorf("status in_progress: %d %s", rec.Code, rec.Body.String())
			}
			// A form post is read too.
			r := httptest.NewRequest(http.MethodPost, base+"/status", strings.NewReader("status=closed"))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r.Header.Set(headerUser, staffNoRole)
			r.Header.Set(headerRole, "agent")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, r)
			if rec.Code != http.StatusForbidden {
				t.Errorf("form status closed: %d, want 403", rec.Code)
			}

			f.customRole(t, staffNoRole, "ticket.view", "ticket.edit", "ticket.close")
			if rec := send(h, http.MethodPost, base+"/status", staffNoRole, "agent", `{"status":"closed"}`); rec.Code >= 400 {
				t.Errorf("status closed with ticket.close: %d %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestRolesPermissionsBulkActionsCheckEachAction(t *testing.T) {
	for _, name := range []string{"chi", "stdlib"} {
		t.Run(name, func(t *testing.T) {
			f := newRolesFixture(t, rolesOn(true))
			h := f.mounts()[name]
			f.customRole(t, staffNoRole, "ticket.view", "ticket.edit")
			ids := `[` + itoa(f.ticket.ID) + `]`
			bulk := func(action, value string) int {
				body := `{"ticket_ids":` + ids + `,"action":"` + action + `"`
				if value != "" {
					body += `,"value":` + value
				}
				return send(h, http.MethodPost, "/escalated/admin/tickets/bulk", staffNoRole, "admin", body+`}`).Code
			}
			if code := bulk("priority", `"low"`); code >= 400 {
				t.Errorf("bulk priority with ticket.edit: %d", code)
			}
			if code := bulk("assign", `900`); code != http.StatusForbidden {
				t.Errorf("bulk assign without ticket.assign: %d", code)
			}
			if code := bulk("delete", ""); code != http.StatusForbidden {
				t.Errorf("bulk delete without ticket.delete: %d", code)
			}
			if code := bulk("status", `"closed"`); code != http.StatusForbidden {
				t.Errorf("bulk close without ticket.close: %d", code)
			}
			if code := bulk("status", `"in_progress"`); code >= 400 {
				t.Errorf("bulk status in_progress: %d", code)
			}
		})
	}
}

func TestRolesPermissionsInternalRepliesNeedTheNotePermission(t *testing.T) {
	f := newRolesFixture(t, rolesOn(true))
	h := f.mounts()["chi"]
	f.customRole(t, staffNoRole, "ticket.view", "reply.create")
	path := "/escalated/agent/tickets/" + itoa(f.ticket.ID) + "/replies"
	if rec := send(h, http.MethodPost, path, staffNoRole, "agent", `{"body":"x","is_internal":true}`); rec.Code != http.StatusForbidden {
		t.Errorf("internal reply without reply.create_internal: %d", rec.Code)
	}
	if rec := send(h, http.MethodPost, path, staffNoRole, "agent", `{"body":"x"}`); rec.Code >= 400 {
		t.Errorf("public reply: %d %s", rec.Code, rec.Body.String())
	}
}

func TestRolesPermissionsChecksTheAPITokenOwner(t *testing.T) {
	for _, name := range []string{"chi", "stdlib"} {
		t.Run(name, func(t *testing.T) {
			owners := &tokenOwners{roles: map[models.UserID]*handlers.APITokenOwner{uiAgent: {IsAgent: true, IsAdmin: true}}}
			f := newRolesFixture(t, rolesOn(true), func(cfg *escalated.Config) {
				cfg.APITokensEnabled = true
				cfg.APITokenOwner = owners.lookup
			})
			h := f.mounts()[name]
			token := issue(t, h, `{"name":"CLI","user_id":900,"abilities":["*"]}`)

			if rec := bearer(h, http.MethodGet, "/escalated/api/tickets", token, ""); rec.Code != http.StatusOK {
				t.Fatalf("owner with the admin role: %d", rec.Code)
			}
			// The owner loses their roles: the token can do nothing.
			if err := f.esc.Roles().ReplaceUserRoles(context.Background(), uiAgent, nil); err != nil {
				t.Fatal(err)
			}
			if rec := bearer(h, http.MethodGet, "/escalated/api/tickets", token, ""); rec.Code != http.StatusForbidden {
				t.Errorf("owner without a role: %d, want 403", rec.Code)
			}
			// ticket.view but not ticket.create.
			f.customRole(t, uiAgent, "ticket.view")
			if rec := bearer(h, http.MethodGet, "/escalated/api/tickets", token, ""); rec.Code != http.StatusOK {
				t.Errorf("owner with ticket.view: %d", rec.Code)
			}
			if rec := bearer(h, http.MethodPost, "/escalated/api/tickets", token, `{"subject":"x","description":"y"}`); rec.Code != http.StatusForbidden {
				t.Errorf("create without ticket.create: %d, want 403", rec.Code)
			}
			if rec := bearer(h, http.MethodPatch, "/escalated/api/tickets/"+itoa(f.ticket.ID), token, `{"status":"closed"}`); rec.Code != http.StatusForbidden {
				t.Errorf("close without ticket.edit/close: %d, want 403", rec.Code)
			}
		})
	}
}

func TestRolesPermissionsNewslettersNeedTheirPermission(t *testing.T) {
	f := newRolesFixture(t, rolesOn(true), func(cfg *escalated.Config) { cfg.EnableNewsletters = true })
	// Chi only: the stdlib mux cannot mount the newsletter routes (two of
	// its patterns overlap), a gap of its own.
	r := chi.NewRouter()
	router.MountChi(r, f.esc)
	var h http.Handler = r
	f.customRole(t, staffNoRole, "ticket.view")
	if rec := send(h, http.MethodGet, "/escalated/admin/newsletters", staffNoRole, "admin", ""); rec.Code != http.StatusForbidden {
		t.Errorf("newsletters without newsletters.manage: %d", rec.Code)
	}
	if rec := send(h, http.MethodGet, "/escalated/admin/newsletters", uiAgent, "admin", ""); rec.Code != http.StatusOK {
		t.Errorf("newsletters as admin role: %d", rec.Code)
	}
}

// Every admin, agent and staff API route is in the route-permission map, so
// a new route cannot ship unguarded: this walks the chi router with every
// optional surface mounted.
func TestEveryStaffRouteIsInThePermissionMap(t *testing.T) {
	f := newRolesFixture(t, rolesOn(true), func(cfg *escalated.Config) {
		cfg.EnableNewsletters = true
		cfg.EnableWidget = true
		cfg.APITokensEnabled = true
	})
	r := chi.NewRouter()
	router.MountChi(r, f.esc)
	concrete := strings.NewReplacer("{id}", "1", "{user}", "1", "{record}", "1", "{linkId}", "1", "{scId}", "1",
		"{reply}", "1", "{subject}", "1", "{action}", "x", "{a}", "a", "{b}", "b", "{session}", "1", "{ticketId}", "1",
		"{macroId}", "1", "{list}", "1", "{contactId}", "1", "{template}", "1", "{newsletter}", "1", "*", "slug/page")
	checked := 0
	err := chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		switch method {
		case http.MethodOptions, http.MethodHead, http.MethodConnect, http.MethodTrace, "QUERY":
			return nil
		}
		path, ok := strings.CutPrefix(route, "/escalated")
		if !ok || !access.Governed(path) {
			return nil
		}
		checked++
		if _, mapped := access.Mapped(method, concrete.Replace(path)); !mapped {
			t.Errorf("%s %s is not in access.RoutePermissions", method, route)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked < 250 {
		t.Errorf("checked only %d routes; the walk is not seeing the router", checked)
	}
	// The public API routes are not staff routes.
	for _, p := range []string{"/api/auth/login", "/api/guest/tickets", "/api/mobile/guest/tickets", "/api/kb/articles", "/tickets", "/widget/config"} {
		if access.Governed(p) {
			t.Errorf("%s is governed", p)
		}
	}
}

func ftoa(f float64) string { return itoa(int64(f)) }

// apiJSON sends a JSON request as an API client (no X-Inertia) as uiAgent,
// an admin.
func apiJSON(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	r.Header.Set("Accept", "application/json")
	r.Header.Set(headerUser, uiAgent)
	r.Header.Set(headerRole, "admin")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

// fakeUserDirectory is a host user table for the users page.
type fakeUserDirectory struct {
	users map[int64]*handlers.AdminUser
}

func (d *fakeUserDirectory) ListUsers(_ context.Context, _ string, page, perPage int) (handlers.UserPage, error) {
	var ids []int64
	for id := range d.users {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	out := handlers.UserPage{CurrentPage: page, LastPage: 1, PerPage: perPage, Total: len(ids)}
	for _, id := range ids {
		out.Data = append(out.Data, *d.users[id])
	}
	return out, nil
}

func (d *fakeUserDirectory) GetUser(_ context.Context, id int64) (*handlers.AdminUser, error) {
	return d.users[id], nil
}

func (d *fakeUserDirectory) UpdateUserRoles(_ context.Context, id int64, u handlers.UserRoleUpdates) error {
	if user := d.users[id]; user != nil {
		if u.IsAdmin != nil {
			user.IsAdmin = *u.IsAdmin
		}
		if u.IsAgent != nil {
			user.IsAgent = *u.IsAgent
		}
	}
	return nil
}
