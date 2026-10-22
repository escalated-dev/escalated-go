package router_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	escalated "github.com/escalated-dev/escalated-go"
	"github.com/escalated-dev/escalated-go/internal/sqldialect"
	"github.com/escalated-dev/escalated-go/internal/testdb"
	"github.com/escalated-dev/escalated-go/migrations"
	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/router"
)

// Custom fields and custom objects: the five admin pages with Laravel's props
// and writes, and ticket values through custom_fields (CUSTOM-FIELDS-SPEC).

const cfIndex = "/escalated/admin/custom-fields"
const coIndex = "/escalated/admin/custom-objects"

func countRows(t *testing.T, f *uiFixture, table, where string, args ...any) int {
	t.Helper()
	db := f.esc.Config.DB
	var n int
	q := "SELECT COUNT(1) FROM escalated_" + table
	if where != "" {
		q += " WHERE " + where
	}
	if err := db.QueryRow(sqldialect.Rebind(db, q), args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

// fieldBySlug finds a field on the index page.
func fieldBySlug(t *testing.T, h http.Handler, slug string) map[string]any {
	t.Helper()
	page := visit(t, h, cfIndex, uiAgent, "admin")
	for _, item := range list(t, page.Props, "fields") {
		if m := item.(map[string]any); m["slug"] == slug {
			return m
		}
	}
	t.Fatalf("no field %q in %v", slug, page.Props["fields"])
	return nil
}

func fieldID(t *testing.T, h http.Handler, slug string) string {
	t.Helper()
	return fmt.Sprint(int64(fieldBySlug(t, h, slug)["id"].(float64)))
}

func TestAdminCustomFieldPages(t *testing.T) {
	f := newUIFixture(t)
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			create := visit(t, h, cfIndex+"/create", uiAgent, "admin")
			if create.Component != "Escalated/Admin/CustomFields/Form" {
				t.Fatalf("create component %q", create.Component)
			}
			if got := fmt.Sprint(list(t, create.Props, "contexts")); got != "[ticket user organization]" {
				t.Errorf("contexts %s", got)
			}

			label := "Plan " + name
			wantRedirect(t, pageJSON(h, http.MethodPost, cfIndex, `{"name":"`+label+`","type":"select","context":"ticket",
				"options":["Free","Pro"],"required":true,"placeholder":"Pick one","description":"Billing plan","position":3,"active":true}`),
				"store", cfIndex)
			// The same name again gets a de-duplicated slug.
			wantRedirect(t, pageJSON(h, http.MethodPost, cfIndex, `{"name":"`+label+`","type":"text","context":"user"}`), "store again", cfIndex)

			slug := "plan_" + name
			row := fieldBySlug(t, h, slug)
			hasKeys(t, "field", row, "id", "name", "slug", "type", "context", "options", "required", "placeholder",
				"description", "validation_rules", "conditions", "position", "active", "created_at", "updated_at")
			if row["type"] != "select" || row["context"] != "ticket" || row["required"] != true || row["active"] != true ||
				row["position"] != float64(3) || row["placeholder"] != "Pick one" || fmt.Sprint(row["options"]) != "[Free Pro]" {
				t.Errorf("field %v", row)
			}
			second := fieldBySlug(t, h, slug+"_2")
			if second["context"] != "user" || second["required"] != false || second["active"] != true {
				t.Errorf("second field %v", second)
			}

			id := fieldID(t, h, slug)
			edit := visit(t, h, cfIndex+"/"+id+"/edit", uiAgent, "admin")
			if edit.Component != "Escalated/Admin/CustomFields/Form" {
				t.Fatalf("edit component %q", edit.Component)
			}
			hasKeys(t, "edit props", edit.Props, "field", "contexts")
			hasKeys(t, "edit field", obj(t, edit.Props, "field"), "id", "name", "type", "context", "options", "required", "placeholder", "description", "position", "active")

			wantRedirect(t, pageJSON(h, http.MethodPut, cfIndex+"/"+id, `{"name":"Tier `+name+`","type":"multi_select","context":"ticket",
				"options":["Free","Pro","Enterprise"],"required":false,"placeholder":"","description":"","position":1,"active":false}`), "update", cfIndex)
			updated := fieldBySlug(t, h, slug)
			if updated["name"] != "Tier "+name || updated["type"] != "multi_select" || updated["active"] != false ||
				updated["required"] != false || updated["placeholder"] != nil || len(updated["options"].([]any)) != 3 {
				t.Errorf("updated %v", updated)
			}

			for what, body := range map[string]string{
				"name":    `{"name":"","type":"text","context":"ticket"}`,
				"type":    `{"name":"X","type":"url","context":"ticket"}`,
				"context": `{"name":"X","type":"text","context":"account"}`,
				"options": `{"name":"X","type":"select","context":"ticket","options":[]}`,
			} {
				wantPageErrors(t, pageJSON(h, http.MethodPost, cfIndex, body), "store with a bad "+what, "Escalated/Admin/CustomFields/Form", what)
			}
			// An API client gets 422 for a select without options.
			if rec := sendAPI(h, http.MethodPost, cfIndex, uiAgent, "admin", `{"name":"X","type":"multi_select","context":"ticket"}`); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), `"options"`) {
				t.Errorf("select without options: %d %s", rec.Code, rec.Body.String())
			}

			// Reorder, then back to where the page came from.
			secondID := fieldID(t, h, slug+"_2")
			r := httptest.NewRequest(http.MethodPost, cfIndex+"/reorder", strings.NewReader(`{"positions":[{"id":`+id+`,"position":9},{"id":`+secondID+`,"position":8}]}`))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-Inertia", "true")
			r.Header.Set("Referer", "http://example.com"+cfIndex)
			r.Header.Set(headerUser, uiAgent)
			r.Header.Set(headerRole, "admin")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, r)
			wantRedirect(t, rec, "reorder", cfIndex)
			if fieldBySlug(t, h, slug)["position"] != float64(9) || fieldBySlug(t, h, slug+"_2")["position"] != float64(8) {
				t.Error("positions were not saved")
			}
			if rec := sendAPI(h, http.MethodPost, cfIndex+"/reorder", uiAgent, "admin", `{"positions":[{"id":999999,"position":1}]}`); rec.Code != http.StatusUnprocessableEntity {
				t.Errorf("reorder an unknown field: %d", rec.Code)
			}

			if rec := sendAPI(h, http.MethodGet, cfIndex, uiAgent, "agent", ""); rec.Code != http.StatusForbidden && rec.Code != http.StatusNotFound && rec.Code != http.StatusUnauthorized {
				t.Errorf("an agent reached the admin page: %d", rec.Code)
			}

			// Destroy takes the field's stored values with it.
			db := f.esc.Config.DB
			if _, err := db.Exec(sqldialect.Rebind(db, "INSERT INTO escalated_custom_field_values (custom_field_id, entity_type, entity_id, value) VALUES (?, 'ticket', ?, ?)"), id, f.ticket.ID, `["Pro"]`); err != nil {
				t.Fatal(err)
			}
			wantRedirect(t, pageJSON(h, http.MethodDelete, cfIndex+"/"+id, ""), "destroy", cfIndex)
			if countRows(t, f, "custom_fields", "id = ?", id) != 0 || countRows(t, f, "custom_field_values", "custom_field_id = ?", id) != 0 {
				t.Error("the field or its values survived the delete")
			}
			wantRedirect(t, pageJSON(h, http.MethodDelete, cfIndex+"/"+secondID, ""), "destroy second", cfIndex)
		})
	}
}

