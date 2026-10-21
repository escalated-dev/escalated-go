package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/escalated-dev/escalated-go/ticketaccess"

	"github.com/escalated-dev/escalated-go/internal/sqldialect"
	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/services"
)

// WorkflowHandler exposes admin CRUD over event-driven Workflow rows plus a
// read-only per-workflow execution log.
//
// Distinct from AutomationHandler (admin time-based) and MacroHandler (agent
// manual). See escalated-developer-context/domain-model/
// workflows-automations-macros.md.
//
// Like the sibling engine handlers it talks to *sql.DB directly. It manages
// workflow definitions only; the wired services.WorkflowRunner is what actually
// fires them on the ticket lifecycle and writes the escalated_workflow_logs
// rows surfaced by Logs.
type WorkflowHandler struct {
	DB *sql.DB

	// Pages serves Escalated/Admin/Workflows/Index, Form and Logs and answers
	// the builder's writes the Inertia way. Set by the router.
	Pages PageSupport
}

// NewWorkflowHandler constructs the handler.
func NewWorkflowHandler(db *sql.DB) *WorkflowHandler {
	return &WorkflowHandler{DB: db}
}

// List handles GET /admin/workflows.
func (h *WorkflowHandler) List(w http.ResponseWriter, r *http.Request) {
	rows, err := ticketaccess.QueryContext(r.Context(), h.DB, sqldialect.Rebind(h.DB, `SELECT id, name, description, trigger_event, conditions, actions, position, is_active, stop_on_match, created_at, updated_at
		   FROM escalated_workflows
		  ORDER BY position ASC, id ASC`),
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	out := []models.Workflow{}
	for rows.Next() {
		var wf models.Workflow
		var desc sql.NullString
		// Scan JSON columns through []byte so a string driver value (SQLite TEXT)
		// as well as a []byte one (Postgres) both work.
		var conditions, actions []byte
		if err := rows.Scan(
			&wf.ID, &wf.Name, &desc, &wf.TriggerEvent, &conditions, &actions,
			&wf.Position, &wf.IsActive, &wf.StopOnMatch, &wf.CreatedAt, &wf.UpdatedAt,
		); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if desc.Valid {
			wf.Description = &desc.String
		}
		wf.Conditions = models.JSONText(conditions)
		wf.Actions = models.JSONText(actions)
		wf.ComputeTrigger()
		out = append(out, wf)
	}

	if h.Pages.wantsPage(r) {
		h.renderIndex(w, r, out)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"workflows":        out,
		"available_events": services.TriggerEvents,
	})
}

