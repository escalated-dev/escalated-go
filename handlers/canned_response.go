package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/services"
)

// CannedResponseHandler exposes admin CRUD over canned reply templates and
// the agent-facing list scoped to responses the agent can see.
//
// Mirrors MacroHandler: admin routes are unscoped; the agent list returns
// shared responses plus the agent's own. Talks to *sql.DB via
// CannedResponseService.
type CannedResponseHandler struct {
	DB      *sql.DB
	Service *services.CannedResponseService
}

// NewCannedResponseHandler constructs the handler.
func NewCannedResponseHandler(db *sql.DB, svc *services.CannedResponseService) *CannedResponseHandler {
	if svc == nil {
		svc = services.NewCannedResponseService(db, nil)
	}
	return &CannedResponseHandler{DB: db, Service: svc}
}

// AdminList handles GET /admin/canned-responses — list all responses.
func (h *CannedResponseHandler) AdminList(w http.ResponseWriter, r *http.Request) {
	responses, err := h.Service.ListAll()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if responses == nil {
		responses = []models.CannedResponse{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"responses": responses})
}

// Create handles POST /admin/canned-responses.
func (h *CannedResponseHandler) Create(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title    string  `json:"title"`
		Body     string  `json:"body"`
		Category *string `json:"category"`
		IsShared *bool   `json:"is_shared"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Title == "" || in.Body == "" {
		http.Error(w, "title and body are required", http.StatusBadRequest)
		return
	}

	isShared := true
	if in.IsShared != nil {
		isShared = *in.IsShared
	}
	creator := currentAgentID(r)
	var creatorPtr *models.UserID
	if !creator.Empty() {
		creatorPtr = &creator
	}

	c := &models.CannedResponse{
		Title:     in.Title,
		Body:      in.Body,
		Category:  in.Category,
		IsShared:  isShared,
		CreatedBy: creatorPtr,
	}
	if err := h.Service.Create(c); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": c.ID})
}

// Update handles PATCH /admin/canned-responses/{id}.
func (h *CannedResponseHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	c, err := h.Service.FindByID(id)
	if err != nil || c == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	var in struct {
		Title    *string `json:"title"`
		Body     *string `json:"body"`
		Category *string `json:"category"`
		IsShared *bool   `json:"is_shared"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	if in.Title != nil {
		c.Title = *in.Title
	}
	if in.Body != nil {
		c.Body = *in.Body
	}
	if in.Category != nil {
		c.Category = in.Category
	}
	if in.IsShared != nil {
		c.IsShared = *in.IsShared
	}

	if err := h.Service.Update(c); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": c.ID})
}

// Delete handles DELETE /admin/canned-responses/{id}.
func (h *CannedResponseHandler) Delete(w http.ResponseWriter, r *http.Request) {
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

// AgentList handles GET /agent/canned-responses — responses the agent can see.
func (h *CannedResponseHandler) AgentList(w http.ResponseWriter, r *http.Request) {
	agentID := currentAgentID(r)
	responses, err := h.Service.ListForAgent(agentID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if responses == nil {
		responses = []models.CannedResponse{}
	}
	writeJSON(w, http.StatusOK, responses)
}
