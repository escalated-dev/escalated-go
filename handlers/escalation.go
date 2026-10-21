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

// EscalationHandler exposes admin CRUD over time-based EscalationRule rows
// plus a manual `run` trigger. Mirrors AutomationHandler.
type EscalationHandler struct {
	DB      *sql.DB
	Service *services.EscalationService

	// Pages serves Escalated/Admin/EscalationRules/Index and Form and answers
	// their writes the Inertia way. Set by the router.
	Pages PageSupport
}

func (h *EscalationHandler) indexURL() string { return h.Pages.path("/admin/escalation-rules") }

// ruleProps is a rule as the pages read it, with conditions and actions as
// arrays.
func ruleProps(rule models.EscalationRule) map[string]any {
	conditions, _ := uiJSON(rule.Conditions).([]any)
	if conditions == nil {
		conditions = []any{}
	}
	actions, _ := uiJSON(rule.Actions).([]any)
	if actions == nil {
		actions = []any{}
	}
	return map[string]any{
		"id":           rule.ID,
		"name":         rule.Name,
		"description":  rule.Description,
		"trigger_type": rule.TriggerType,
		"conditions":   conditions,
		"actions":      actions,
		"order":        rule.Order,
		"is_active":    rule.IsActive,
		"created_at":   uiTimeValue(rule.CreatedAt),
		"updated_at":   uiTimeValue(rule.UpdatedAt),
	}
}

