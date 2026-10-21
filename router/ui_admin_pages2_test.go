package router_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/escalated-dev/escalated-go/internal/sqldialect"
	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/services"
)

// The remaining admin pages backed by this package's features: departments
// (whose index lacked the counts the page reads), tags, automations, agent
// capacity and the public-ticket guest policy.

func TestAdminDepartmentAndTagPages(t *testing.T) {
	for _, name := range []string{"chi", "stdlib"} {
		t.Run(name, func(t *testing.T) {
			f := newUIFixture(t)
			h := f.mounts()[name]
			ctx := context.Background()
			departments := "/escalated/admin/departments"

			page := visit(t, h, departments, uiAgent, "admin")
			if page.Component != "Escalated/Admin/Departments/Index" {
				t.Fatalf("component %q", page.Component)
			}
			var billing map[string]any
			for _, item := range list(t, page.Props, "departments") {
				if d := item.(map[string]any); d["name"] == "Billing" {
					billing = d
				}
			}
			hasKeys(t, "department", billing, "id", "name", "slug", "description", "is_active", "tickets_count", "agents_count")
			if billing["tickets_count"].(float64) != 1 {
				t.Errorf("tickets_count %v", billing["tickets_count"])
			}

			if form := visit(t, h, departments+"/create", uiAgent, "admin"); form.Component != "Escalated/Admin/Departments/Form" {
				t.Fatalf("create component %q", form.Component)
			}
			wantRedirect(t, pageJSON(h, http.MethodPost, departments, `{"name":"Returns","slug":"","description":"RMA","is_active":true}`), "store", departments)
			wantPageErrors(t, pageJSON(h, http.MethodPost, departments, `{"name":""}`), "store without a name", "Escalated/Admin/Departments/Form", "name")
			id := itoa(int64(billing["id"].(float64)))
			edit := visit(t, h, departments+"/"+id+"/edit", uiAgent, "admin")
			hasKeys(t, "edit department", obj(t, edit.Props, "department"), "id", "name", "slug", "description", "is_active")
			wantRedirect(t, pageJSON(h, http.MethodPut, departments+"/"+id, `{"name":"Billing & Payments","slug":"billing","description":"Invoices","is_active":false}`), "update", departments)
			got, _ := f.esc.Store.GetDepartment(ctx, int64(billing["id"].(float64)))
			if got.Name != "Billing & Payments" || got.IsActive || got.Description == nil || *got.Description != "Invoices" {
				t.Errorf("department %+v", got)
			}

			tags := "/escalated/admin/tags"
			tagPage := visit(t, h, tags, uiAgent, "admin")
			tag := first(t, list(t, tagPage.Props, "tags"), "tags")
			hasKeys(t, "tag", tag, "id", "name", "color", "tickets_count")
			if tag["tickets_count"].(float64) != 1 {
				t.Errorf("tag %v", tag)
			}
			tagID := itoa(int64(tag["id"].(float64)))
			wantRedirect(t, pageJSON(h, http.MethodPut, tags+"/"+tagID, `{"name":"vip","color":"#00ff00"}`), "update tag", tags)
			wantRedirect(t, pageJSON(h, http.MethodPost, tags, `{"name":"urgent-care","color":"#123456"}`), "store tag", tags)
			wantPageErrors(t, pageJSON(h, http.MethodPost, tags, `{"name":""}`), "tag without a name", "Escalated/Admin/Tags/Index", "name")
			if updated, _ := f.esc.Store.GetTag(ctx, int64(tag["id"].(float64))); updated.Color == nil || *updated.Color != "#00ff00" {
				t.Errorf("tag color %v", updated.Color)
			}
		})
	}
}