// Create handles POST /admin/workflows.
func (h *WorkflowHandler) Create(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name         string          `json:"name"`
		Description  *string         `json:"description"`
		TriggerEvent string          `json:"trigger_event"`
		Conditions   json.RawMessage `json:"conditions"`
		Actions      json.RawMessage `json:"actions"`
		IsActive     *bool           `json:"is_active"`
		StopOnMatch  *bool           `json:"stop_on_match"`
		Position     *int            `json:"position"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "name and trigger_event are required", http.StatusBadRequest)
		return
	}
	if errs := validateWorkflow(&in.Name, &in.TriggerEvent, in.Actions, false, isInertia(r)); len(errs) > 0 {
		if in.Name == "" || in.TriggerEvent == "" {
			h.Pages.fail(w, r, http.StatusBadRequest, "name and trigger_event are required", errs, func(errs map[string]string) { h.renderForm(w, r, nil, errs) })
			return
		}
		if isInertia(r) && h.Pages.Renderer != nil {
			h.renderForm(w, r, nil, errs)
			return
		}
		validationErrors(w, r, errs, nil)
		return
	}

	isActive := true
	if in.IsActive != nil {
		isActive = *in.IsActive
	}
	stopOnMatch := false
	if in.StopOnMatch != nil {
		stopOnMatch = *in.StopOnMatch
	}
	// A new workflow goes last, as Laravel's store() places it.
	position := h.nextPosition(r)
	if in.Position != nil {
		position = *in.Position
	}

	res, err := ticketaccess.ExecInsertContext(r.Context(), h.DB, `INSERT INTO escalated_workflows
			(name, description, trigger_event, conditions, actions, position, is_active, stop_on_match, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		in.Name, in.Description, models.CanonicalEventName(in.TriggerEvent),
		workflowDefaultConditions(models.JSONText(in.Conditions)), defaultJSONArray(models.JSONText(in.Actions)),
		position, isActive, stopOnMatch, time.Now(), time.Now(),
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()
	h.Pages.succeed(w, r, h.Pages.path("/admin/workflows"), http.StatusCreated, map[string]any{"id": id})
}

// nextPosition is one past the highest position.
func (h *WorkflowHandler) nextPosition(r *http.Request) int {
	var max sql.NullInt64
	if err := ticketaccess.QueryRowContext(r.Context(), h.DB, sqldialect.Rebind(h.DB,
		`SELECT MAX(position) FROM escalated_workflows`)).Scan(&max); err != nil || !max.Valid {
		return 0
	}
	return int(max.Int64) + 1
}

// Update handles PATCH /admin/workflows/{id}. Only supplied fields are changed.
func (h *WorkflowHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	var in struct {
		Name         *string         `json:"name"`
		Description  *string         `json:"description"`
		TriggerEvent *string         `json:"trigger_event"`
		Conditions   json.RawMessage `json:"conditions"`
		Actions      json.RawMessage `json:"actions"`
		IsActive     *bool           `json:"is_active"`
		StopOnMatch  *bool           `json:"stop_on_match"`
		Position     *int            `json:"position"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	// PUT is the builder's full save (contract); PATCH may send some fields.
	if errs := validateWorkflow(in.Name, in.TriggerEvent, in.Actions, r.Method == http.MethodPatch || !isInertia(r), isInertia(r)); len(errs) > 0 {
		if isInertia(r) && h.Pages.Renderer != nil {
			wf, _ := h.find(r, id)
			h.renderForm(w, r, wf, errs)
			return
		}
		validationErrors(w, r, errs, nil)
		return
	}

	sets := []string{}
	args := []any{}

	if in.Name != nil {
		sets = append(sets, "name = ?")
		args = append(args, *in.Name)
	}
	if in.Description != nil {
		sets = append(sets, "description = ?")
		args = append(args, *in.Description)
	}
	if in.TriggerEvent != nil {
		sets = append(sets, "trigger_event = ?")
		args = append(args, models.CanonicalEventName(*in.TriggerEvent))
	}
	if len(in.Conditions) > 0 {
		sets = append(sets, "conditions = ?")
		args = append(args, workflowDefaultConditions(models.JSONText(in.Conditions)))
	}
	if len(in.Actions) > 0 {
		sets = append(sets, "actions = ?")
		args = append(args, []byte(in.Actions))
	}
	if in.IsActive != nil {
		sets = append(sets, "is_active = ?")
		args = append(args, *in.IsActive)
	}
	if in.StopOnMatch != nil {
		sets = append(sets, "stop_on_match = ?")
		args = append(args, *in.StopOnMatch)
	}
	if in.Position != nil {
		sets = append(sets, "position = ?")
		args = append(args, *in.Position)
	}

	if len(sets) == 0 {
		h.Pages.succeed(w, r, h.Pages.path("/admin/workflows"), http.StatusOK, map[string]any{"id": id})
		return
	}

	sets = append(sets, "updated_at = ?")
	args = append(args, time.Now())
	args = append(args, id)

	q := "UPDATE escalated_workflows SET " + joinSets(sets) + " WHERE id = ?"
	if _, err := ticketaccess.ExecContext(r.Context(), h.DB, sqldialect.Rebind(h.DB, q), args...); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.Pages.succeed(w, r, h.Pages.path("/admin/workflows"), http.StatusOK, map[string]any{"id": id})
}

// Delete handles DELETE /admin/workflows/{id}. Its execution logs are removed too.
func (h *WorkflowHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	if _, err := ticketaccess.ExecContext(r.Context(), h.DB, sqldialect.Rebind(h.DB, `DELETE FROM escalated_workflow_logs WHERE workflow_id = ?`), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if _, err := ticketaccess.ExecContext(r.Context(), h.DB, sqldialect.Rebind(h.DB, `DELETE FROM escalated_workflows WHERE id = ?`), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.Pages.succeed(w, r, h.Pages.path("/admin/workflows"), http.StatusNoContent, nil)
}

// workflowLogRow is the serialized shape of one escalated_workflow_logs row.
// Kept local to the handler because the aspirational models.WorkflowLog struct
// predates the live table shape (which stores a status string, not per-phase
// timestamps).
type workflowLogRow struct {
	ID              int64           `json:"id"`
	WorkflowID      int64           `json:"workflow_id"`
	TicketID        int64           `json:"ticket_id"`
	TriggerEvent    string          `json:"trigger_event"`
	Status          string          `json:"status"`
	ActionsExecuted json.RawMessage `json:"actions_executed"`
	ErrorMessage    *string         `json:"error_message,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
}

// Logs handles GET /admin/workflows/{id}/logs — the workflow's run history.
// A page visit goes to the shared Logs page filtered to the workflow, as
// Laravel's workflowLogs() does.
func (h *WorkflowHandler) Logs(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if h.Pages.wantsPage(r) {
		http.Redirect(w, r, h.Pages.path("/admin/workflows/logs?workflow="+strconv.FormatInt(id, 10)), http.StatusFound)
		return
	}

	rows, err := ticketaccess.QueryContext(r.Context(), h.DB, sqldialect.Rebind(h.DB, `SELECT id, workflow_id, ticket_id, trigger_event, status, actions_executed, error_message, created_at
		   FROM escalated_workflow_logs
		  WHERE workflow_id = ?
		  ORDER BY created_at DESC, id DESC
		  LIMIT 100`),
		id,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	out := []workflowLogRow{}
	for rows.Next() {
		var lg workflowLogRow
		var actions []byte
		var errMsg sql.NullString
		if err := rows.Scan(
			&lg.ID, &lg.WorkflowID, &lg.TicketID, &lg.TriggerEvent, &lg.Status,
			&actions, &errMsg, &lg.CreatedAt,
		); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		lg.ActionsExecuted = actions
		if errMsg.Valid {
			lg.ErrorMessage = &errMsg.String
		}
		out = append(out, lg)
	}

	writeJSON(w, http.StatusOK, map[string]any{"logs": out})
}

// workflowDefaultConditions returns a "{}" RawMessage when raw is empty so the
// conditions column is never NULL. Workflow conditions are a JSON object
// ({all|any:[…]}), unlike the JSON-array actions handled by defaultJSONArray.
func workflowDefaultConditions(raw models.JSONText) []byte {
	if len(raw) == 0 {
		return []byte("{}")
	}
	return []byte(raw)
}
