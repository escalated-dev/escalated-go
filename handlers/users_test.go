package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/renderer"
)

// fakeDirectory is an in-memory UserDirectory used by these tests. It
// keeps the test wiring small — there's no need to stand up a SQL
// store for a handler that talks exclusively through the directory
// hook.
type fakeDirectory struct {
	users []*AdminUser
}

func newFakeDirectory(seed ...*AdminUser) *fakeDirectory {
	d := &fakeDirectory{}
	for _, u := range seed {
		cp := *u
		d.users = append(d.users, &cp)
	}
	return d
}

func (d *fakeDirectory) ListUsers(_ context.Context, search string, page, perPage int) (UserPage, error) {
	var matched []AdminUser
	needle := strings.ToLower(search)
	for _, u := range d.users {
		if needle != "" && !strings.Contains(strings.ToLower(u.Email), needle) && !strings.Contains(strings.ToLower(u.Name), needle) {
			continue
		}
		matched = append(matched, *u)
	}
	// Order: is_admin DESC, is_agent DESC, id ASC (mirrors the
	// Laravel reference). Stable enough for assertions.
	for i := 0; i < len(matched); i++ {
		for j := i + 1; j < len(matched); j++ {
			a, b := matched[i], matched[j]
			swap := false
			switch {
			case a.IsAdmin != b.IsAdmin:
				swap = !a.IsAdmin && b.IsAdmin
			case a.IsAgent != b.IsAgent:
				swap = !a.IsAgent && b.IsAgent
			default:
				swap = a.ID > b.ID
			}
			if swap {
				matched[i], matched[j] = matched[j], matched[i]
			}
		}
	}
	if page < 1 {
		page = 1
	}
	if perPage <= 0 {
		perPage = 20
	}
	total := len(matched)
	start := (page - 1) * perPage
	end := start + perPage
	if start > total {
		start = total
	}
	if end > total {
		end = total
	}
	data := matched[start:end]
	if data == nil {
		data = []AdminUser{}
	}
	last := 1
	if total > 0 {
		last = (total + perPage - 1) / perPage
	}
	return UserPage{Data: data, CurrentPage: page, LastPage: last, PerPage: perPage, Total: total}, nil
}

func (d *fakeDirectory) GetUser(_ context.Context, id int64) (*AdminUser, error) {
	for _, u := range d.users {
		if u.ID == id {
			cp := *u
			return &cp, nil
		}
	}
	return nil, nil
}

func (d *fakeDirectory) UpdateUserRoles(_ context.Context, id int64, updates UserRoleUpdates) error {
	for _, u := range d.users {
		if u.ID != id {
			continue
		}
		if updates.IsAdmin != nil {
			u.IsAdmin = *updates.IsAdmin
		}
		if updates.IsAgent != nil {
			u.IsAgent = *updates.IsAgent
		}
		return nil
	}
	return nil
}

func newUserHandlerForTest(currentID int64, seed ...*AdminUser) (*UserHandler, *fakeDirectory) {
	dir := newFakeDirectory(seed...)
	h := NewUserHandler(dir, renderer.NewJSONRenderer(), func(_ *http.Request) models.UserID {
		return models.UserID(strconv.FormatInt(currentID, 10))
	})
	return h, dir
}

func decodeUsersIndex(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, body)
	}
	return out
}

