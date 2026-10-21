package router_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/escalated-dev/escalated-go/internal/sqldialect"
	"github.com/escalated-dev/escalated-go/models"
)

// The admin config pages: SLA policies, escalation rules, macros and canned
// responses. Their forms post JSON with Inertia (useForm) and expect a 303
// to the index, or the page again with errors; API clients keep their JSON.

// pageJSON sends a JSON body as a page visit (useForm's put/post/delete).
func pageJSON(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	r.Header.Set("X-Inertia", "true")
	r.Header.Set(headerUser, uiAgent)
	r.Header.Set(headerRole, "admin")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func wantRedirect(t *testing.T, rec *httptest.ResponseRecorder, what, to string) {
	t.Helper()
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != to {
		t.Fatalf("%s: got %d to %q, want 303 to %q: %s", what, rec.Code, rec.Header().Get("Location"), to, rec.Body.String())
	}
}

func (f *uiFixture) count(t *testing.T, table, where string, args ...any) int {
	t.Helper()
	db := f.esc.Config.DB
	var n int
	if err := db.QueryRow(sqldialect.Rebind(db, "SELECT COUNT(1) FROM escalated_"+table+" WHERE "+where), args...).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func TestAdminSlaPolicyFormPages(t *testing.T) {
	f := newUIFixture(t)
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			index := "/escalated/admin/sla-policies"
			page := visit(t, h, index+"/create", uiAgent, "admin")
			if page.Component != "Escalated/Admin/SlaPolicies/Form" {
				t.Fatalf("component %q", page.Component)
			}
			hasKeys(t, "create props", page.Props, "priorities", "targeted", "departments", "ticketTypes")

			wantRedirect(t, pageJSON(h, http.MethodPost, index,
				`{"name":"Gold `+name+`","description":"","is_default":false,"first_response_hours":{"high":2,"low":"8"},"resolution_hours":{"high":12},"business_hours_only":false,"is_active":true}`),
				"store", index)
			policies, _ := f.esc.Store.ListSLAPolicies(context.Background(), false)
			var gold *models.SLAPolicy
			for _, p := range policies {
				if p.Name == "Gold "+name {
					gold = p
				}
			}
			if gold == nil {
				t.Fatal("the policy was not stored")
			}
			if hours, ok := gold.FirstResponseHoursFor("high"); !ok || hours != 2 {
				t.Errorf("first response hours for high: %v %v", hours, ok)
			}

			edit := visit(t, h, index+"/"+itoa(gold.ID)+"/edit", uiAgent, "admin")
			policy := obj(t, edit.Props, "policy")
			hasKeys(t, "policy", policy, "id", "name", "description", "is_default", "first_response_hours", "resolution_hours", "business_hours_only", "is_active")
			if obj(t, policy, "first_response_hours")["high"].(float64) != 2 {
				t.Errorf("policy %v", policy)
			}

			wantRedirect(t, pageJSON(h, http.MethodPut, index+"/"+itoa(gold.ID), `{"name":"Platinum `+name+`","is_active":false}`), "update", index)
			updated, _ := f.esc.Store.GetSLAPolicy(context.Background(), gold.ID)
			if updated.Name != "Platinum "+name || updated.IsActive {
				t.Errorf("updated %+v", updated)
			}

			wantPageErrors(t, pageJSON(h, http.MethodPost, index, `{"name":""}`), "store without a name", "Escalated/Admin/SlaPolicies/Form", "name")
			wantPageErrors(t, pageJSON(h, http.MethodPut, index+"/"+itoa(gold.ID), `{"name":" "}`), "update without a name", "Escalated/Admin/SlaPolicies/Form", "name")

			wantRedirect(t, pageJSON(h, http.MethodDelete, index+"/"+itoa(gold.ID), ""), "destroy", index)
			if got, _ := f.esc.Store.GetSLAPolicy(context.Background(), gold.ID); got != nil {
				t.Error("the policy was not deleted")
			}

			// API clients keep their answers.
			if rec := sendAPI(h, http.MethodPost, index, uiAgent, "admin", `{"name":"API `+name+`"}`); rec.Code != http.StatusCreated {
				t.Errorf("API store: %d %s", rec.Code, rec.Body.String())
			}
			if rec := sendAPI(h, http.MethodPost, index, uiAgent, "admin", `{"name":""}`); rec.Code != http.StatusBadRequest {
				t.Errorf("API store without a name: %d", rec.Code)
			}
		})
	}
}

