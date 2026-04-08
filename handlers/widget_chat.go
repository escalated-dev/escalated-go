package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/services"
	"github.com/escalated-dev/escalated-go/store"
)

// WidgetChatHandler serves the public-facing widget chat endpoints.
type WidgetChatHandler struct {
	config       WidgetConfig
	store        store.Store
	sessions     *services.ChatSessionService
	availability *services.ChatAvailabilityService
	limiter      *rateLimiter
}

// NewWidgetChatHandler creates a new WidgetChatHandler.
func NewWidgetChatHandler(cfg WidgetConfig, s store.Store, cs *services.ChatSessionService, avail *services.ChatAvailabilityService) *WidgetChatHandler {
	return &WidgetChatHandler{
		config:       cfg,
		store:        s,
		sessions:     cs,
		availability: avail,
		limiter:      newRateLimiter(cfg.RateLimitPerMin, 0),
	}
}

// Availability handles GET /widget/chat/availability.
func (h *WidgetChatHandler) Availability(w http.ResponseWriter, r *http.Request) {
	h.setCORS(w, r)
	if !h.config.Enabled {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "widget is disabled"})
		return
	}

	status, err := h.availability.GetStatus(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to check availability"})
		return
	}

	writeJSON(w, http.StatusOK, status)
}

// StartChat handles POST /widget/chat/start.
func (h *WidgetChatHandler) StartChat(w http.ResponseWriter, r *http.Request) {
	h.setCORS(w, r)
	if !h.config.Enabled {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "widget is disabled"})
		return
	}
	if !h.rateLimit(w, r) {
		return
	}

	var in struct {
		Name    string `json:"name"`
		Email   string `json:"email"`
		Subject string `json:"subject"`
		Message string `json:"message"`
		PageURL string `json:"page_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if in.Email == "" || in.Name == "" {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "name and email are required"})
		return
	}

	ticket, session, err := h.sessions.StartSession(r.Context(), services.StartSessionInput{
		GuestName:  in.Name,
		GuestEmail: in.Email,
		Subject:    in.Subject,
		Message:    in.Message,
		PageURL:    in.PageURL,
		VisitorIP:  r.RemoteAddr,
		VisitorUA:  r.UserAgent(),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"session_id":       session.ID,
		"ticket_reference": ticket.Reference,
		"guest_token":      ticket.GuestToken,
		"status":           session.StatusString(),
	})
}

// SendMessage handles POST /widget/chat/sessions/{ref}/messages.
func (h *WidgetChatHandler) SendMessage(w http.ResponseWriter, r *http.Request) {
	h.setCORS(w, r)
	if !h.config.Enabled {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "widget is disabled"})
		return
	}

	token := r.Header.Get("X-Guest-Token")
	if token == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "guest token required"})
		return
	}

	ref := refFromPath(r)
	session, err := h.findSessionByRef(r, ref, token)
	if err != nil || session == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}

	var in struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Body == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "body is required"})
		return
	}

	if err := h.sessions.SendMessage(r.Context(), session, in.Body, nil, nil); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{"status": "sent"})
}

// EndChat handles POST /widget/chat/sessions/{ref}/end.
func (h *WidgetChatHandler) EndChat(w http.ResponseWriter, r *http.Request) {
	h.setCORS(w, r)
	if !h.config.Enabled {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "widget is disabled"})
		return
	}

	token := r.Header.Get("X-Guest-Token")
	if token == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "guest token required"})
		return
	}

	ref := refFromPath(r)
	session, err := h.findSessionByRef(r, ref, token)
	if err != nil || session == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}

	if err := h.sessions.EndSession(r.Context(), session, nil); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ended"})
}

func (h *WidgetChatHandler) findSessionByRef(r *http.Request, ref, token string) (*models.ChatSession, error) {
	t, err := h.store.GetTicketByReference(r.Context(), ref)
	if err != nil || t == nil {
		return nil, err
	}
	if t.GuestToken == nil || *t.GuestToken != token {
		return nil, nil
	}
	if t.Channel == nil || *t.Channel != models.ChannelChat {
		return nil, nil
	}
	return h.store.GetChatSessionByTicket(r.Context(), t.ID)
}

func (h *WidgetChatHandler) setCORS(w http.ResponseWriter, _ *http.Request) {
	origins := h.config.AllowedOrigins
	if origins == "" {
		origins = "*"
	}
	w.Header().Set("Access-Control-Allow-Origin", origins)
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Guest-Token")
}

func (h *WidgetChatHandler) rateLimit(w http.ResponseWriter, r *http.Request) bool {
	ip := r.RemoteAddr
	if !h.limiter.allow(ip) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "rate limit exceeded"})
		return false
	}
	return true
}
