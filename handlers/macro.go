package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/services"
)

// MacroHandler exposes admin CRUD over Macro definitions and the
// agent-facing apply endpoint.
//
// Distinct from WorkflowHandler (admin event-driven) and
// AutomationHandler (admin time-based). See escalated-developer-context/
// domain-model/workflows-automations-macros.md.
//
// Today this handler talks to *sql.DB directly via MacroService. A
// follow-up will move CRUD onto a store.Store-style interface for parity
// with the rest of the admin/agent handlers.
type MacroHandler struct {
	DB      *sql.DB
	Service *services.MacroService
}

// NewMacroHandler constructs the handler.
func NewMacroHandler(db *sql.DB, svc *services.MacroService) *MacroHandler {
	if svc == nil {
		svc = services.NewMacroService(db, nil)
	}
	return &MacroHandler{DB: db, Service: svc}
}

// AdminList handles GET /admin/macros — list all macros for admin.
func (h *MacroHandler) AdminList(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.QueryContext(
		r.Context(),
		`SELECT id, name, description, actions, is_shared, created_by, created_at, updated_at
		   FROM escalated_macros
		  ORDER BY name ASC`,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	out := []models.Macro{}
	for rows.Next() {
		var m models.Macro
		var createdBy sql.NullInt64
		if err := rows.Scan(
			&m.ID, &m.Name, &m.Description, &m.Actions, &m.IsShared,
			&createdBy, &m.CreatedAt, &m.UpdatedAt,
		); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if createdBy.Valid {
			m.CreatedBy = &createdBy.Int64
		}
		out = append(out, m)
	}

	writeJSON(w, http.StatusOK, map[string]any{"macros": out})
}

// Create handles POST /admin/macros.
func (h *MacroHandler) Create(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name        string          `json:"name"`
		Description *string         `json:"description"`
		Actions     json.RawMessage `json:"actions"`
		IsShared    *bool           `json:"is_shared"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	isShared := true
	if in.IsShared != nil {
		isShared = *in.IsShared
	}
	creator := currentAgentID(r)
	var creatorPtr *int64
	if creator != 0 {
		creatorPtr = &creator
	}

	m := &models.Macro{
		Name:        in.Name,
		Description: in.Description,
		Actions:     macroDefaultJSONArray(in.Actions),
		IsShared:    isShared,
		CreatedBy:   creatorPtr,
	}
	if err := h.Service.Create(m); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": m.ID})
}

// Update handles PATCH /admin/macros/{id}.
func (h *MacroHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	m, err := h.Service.FindByID(id)
	if err != nil || m == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	var in struct {
		Name        *string         `json:"name"`
		Description *string         `json:"description"`
		Actions     json.RawMessage `json:"actions"`
		IsShared    *bool           `json:"is_shared"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	if in.Name != nil {
		m.Name = *in.Name
	}
	if in.Description != nil {
		m.Description = in.Description
	}
	if len(in.Actions) > 0 {
		m.Actions = []byte(in.Actions)
	}
	if in.IsShared != nil {
		m.IsShared = *in.IsShared
	}

	if err := h.Service.Update(m); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": m.ID})
}

// Delete handles DELETE /admin/macros/{id}.
func (h *MacroHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := h.Service.Delete(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// AgentList handles GET /agent/macros — list visible to the current agent.
func (h *MacroHandler) AgentList(w http.ResponseWriter, r *http.Request) {
	agentID := currentAgentID(r)
	macros, err := h.Service.ListForAgent(agentID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, macros)
}

// AgentApply handles POST /agent/tickets/{ticketId}/macros/{macroId}/apply.
func (h *MacroHandler) AgentApply(w http.ResponseWriter, r *http.Request) {
	ticketID, err := idFromPathName(r, "ticketId")
	if err != nil {
		http.Error(w, "invalid ticket id", http.StatusBadRequest)
		return
	}
	macroID, err := idFromPathName(r, "macroId")
	if err != nil {
		http.Error(w, "invalid macro id", http.StatusBadRequest)
		return
	}

	macro, err := h.Service.FindByID(macroID)
	if err != nil || macro == nil {
		http.Error(w, "macro not found", http.StatusNotFound)
		return
	}

	if err := h.Service.Apply(macro, ticketID, currentAgentID(r)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// currentAgentID extracts the authenticated agent's id from the request
// context. The actual key depends on the host's auth middleware; falls
// back to 0 (= anonymous / system) if not set.
func currentAgentID(r *http.Request) int64 {
	v := r.Context().Value(ctxKeyUserID{})
	if id, ok := v.(int64); ok {
		return id
	}
	if id, ok := v.(int); ok {
		return int64(id)
	}
	return 0
}

// ctxKeyUserID is the canonical context key the host auth middleware
// populates with the current agent/user id. Defined here so all
// handlers in this package can share it.
type ctxKeyUserID struct{}

// defaultJSONArray returns a "[]" RawMessage when raw is empty so the
// DB column is never NULL. Mirrors automation.go's helper of the same
// name (kept duplicated under the same name; both files are in the
// same package, but they live on independent feature branches today;
// once both PRs merge the duplicate will collapse).
//
// (Same-name redeclaration would conflict if both branches landed at
// once. To prevent that, this version is renamed to a macro-prefixed
// variant.)
func macroDefaultJSONArray(raw json.RawMessage) []byte {
	if len(raw) == 0 {
		return []byte("[]")
	}
	return []byte(raw)
}

// idFromPathName extracts a named URL parameter as an int64. Used for
// nested routes like /agent/tickets/{ticketId}/macros/{macroId}/apply.
func idFromPathName(r *http.Request, name string) (int64, error) {
	if v := r.PathValue(name); v != "" {
		return strconv.ParseInt(v, 10, 64)
	}
	return 0, http.ErrNoCookie // sentinel-only — never returned given the route shape
}
