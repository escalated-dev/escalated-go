package router_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/escalated-dev/escalated-go/internal/sqldialect"
	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/services"
)

// The workflow pages against the workflow admin contract
// (escalated-developer-context domain-model/workflow-admin-contract.md):
// the builder's exact request body over HTTP, the stored workflow running on
// its trigger, and the form's option lists.

func TestWorkflowPagesFollowTheAdminContract(t *testing.T) {
	for _, name := range []string{"chi", "stdlib"} {
		t.Run(name, func(t *testing.T) {
			f := newUIFixture(t)
			h := f.mounts()[name]
			db := f.esc.Config.DB
			ctx := context.Background()
			index := "/escalated/admin/workflows"

			// 3. Form props.
			form := visit(t, h, index+"/create", uiAgent, "admin")
			if form.Component != "Escalated/Admin/Workflows/Form" {
				t.Fatalf("form component %q", form.Component)
			}
			hasKeys(t, "form props", form.Props, "workflow", "trigger_events", "action_types", "operators")
			if form.Props["workflow"] != nil {
				t.Errorf("workflow %v, want null when creating", form.Props["workflow"])
			}
			option := first(t, list(t, form.Props, "trigger_events"), "trigger_events")
			hasKeys(t, "trigger option", option, "value", "label")
			types := map[string]bool{}
			for _, o := range list(t, form.Props, "action_types") {
				types[o.(map[string]any)["value"].(string)] = true
			}
			for _, core := range []string{"change_status", "change_priority", "add_tag", "remove_tag", "set_department", "assign_agent", "add_note", "insert_canned_reply"} {
				if !types[core] {
					t.Errorf("action_types lacks %s", core)
				}
			}
			if types["send_webhook"] {
				t.Error("send_webhook is offered, but the runner skips it")
			}

			// 1. Create with the contract's example body.
			dept := *f.ticket.DepartmentID
			body := `{"name":"Route refunds to billing","description":null,"trigger_event":"ticket.created",` +
				`"conditions":{"all":[{"field":"subject","operator":"contains","value":"refund"}]},` +
				`"actions":[{"type":"change_priority","value":"high"},{"type":"set_department","value":"` + itoa(dept) + `"},{"type":"change_status","value":"in_progress"}],"is_active":true}`
			wantRedirect(t, pageJSON(h, http.MethodPost, index, body), "store", index)
			var id int64
			var trigger, conditions, actions string
			if err := db.QueryRow(sqldialect.Rebind(db, `SELECT id, trigger_event, conditions, actions FROM escalated_workflows WHERE name = ?`), "Route refunds to billing").
				Scan(&id, &trigger, &conditions, &actions); err != nil {
				t.Fatalf("stored workflow: %v", err)
			}
			if trigger != "ticket.created" || !strings.Contains(conditions, `"contains"`) || !strings.Contains(actions, `"change_priority"`) {
				t.Errorf("stored %s %s %s", trigger, conditions, actions)
			}

			// 2. Execution: a matching ticket gets the actions; another does not.
			tickets := services.NewTicketService(f.esc.Store)
			match, _ := tickets.Create(ctx, services.CreateTicketInput{Subject: "Please refund me", Description: "x", GuestName: ptr("R"), GuestEmail: ptr("r@example.com")})
			other, _ := tickets.Create(ctx, services.CreateTicketInput{Subject: "Password reset", Description: "x", GuestName: ptr("P"), GuestEmail: ptr("p@example.com")})
			runner := services.NewWorkflowRunner(db, nil, f.esc.Access)
			runner.RunForEvent("ticket.created", match)
			runner.RunForEvent("ticket.created", other)
			got, _ := f.esc.Store.GetTicket(ctx, match.ID)
			if got.Priority != models.PriorityHigh || got.DepartmentID == nil || *got.DepartmentID != dept || got.Status != models.StatusInProgress {
				t.Errorf("matching ticket after the workflow: priority %d department %v status %d", got.Priority, got.DepartmentID, got.Status)
			}
			if got, _ := f.esc.Store.GetTicket(ctx, other.ID); got.Priority == models.PriorityHigh {
				t.Error("the workflow ran on a ticket its conditions do not match")
			}

			// Index, edit, update, toggle, reorder, logs, delete.
			page := visit(t, h, index, uiAgent, "admin")
			if page.Component != "Escalated/Admin/Workflows/Index" {
				t.Fatalf("index component %q", page.Component)
			}
			wf := first(t, list(t, page.Props, "workflows"), "workflows")
			hasKeys(t, "workflow", wf, "id", "name", "description", "trigger_event", "conditions", "actions", "is_active", "position", "trigger_count", "last_triggered_at")
			if wf["trigger_count"].(float64) != 1 || wf["last_triggered_at"] == nil {
				t.Errorf("workflow runs %v / %v", wf["trigger_count"], wf["last_triggered_at"])
			}
			hasKeys(t, "conditions", obj(t, wf, "conditions"), "all")

			edit := visit(t, h, index+"/"+itoa(id), uiAgent, "admin")
			if edit.Component != "Escalated/Admin/Workflows/Form" {
				t.Fatalf("edit component %q", edit.Component)
			}
			hasKeys(t, "edit workflow", obj(t, edit.Props, "workflow"), "id", "name", "trigger_event", "conditions", "actions", "is_active")

			wantRedirect(t, pageJSON(h, http.MethodPut, index+"/"+itoa(id), strings.Replace(body, "Route refunds to billing", "Refunds", 1)), "update", index)
			wantPageErrors(t, pageJSON(h, http.MethodPost, index, `{"name":"No actions","trigger_event":"ticket.created","actions":[]}`), "no actions", "Escalated/Admin/Workflows/Form", "actions")
			wantPageErrors(t, pageJSON(h, http.MethodPost, index, `{"name":"","trigger_event":"","actions":[{"type":"add_note","value":"x"}]}`), "no name", "Escalated/Admin/Workflows/Form", "name")
			wantPageErrors(t, pageJSON(h, http.MethodPut, index+"/"+itoa(id), `{"name":"x","trigger_event":"ticket.created","actions":[{"type":"explode","value":"x"}]}`), "unknown action", "Escalated/Admin/Workflows/Form", "actions.0.type")

			wantRedirect(t, pageJSON(h, http.MethodPost, index+"/"+itoa(id)+"/toggle", ""), "toggle", index)
			var active bool
			_ = db.QueryRow(sqldialect.Rebind(db, `SELECT is_active FROM escalated_workflows WHERE id = ?`), id).Scan(&active)
			if active {
				t.Error("toggle left the workflow active")
			}

			wantRedirect(t, pageJSON(h, http.MethodPost, index, `{"name":"Second","trigger_event":"reply.created","actions":[{"type":"add_note","value":"seen"}]}`), "store second", index)
			var second int64
			_ = db.QueryRow(sqldialect.Rebind(db, `SELECT id FROM escalated_workflows WHERE name = ?`), "Second").Scan(&second)
			wantRedirect(t, pageJSON(h, http.MethodPost, index+"/reorder", `{"workflow_ids":[`+itoa(second)+`,`+itoa(id)+`]}`), "reorder", index)
			var pos int
			_ = db.QueryRow(sqldialect.Rebind(db, `SELECT position FROM escalated_workflows WHERE id = ?`), second).Scan(&pos)
			if pos != 0 {
				t.Errorf("second workflow at position %d, want 0", pos)
			}

			logs := visit(t, h, index+"/logs?workflow="+itoa(id), uiAgent, "admin")
			if logs.Component != "Escalated/Admin/Workflows/Logs" {
				t.Fatalf("logs component %q", logs.Component)
			}
			hasKeys(t, "logs props", logs.Props, "logs", "workflows")
			entries := list(t, logs.Props, "logs")
			if len(entries) != 2 {
				t.Fatalf("logs %d, want the 2 runs", len(entries))
			}
			for _, e := range entries {
				hasKeys(t, "log", e.(map[string]any), "id", "event", "matched", "status", "workflow_name", "ticket_reference", "action_details", "actions_executed", "created_at")
			}
			hasKeys(t, "workflow option", first(t, list(t, logs.Props, "workflows"), "workflows"), "id", "name")
			if rec := send(h, http.MethodGet, index+"/"+itoa(id)+"/logs", uiAgent, "admin", ""); rec.Code != http.StatusFound || !strings.HasSuffix(rec.Header().Get("Location"), "/admin/workflows/logs?workflow="+itoa(id)) {
				t.Errorf("per-workflow logs page: %d %q", rec.Code, rec.Header().Get("Location"))
			}

			wantRedirect(t, pageJSON(h, http.MethodDelete, index+"/"+itoa(second), ""), "destroy", index)

			// API clients keep the JSON list and may still save without actions.
			rec := sendAPI(h, http.MethodGet, index, uiAgent, "admin", "")
			var out map[string]any
			if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &out) != nil || out["workflows"] == nil {
				t.Errorf("API list: %d %s", rec.Code, rec.Body.String())
			}
		})
	}
}