func TestUserHandler_Index_ListsUsersWithRoleFlags(t *testing.T) {
	h, _ := newUserHandlerForTest(1,
		&AdminUser{ID: 1, Email: "admin@example.com", IsAdmin: true, IsAgent: true},
		&AdminUser{ID: 2, Email: "customer@example.com"},
		&AdminUser{ID: 3, Email: "agent@example.com", IsAgent: true},
	)

	req := httptest.NewRequest(http.MethodGet, "/admin/users", nil)
	rec := httptest.NewRecorder()
	h.Index(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	out := decodeUsersIndex(t, rec.Body.Bytes())
	users, _ := out["users"].(map[string]any)
	if users == nil {
		t.Fatalf("missing users prop: %v", out)
	}
	data, _ := users["data"].([]any)
	if len(data) != 3 {
		t.Fatalf("want 3 users in page, got %d", len(data))
	}
	emails := map[string]bool{}
	for _, item := range data {
		row, _ := item.(map[string]any)
		emails[row["email"].(string)] = true
	}
	for _, want := range []string{"admin@example.com", "customer@example.com", "agent@example.com"} {
		if !emails[want] {
			t.Errorf("expected %q in user page, got %v", want, emails)
		}
	}
	if got, _ := out["currentUserId"].(float64); int64(got) != 1 {
		t.Errorf("currentUserId: want 1, got %v", out["currentUserId"])
	}
}

func TestUserHandler_Index_FiltersBySearchTerm(t *testing.T) {
	h, _ := newUserHandlerForTest(1,
		&AdminUser{ID: 1, Email: "admin@example.com", IsAdmin: true, IsAgent: true},
		&AdminUser{ID: 2, Email: "jane@acme.test"},
		&AdminUser{ID: 3, Email: "bob@globex.test"},
	)

	req := httptest.NewRequest(http.MethodGet, "/admin/users?search=acme", nil)
	rec := httptest.NewRecorder()
	h.Index(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	out := decodeUsersIndex(t, rec.Body.Bytes())
	users := out["users"].(map[string]any)
	data := users["data"].([]any)
	if len(data) != 1 {
		t.Fatalf("want 1 user matching 'acme', got %d", len(data))
	}
	if got := data[0].(map[string]any)["email"].(string); got != "jane@acme.test" {
		t.Errorf("got %s, want jane@acme.test", got)
	}
	filters, _ := out["filters"].(map[string]any)
	if filters["search"] != "acme" {
		t.Errorf("filters.search: got %v", filters["search"])
	}
}

func TestUserHandler_Index_BlocksWhenNoDirectoryHostHasOptedOut(t *testing.T) {
	// When the host does not wire a UserDirectory the page must still
	// render — empty list, not 500. The admin-only routing middleware
	// already handles "not authorised" upstream.
	h := NewUserHandler(nil, renderer.NewJSONRenderer(), func(_ *http.Request) models.UserID { return "" })

	req := httptest.NewRequest(http.MethodGet, "/admin/users", nil)
	rec := httptest.NewRecorder()
	h.Index(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	out := decodeUsersIndex(t, rec.Body.Bytes())
	users := out["users"].(map[string]any)
	data := users["data"].([]any)
	if len(data) != 0 {
		t.Errorf("want empty list when no directory, got %d entries", len(data))
	}
}

func newPatchReq(t *testing.T, id, body string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPatch, "/admin/users/"+id+"/role", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("user", id)
	return req
}

func TestUserHandler_UpdateRole_PromotesUserToAdmin(t *testing.T) {
	h, dir := newUserHandlerForTest(1,
		&AdminUser{ID: 1, Email: "admin@example.com", IsAdmin: true, IsAgent: true},
		&AdminUser{ID: 2, Email: "someone@example.com"},
	)

	req := newPatchReq(t, "2", `{"role":"admin","value":true}`)
	rec := httptest.NewRecorder()
	h.UpdateRole(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	target, _ := dir.GetUser(context.Background(), 2)
	if !target.IsAdmin {
		t.Errorf("target.IsAdmin: want true")
	}
	if !target.IsAgent {
		t.Errorf("target.IsAgent: want true (admin implies agent)")
	}
}

func TestUserHandler_UpdateRole_PromotesUserToAgentOnly(t *testing.T) {
	h, dir := newUserHandlerForTest(1,
		&AdminUser{ID: 1, Email: "admin@example.com", IsAdmin: true, IsAgent: true},
		&AdminUser{ID: 2, Email: "someone@example.com"},
	)

	req := newPatchReq(t, "2", `{"role":"agent","value":true}`)
	rec := httptest.NewRecorder()
	h.UpdateRole(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	target, _ := dir.GetUser(context.Background(), 2)
	if !target.IsAgent {
		t.Errorf("target.IsAgent: want true")
	}
	if target.IsAdmin {
		t.Errorf("target.IsAdmin: want false (agent grant does not promote to admin)")
	}
}

func TestUserHandler_UpdateRole_PreventsSelfDemote(t *testing.T) {
	h, dir := newUserHandlerForTest(7,
		&AdminUser{ID: 7, Email: "admin@example.com", IsAdmin: true, IsAgent: true},
	)

	req := newPatchReq(t, "7", `{"role":"admin","value":false}`)
	rec := httptest.NewRecorder()
	h.UpdateRole(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", rec.Code, rec.Body.String())
	}
	target, _ := dir.GetUser(context.Background(), 7)
	if !target.IsAdmin {
		t.Errorf("self admin flag must be preserved after blocked self-demote")
	}
}

func TestUserHandler_UpdateRole_DemotesAdminAndClearsAgent(t *testing.T) {
	h, dir := newUserHandlerForTest(1,
		&AdminUser{ID: 1, Email: "admin@example.com", IsAdmin: true, IsAgent: true},
		&AdminUser{ID: 2, Email: "someone@example.com", IsAdmin: true, IsAgent: true},
	)

	req := newPatchReq(t, "2", `{"role":"agent","value":false}`)
	rec := httptest.NewRecorder()
	h.UpdateRole(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	target, _ := dir.GetUser(context.Background(), 2)
	if target.IsAgent {
		t.Errorf("target.IsAgent: want false")
	}
	if target.IsAdmin {
		t.Errorf("target.IsAdmin: want false (revoking agent from an admin demotes fully)")
	}
}
