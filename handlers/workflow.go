package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

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
}

// NewWorkflowHandler constructs the handler.
func NewWorkflowHandler(db *sql.DB) *WorkflowHandler {
	return &WorkflowHandler{DB: db}
}

// List handles GET /admin/workflows.
func (h *WorkflowHandler) List(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.QueryContext(
		r.Context(),
		`SELECT id, name, description, trigger_event, conditions, actions, position, is_active, stop_on_match, created_at, updated_at
		   FROM escalated_workflows
		  ORDER BY position ASC, id ASC`,
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
		wf.Conditions = json.RawMessage(conditions)
		wf.Actions = json.RawMessage(actions)
		wf.ComputeTrigger()
		out = append(out, wf)
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
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Name == "" || in.TriggerEvent == "" {
		http.Error(w, "name and trigger_event are required", http.StatusBadRequest)
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
	position := 0
	if in.Position != nil {
		position = *in.Position
	}

	res, err := h.DB.ExecContext(
		r.Context(),
		`INSERT INTO escalated_workflows
			(name, description, trigger_event, conditions, actions, position, is_active, stop_on_match, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		in.Name, in.Description, in.TriggerEvent,
		workflowDefaultConditions(in.Conditions), defaultJSONArray(in.Actions),
		position, isActive, stopOnMatch, time.Now(), time.Now(),
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
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
		args = append(args, *in.TriggerEvent)
	}
	if len(in.Conditions) > 0 {
		sets = append(sets, "conditions = ?")
		args = append(args, []byte(in.Conditions))
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
		writeJSON(w, http.StatusOK, map[string]any{"id": id})
		return
	}

	sets = append(sets, "updated_at = ?")
	args = append(args, time.Now())
	args = append(args, id)

	q := "UPDATE escalated_workflows SET " + joinSets(sets) + " WHERE id = ?"
	if _, err := h.DB.ExecContext(r.Context(), q, args...); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

// Delete handles DELETE /admin/workflows/{id}. Its execution logs are removed too.
func (h *WorkflowHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	if _, err := h.DB.ExecContext(
		r.Context(), `DELETE FROM escalated_workflow_logs WHERE workflow_id = ?`, id,
	); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if _, err := h.DB.ExecContext(
		r.Context(), `DELETE FROM escalated_workflows WHERE id = ?`, id,
	); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
func (h *WorkflowHandler) Logs(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	rows, err := h.DB.QueryContext(
		r.Context(),
		`SELECT id, workflow_id, ticket_id, trigger_event, status, actions_executed, error_message, created_at
		   FROM escalated_workflow_logs
		  WHERE workflow_id = ?
		  ORDER BY created_at DESC, id DESC
		  LIMIT 100`,
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
		lg.ActionsExecuted = json.RawMessage(actions)
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
func workflowDefaultConditions(raw json.RawMessage) []byte {
	if len(raw) == 0 {
		return []byte("{}")
	}
	return []byte(raw)
}