func TestAdminAutomationPages(t *testing.T) {
	for _, name := range []string{"chi", "stdlib"} {
		t.Run(name, func(t *testing.T) {
			f := newUIFixture(t)
			h := f.mounts()[name]
			db := f.esc.Config.DB
			index := "/escalated/admin/automations"

			if form := visit(t, h, index+"/create", uiAgent, "admin"); form.Component != "Escalated/Admin/Automations/Form" || form.Props["automation"] != nil {
				t.Fatalf("create form %q %v", form.Component, form.Props["automation"])
			}
			// The editor's own field, operator and action names, and slugs.
			wantRedirect(t, pageJSON(h, http.MethodPost, index,
				`{"name":"Close stale guests","conditions":[{"field":"status","operator":"=","value":"open"},{"field":"assigned","operator":"=","value":"unassigned"}],"actions":[{"type":"change_status","value":"closed"},{"type":"set_ticket_type","value":"task"}],"active":true}`),
				"store", index)
			wantPageErrors(t, pageJSON(h, http.MethodPost, index, `{"name":"Nothing","conditions":[],"actions":[{"type":"add_note","value":"x"}]}`), "no conditions", "Escalated/Admin/Automations/Form", "conditions")

			page := visit(t, h, index, uiAgent, "admin")
			if page.Component != "Escalated/Admin/Automations/Index" {
				t.Fatalf("index component %q", page.Component)
			}
			automation := first(t, list(t, page.Props, "automations"), "automations")
			hasKeys(t, "automation", automation, "id", "name", "conditions", "actions", "active", "position", "last_run_at")
			id := itoa(int64(automation["id"].(float64)))
			edit := visit(t, h, index+"/"+id+"/edit", uiAgent, "admin")
			hasKeys(t, "edit automation", obj(t, edit.Props, "automation"), "id", "name", "conditions", "actions", "active")

			// Run it: the unassigned open guest ticket closes as a task; the
			// assigned fixture ticket does not match.
			runner := services.NewAutomationRunner(db, nil, f.esc.Access)
			if _, err := runner.Run(); err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			if other, _ := f.esc.Store.GetTicket(ctx, f.other.ID); other.Status != models.StatusClosed || other.TicketType != "task" {
				t.Errorf("guest ticket after the automation: status %d type %q", other.Status, other.TicketType)
			}
			if mine, _ := f.esc.Store.GetTicket(ctx, f.ticket.ID); mine.Status == models.StatusClosed {
				t.Error("the automation closed an assigned ticket")
			}

			wantRedirect(t, pageJSON(h, http.MethodPut, index+"/"+id, `{"name":"Close stale guests","conditions":[{"field":"status","operator":"=","value":"open"}],"actions":[{"type":"add_note","value":"x"}],"active":false}`), "update", index)
			wantRedirect(t, pageJSON(h, http.MethodDelete, index+"/"+id, ""), "destroy", index)
			if rec := sendAPI(h, http.MethodGet, index, uiAgent, "admin", ""); rec.Code != http.StatusOK {
				t.Errorf("API list: %d", rec.Code)
			}
		})
	}
}

func TestAdminCapacityAndPublicTicketsPages(t *testing.T) {
	for _, name := range []string{"chi", "stdlib"} {
		t.Run(name, func(t *testing.T) {
			f := newUIFixture(t)
			h := f.mounts()[name]
			db := f.esc.Config.DB
			if _, err := db.Exec(sqldialect.Rebind(db, `INSERT INTO escalated_agent_capacity (user_id, channel, max_concurrent, current_count) VALUES (?, ?, ?, ?)`), uiAgent, "default", 5, 2); err != nil {
				t.Fatal(err)
			}

			capacity := "/escalated/admin/capacity"
			page := visit(t, h, capacity, uiAgent, "admin")
			if page.Component != "Escalated/Admin/Capacity/Index" {
				t.Fatalf("component %q", page.Component)
			}
			row := first(t, list(t, page.Props, "capacities"), "capacities")
			hasKeys(t, "capacity", row, "id", "user_id", "agent_name", "channel", "max_concurrent", "current_count", "load_percentage")
			if row["agent_name"] != "Ada Agent" {
				t.Errorf("capacity %v", row)
			}
			id := itoa(int64(row["id"].(float64)))
			wantRedirect(t, pageJSON(h, http.MethodPut, capacity+"/"+id, `{"max_concurrent":7}`), "update", capacity)
			wantPageErrors(t, pageJSON(h, http.MethodPut, capacity+"/"+id, `{"max_concurrent":0}`), "zero", "Escalated/Admin/Capacity/Index", "max_concurrent")

			settings := "/escalated/admin/settings/public-tickets"
			sp := visit(t, h, settings, uiAgent, "admin")
			if sp.Component != "Escalated/Admin/Settings/PublicTickets" {
				t.Fatalf("settings component %q", sp.Component)
			}
			hasKeys(t, "settings", obj(t, sp.Props, "settings"), "guest_policy_mode", "guest_policy_user_id", "guest_policy_signup_url_template")
			wantPageErrors(t, pageJSON(h, http.MethodPut, settings, `{"guest_policy_mode":"guest_user","guest_policy_user_id":null}`), "guest_user without a user", "Escalated/Admin/Settings/PublicTickets", "guest_policy_user_id")
			wantRedirect(t, pageJSON(h, http.MethodPut, settings, `{"guest_policy_mode":"prompt_signup","guest_policy_signup_url_template":"https://example.com/signup?email={email}"}`), "update", settings)
			if mode, _ := f.esc.Store.GetSetting(context.Background(), "guest_policy_mode"); mode != "prompt_signup" {
				t.Errorf("mode %q", mode)
			}
			rec := sendAPI(h, http.MethodGet, settings, uiAgent, "admin", "")
			var out map[string]any
			if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &out) != nil || out["guest_policy_mode"] != "prompt_signup" {
				t.Errorf("API settings: %d %s", rec.Code, rec.Body.String())
			}
		})
	}
}
