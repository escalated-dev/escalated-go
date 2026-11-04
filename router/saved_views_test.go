package router_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	escalated "github.com/escalated-dev/escalated-go"
	"github.com/escalated-dev/escalated-go/internal/testdb"
	"github.com/escalated-dev/escalated-go/models"
)

// Saved views, after escalated-laravel's SavedViewController and
// SavedViewTest: the CRUD the shared frontend's SaveViewDialog and
// SavedViewSidebar call (escalated.admin.saved-views.*), with Laravel's
// visibility rules -- an agent sees their own views and every shared one, and
// only the owner (or anyone, for a view with no owner) changes or deletes it.

const (
	svAdmin = "700"
	svOther = "701"
)

func newSavedViewFixture(t *testing.T) *escalated.Escalated {
	t.Helper()
	cfg := escalated.DefaultConfig()
	cfg.DB = testdb.Open(t)
	cfg.UIEnabled = true
	cfg.AgentCheck = func(r *http.Request) bool {
		role := r.Header.Get(headerRole)
		return role == "agent" || role == "admin"
	}
	cfg.AdminCheck = func(r *http.Request) bool { return r.Header.Get(headerRole) == "admin" }
	cfg.UserIDFunc = func(r *http.Request) models.UserID { return models.UserID(r.Header.Get(headerUser)) }
	esc, err := escalated.New(cfg)
	if err != nil {
		t.Fatalf("new escalated: %v", err)
	}
	return esc
}

func svCall(t *testing.T, h http.Handler, user, role, method, path, body string) (int, any) {
	t.Helper()
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	r.Header.Set("Accept", "application/json")
	r.Header.Set(headerUser, user)
	r.Header.Set(headerRole, role)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	var out any
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
	}
	return rec.Code, out
}

func svCreate(t *testing.T, h http.Handler, user, body string) map[string]any {
	t.Helper()
	code, out := svCall(t, h, user, "admin", http.MethodPost, "/escalated/admin/saved-views", body)
	if code != http.StatusCreated {
		t.Fatalf("store: %d %v", code, out)
	}
	return out.(map[string]any)
}

func svNames(t *testing.T, views any) []string {
	t.Helper()
	var names []string
	for _, v := range views.([]any) {
		names = append(names, v.(map[string]any)["name"].(string))
	}
	return names
}

