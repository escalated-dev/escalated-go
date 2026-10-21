package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/escalated-dev/escalated-go/internal/sqldialect"
	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/services"
	"github.com/escalated-dev/escalated-go/ticketaccess"
)

// The workflow pages and the writes the shared builder makes, per the
// workflow admin contract (escalated-developer-context
// domain-model/workflow-admin-contract.md).

// workflowActionTypes are the actions the builder offers: the contract's core
// catalog plus the optional ones the runner carries out. send_webhook is left
// out: the runner skips it in favour of the outbound webhooks.
var workflowActionTypes = []option{
	{"change_status", "Change Status"},
	{"change_priority", "Change Priority"},
	{"add_tag", "Add Tag"},
	{"remove_tag", "Remove Tag"},
	{"set_department", "Set Department"},
	{"assign_agent", "Assign Agent"},
	{"add_note", "Add Internal Note"},
	{"insert_canned_reply", "Send Canned Reply"},
	{"add_follower", "Add Follower"},
	{"delay", "Delay (minutes)"},
}

// acceptedWorkflowActions are every action type a stored workflow may hold:
// the offered ones plus the aliases and extras the runner still runs.
var acceptedWorkflowActions = map[string]bool{
	"add_internal_note": true, "set_type": true, "send_notification": true, "send_webhook": true,
}

func init() {
	for _, o := range workflowActionTypes {
		acceptedWorkflowActions[o.Value] = true
	}
}

// workflowOperators are the operators the engine evaluates.
var workflowOperators = []option{
	{"equals", "Equals"},
	{"not_equals", "Does Not Equal"},
	{"contains", "Contains"},
	{"not_contains", "Does Not Contain"},
	{"starts_with", "Starts With"},
	{"ends_with", "Ends With"},
	{"greater_than", "Greater Than"},
	{"less_than", "Less Than"},
	{"greater_or_equal", "Greater Than or Equal"},
	{"less_or_equal", "Less Than or Equal"},
	{"is_empty", "Is Empty"},
	{"is_not_empty", "Is Not Empty"},
}

// option is one entry of a page's option list: the contract's preferred
// {value, label}.
type option struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// triggerEventOptions are the events this package fires, labelled.
func triggerEventOptions() []option {
	out := make([]option, 0, len(services.TriggerEvents))
	for _, event := range services.TriggerEvents {
		label := strings.NewReplacer(".", " ", "_", " ").Replace(event)
		if label != "" {
			label = strings.ToUpper(label[:1]) + label[1:]
		}
		out = append(out, option{Value: event, Label: label})
	}
	return out
}

// workflowProps is a workflow as the pages read it (the contract's workflow
// object), with its run count and last run from the logs.
func workflowProps(wf models.Workflow, runs int, lastRun *time.Time) map[string]any {
	conditions := uiJSON(wf.Conditions)
	if list, ok := conditions.([]any); ok {
		conditions = map[string]any{"all": list}
	}
	if conditions == nil {
		conditions = map[string]any{"all": []any{}}
	}
	actions, _ := uiJSON(wf.Actions).([]any)
	if actions == nil {
		actions = []any{}
	}
	return map[string]any{
		"id":                wf.ID,
		"name":              wf.Name,
		"description":       wf.Description,
		"trigger_event":     wf.TriggerEvent,
		"trigger":           wf.TriggerEvent,
		"conditions":        conditions,
		"actions":           actions,
		"is_active":         wf.IsActive,
		"position":          wf.Position,
		"stop_on_match":     wf.StopOnMatch,
		"trigger_count":     runs,
		"last_triggered_at": uiTime(lastRun),
		"created_at":        uiTimeValue(wf.CreatedAt),
		"updated_at":        uiTimeValue(wf.UpdatedAt),
	}
}

// runStats is each workflow's matched runs and its last one.
func (h *WorkflowHandler) runStats(r *http.Request) (map[int64]int, map[int64]*time.Time) {
	counts := map[int64]int{}
	last := map[int64]*time.Time{}
	rows, err := ticketaccess.QueryContext(r.Context(), h.DB, sqldialect.Rebind(h.DB,
		`SELECT workflow_id, created_at FROM escalated_workflow_logs WHERE status <> ?`), "skipped")
	if err != nil {
		return counts, last
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id int64
		var at time.Time
		if rows.Scan(&id, &at) != nil {
			continue
		}
		counts[id]++
		if prev := last[id]; prev == nil || at.After(*prev) {
			t := at
			last[id] = &t
		}
	}
	return counts, last
}

