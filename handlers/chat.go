package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/escalated-dev/escalated-go/services"
	"github.com/escalated-dev/escalated-go/store"
)

// ChatHandler serves the agent-facing chat management endpoints.
type ChatHandler struct {
	store    store.Store
	sessions *services.ChatSessionService
	userID   func(r *http.Request) int64
}

// NewChatHandler creates a new ChatHandler.
func NewChatHandler(s store.Store, cs *services.ChatSessionService, userIDFunc func(r *http.Request) int64) *ChatHandler {
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