// find loads one rule, or nil.
func (h *EscalationHandler) find(r *http.Request, id int64) (*models.EscalationRule, error) {
	var rule models.EscalationRule
	err := ticketaccess.QueryRowContext(r.Context(), h.DB, sqldialect.Rebind(h.DB, `SELECT id, name, description, trigger_type, conditions, actions, sort_order, is_active, created_at, updated_at
		   FROM escalated_escalation_rules WHERE id = ?`), id).Scan(
		&rule.ID, &rule.Name, &rule.Description, &rule.TriggerType,
		&rule.Conditions, &rule.Actions, &rule.Order, &rule.IsActive,
		&rule.CreatedAt, &rule.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rule, nil
}

// CreateForm renders Escalated/Admin/EscalationRules/Form for a new rule
// (GET /admin/escalation-rules/create).
func (h *EscalationHandler) CreateForm(w http.ResponseWriter, r *http.Request) {
	h.renderForm(w, r, nil, nil)
}

// EditForm renders Escalated/Admin/EscalationRules/Form with the rule
// (GET /admin/escalation-rules/{id}/edit).
func (h *EscalationHandler) EditForm(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	rule, err := h.find(r, id)
	if err != nil {
		http.Error(w, "could not load the rule", http.StatusInternalServerError)
		return
	}
	if rule == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	h.renderForm(w, r, rule, nil)
}

func (h *EscalationHandler) renderForm(w http.ResponseWriter, r *http.Request, rule *models.EscalationRule, errs map[string]string) {
	props := map[string]any{}
	if rule != nil {
		props["rule"] = ruleProps(*rule)
	}
	h.Pages.render(w, r, "Escalated/Admin/EscalationRules/Form", props, errs)
}

// NewEscalationHandler constructs the handler. Pass in the same *sql.DB the
// service uses.
func NewEscalationHandler(db *sql.DB, service *services.EscalationService) *EscalationHandler {
	if service == nil {
		service = services.NewEscalationService(db, nil)
	}
	return &EscalationHandler{DB: db, Service: service}
}

// List handles GET /admin/escalation-rules.
func (h *EscalationHandler) List(w http.ResponseWriter, r *http.Request) {
	rows, err := ticketaccess.QueryContext(r.Context(), h.DB, sqldialect.Rebind(h.DB, `SELECT id, name, description, trigger_type, conditions, actions, sort_order, is_active, created_at, updated_at
		   FROM escalated_escalation_rules
		  ORDER BY sort_order ASC, id ASC`),
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	out := []models.EscalationRule{}
	for rows.Next() {
		var rule models.EscalationRule
		if err := rows.Scan(
			&rule.ID, &rule.Name, &rule.Description, &rule.TriggerType,
			&rule.Conditions, &rule.Actions, &rule.Order, &rule.IsActive,
			&rule.CreatedAt, &rule.UpdatedAt,
		); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		out = append(out, rule)
	}

	if h.Pages.wantsPage(r) {
		rows := make([]map[string]any, 0, len(out))
		for _, rule := range out {
			rows = append(rows, ruleProps(rule))
		}
		h.Pages.render(w, r, "Escalated/Admin/EscalationRules/Index", map[string]any{"rules": rows}, nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rules": out})
}

// Create handles POST /admin/escalation-rules.
func (h *EscalationHandler) Create(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name        string          `json:"name"`
		Description *string         `json:"description"`
		TriggerType *string         `json:"trigger_type"`
		Conditions  json.RawMessage `json:"conditions"`
		Actions     json.RawMessage `json:"actions"`
		Order       *int            `json:"order"`
		IsActive    *bool           `json:"is_active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || strings.TrimSpace(in.Name) == "" {
		h.Pages.fail(w, r, http.StatusBadRequest, "name is required", map[string]string{"name": requiredMessage("name")},
			func(errs map[string]string) { h.renderForm(w, r, nil, errs) })
		return
	}

	conditions := defaultJSONArray(models.JSONText(in.Conditions))
	actions := defaultJSONArray(models.JSONText(in.Actions))
	order := 0
	if in.Order != nil {
		order = *in.Order
	}
	isActive := true
	if in.IsActive != nil {
		isActive = *in.IsActive
	}

	res, err := ticketaccess.ExecInsertContext(r.Context(), h.DB, `INSERT INTO escalated_escalation_rules (name, description, trigger_type, conditions, actions, sort_order, is_active, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		in.Name, in.Description, in.TriggerType, conditions, actions, order, isActive, time.Now(), time.Now(),
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()
	h.Pages.succeed(w, r, h.indexURL(), http.StatusCreated, map[string]any{"id": id})
}

// Update handles PATCH /admin/escalation-rules/{id}.
func (h *EscalationHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	var in struct {
		Name        *string         `json:"name"`
		Description *string         `json:"description"`
		TriggerType *string         `json:"trigger_type"`
		Conditions  json.RawMessage `json:"conditions"`
		Actions     json.RawMessage `json:"actions"`
		Order       *int            `json:"order"`
		IsActive    *bool           `json:"is_active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	// Build a dynamic UPDATE — only set supplied fields (PATCH semantics).
	sets := []string{}
	args := []any{}

	if in.Name != nil {
		if strings.TrimSpace(*in.Name) == "" {
			h.Pages.fail(w, r, http.StatusBadRequest, "name is required", map[string]string{"name": requiredMessage("name")},
				func(errs map[string]string) {
					rule, _ := h.find(r, id)
					h.renderForm(w, r, rule, errs)
				})
			return
		}
		sets = append(sets, "name = ?")
		args = append(args, *in.Name)
	}
	if in.Description != nil {
		sets = append(sets, "description = ?")
		args = append(args, *in.Description)
	}
	if in.TriggerType != nil {
		sets = append(sets, "trigger_type = ?")
		args = append(args, *in.TriggerType)
	}
	if len(in.Conditions) > 0 {
		sets = append(sets, "conditions = ?")
		args = append(args, []byte(in.Conditions))
	}
	if len(in.Actions) > 0 {
		sets = append(sets, "actions = ?")
		args = append(args, []byte(in.Actions))
	}
	if in.Order != nil {
		sets = append(sets, "sort_order = ?")
		args = append(args, *in.Order)
	}
	if in.IsActive != nil {
		sets = append(sets, "is_active = ?")
		args = append(args, *in.IsActive)
	}

	if len(sets) == 0 {
		h.Pages.succeed(w, r, h.indexURL(), http.StatusOK, map[string]any{"id": id})
		return
	}

	sets = append(sets, "updated_at = ?")
	args = append(args, time.Now())
	args = append(args, id)

	q := "UPDATE escalated_escalation_rules SET " + joinSets(sets) + " WHERE id = ?"
	if _, err := ticketaccess.ExecContext(r.Context(), h.DB, sqldialect.Rebind(h.DB, q), args...); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.Pages.succeed(w, r, h.indexURL(), http.StatusOK, map[string]any{"id": id})
}

// Delete handles DELETE /admin/escalation-rules/{id}.
func (h *EscalationHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	if _, err := ticketaccess.ExecContext(r.Context(), h.DB, sqldialect.Rebind(h.DB, `DELETE FROM escalated_escalation_rules WHERE id = ?`),
		id,
	); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.Pages.succeed(w, r, h.indexURL(), http.StatusNoContent, nil)
}

// Run handles POST /admin/escalation-rules/run — manual trigger of the
// evaluator for admin smoke-testing.
func (h *EscalationHandler) Run(w http.ResponseWriter, _ *http.Request) {
	affected, err := h.Service.EvaluateRules()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"affected": affected})
}