func objectByName(t *testing.T, h http.Handler, name string) map[string]any {
	t.Helper()
	page := visit(t, h, coIndex, uiAgent, "admin")
	if page.Component != "Escalated/Admin/CustomObjects/Index" {
		t.Fatalf("index component %q", page.Component)
	}
	for _, item := range list(t, page.Props, "objects") {
		if m := item.(map[string]any); m["name"] == name {
			return m
		}
	}
	t.Fatalf("no object %q in %v", name, page.Props["objects"])
	return nil
}

func TestAdminCustomObjectPages(t *testing.T) {
	f := newUIFixture(t)
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			create := visit(t, h, coIndex+"/create", uiAgent, "admin")
			if create.Component != "Escalated/Admin/CustomObjects/Form" {
				t.Fatalf("create component %q", create.Component)
			}

			objName := "Assets " + name
			slug := "assets_" + name
			schema := `[{"name":"Serial","type":"text","required":true},{"name":"Cost","type":"number"},
				{"name":"Bought","type":"date"},{"name":"Kind","type":"select","options":["Laptop","Phone"]}]`
			wantRedirect(t, pageJSON(h, http.MethodPost, coIndex, `{"name":"`+objName+`","slug":"`+slug+`","fields_schema":`+schema+`}`), "store", coIndex)

			for what, body := range map[string]string{
				"name":                 `{"name":"","slug":"x_` + name + `","fields_schema":` + schema + `}`,
				"slug":                 `{"name":"Dup","slug":"` + slug + `","fields_schema":` + schema + `}`,
				"fields_schema":        `{"name":"Empty","slug":"empty_` + name + `","fields_schema":[]}`,
				"fields_schema.0.type": `{"name":"Bad","slug":"bad_` + name + `","fields_schema":[{"name":"A","type":"blob"}]}`,
				"fields_schema.0.name": `{"name":"Bad","slug":"bad_` + name + `","fields_schema":[{"name":"","type":"text"}]}`,
			} {
				wantPageErrors(t, pageJSON(h, http.MethodPost, coIndex, body), "store with a bad "+what, "Escalated/Admin/CustomObjects/Form", what)
			}
			wantPageErrors(t, pageJSON(h, http.MethodPost, coIndex, `{"name":"Spaces","slug":"has spaces","fields_schema":`+schema+`}`), "store with a bad slug", "Escalated/Admin/CustomObjects/Form", "slug")

			row := objectByName(t, h, objName)
			hasKeys(t, "object", row, "id", "name", "slug", "fields_schema", "records_count", "created_at", "updated_at")
			if row["slug"] != slug || row["records_count"] != float64(0) || len(row["fields_schema"].([]any)) != 4 {
				t.Errorf("object %v", row)
			}
			hasKeys(t, "schema field", first(t, list(t, row, "fields_schema"), "fields_schema"), "name", "type", "required")
			id := fmt.Sprint(int64(row["id"].(float64)))

			edit := visit(t, h, coIndex+"/"+id+"/edit", uiAgent, "admin")
			if edit.Component != "Escalated/Admin/CustomObjects/Form" {
				t.Fatalf("edit component %q", edit.Component)
			}
			hasKeys(t, "edit object", obj(t, edit.Props, "object"), "id", "name", "slug", "fields_schema")
			// Keeping its own slug is not a clash.
			wantRedirect(t, pageJSON(h, http.MethodPut, coIndex+"/"+id, `{"name":"Devices `+name+`","slug":"`+slug+`","fields_schema":`+schema+`}`), "update", coIndex)
			objName = "Devices " + name

			recordsURL := coIndex + "/" + id + "/records"
			records := func() inertiaPage {
				t.Helper()
				page := visit(t, h, recordsURL, uiAgent, "admin")
				if page.Component != "Escalated/Admin/CustomObjects/Records" {
					t.Fatalf("records component %q", page.Component)
				}
				return page
			}
			page := records()
			hasKeys(t, "records props", page.Props, "object", "records")
			hasKeys(t, "records object", obj(t, page.Props, "object"), "id", "name", "fields_schema")

			back := func(method, path, body string) *httptest.ResponseRecorder {
				r := httptest.NewRequest(method, path, strings.NewReader(body))
				if body != "" {
					r.Header.Set("Content-Type", "application/json")
				}
				r.Header.Set("X-Inertia", "true")
				r.Header.Set("Referer", "http://example.com"+recordsURL)
				r.Header.Set(headerUser, uiAgent)
				r.Header.Set(headerRole, "admin")
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, r)
				return rec
			}
			wantRedirect(t, back(http.MethodPost, recordsURL, `{"data":{"Serial":"A1","Cost":"1200.50","Bought":"2026-01-02","Kind":"Laptop"}}`), "record store", recordsURL)
			wantRedirect(t, back(http.MethodPost, recordsURL, `{"data":{"Serial":"B2","Cost":"","Bought":"","Kind":""}}`), "record store with blanks", recordsURL)

			for what, body := range map[string]string{
				"data":         `{}`,
				"data.Serial":  `{"data":{"Cost":"1"}}`,
				"data.Cost":    `{"data":{"Serial":"x","Cost":"lots"}}`,
				"data.Bought":  `{"data":{"Serial":"x","Bought":"2026-02-31"}}`,
				"data.Kind":    `{"data":{"Serial":"x","Kind":"Tablet"}}`,
				"data.Unknown": `{"data":{"Serial":"x","Unknown":"y"}}`,
			} {
				wantPageErrors(t, back(http.MethodPost, recordsURL, body), "record with a bad "+what, "Escalated/Admin/CustomObjects/Records", what)
			}

			page = records()
			recs := list(t, page.Props, "records")
			if len(recs) != 2 {
				t.Fatalf("records %v", recs)
			}
			newest := recs[0].(map[string]any)
			hasKeys(t, "record", newest, "id", "object_id", "data", "created_at", "updated_at")
			if obj(t, newest, "data")["Serial"] != "B2" || obj(t, recs[1].(map[string]any), "data")["Kind"] != "Laptop" {
				t.Errorf("records newest first %v", recs)
			}
			if objectByName(t, h, objName)["records_count"] != float64(2) {
				t.Error("records_count")
			}
			recID := fmt.Sprint(int64(newest["id"].(float64)))

			wantRedirect(t, back(http.MethodPut, recordsURL+"/"+recID, `{"data":{"Serial":"B3","Cost":"5"}}`), "record update", recordsURL)
			if got := obj(t, list(t, records().Props, "records")[0].(map[string]any), "data"); got["Serial"] != "B3" || got["Cost"] != "5" {
				t.Errorf("updated record %v", got)
			}
			wantPageErrors(t, back(http.MethodPut, recordsURL+"/"+recID, `{"data":{"Serial":""}}`), "record update without serial", "Escalated/Admin/CustomObjects/Records", "data.Serial")

			// A record id that belongs to another object is a 404.
			wantRedirect(t, pageJSON(h, http.MethodPost, coIndex, `{"name":"Other `+name+`","slug":"other_`+name+`","fields_schema":[{"name":"N","type":"text"}]}`), "store other", coIndex)
			other := fmt.Sprint(int64(objectByName(t, h, "Other "+name)["id"].(float64)))
			if rec := back(http.MethodPut, coIndex+"/"+other+"/records/"+recID, `{"data":{"N":"x"}}`); rec.Code != http.StatusNotFound {
				t.Errorf("update through another object: %d", rec.Code)
			}
			if rec := back(http.MethodDelete, coIndex+"/"+other+"/records/"+recID, ""); rec.Code != http.StatusNotFound {
				t.Errorf("delete through another object: %d", rec.Code)
			}

			wantRedirect(t, back(http.MethodDelete, recordsURL+"/"+recID, ""), "record destroy", recordsURL)
			if len(list(t, records().Props, "records")) != 1 {
				t.Error("the record was not deleted")
			}

			// Destroying the object takes its records with it.
			wantRedirect(t, pageJSON(h, http.MethodDelete, coIndex+"/"+id, ""), "destroy", coIndex)
			if countRows(t, f, "custom_objects", "id = ?", id) != 0 || countRows(t, f, "custom_object_records", "custom_object_id = ?", id) != 0 {
				t.Error("the object or its records survived the delete")
			}
			wantRedirect(t, pageJSON(h, http.MethodDelete, coIndex+"/"+other, ""), "destroy other", coIndex)
		})
	}
}