func TestSavedViewsCRUDThroughTheRouter(t *testing.T) {
	esc := newSavedViewFixture(t)
	for name, h := range (&uiFixture{esc: esc}).mounts() {
		t.Run(name, func(t *testing.T) {
			// Each mount shares the database, so start from no views.
			if _, err := esc.Config.DB.Exec("DELETE FROM escalated_saved_views"); err != nil {
				t.Fatal(err)
			}

			first := svCreate(t, h, svAdmin, `{"name":"My View","filters":{"status":"open"}}`)
			if first["name"] != "My View" || first["is_shared"] != false || first["is_default"] != false {
				t.Errorf("created view %v", first)
			}
			if fmt.Sprint(first["user_id"]) != svAdmin {
				t.Errorf("user_id %v, want %s", first["user_id"], svAdmin)
			}
			if got := asJSON(t, first["filters"]); got != `{"status":"open"}` {
				t.Errorf("filters %s", got)
			}
			second := svCreate(t, h, svAdmin, `{"name":"Second","filters":{"priority":"high"},"is_shared":true}`)
			// Positions follow the views the creator can see.
			if first["position"].(float64) != 1 || second["position"].(float64) != 2 {
				t.Errorf("positions %v %v, want 1 and 2", first["position"], second["position"])
			}
			svCreate(t, h, svOther, `{"name":"Other private","filters":{"priority":"low"}}`)
			svCreate(t, h, svOther, `{"name":"Other shared","filters":{"priority":"low"},"is_shared":true}`)

			// index: own and shared, by position.
			code, out := svCall(t, h, svAdmin, "admin", http.MethodGet, "/escalated/admin/saved-views", "")
			if code != http.StatusOK {
				t.Fatalf("index: %d %v", code, out)
			}
			if got := strings.Join(svNames(t, out), ","); got != "My View,Second,Other shared" {
				t.Errorf("index names %s", got)
			}

			id := int64(first["id"].(float64))
			path := fmt.Sprintf("/escalated/admin/saved-views/%d", id)

			// update own (the sidebar's rename sends name and filters).
			code, out = svCall(t, h, svAdmin, "admin", http.MethodPut, path, `{"name":"New Name","filters":{"status":"closed"}}`)
			if code != http.StatusOK || out.(map[string]any)["name"] != "New Name" {
				t.Fatalf("update: %d %v", code, out)
			}
			if got := asJSON(t, out.(map[string]any)["filters"]); got != `{"status":"closed"}` {
				t.Errorf("updated filters %s", got)
			}

			// Another user may neither change nor delete it, shared or not.
			if code, _ := svCall(t, h, svOther, "admin", http.MethodPut, path, `{"name":"Hacked","filters":{"status":"open"}}`); code != http.StatusForbidden {
				t.Errorf("other user's update: %d, want 403", code)
			}
			sharedPath := fmt.Sprintf("/escalated/admin/saved-views/%d", int64(second["id"].(float64)))
			if code, _ := svCall(t, h, svOther, "admin", http.MethodDelete, sharedPath, ""); code != http.StatusForbidden {
				t.Errorf("other user's delete of a shared view: %d, want 403", code)
			}

			// Validation: name and filters are required.
			code, out = svCall(t, h, svAdmin, "admin", http.MethodPost, "/escalated/admin/saved-views", `{"filters":{"status":"open"}}`)
			if code != http.StatusUnprocessableEntity {
				t.Fatalf("missing name: %d %v", code, out)
			}
			if _, ok := out.(map[string]any)["errors"].(map[string]any)["name"]; !ok || out.(map[string]any)["message"] == nil {
				t.Errorf("missing name errors %v", out)
			}
			if code, out := svCall(t, h, svAdmin, "admin", http.MethodPost, "/escalated/admin/saved-views", `{"name":"x"}`); code != http.StatusUnprocessableEntity {
				t.Errorf("missing filters: %d %v", code, out)
			}
			if code, out := svCall(t, h, svAdmin, "admin", http.MethodPost, "/escalated/admin/saved-views", `{"name":"x","filters":"status=open"}`); code != http.StatusUnprocessableEntity {
				t.Errorf("string filters: %d %v", code, out)
			}

			// reorder: own and shared views.
			code, _ = svCall(t, h, svAdmin, "admin", http.MethodPost, "/escalated/admin/saved-views/reorder",
				fmt.Sprintf(`{"ids":[%d,%d]}`, int64(second["id"].(float64)), id))
			if code != http.StatusOK {
				t.Fatalf("reorder: %d", code)
			}
			_, out = svCall(t, h, svAdmin, "admin", http.MethodGet, "/escalated/admin/saved-views", "")
			if got := strings.Join(svNames(t, out), ","); !strings.HasPrefix(got, "Second,New Name") {
				t.Errorf("after reorder %s", got)
			}

			// delete own.
			code, out = svCall(t, h, svAdmin, "admin", http.MethodDelete, path, "")
			if code != http.StatusOK || out.(map[string]any)["message"] != "View deleted." {
				t.Fatalf("delete: %d %v", code, out)
			}
			if code, _ := svCall(t, h, svAdmin, "admin", http.MethodPut, path, `{"name":"x","filters":{"a":"b"}}`); code != http.StatusNotFound {
				t.Errorf("update of a deleted view: %d, want 404", code)
			}

			// Admin routes: an agent who is not an admin is refused.
			if code, _ := svCall(t, h, svAdmin, "agent", http.MethodGet, "/escalated/admin/saved-views", ""); code != http.StatusForbidden {
				t.Errorf("agent index: %d, want 403", code)
			}
		})
	}
}

