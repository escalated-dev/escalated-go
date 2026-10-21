package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"

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

	// Pages serves Escalated/Admin/CannedResponses/Index from
	// GET /admin/canned-responses and answers the page's writes the Inertia
	// way. Set by the router.
	Pages PageSupport
}

func (h *CannedResponseHandler) rerender(w http.ResponseWriter, r *http.Request) func(map[string]string) {
	return func(errs map[string]string) { h.renderIndex(w, r, nil, errs) }
}

// renderIndex renders Escalated/Admin/CannedResponses/Index (Laravel
// Admin\CannedResponseController::index): each response with its creator.
func (h *CannedResponseHandler) renderIndex(w http.ResponseWriter, r *http.Request, responses []models.CannedResponse, errs map[string]string) {
	if responses == nil {
		var err error
		if responses, err = h.Service.ListAll(); err != nil {
			http.Error(w, "could not list canned responses", http.StatusInternalServerError)
			return
		}
	}
	rows := make([]map[string]any, 0, len(responses))
	for _, c := range responses {
		rows = append(rows, map[string]any{
			"id":         c.ID,
			"title":      c.Title,
			"body":       c.Body,
			"category":   c.Category,
			"is_shared":  c.IsShared,
			"created_by": c.CreatedBy,
			"creator":    h.Pages.creator(r, c.CreatedBy),
			"created_at": uiTimeValue(c.CreatedAt),
			"updated_at": uiTimeValue(c.UpdatedAt),
		})
	}
	h.Pages.render(w, r, "Escalated/Admin/CannedResponses/Index", map[string]any{"responses": rows}, errs)
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
	if h.Pages.wantsPage(r) {
		h.renderIndex(w, r, responses, nil)
		return
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
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || strings.TrimSpace(in.Title) == "" || strings.TrimSpace(in.Body) == "" {
		errs := map[string]string{}
		if strings.TrimSpace(in.Title) == "" {
			errs["title"] = requiredMessage("title")
		}
		if strings.TrimSpace(in.Body) == "" {
			errs["body"] = requiredMessage("body")
		}
		h.Pages.fail(w, r, http.StatusBadRequest, "title and body are required", errs, h.rerender(w, r))
		return
	}
	if in.Category != nil && strings.TrimSpace(*in.Category) == "" {
		in.Category = nil
	}

	isShared := true
	if in.IsShared != nil {
		isShared = *in.IsShared
	}
	creator := h.Pages.currentUser(r)
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
	h.Pages.succeed(w, r, h.Pages.path("/admin/canned-responses"), http.StatusCreated, map[string]any{"id": c.ID})
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
	h.Pages.succeed(w, r, h.Pages.path("/admin/canned-responses"), http.StatusOK, map[string]any{"id": c.ID})
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
	h.Pages.succeed(w, r, h.Pages.path("/admin/canned-responses"), http.StatusNoContent, nil)
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