// seedTicketFields defines the ticket fields the value tests use, through the
// admin.
func seedTicketFields(t *testing.T, h http.Handler) {
	t.Helper()
	for _, body := range []string{
		`{"name":"Order ref","type":"text","context":"ticket","required":true,"position":1}`,
		`{"name":"Seats","type":"number","context":"ticket","position":2}`,
		`{"name":"Renewal","type":"date","context":"ticket","position":3}`,
		`{"name":"Plan","type":"select","context":"ticket","options":["Free","Pro"],"position":4}`,
		`{"name":"Addons","type":"multi_select","context":"ticket","options":["SSO","Audit"],"position":5}`,
		`{"name":"Urgent","type":"checkbox","context":"ticket","position":6}`,
		`{"name":"Notes","type":"textarea","context":"ticket","position":7}`,
		`{"name":"Legacy","type":"text","context":"ticket","active":false}`,
		`{"name":"Company size","type":"number","context":"organization"}`,
	} {
		wantRedirect(t, pageJSON(h, http.MethodPost, cfIndex, body), "seed field", cfIndex)
	}
}

func valuesBySlug(t *testing.T, fields []any) map[string]any {
	t.Helper()
	out := map[string]any{}
	for _, item := range fields {
		m := item.(map[string]any)
		hasKeys(t, "custom field", m, "id", "name", "slug", "type", "options", "required", "placeholder", "description", "position", "value")
		out[m["slug"].(string)] = m["value"]
	}
	return out
}

