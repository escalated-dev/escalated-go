package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/services"
)

// EscalationHandler exposes admin CRUD over time-based EscalationRule rows
// plus a manual `run` trigger. Mirrors AutomationHandler.
type EscalationHandler struct {
	DB      *sql.DB
	Service *services.EscalationService
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
	rows, err := h.DB.QueryContext(
		r.Context(),
		`SELECT id, name, description, trigger_type, conditions, actions, sort_order, is_active, created_at, updated_at
		   FROM escalated_escalation_rules
		  ORDER BY sort_order ASC, id ASC`,
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
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	conditions := defaultJSONArray(in.Conditions)
	actions := defaultJSONArray(in.Actions)
	order := 0
	if in.Order != nil {
		order = *in.Order
	}
	isActive := true
	if in.IsActive != nil {
		isActive = *in.IsActive
	}

	res, err := h.DB.ExecContext(
		r.Context(),
		`INSERT INTO escalated_escalation_rules (name, description, trigger_type, conditions, actions, sort_order, is_active, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		in.Name, in.Description, in.TriggerType, conditions, actions, order, isActive, time.Now(), time.Now(),
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
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
		writeJSON(w, http.StatusOK, map[string]any{"id": id})
		return
	}

	sets = append(sets, "updated_at = ?")
	args = append(args, time.Now())
	args = append(args, id)

	q := "UPDATE escalated_escalation_rules SET " + joinSets(sets) + " WHERE id = ?"
	if _, err := h.DB.ExecContext(r.Context(), q, args...); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

// Delete handles DELETE /admin/escalation-rules/{id}.
func (h *EscalationHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	if _, err := h.DB.ExecContext(
		r.Context(),
		`DELETE FROM escalated_escalation_rules WHERE id = ?`,
		id,
	); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
