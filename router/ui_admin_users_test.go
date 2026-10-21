package router_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"

	escalated "github.com/escalated-dev/escalated-go"
	"github.com/escalated-dev/escalated-go/handlers"
	"github.com/escalated-dev/escalated-go/internal/testdb"
	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/router"
)

// usersDir is a host user table with three users, 2 a page.
type usersDir struct{ users []handlers.AdminUser }

func (d *usersDir) ListUsers(_ context.Context, _ string, page, _ int) (handlers.UserPage, error) {
	perPage := 2
	start := (page - 1) * perPage
	end := start + perPage
	if start > len(d.users) {
		start = len(d.users)
	}
	if end > len(d.users) {
		end = len(d.users)
	}
	return handlers.UserPage{Data: append([]handlers.AdminUser{}, d.users[start:end]...), CurrentPage: page, LastPage: (len(d.users) + 1) / 2, PerPage: perPage, Total: len(d.users)}, nil
}

func (d *usersDir) GetUser(_ context.Context, id int64) (*handlers.AdminUser, error) {
	for i := range d.users {
		if d.users[i].ID == id {
			u := d.users[i]
			return &u, nil
		}
	}
	return nil, nil
}

func (d *usersDir) UpdateUserRoles(_ context.Context, id int64, u handlers.UserRoleUpdates) error {
	for i := range d.users {
		if d.users[i].ID == id {
			if u.IsAdmin != nil {
				d.users[i].IsAdmin = *u.IsAdmin
			}
			if u.IsAgent != nil {
				d.users[i].IsAgent = *u.IsAgent
			}
		}
	}
	return nil
}

// The users page reads Laravel's paginator, links included, and its role
// toggle (router.patch) expects a redirect.
func TestAdminUsersPage(t *testing.T) {
	dir := &usersDir{users: []handlers.AdminUser{
		{ID: 900, Name: "Ada", Email: "ada@example.com", IsAdmin: true, IsAgent: true},
		{ID: 901, Name: "Bo", Email: "bo@example.com"},
		{ID: 902, Name: "Cy", Email: "cy@example.com", IsAgent: true},
	}}
	cfg := escalated.DefaultConfig()
	cfg.DB = testdb.Open(t)
	cfg.UserDirectory = dir
	cfg.AdminCheck = func(r *http.Request) bool { return r.Header.Get(headerRole) == "admin" }
	cfg.AgentCheck = cfg.AdminCheck
	cfg.UserIDFunc = func(r *http.Request) models.UserID { return models.UserID(r.Header.Get(headerUser)) }
	esc, err := escalated.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	router.MountChi(r, esc)
	mux := http.NewServeMux()
	router.MountStdlib(mux, esc)

	for name, h := range map[string]http.Handler{"chi": r, "stdlib": mux} {
		t.Run(name, func(t *testing.T) {
			page := visit(t, h, "/escalated/admin/users?page=2", uiAgent, "admin")
			if page.Component != "Escalated/Admin/Users/Index" {
				t.Fatalf("component %q", page.Component)
			}
			hasKeys(t, "props", page.Props, "users", "filters", "currentUserId")
			users := obj(t, page.Props, "users")
			paginatorKeys(t, "users", users)
			if users["current_page"].(float64) != 2 || users["last_page"].(float64) != 2 || len(list(t, users, "links")) != 4 {
				t.Errorf("users paginator %v", users)
			}
			hasKeys(t, "user", first(t, list(t, users, "data"), "users.data"), "id", "name", "email", "is_admin", "is_agent")

			wantRedirect(t, pageJSON(h, http.MethodPatch, "/escalated/admin/users/902/role", `{"role":"admin","value":true}`),
				"role change", "/escalated/admin/users")
			if got, _ := dir.GetUser(context.Background(), 902); !got.IsAdmin {
				t.Error("the role did not change")
			}

			// Demoting yourself is refused on the page.
			wantPageErrors(t, pageJSON(h, http.MethodPatch, "/escalated/admin/users/900/role", `{"role":"admin","value":false}`),
				"self-demote", "Escalated/Admin/Users/Index", "role")
		})
	}
}
