// Package handlers provides HTTP handlers for the Escalated ticket system.
// All handlers use standard http.HandlerFunc signatures and work with any router.
package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/renderer"
	"github.com/escalated-dev/escalated-go/services"
	"github.com/escalated-dev/escalated-go/store"
)

// APIHandler serves the JSON REST API endpoints.
type APIHandler struct {
	store    store.Store
	tickets  *services.TicketService
	renderer renderer.Renderer
	userID   func(r *http.Request) int64
}

// NewAPIHandler creates a new APIHandler.
func NewAPIHandler(s store.Store, ts *services.TicketService, rend renderer.Renderer, userIDFunc func(r *http.Request) int64) *APIHandler {
	return &APIHandler{
		store:    s,
		tickets:  ts,
		renderer: rend,
		userID:   userIDFunc,
	}
}

// ListTickets handles GET /api/tickets
func (h *APIHandler) ListTickets(w http.ResponseWriter, r *http.Request) {
	f := models.TicketFilters{
		Search: r.URL.Query().Get("search"),
	}
	if v := r.URL.Query().Get("status"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			f.Status = &i
		}
	}
	if v := r.URL.Query().Get("priority"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			f.Priority = &i
		}
	}
	if v := r.URL.Query().Get("department_id"); v != "" {
		if i, err := strconv.ParseInt(v, 10, 64); err == nil {
			f.DepartmentID = &i
		}
	}
	if v := r.URL.Query().Get("assigned_to"); v != "" {
		if i, err := strconv.ParseInt(v, 10, 64); err == nil {
			f.AssignedTo = &i
		}
	}
	if r.URL.Query().Get("unassigned") == "true" {
		f.Unassigned = true
	}
	if v := r.URL.Query().Get("limit"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			f.Limit = i
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			f.Offset = i
		}
	}
	f.SortBy = r.URL.Query().Get("sort_by")
	f.SortOrder = r.URL.Query().Get("sort_order")

	tickets, total, err := h.store.ListTickets(r.Context(), f)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"tickets": tickets,
		"total":   total,
	})
}

// ShowTicket handles GET /api/tickets/{id}
func (h *APIHandler) ShowTicket(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid ticket id"})
		return
	}

	t, err := h.tickets.Get(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if t == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "ticket not found"})
		return
	}

	// Load replies
	replies, _ := h.store.ListReplies(r.Context(), models.ReplyFilters{TicketID: id})
	activities, _ := h.store.ListActivities(r.Context(), id, 50)

	writeJSON(w, http.StatusOK, map[string]any{
		"ticket":     t,
		"replies":    replies,
		"activities": activities,
	})
}

// CreateTicket handles POST /api/tickets
func (h *APIHandler) CreateTicket(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Subject      string `json:"subject"`
		Description  string `json:"description"`
		Priority     int    `json:"priority"`
		TicketType   string `json:"ticket_type"`
		DepartmentID *int64 `json:"department_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if in.Subject == "" || in.Description == "" {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "subject and description are required"})
		return
	}

	uid := h.userID(r)
	var reqType *string
	var reqID *int64
	if uid > 0 {
		rt := "User"
		reqType = &rt
		reqID = &uid
	}

	t, err := h.tickets.Create(r.Context(), services.CreateTicketInput{
		Subject:       in.Subject,
		Description:   in.Description,
		Priority:      in.Priority,
		TicketType:    in.TicketType,
		RequesterType: reqType,
		RequesterID:   reqID,
		DepartmentID:  in.DepartmentID,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"ticket": t})
}

// UpdateTicket handles PATCH /api/tickets/{id}
func (h *APIHandler) UpdateTicket(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid ticket id"})
		return
	}

	t, err := h.store.GetTicket(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if t == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "ticket not found"})
		return
	}

	var updates map[string]any
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if v, ok := updates["subject"].(string); ok {
		t.Subject = v
	}
	if v, ok := updates["description"].(string); ok {
		t.Description = v
	}
	if v, ok := updates["status"].(float64); ok {
		t.Status = int(v)
	}
	if v, ok := updates["priority"].(float64); ok {
		t.Priority = int(v)
	}
	if v, ok := updates["ticket_type"].(string); ok {
		t.TicketType = v
	}

	if err := h.store.UpdateTicket(r.Context(), t); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ticket": t})
}

// CreateReply handles POST /api/tickets/{id}/replies
func (h *APIHandler) CreateReply(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid ticket id"})
		return
	}

	var in struct {
		Body       string `json:"body"`
		IsInternal bool   `json:"is_internal"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if in.Body == "" {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "body is required"})
		return
	}

	uid := h.userID(r)
	var authorType *string
	var authorID *int64
	if uid > 0 {
		at := "User"
		authorType = &at
		authorID = &uid
	}

	reply, err := h.tickets.AddReply(r.Context(), id, in.Body, authorType, authorID, in.IsInternal)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"reply": reply})
}

// SplitTicket handles POST /api/tickets/{id}/split
func (h *APIHandler) SplitTicket(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid ticket id"})
		return
	}

	var in struct {
		ReplyID int64  `json:"reply_id"`
		Subject string `json:"subject"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if in.ReplyID == 0 {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "reply_id is required"})
		return
	}

	uid := h.userID(r)
	var causerID *int64
	if uid > 0 {
		causerID = &uid
	}

	newTicket, err := h.tickets.SplitTicket(r.Context(), services.SplitTicketInput{
		TicketID: id,
		ReplyID:  in.ReplyID,
		Subject:  in.Subject,
		CauserID: causerID,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"ticket": newTicket})
}

// ListDepartments handles GET /api/departments
func (h *APIHandler) ListDepartments(w http.ResponseWriter, r *http.Request) {
	depts, err := h.store.ListDepartments(r.Context(), true)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"departments": depts})
}

// ListTags handles GET /api/tags
func (h *APIHandler) ListTags(w http.ResponseWriter, r *http.Request) {
	tags, err := h.store.ListTags(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tags": tags})
}

// --- helpers ---

func writeJSON(w http.ResponseWriter, code int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(data)
}

// idFromPath extracts the last path segment as an int64.
// Works with both chi ({id} in URL params) and stdlib patterns.
func idFromPath(r *http.Request) (int64, error) {
	// Try standard library PathValue first (Go 1.22+)
	if v := r.PathValue("id"); v != "" {
		return strconv.ParseInt(v, 10, 64)
	}
	// Fallback: extract last path segment
	path := r.URL.Path
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return strconv.ParseInt(path[i+1:], 10, 64)
			//nolint:staticcheck // intentional break
		}
	}
	return strconv.ParseInt(path, 10, 64)
}

// refFromPath extracts a reference string from the URL path.
func refFromPath(r *http.Request) string {
	if v := r.PathValue("reference"); v != "" {
		return v
	}
	if v := r.PathValue("ref"); v != "" {
		return v
	}
	path := r.URL.Path
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[i+1:]
		}
	}
	return path
}