func TestTicketCustomFieldValues(t *testing.T) {
	f := newUIFixture(t)
	chiH := f.mounts()["chi"]
	seedTicketFields(t, chiH)

	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			tickets := func() int { return countRows(t, f, "tickets", "") }
			createAPI := func(fields string) *httptest.ResponseRecorder {
				return sendAPI(h, http.MethodPost, "/escalated/api/tickets", uiAgent, "agent",
					`{"subject":"Seats `+name+`","description":"Need more seats","custom_fields":`+fields+`}`)
			}

			// Validation fails before the ticket is written.
			for key, fields := range map[string]string{
				"custom_fields.order_ref":    `{"seats":2}`,
				"custom_fields.seats":        `{"order_ref":"A1","seats":"many"}`,
				"custom_fields.renewal":      `{"order_ref":"A1","renewal":"2026-02-30"}`,
				"custom_fields.plan":         `{"order_ref":"A1","plan":"Gold"}`,
				"custom_fields.addons":       `{"order_ref":"A1","addons":["SSO","Gold"]}`,
				"custom_fields.urgent":       `{"order_ref":"A1","urgent":"maybe"}`,
				"custom_fields.nope":         `{"order_ref":"A1","nope":"x"}`,
				"custom_fields.legacy":       `{"order_ref":"A1","legacy":"x"}`,
				"custom_fields.company_size": `{"order_ref":"A1","company_size":5}`,
			} {
				before := tickets()
				rec := createAPI(fields)
				var body struct {
					Errors map[string][]string `json:"errors"`
				}
				_ = json.Unmarshal(rec.Body.Bytes(), &body)
				if rec.Code != http.StatusUnprocessableEntity || body.Errors[key] == nil {
					t.Errorf("%s: %d %s", key, rec.Code, rec.Body.String())
				}
				if tickets() != before {
					t.Errorf("%s: a failed create wrote a ticket", key)
				}
			}
			// Required on create even when custom_fields is left out.
			if rec := sendAPI(h, http.MethodPost, "/escalated/api/tickets", uiAgent, "agent", `{"subject":"x","description":"y"}`); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "custom_fields.order_ref") {
				t.Errorf("create without the required field: %d %s", rec.Code, rec.Body.String())
			}

			rec := createAPI(`{"order_ref":"A-100","seats":"12","renewal":"2026-12-01","plan":"Pro","addons":["SSO","Audit"],"urgent":true,"notes":""}`)
			if rec.Code != http.StatusCreated {
				t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
			}
			var created struct {
				Ticket models.Ticket `json:"ticket"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
				t.Fatal(err)
			}
			id := fmt.Sprint(created.Ticket.ID)

			show := func() map[string]any {
				t.Helper()
				rec := sendAPI(h, http.MethodGet, "/escalated/api/tickets/"+id, uiAgent, "agent", "")
				var detail map[string]any
				if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil || rec.Code != http.StatusOK {
					t.Fatalf("show: %d %s", rec.Code, rec.Body.String())
				}
				fields := list(t, obj(t, detail, "ticket"), "custom_fields")
				if len(fields) != 7 {
					t.Errorf("only the active ticket fields: %d", len(fields))
				}
				return valuesBySlug(t, fields)
			}
			got := show()
			if got["order_ref"] != "A-100" || got["seats"] != float64(12) || got["renewal"] != "2026-12-01" || got["plan"] != "Pro" ||
				fmt.Sprint(got["addons"]) != "[SSO Audit]" || got["urgent"] != true || got["notes"] != nil {
				t.Errorf("values %v", got)
			}

			// A partial update touches only the keys sent; a blank optional
			// value clears it; a sent required field cannot be blank.
			patch := func(body string) *httptest.ResponseRecorder {
				return sendAPI(h, http.MethodPatch, "/escalated/api/tickets/"+id, uiAgent, "agent", body)
			}
			if rec := patch(`{"custom_fields":{"seats":3,"plan":null,"urgent":"0"}}`); rec.Code != http.StatusOK {
				t.Fatalf("patch: %d %s", rec.Code, rec.Body.String())
			}
			got = show()
			if got["seats"] != float64(3) || got["plan"] != nil || got["urgent"] != false || got["order_ref"] != "A-100" || got["renewal"] != "2026-12-01" {
				t.Errorf("after patch %v", got)
			}
			if rec := patch(`{"custom_fields":{"order_ref":""}}`); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "custom_fields.order_ref") {
				t.Errorf("blank required: %d %s", rec.Code, rec.Body.String())
			}
			if rec := patch(`{"subject":"Renamed ` + name + `"}`); rec.Code != http.StatusOK {
				t.Errorf("patch without custom_fields: %d", rec.Code)
			}
			if show()["order_ref"] != "A-100" {
				t.Error("omitting custom_fields changed a value")
			}

			// The agent ticket screen writes them too and shows them.
			if rec := pageJSON(h, http.MethodPut, "/escalated/agent/tickets/"+id, `{"custom_fields":{"notes":"Call back","addons":[]}}`); rec.Code != http.StatusSeeOther && rec.Code != http.StatusOK {
				t.Fatalf("agent update: %d %s", rec.Code, rec.Body.String())
			}
			agentPage := visit(t, h, "/escalated/agent/tickets/"+id, uiAgent, "agent")
			got = valuesBySlug(t, list(t, agentPage.Props, "custom_fields"))
			if got["notes"] != "Call back" || got["addons"] != nil || got["seats"] != float64(3) {
				t.Errorf("agent show %v", got)
			}
			wantPageErrors(t, pageJSON(h, http.MethodPut, "/escalated/agent/tickets/"+id, `{"custom_fields":{"seats":"x"}}`),
				"agent update with a bad number", "Escalated/Agent/TicketShow", "custom_fields.seats")
			adminPage := visit(t, h, "/escalated/admin/tickets/"+id, uiAgent, "admin")
			if valuesBySlug(t, list(t, adminPage.Props, "custom_fields"))["order_ref"] != "A-100" {
				t.Errorf("admin show %v", adminPage.Props["custom_fields"])
			}
		})
	}
}

func TestCustomerTicketCustomFields(t *testing.T) {
	f := newUIFixture(t)
	seedTicketFields(t, f.mounts()["chi"])
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			page := visit(t, h, "/escalated/tickets/create", customerB, "")
			defs := list(t, page.Props, "custom_fields")
			if len(defs) != 7 {
				t.Fatalf("create page custom_fields %v", defs)
			}
			def := first(t, defs, "custom_fields")
			hasKeys(t, "definition", def, "id", "name", "slug", "type", "options", "required", "placeholder", "description", "position")
			if def["slug"] != "order_ref" || def["required"] != true {
				t.Errorf("first definition %v", def)
			}

			post := func(form url.Values) *httptest.ResponseRecorder {
				r := httptest.NewRequest(http.MethodPost, "/escalated/tickets", strings.NewReader(form.Encode()))
				r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				r.Header.Set("X-Inertia", "true")
				r.Header.Set(headerUser, customerB)
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, r)
				return rec
			}
			before := countRows(t, f, "tickets", "")
			wantPageErrors(t, post(url.Values{"subject": {"No ref"}, "description": {"x"}}), "missing required field", "Escalated/Customer/Create", "custom_fields.order_ref")
			if countRows(t, f, "tickets", "") != before {
				t.Error("a failed create wrote a ticket")
			}

			rec := post(url.Values{"subject": {"Form " + name}, "description": {"x"}, "custom_fields[order_ref]": {"F-1"},
				"custom_fields[addons][]": {"SSO", "Audit"}, "custom_fields[urgent]": {"1"}})
			if rec.Code != http.StatusSeeOther {
				t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
			}
			loc := rec.Header().Get("Location")
			show := visit(t, h, loc, customerB, "")
			got := valuesBySlug(t, list(t, show.Props, "custom_fields"))
			if got["order_ref"] != "F-1" || fmt.Sprint(got["addons"]) != "[SSO Audit]" || got["urgent"] != true || got["seats"] != nil {
				t.Errorf("customer show %v", got)
			}

			// A JSON body nests them as an object.
			r := httptest.NewRequest(http.MethodPost, "/escalated/tickets", strings.NewReader(`{"subject":"Json `+name+`","description":"x","custom_fields":{"order_ref":"J-1","seats":"x"}}`))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set(headerUser, customerB)
			rec = httptest.NewRecorder()
			h.ServeHTTP(rec, r)
			if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "custom_fields.seats") {
				t.Errorf("json create: %d %s", rec.Code, rec.Body.String())
			}
		})
	}
}

// Every new table name goes through Config.TablePrefix.
func TestCustomFieldsHonourTablePrefix(t *testing.T) {
	db := testdb.Open(t)
	const prefix = "support_"
	migrate := migrations.MigrateSQLite
	if testdb.Driver(t) != "sqlite" {
		migrate = migrations.Migrate
	}
	if err := migrate(db, prefix); err != nil {
		t.Fatalf("migrate %s: %v", prefix, err)
	}
	cfg := escalated.DefaultConfig()
	cfg.DB = db
	cfg.TablePrefix = prefix
	cfg.UIEnabled = true
	cfg.AgentCheck = func(r *http.Request) bool { return r.Header.Get(headerRole) != "" }
	cfg.AdminCheck = func(r *http.Request) bool { return r.Header.Get(headerRole) == "admin" }
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
			if rec := pageJSON(h, http.MethodPost, cfIndex, `{"name":"Code `+name+`","type":"text","context":"ticket"}`); rec.Code != http.StatusSeeOther {
				t.Fatalf("store field: %d %s", rec.Code, rec.Body.String())
			}
			rec := sendAPI(h, http.MethodPost, "/escalated/api/tickets", uiAgent, "agent", `{"subject":"s","description":"d","custom_fields":{"code_`+name+`":"Z9"}}`)
			if rec.Code != http.StatusCreated {
				t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
			}
			var created struct {
				Ticket models.Ticket `json:"ticket"`
			}
			_ = json.Unmarshal(rec.Body.Bytes(), &created)
			rec = sendAPI(h, http.MethodGet, "/escalated/api/tickets/"+fmt.Sprint(created.Ticket.ID), uiAgent, "agent", "")
			if !strings.Contains(rec.Body.String(), `"value":"Z9"`) {
				t.Errorf("show: %d %s", rec.Code, rec.Body.String())
			}
			if rec := pageJSON(h, http.MethodPost, coIndex, `{"name":"Obj `+name+`","slug":"obj_`+name+`","fields_schema":[{"name":"A","type":"text"}]}`); rec.Code != http.StatusSeeOther {
				t.Fatalf("store object: %d %s", rec.Code, rec.Body.String())
			}
			if page := visit(t, h, coIndex, uiAgent, "admin"); len(list(t, page.Props, "objects")) == 0 {
				t.Error("no objects listed")
			}
		})
	}
	var n int
	if err := db.QueryRow("SELECT COUNT(1) FROM escalated_custom_fields").Scan(&n); err != nil || n != 0 {
		t.Errorf("the default-prefix table was written: %d %v", n, err)
	}
}