func TestOwnerlessSavedViewCanBeChangedByAnyAdmin(t *testing.T) {
	esc := newSavedViewFixture(t)
	h := (&uiFixture{esc: esc}).mounts()["chi"]
	if _, err := esc.Config.DB.Exec(`INSERT INTO escalated_saved_views (name, filters, position) VALUES ('Team', '{"priority":"high"}', 0)`); err != nil {
		t.Fatal(err)
	}
	code, out := svCall(t, h, svAdmin, "admin", http.MethodGet, "/escalated/admin/saved-views", "")
	if code != http.StatusOK {
		t.Fatalf("index: %d %v", code, out)
	}
	// An ownerless private view is nobody's, so it is not listed.
	if len(out.([]any)) != 0 {
		t.Errorf("index %v, want none", out)
	}
	var id int64
	if err := esc.Config.DB.QueryRow(`SELECT id FROM escalated_saved_views`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if code, out := svCall(t, h, svAdmin, "admin", http.MethodPut, fmt.Sprintf("/escalated/admin/saved-views/%d", id), `{"name":"Renamed","filters":{"priority":"high"},"is_shared":true}`); code != http.StatusOK {
		t.Fatalf("update ownerless: %d %v", code, out)
	}
	_, out = svCall(t, h, svOther, "admin", http.MethodGet, "/escalated/admin/saved-views", "")
	if got := strings.Join(svNames(t, out), ","); got != "Renamed" {
		t.Errorf("shared ownerless view listed as %q", got)
	}
}

func TestAdminTicketIndexListsSavedViews(t *testing.T) {
	esc := newSavedViewFixture(t)
	h := (&uiFixture{esc: esc}).mounts()["chi"]
	svCreate(t, h, svAdmin, `{"name":"Mine","filters":{"priority":"high"}}`)
	svCreate(t, h, svOther, `{"name":"Private","filters":{"priority":"low"}}`)

	page := visit(t, h, "/escalated/admin/tickets", svAdmin, "admin")
	if got := strings.Join(svNames(t, page.Props["savedViews"]), ","); got != "Mine" {
		t.Errorf("savedViews %q, want Mine", got)
	}
}

// The agent inbox lists the agent's own and shared saved views after the
// built-in ones, with counts (laravel AgentInboxPropsTest).
func (f *inboxFixture) savedView(owner, name, filters string, shared bool, position int) int64 {
	f.t.Helper()
	f.exec(`INSERT INTO escalated_saved_views (name, filters, user_id, is_shared, position) VALUES (?, ?, ?, ?, ?)`,
		name, filters, owner, shared, position)
	var id int64
	if err := f.esc.Config.DB.QueryRow(`SELECT MAX(id) FROM escalated_saved_views`).Scan(&id); err != nil {
		f.t.Fatal(err)
	}
	return id
}

func TestInboxListsOwnAndSharedSavedViewsWithCounts(t *testing.T) {
	f := newInboxFixture(t)
	mine := f.savedView(inboxAgent, "High priority", `{"priority":"high"}`, false, 1)
	shared := f.savedView(inboxOther, "Waiting", `{"status":"waiting_on_agent"}`, true, 2)
	f.savedView(inboxOther, "Private", `{"priority":"low"}`, false, 0)

	f.ticket(priority(models.PriorityHigh))
	f.ticket(priority(models.PriorityHigh))
	f.ticket(priority(models.PriorityHigh), closedNow())
	f.ticket(status(models.StatusWaitingOnAgent))

	views := list(t, obj(t, f.index("").Props, "inbox"), "views")
	var saved []any
	for _, v := range views {
		id := v.(map[string]any)["id"]
		if _, builtIn := id.(string); !builtIn {
			saved = append(saved, v)
		}
	}
	wantJSON(t, "saved views", saved, fmt.Sprintf(`[
		{"id":%d,"name":"High priority","count":2,"filters":{"priority":"high"}},
		{"id":%d,"name":"Waiting","count":1,"filters":{"status":"waiting_on_agent"}}
	]`, mine, shared))
}

func TestInboxSelectsASavedViewByIDOrFilters(t *testing.T) {
	for _, by := range []string{"id", "filters"} {
		t.Run(by, func(t *testing.T) {
			f := newInboxFixture(t)
			id := f.savedView(inboxAgent, "High priority", `{"priority":"high"}`, false, 1)
			high := f.ticket(priority(models.PriorityHigh))
			f.ticket(priority(models.PriorityLow))

			query := "?priority=high"
			if by == "id" {
				query = fmt.Sprintf("?view=%d", id)
			}
			page := f.index(query)
			wantJSON(t, "queue", page.Props["queue"], fmt.Sprintf(`{"key":"view:%d","title":"High priority"}`, id))
			if got := ticketIDs(t, page); len(got) != 1 || got[0] != high.ID {
				t.Errorf("tickets %v, want [%d]", got, high.ID)
			}
		})
	}
}

func TestInboxDoesNotSelectAnotherAgentsPrivateView(t *testing.T) {
	f := newInboxFixture(t)
	id := f.savedView(inboxOther, "Private", `{"priority":"high"}`, false, 1)
	f.ticket(priority(models.PriorityHigh))
	f.ticket(priority(models.PriorityLow))

	page := f.index(fmt.Sprintf("?view=%d", id))
	wantJSON(t, "queue", page.Props["queue"], `{"key":"all","title":"All tickets"}`)
	if total := obj(t, page.Props, "tickets")["total"]; total != float64(2) {
		t.Errorf("total %v, want 2", total)
	}
}
