package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/escalated-dev/escalated-go/ticketaccess"

	"github.com/escalated-dev/escalated-go/internal/sqldialect"
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

	// Pages serves Escalated/Admin/Automations/Index and Form and answers
	// their writes the Inertia way. Set by the router.
	Pages PageSupport
}

// automationProps is an automation as the pages read it.
func automationProps(a models.Automation) map[string]any {
	conditions, _ := uiJSON(a.Conditions).([]any)
	if conditions == nil {
		conditions = []any{}
	}
	actions, _ := uiJSON(a.Actions).([]any)
	if actions == nil {
		actions = []any{}
	}
	return map[string]any{
		"id":          a.ID,
		"name":        a.Name,
		"description": a.Description,
		"conditions":  conditions,
		"actions":     actions,
		"active":      a.Active,
		"position":    a.Position,
		"last_run_at": uiTime(a.LastRunAt),
		"created_at":  uiTimeValue(a.CreatedAt),
		"updated_at":  uiTimeValue(a.UpdatedAt),
	}
}

func (h *AutomationHandler) indexURL() string { return h.Pages.path("/admin/automations") }

// find loads one automation, or nil.
func (h *AutomationHandler) find(r *http.Request, id int64) (*models.Automation, error) {
	var a models.Automation
	var lastRun sql.NullTime
	err := ticketaccess.QueryRowContext(r.Context(), h.DB, sqldialect.Rebind(h.DB, `SELECT id, name, description, conditions, actions, active, position, last_run_at, created_at, updated_at
		   FROM escalated_automations WHERE id = ?`), id).Scan(
		&a.ID, &a.Name, &a.Description, &a.Conditions, &a.Actions, &a.Active, &a.Position, &lastRun, &a.CreatedAt, &a.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if lastRun.Valid {
		a.LastRunAt = &lastRun.Time
	}
	return &a, nil
}

func (h *AutomationHandler) renderForm(w http.ResponseWriter, r *http.Request, a *models.Automation, errs map[string]string) {
	props := map[string]any{"automation": nil}
	if a != nil {
		props["automation"] = automationProps(*a)
	}
	h.Pages.render(w, r, "Escalated/Admin/Automations/Form", props, errs)
}

// CreateForm renders Escalated/Admin/Automations/Form for a new automation
// (GET /admin/automations/create).
func (h *AutomationHandler) CreateForm(w http.ResponseWriter, r *http.Request) {
	h.renderForm(w, r, nil, nil)
}

// EditForm renders the form with the automation
// (GET /admin/automations/{id}/edit).
func (h *AutomationHandler) EditForm(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	a, err := h.find(r, id)
	if err != nil {
		http.Error(w, "could not load the automation", http.StatusInternalServerError)
		return
	}
	if a == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	h.renderForm(w, r, a, nil)
}

// automationErrors is Laravel's AutomationController validation for a page
// visit: a name, and at least one condition and one action.
func automationErrors(name *string, conditions, actions json.RawMessage, partial bool) map[string]string {
	errs := map[string]string{}
	if (name == nil && !partial) || (name != nil && strings.TrimSpace(*name) == "") {
		errs["name"] = requiredMessage("name")
	}
	nonEmpty := func(raw json.RawMessage) bool {
		var list []any
		return json.Unmarshal(raw, &list) == nil && len(list) > 0
	}
	if (len(conditions) > 0 || !partial) && !nonEmpty(conditions) {
		errs["conditions"] = "Add at least one condition."
	}
	if (len(actions) > 0 || !partial) && !nonEmpty(actions) {
		errs["actions"] = "Add at least one action."
	}
	return errs
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
	rows, err := ticketaccess.QueryContext(r.Context(), h.DB, sqldialect.Rebind(h.DB, `SELECT id, name, description, conditions, actions, active, position, last_run_at, created_at, updated_at
		   FROM escalated_automations
		  ORDER BY position ASC, id ASC`),
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

	if h.Pages.wantsPage(r) {
		rows := make([]map[string]any, 0, len(out))
		for _, a := range out {
			rows = append(rows, automationProps(a))
		}
		h.Pages.render(w, r, "Escalated/Admin/Automations/Index", map[string]any{"automations": rows}, nil)
		return
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
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	if isInertia(r) && h.Pages.Renderer != nil {
		if errs := automationErrors(&in.Name, in.Conditions, in.Actions, false); len(errs) > 0 {
			h.renderForm(w, r, nil, errs)
			return
		}
	}
	if in.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	conditions := defaultJSONArray(models.JSONText(in.Conditions))
	actions := defaultJSONArray(models.JSONText(in.Actions))
	active := true
	if in.Active != nil {
		active = *in.Active
	}
	// A new automation goes last, as Laravel's store() places it.
	position := 0
	var max sql.NullInt64
	if ticketaccess.QueryRowContext(r.Context(), h.DB, sqldialect.Rebind(h.DB, `SELECT MAX(position) FROM escalated_automations`)).Scan(&max) == nil && max.Valid {
		position = int(max.Int64) + 1
	}
	if in.Position != nil {
		position = *in.Position
	}

	res, err := ticketaccess.ExecInsertContext(r.Context(), h.DB, `INSERT INTO escalated_automations (name, description, conditions, actions, active, position, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		in.Name, in.Description, conditions, actions, active, position, time.Now(), time.Now(),
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()
	h.Pages.succeed(w, r, h.indexURL(), http.StatusCreated, map[string]any{"id": id})
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
	if isInertia(r) && h.Pages.Renderer != nil {
		if errs := automationErrors(in.Name, in.Conditions, in.Actions, r.Method == http.MethodPatch); len(errs) > 0 {
			a, _ := h.find(r, id)
			h.renderForm(w, r, a, errs)
			return
		}
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
		h.Pages.succeed(w, r, h.indexURL(), http.StatusOK, map[string]any{"id": id})
		return
	}

	sets = append(sets, "updated_at = ?")
	args = append(args, time.Now())
	args = append(args, id)

	q := "UPDATE escalated_automations SET " + joinSets(sets) + " WHERE id = ?"
	if _, err := ticketaccess.ExecContext(r.Context(), h.DB, sqldialect.Rebind(h.DB, q), args...); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.Pages.succeed(w, r, h.indexURL(), http.StatusOK, map[string]any{"id": id})
}

// Delete handles DELETE /admin/automations/{id}.
func (h *AutomationHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	if _, err := ticketaccess.ExecContext(r.Context(), h.DB, sqldialect.Rebind(h.DB, `DELETE FROM escalated_automations WHERE id = ?`),
		id,
	); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.Pages.succeed(w, r, h.indexURL(), http.StatusNoContent, nil)
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

func defaultJSONArray(raw models.JSONText) []byte {
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