func (h *WorkflowHandler) renderIndex(w http.ResponseWriter, r *http.Request, workflows []models.Workflow) {
	counts, last := h.runStats(r)
	rows := make([]map[string]any, 0, len(workflows))
	for _, wf := range workflows {
		rows = append(rows, workflowProps(wf, counts[wf.ID], last[wf.ID]))
	}
	h.Pages.render(w, r, "Escalated/Admin/Workflows/Index", map[string]any{"workflows": rows}, nil)
}

// find loads one workflow, or nil.
func (h *WorkflowHandler) find(r *http.Request, id int64) (*models.Workflow, error) {
	var wf models.Workflow
	var desc sql.NullString
	var conditions, actions []byte
	err := ticketaccess.QueryRowContext(r.Context(), h.DB, sqldialect.Rebind(h.DB, `SELECT id, name, description, trigger_event, conditions, actions, position, is_active, stop_on_match, created_at, updated_at
		   FROM escalated_workflows WHERE id = ?`), id).Scan(
		&wf.ID, &wf.Name, &desc, &wf.TriggerEvent, &conditions, &actions,
		&wf.Position, &wf.IsActive, &wf.StopOnMatch, &wf.CreatedAt, &wf.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if desc.Valid {
		wf.Description = &desc.String
	}
	wf.Conditions = models.JSONText(conditions)
	wf.Actions = models.JSONText(actions)
	wf.ComputeTrigger()
	return &wf, nil
}

func (h *WorkflowHandler) renderForm(w http.ResponseWriter, r *http.Request, wf *models.Workflow, errs map[string]string) {
	var workflow any
	if wf != nil {
		workflow = workflowProps(*wf, 0, nil)
	}
	h.Pages.render(w, r, "Escalated/Admin/Workflows/Form", map[string]any{
		"workflow":       workflow,
		"trigger_events": triggerEventOptions(),
		"action_types":   workflowActionTypes,
		"operators":      workflowOperators,
	}, errs)
}

// CreateForm renders Escalated/Admin/Workflows/Form for a new workflow
// (GET /admin/workflows/create).
func (h *WorkflowHandler) CreateForm(w http.ResponseWriter, r *http.Request) {
	h.renderForm(w, r, nil, nil)
}

// Edit handles GET /admin/workflows/{id} (escalated.admin.workflows.edit): the
// form with the workflow for a page visit, the workflow as JSON otherwise.
func (h *WorkflowHandler) Edit(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	wf, err := h.find(r, id)
	if err != nil {
		http.Error(w, "could not load the workflow", http.StatusInternalServerError)
		return
	}
	if wf == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if h.Pages.wantsPage(r) {
		h.renderForm(w, r, wf, nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"workflow": wf})
}

// validateWorkflow checks what the contract requires: a name, a trigger_event
// and at least one action of a type the runner handles. partial skips the
// presence checks for fields that were not sent (a PATCH). requireActions is
// false for API clients, which could always save a workflow without actions.
func validateWorkflow(name, trigger *string, actions json.RawMessage, partial, requireActions bool) map[string]string {
	errs := map[string]string{}
	if (name == nil && !partial) || (name != nil && strings.TrimSpace(*name) == "") {
		errs["name"] = requiredMessage("name")
	}
	if (trigger == nil && !partial) || (trigger != nil && strings.TrimSpace(*trigger) == "") {
		errs["trigger_event"] = requiredMessage("trigger event")
	}
	if len(actions) == 0 || string(actions) == "null" {
		if !partial && requireActions {
			errs["actions"] = "Add at least one action."
		}
		return errs
	}
	var list []struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(actions, &list) != nil {
		errs["actions"] = "The actions must be a list."
		return errs
	}
	if len(list) == 0 && requireActions {
		errs["actions"] = "Add at least one action."
	}
	for i, a := range list {
		if !acceptedWorkflowActions[a.Type] {
			errs["actions."+strconv.Itoa(i)+".type"] = "The selected action type is invalid."
		}
	}
	return errs
}

// Toggle handles POST /admin/workflows/{id}/toggle: flips is_active.
func (h *WorkflowHandler) Toggle(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	wf, err := h.find(r, id)
	if err != nil {
		http.Error(w, "could not load the workflow", http.StatusInternalServerError)
		return
	}
	if wf == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if _, err := ticketaccess.ExecContext(r.Context(), h.DB, sqldialect.Rebind(h.DB,
		`UPDATE escalated_workflows SET is_active = ?, updated_at = ? WHERE id = ?`), !wf.IsActive, time.Now(), id); err != nil {
		http.Error(w, "could not update the workflow", http.StatusInternalServerError)
		return
	}
	h.Pages.succeed(w, r, h.Pages.path("/admin/workflows"), http.StatusOK, map[string]any{"id": id, "is_active": !wf.IsActive})
}

// Reorder handles POST /admin/workflows/reorder with workflow_ids (or the
// older ids) in their new order.
func (h *WorkflowHandler) Reorder(w http.ResponseWriter, r *http.Request) {
	var in struct {
		WorkflowIDs []int64 `json:"workflow_ids"`
		IDs         []int64 `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid input", http.StatusBadRequest)
		return
	}
	ids := in.WorkflowIDs
	if ids == nil {
		ids = in.IDs
	}
	if len(ids) == 0 {
		h.Pages.fail(w, r, http.StatusUnprocessableEntity, "workflow_ids is required", map[string]string{"workflow_ids": requiredMessage("workflow ids")}, func(map[string]string) {
			seeOther(w, r, h.Pages.path("/admin/workflows"))
		})
		return
	}
	for position, id := range ids {
		if _, err := ticketaccess.ExecContext(r.Context(), h.DB, sqldialect.Rebind(h.DB,
			`UPDATE escalated_workflows SET position = ? WHERE id = ?`), position, id); err != nil {
			http.Error(w, "could not reorder", http.StatusInternalServerError)
			return
		}
	}
	h.Pages.succeed(w, r, h.Pages.path("/admin/workflows"), http.StatusOK, map[string]any{"success": true})
}

// LogsPage handles GET /admin/workflows/logs (escalated.admin.workflows.logs):
// recent runs across every workflow, or one with ?workflow=, and the
// workflows for the page's filter.
func (h *WorkflowHandler) LogsPage(w http.ResponseWriter, r *http.Request) {
	where := ""
	var args []any
	if v, err := strconv.ParseInt(r.URL.Query().Get("workflow"), 10, 64); err == nil && v > 0 {
		where = " WHERE l.workflow_id = ?"
		args = append(args, v)
	}
	rows, err := ticketaccess.QueryContext(r.Context(), h.DB, sqldialect.Rebind(h.DB, `SELECT l.id, l.workflow_id, l.ticket_id, l.trigger_event, l.status, l.actions_executed, l.error_message, l.created_at,
			COALESCE(w.name, ''), COALESCE(t.reference, '')
		   FROM escalated_workflow_logs l
		   LEFT JOIN escalated_workflows w ON w.id = l.workflow_id
		   LEFT JOIN escalated_tickets t ON t.id = l.ticket_id`+where+`
		  ORDER BY l.created_at DESC, l.id DESC
		  LIMIT 100`), args...)
	if err != nil {
		http.Error(w, "could not list the runs", http.StatusInternalServerError)
		return
	}
	logs := []map[string]any{}
	for rows.Next() {
		var id, workflowID, ticketID int64
		var event, status, name, reference string
		var actions []byte
		var errMsg sql.NullString
		var at time.Time
		if err := rows.Scan(&id, &workflowID, &ticketID, &event, &status, &actions, &errMsg, &at, &name, &reference); err != nil {
			_ = rows.Close()
			http.Error(w, "could not list the runs", http.StatusInternalServerError)
			return
		}
		details, _ := uiJSON(models.JSONText(actions)).([]any)
		if details == nil {
			details = []any{}
		}
		var message any
		if errMsg.Valid {
			message = errMsg.String
		}
		logs = append(logs, map[string]any{
			"id":               id,
			"workflow_id":      workflowID,
			"workflow_name":    name,
			"ticket_id":        ticketID,
			"ticket_reference": reference,
			"event":            event,
			"trigger_event":    event,
			"matched":          status != "skipped",
			"status":           status,
			"action_details":   details,
			"actions_executed": len(details),
			"error_message":    message,
			"duration_ms":      nil,
			"created_at":       uiTimeValue(at),
		})
	}
	_ = rows.Close()

	workflows := []map[string]any{}
	list, err := ticketaccess.QueryContext(r.Context(), h.DB, sqldialect.Rebind(h.DB,
		`SELECT id, name FROM escalated_workflows ORDER BY position ASC, id ASC`))
	if err == nil {
		for list.Next() {
			var id int64
			var name string
			if list.Scan(&id, &name) == nil {
				workflows = append(workflows, map[string]any{"id": id, "name": name})
			}
		}
		_ = list.Close()
	}
	h.Pages.render(w, r, "Escalated/Admin/Workflows/Logs", map[string]any{"logs": logs, "workflows": workflows}, nil)
}