// The contract's other shapes: a slug status, a tags condition, and a
// workflow stored with a flat condition list.
func TestWorkflowRunnerReadsTheBuildersValues(t *testing.T) {
	f := newUIFixture(t)
	db := f.esc.Config.DB
	ctx := context.Background()
	insert := func(name, conditions, actions string) {
		t.Helper()
		if _, err := sqldialect.ExecInsert(db, `INSERT INTO escalated_workflows (name, trigger_event, conditions, actions, is_active, stop_on_match, position) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			name, "ticket.updated", conditions, actions, true, false, 0); err != nil {
			t.Fatal(err)
		}
	}
	insert("vip resolves", `{"all":[{"field":"tags","operator":"equals","value":"vip"},{"field":"hours_since_created","operator":"less_than","value":"1"}]}`,
		`[{"type":"change_status","value":"resolved"},{"type":"insert_canned_reply","value":"Resolved for {{reference}}"}]`)
	insert("legacy list", `[{"field":"priority","operator":"equals","value":"urgent"}]`, `[{"type":"add_note","value":"legacy matched"}]`)

	runner := services.NewWorkflowRunner(db, nil, f.esc.Access)
	ticket, _ := f.esc.Store.GetTicket(ctx, f.ticket.ID)
	runner.RunForEvent("ticket.updated", ticket)

	got, _ := f.esc.Store.GetTicket(ctx, f.ticket.ID)
	if got.Status != models.StatusResolved {
		t.Errorf("status %d, want resolved (the builder sends the slug)", got.Status)
	}
	if f.count(t, "replies", "ticket_id = ? AND body = ? AND is_internal = ?", f.ticket.ID, "Resolved for "+f.ticket.Reference, false) != 1 {
		t.Error("insert_canned_reply did not post the reply")
	}
	if f.count(t, "replies", "ticket_id = ? AND body = ?", f.ticket.ID, "legacy matched") != 1 {
		t.Error("a flat condition list was not treated as all")
	}
}
