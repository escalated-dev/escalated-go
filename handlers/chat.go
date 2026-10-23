package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/services"
	"github.com/escalated-dev/escalated-go/store"
)

// ChatHandler serves the agent-facing chat management endpoints.
type ChatHandler struct {
	store    store.Store
	sessions *services.ChatSessionService
	userID   func(r *http.Request) models.UserID
}

// NewChatHandler creates a new ChatHandler.
func NewChatHandler(s store.Store, cs *services.ChatSessionService, userIDFunc func(r *http.Request) models.UserID) *ChatHandler {
	return &ChatHandler{
		store:    s,
		sessions: cs,
		userID:   userIDFunc,
	}
}

// ListSessions handles GET /agent/chat/sessions - lists active/waiting sessions.
func (h *ChatHandler) ListSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := h.store.ListChatSessions(r.Context(), services.ActiveChatFilter())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": sessions})
}

// AcceptSession handles POST /agent/chat/sessions/{id}/accept.
func (h *ChatHandler) AcceptSession(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid session id"})
		return
	}

	session, err := h.store.GetChatSessionByTicket(r.Context(), id)
	if err != nil || session == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}

	if !session.IsWaiting() {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "session is not waiting"})
		return
	}

	agentID := h.userID(r)
	if err := h.sessions.AssignAgent(r.Context(), session, agentID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "accepted"})
}

// SendMessage handles POST /agent/chat/sessions/{id}/message.
func (h *ChatHandler) SendMessage(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid session id"})
		return
	}

	session, err := h.store.GetChatSessionByTicket(r.Context(), id)
	if err != nil || session == nil || !session.IsActive() {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "active session not found"})
		return
	}

	var in struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Body == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "body is required"})
		return
	}

	agentID := h.userID(r)
	authorType := "User"
	if err := h.sessions.SendMessage(r.Context(), session, in.Body, &authorType, &agentID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{"status": "sent"})
}

// EndSession handles POST /agent/chat/sessions/{id}/end.
func (h *ChatHandler) EndSession(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid session id"})
		return
	}

	session, err := h.store.GetChatSessionByTicket(r.Context(), id)
	if err != nil || session == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}

	agentID := h.userID(r)
	if err := h.sessions.EndSession(r.Context(), session, &agentID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ended"})
}

// EndChat handles POST /agent/chat/{session}/end (escalated-laravel
// escalated.admin.chat.end). Unlike the console routes, {session} is the chat
// SESSION id, which is what the shared Agent/TicketShow page passes. The body
// is ignored. An already-ended chat is a 422.
func (h *ChatHandler) EndChat(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPathName(r, "session")
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Chat session not found."})
		return
	}

	session, err := h.store.GetChatSession(r.Context(), id)
	if err != nil || session == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Chat session not found."})
		return
	}
	if !session.IsActive() && !session.IsWaiting() {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": "Chat is already ended."})
		return
	}

	agentID := h.userID(r)
	if err := h.sessions.EndSession(r.Context(), session, &agentID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "Chat ended."})
}

// TransferSession handles POST /agent/chat/sessions/{id}/transfer
// (escalated-laravel escalated.admin.chat.transfer). The body names the new
// agent and/or department as {"agent_id", "department_id"}; the shared
// ChatActionBar's {"type": "agent"|"department", "id"} is read too.
func (h *ChatHandler) TransferSession(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid session id"})
		return
	}

	session, err := h.store.GetChatSessionByTicket(r.Context(), id)
	if err != nil || session == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}
	h.transfer(w, r, session)
}

// TransferChat handles POST /agent/chat/{session}/transfer (escalated-laravel
// escalated.admin.chat.transfer). Unlike the console route, {session} is the
// chat SESSION id, which is what the shared Agent/TicketShow page passes. The
// body and answers are those of TransferSession.
func (h *ChatHandler) TransferChat(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPathName(r, "session")
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Chat session not found."})
		return
	}
	session, err := h.store.GetChatSession(r.Context(), id)
	if err != nil || session == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Chat session not found."})
		return
	}
	h.transfer(w, r, session)
}

// transfer reads the transfer body and moves the chat; shared by both routes.
func (h *ChatHandler) transfer(w http.ResponseWriter, r *http.Request, session *models.ChatSession) {
	if !session.IsActive() && !session.IsWaiting() {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": "Chat is already ended."})
		return
	}

	var in struct {
		AgentID      json.RawMessage `json:"agent_id"`
		DepartmentID json.RawMessage `json:"department_id"`
		Type         string          `json:"type"`
		ID           json.RawMessage `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil && !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	switch in.Type {
	case "agent":
		if len(in.AgentID) == 0 {
			in.AgentID = in.ID
		}
	case "department":
		if len(in.DepartmentID) == 0 {
			in.DepartmentID = in.ID
		}
	}

	var toAgent *models.UserID
	if agentID := rawID(in.AgentID); agentID != "" {
		a := models.UserID(agentID)
		toAgent = &a
	}
	var toDepartment *int64
	if deptID := rawID(in.DepartmentID); deptID != "" {
		n, err := strconv.ParseInt(deptID, 10, 64)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": "The department_id must be an integer."})
			return
		}
		if d, err := h.store.GetDepartment(r.Context(), n); err != nil || d == nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": "The selected department_id is invalid."})
			return
		}
		toDepartment = &n
	}
	if toAgent == nil && toDepartment == nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": "Provide an agent_id or department_id."})
		return
	}

	if err := h.sessions.TransferSession(r.Context(), session, toAgent, toDepartment); err != nil {
		if errors.Is(err, services.ErrChatAgentCannotAccess) {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": err.Error()})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	fresh, _ := h.store.GetChatSession(r.Context(), session.ID)
	writeJSON(w, http.StatusOK, map[string]any{"message": "Chat transferred.", "session": fresh})
}

// rawID reads a JSON id that may be a number or a string; null and "" are
// empty.
func rawID(raw json.RawMessage) string {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return ""
	}
	if strings.HasPrefix(s, `"`) {
		var str string
		if json.Unmarshal(raw, &str) != nil {
			return ""
		}
		return strings.TrimSpace(str)
	}
	return s
}
