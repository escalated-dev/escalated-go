package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/services"
)

// AutomationHandler exposes admin CRUD over time-based Automation rows
// plus a manual `run` trigger.
//
// Distinct from WorkflowHandler (event-driven) and MacroHandler (agent
// manual). See escalated-developer-context/domain-model/
// workflows-automations-macros.md.
//
// Today this handler talks to *sql.DB directly to stay close to the
// AutomationRunner's surface. A follow-up will move CRUD onto a
// store.Store-style interface for parity with the rest of the
// admin/agent handlers.
type AutomationHandler struct {
	DB     *sql.DB
	Runner *services.AutomationRunner
}

// NewAutomationHandler constructs the handler. Pass in the same *sql.DB
// the runner uses.
func NewAutomationHandler(db *sql.DB, runner *services.AutomationRunner) *AutomationHandler {
	if runner == nil {
		runner = services.NewAutomationRunner(db, nil)
	}
	return &AutomationHandler{DB: db, Runner: runner}
}

// List handles GET /admin/automations.
func (h *AutomationHandler) List(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.QueryContext(
		r.Context(),
		`SELECT id, name, description, conditions, actions, active, position, last_run_at, created_at, updated_at
		   FROM escalated_automations
		  ORDER BY position ASC, id ASC`,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	out := []models.Automation{}
	for rows.Next() {
		var a models.Automation
		var lastRun sql.NullTime
		if err := rows.Scan(
			&a.ID, &a.Name, &a.Description, &a.Conditions, &a.Actions,
			&a.Active, &a.Position, &lastRun, &a.CreatedAt, &a.UpdatedAt,
		); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if lastRun.Valid {
			a.LastRunAt = &lastRun.Time
		}
		out = append(out, a)
	}

	writeJSON(w, http.StatusOK, map[string]any{"automations": out})
}

// Create handles POST /admin/automations.
func (h *AutomationHandler) Create(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name        string          `json:"name"`
		Description *string         `json:"description"`
		Conditions  json.RawMessage `json:"conditions"`
		Actions     json.RawMessage `json:"actions"`
		Active      *bool           `json:"active"`
		Position    *int            `json:"position"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	conditions := defaultJSONArray(in.Conditions)
	actions := defaultJSONArray(in.Actions)
	active := true
	if in.Active != nil {
		active = *in.Active
	}
	position := 0
	if in.Position != nil {
		position = *in.Position
	}

	res, err := h.DB.ExecContext(
		r.Context(),
		`INSERT INTO escalated_automations (name, description, conditions, actions, active, position, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		in.Name, in.Description, conditions, actions, active, position, time.Now(), time.Now(),
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

// Update handles PATCH /admin/automations/{id}.
func (h *AutomationHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	var in struct {
		Name        *string         `json:"name"`
		Description *string         `json:"description"`
		Conditions  json.RawMessage `json:"conditions"`
		Actions     json.RawMessage `json:"actions"`
		Active      *bool           `json:"active"`
		Position    *int            `json:"position"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	// Build dynamic UPDATE — only set fields that were supplied. Keeps
	// PATCH semantics correct (omitted fields stay as-is).
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
	if len(in.Conditions) > 0 {
		sets = append(sets, "conditions = ?")
		args = append(args, []byte(in.Conditions))
	}
	if len(in.Actions) > 0 {
		sets = append(sets, "actions = ?")
		args = append(args, []byte(in.Actions))
	}
	if in.Active != nil {
		sets = append(sets, "active = ?")
		args = append(args, *in.Active)
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

	q := "UPDATE escalated_automations SET " + joinSets(sets) + " WHERE id = ?"
	if _, err := h.DB.ExecContext(r.Context(), q, args...); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

// Delete handles DELETE /admin/automations/{id}.
func (h *AutomationHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	if _, err := h.DB.ExecContext(
		r.Context(),
		`DELETE FROM escalated_automations WHERE id = ?`,
		id,
	); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Run handles POST /admin/automations/run — manual trigger of the runner
// for admin smoke-testing.
func (h *AutomationHandler) Run(w http.ResponseWriter, _ *http.Request) {
	affected, err := h.Runner.Run()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"affected": affected})
}

func defaultJSONArray(raw json.RawMessage) []byte {
	if len(raw) == 0 {
		return []byte("[]")
	}
	return []byte(raw)
}

func joinSets(sets []string) string {
	out := ""
	for i, s := range sets {
		if i > 0 {
			out += ", "
		}
		out += s
	}
	return out
}