func TestAdminEscalationRulePages(t *testing.T) {
	f := newUIFixture(t)
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			index := "/escalated/admin/escalation-rules"
			if page := visit(t, h, index+"/create", uiAgent, "admin"); page.Component != "Escalated/Admin/EscalationRules/Form" {
				t.Fatalf("create component %q", page.Component)
			}
			wantRedirect(t, pageJSON(h, http.MethodPost, index,
				`{"name":"Stale `+name+`","description":"","trigger_type":"time_based","conditions":[{"field":"age_hours","value":"24"}],"actions":[{"type":"escalate","value":""}],"order":2,"is_active":true}`),
				"store", index)

			page := visit(t, h, index, uiAgent, "admin")
			if page.Component != "Escalated/Admin/EscalationRules/Index" {
				t.Fatalf("index component %q", page.Component)
			}
			var rule map[string]any
			for _, item := range list(t, page.Props, "rules") {
				if m := item.(map[string]any); m["name"] == "Stale "+name {
					rule = m
				}
			}
			if rule == nil {
				t.Fatalf("rules %v", page.Props["rules"])
			}
			hasKeys(t, "rule", rule, "id", "name", "description", "trigger_type", "conditions", "actions", "order", "is_active")
			cond := first(t, list(t, rule, "conditions"), "conditions")
			if cond["field"] != "age_hours" {
				t.Errorf("conditions %v", rule["conditions"])
			}
			id := itoa(int64(rule["id"].(float64)))

			edit := visit(t, h, index+"/"+id+"/edit", uiAgent, "admin")
			hasKeys(t, "edit rule", obj(t, edit.Props, "rule"), "id", "name", "conditions", "actions", "order", "is_active", "trigger_type")

			wantRedirect(t, pageJSON(h, http.MethodPut, index+"/"+id, `{"name":"Renamed `+name+`","is_active":false}`), "update", index)
			if f.count(t, "escalation_rules", "name = ?", "Renamed "+name) != 1 {
				t.Error("the rule was not renamed")
			}
			wantPageErrors(t, pageJSON(h, http.MethodPut, index+"/"+id, `{"name":""}`), "update without a name", "Escalated/Admin/EscalationRules/Form", "name")
			wantPageErrors(t, pageJSON(h, http.MethodPost, index, `{"name":""}`), "store without a name", "Escalated/Admin/EscalationRules/Form", "name")
			wantRedirect(t, pageJSON(h, http.MethodDelete, index+"/"+id, ""), "destroy", index)

			// API clients still get JSON from the same GET.
			rec := sendAPI(h, http.MethodGet, index, uiAgent, "admin", "")
			var out map[string]any
			if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &out) != nil || out["rules"] == nil {
				t.Errorf("API list: %d %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestAdminMacroPage(t *testing.T) {
	f := newUIFixture(t)
	ctx := context.Background()
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			index := "/escalated/admin/macros"
			// The macro editor's own action names and values.
			wantRedirect(t, pageJSON(h, http.MethodPost, index,
				`{"name":"Wrap up `+name+`","description":"","is_shared":true,"actions":[{"type":"note","value":"Wrapped by macro `+name+`","order":0},{"type":"tags","value":"vip","order":1},{"type":"priority","value":"low","order":2},{"type":"status","value":"resolved","order":3}]}`),
				"store", index)

			page := visit(t, h, index, uiAgent, "admin")
			if page.Component != "Escalated/Admin/Macros/Index" {
				t.Fatalf("component %q", page.Component)
			}
			var macro map[string]any
			for _, item := range list(t, page.Props, "macros") {
				if m := item.(map[string]any); m["name"] == "Wrap up "+name {
					macro = m
				}
			}
			if macro == nil {
				t.Fatalf("macros %v", page.Props["macros"])
			}
			hasKeys(t, "macro", macro, "id", "name", "description", "is_shared", "actions", "creator")
			if len(list(t, macro, "actions")) != 4 || obj(t, macro, "creator")["name"] != "Ada Agent" {
				t.Errorf("macro %v", macro)
			}
			id := itoa(int64(macro["id"].(float64)))

			// Applied from the ticket screen, every action runs.
			show := "/escalated/agent/tickets/" + url.PathEscape(f.ticket.Reference)
			wantBack(t, postPage(h, http.MethodPost, show+"/macro", uiAgent, "agent", show, url.Values{"macro_id": {id}}), "apply", show)
			got := f.reload(t)
			if got.Status != models.StatusResolved || got.Priority != models.PriorityLow {
				t.Errorf("after the macro: status %d priority %d", got.Status, got.Priority)
			}
			if f.count(t, "replies", "ticket_id = ? AND body = ? AND is_internal = ?", f.ticket.ID, "Wrapped by macro "+name, true) != 1 {
				t.Error("the macro's note was not added")
			}
			if tags, _ := f.esc.Store.GetTicketTags(ctx, f.ticket.ID); len(tags) != 1 || tags[0].Name != "vip" {
				t.Errorf("tags %v", tags)
			}

			wantRedirect(t, pageJSON(h, http.MethodPut, index+"/"+id, `{"name":"Renamed `+name+`","description":"","is_shared":false,"actions":[]}`), "update", index)
			wantPageErrors(t, pageJSON(h, http.MethodPost, index, `{"name":"","actions":[]}`), "store without a name", "Escalated/Admin/Macros/Index", "name")
			wantRedirect(t, pageJSON(h, http.MethodDelete, index+"/"+id, ""), "destroy", index)
			if f.count(t, "macros", "id = ?", macro["id"]) != 0 {
				t.Error("the macro was not deleted")
			}
			if rec := sendAPI(h, http.MethodGet, index, uiAgent, "admin", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"macros"`) {
				t.Errorf("API list: %d %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestAdminCannedResponsePage(t *testing.T) {
	f := newUIFixture(t)
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			index := "/escalated/admin/canned-responses"
			wantRedirect(t, pageJSON(h, http.MethodPost, index, `{"title":"Thanks `+name+`","body":"Thanks for writing in.","category":"","is_shared":true}`), "store", index)
			page := visit(t, h, index, uiAgent, "admin")
			if page.Component != "Escalated/Admin/CannedResponses/Index" {
				t.Fatalf("component %q", page.Component)
			}
			response := first(t, list(t, page.Props, "responses"), "responses")
			hasKeys(t, "response", response, "id", "title", "body", "category", "is_shared", "creator")
			if obj(t, response, "creator")["name"] != "Ada Agent" {
				t.Errorf("response %v", response)
			}
			wantPageErrors(t, pageJSON(h, http.MethodPost, index, `{"title":"","body":""}`), "store empty", "Escalated/Admin/CannedResponses/Index", "title")
			wantRedirect(t, pageJSON(h, http.MethodDelete, index+"/"+itoa(int64(response["id"].(float64))), ""), "destroy", index)
			if rec := sendAPI(h, http.MethodPost, index, uiAgent, "admin", `{"title":"","body":""}`); rec.Code != http.StatusBadRequest {
				t.Errorf("API store empty: %d", rec.Code)
			}
		})
	}
}
