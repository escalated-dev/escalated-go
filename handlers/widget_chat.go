package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/services"
	"github.com/escalated-dev/escalated-go/store"
)

// widgetChatMessageLimit caps how many messages GET /chat/{id}/messages returns
// (the most recent ones).
const widgetChatMessageLimit = 100

// WidgetChatHandler serves the public-facing widget chat endpoints.
//
// A chat is addressed by its id: the chat ticket's guest token, an unguessable
// random string returned as "id" by POST /chat/start. The /chat/{id}/... routes
// authenticate by that token alone. The older /chat/sessions/{ref}/... routes
// keep taking the ticket reference plus an X-Guest-Token header.
type WidgetChatHandler struct {
	config       WidgetConfig
	store        store.Store
	sessions     *services.ChatSessionService
	availability *services.ChatAvailabilityService
	limiter      *rateLimiter

	// AgentName, when set, returns the display name of an agent for the
	// widget's chat transcript. When nil (or it returns ""), agents show as
	// "Support".
	AgentName func(ctx context.Context, id models.UserID) string
}

// NewWidgetChatHandler creates a new WidgetChatHandler. Chat start is limited
// per client IP to cfg.RateLimitPerMin a minute, like ticket creation.
func NewWidgetChatHandler(cfg WidgetConfig, s store.Store, cs *services.ChatSessionService, avail *services.ChatAvailabilityService) *WidgetChatHandler {
	return &WidgetChatHandler{
		config:       cfg,
		store:        s,
		sessions:     cs,
		availability: avail,
		limiter:      newWidgetRateLimiter(cfg),
	}
}

// Availability handles GET /widget/chat/availability.
func (h *WidgetChatHandler) Availability(w http.ResponseWriter, r *http.Request) {
	if !h.config.Enabled {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "widget is disabled"})
		return
	}
	if h.availability == nil {
		writeJSON(w, http.StatusOK, map[string]any{"available": false})
		return
	}

	// department_id scopes the check; departmentId is the older spelling.
	var departmentID *int64
	raw := r.URL.Query().Get("department_id")
	if raw == "" {
		raw = r.URL.Query().Get("departmentId")
	}
	if id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64); err == nil && id > 0 {
		departmentID = &id
	}

	status, err := h.availability.GetStatusForDepartment(r.Context(), departmentID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to check availability"})
		return
	}

	writeJSON(w, http.StatusOK, status)
}

// StartChat handles POST /widget/chat/start.
func (h *WidgetChatHandler) StartChat(w http.ResponseWriter, r *http.Request) {
	if !h.config.Enabled {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "widget is disabled"})
		return
	}
	if !h.rateLimit(w, r) {
		return
	}

	var in struct {
		Name         string          `json:"name"`
		Email        string          `json:"email"`
		Subject      string          `json:"subject"`
		Message      string          `json:"message"`
		PageURL      string          `json:"page_url"`
		DepartmentID *int64          `json:"department_id"`
		Metadata     json.RawMessage `json:"metadata"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body", "message": "Invalid request body."})
		return
	}

	if missing := requiredWidgetFields(map[string]string{"name": in.Name, "email": in.Email}); len(missing) > 0 {
		writeWidgetValidation(w, "name and email are required", missing)
		return
	}
	if !widgetDepartmentValid(r, h.store, in.DepartmentID) {
		writeWidgetValidation(w, "the selected department is invalid", map[string][]string{
			"department_id": {"The selected department is invalid."},
		})
		return
	}
	metadata, err := sanitizeWidgetMetadata(in.Metadata, WidgetCrossOrigin(r.Context()))
	if err != nil {
		writeWidgetValidation(w, err.Error(), map[string][]string{"metadata": {err.Error()}})
		return
	}

	ticket, session, err := h.sessions.StartSession(r.Context(), services.StartSessionInput{
		GuestName:    in.Name,
		GuestEmail:   in.Email,
		Subject:      in.Subject,
		Message:      in.Message,
		PageURL:      in.PageURL,
		VisitorIP:    r.RemoteAddr,
		VisitorUA:    r.UserAgent(),
		DepartmentID: in.DepartmentID,
		Metadata:     metadata,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	id := ""
	if ticket.GuestToken != nil {
		id = *ticket.GuestToken
	}
	resp := map[string]any{
		"id":               id,
		"session_id":       session.ID,
		"ticket_reference": ticket.Reference,
		"guest_token":      ticket.GuestToken,
		"status":           session.StatusString(),
		"messages":         h.transcript(r.Context(), ticket, nil),
	}
	if agent := h.agent(r.Context(), session); agent != nil {
		resp["agent"] = agent
	}
	writeJSON(w, http.StatusCreated, resp)
}

// Messages handles GET /widget/chat/{id}/messages - the chat transcript
// (public replies, oldest first, the latest 100) plus agent and ended state.
// typing is always null: agent typing is broadcast, not stored.
func (h *WidgetChatHandler) Messages(w http.ResponseWriter, r *http.Request) {
	ticket, session, ok := h.chatByID(w, r)
	if !ok {
		return
	}

	isInternal := false
	replies, err := h.store.ListReplies(r.Context(), models.ReplyFilters{TicketID: ticket.ID, Internal: &isInternal})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load messages"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"messages": h.transcript(r.Context(), ticket, replies),
		"typing":   nil,
		"agent":    h.agent(r.Context(), session),
		"ended":    chatEnded(session),
		"status":   session.StatusString(),
	})
}

// PostMessage handles POST /widget/chat/{id}/messages with {body}.
func (h *WidgetChatHandler) PostMessage(w http.ResponseWriter, r *http.Request) {
	_, session, ok := h.chatByID(w, r)
	if !ok {
		return
	}
	if chatEnded(session) {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "chat has ended", "message": "Chat has ended."})
		return
	}
	h.send(w, r, session)
}

// Typing handles POST /widget/chat/{id}/typing - the visitor's typing
// indicator, broadcast to agents.
func (h *WidgetChatHandler) Typing(w http.ResponseWriter, r *http.Request) {
	_, session, ok := h.chatByID(w, r)
	if !ok {
		return
	}
	if chatEnded(session) {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "chat has ended", "message": "Chat has ended."})
		return
	}
	h.sessions.Typing(r.Context(), session, false)
	writeJSON(w, http.StatusOK, map[string]string{"message": "Typing updated."})
}

// End handles POST /widget/chat/{id}/end.
func (h *WidgetChatHandler) End(w http.ResponseWriter, r *http.Request) {
	_, session, ok := h.chatByID(w, r)
	if !ok {
		return
	}
	if chatEnded(session) {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "chat has already ended", "message": "Chat has already ended."})
		return
	}
	if err := h.sessions.EndSession(r.Context(), session, nil); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Chat ended.", "status": "ended"})
}

// Rate handles POST /widget/chat/{id}/rate with {rating: 1-5, comment?}. Only
// an ended chat can be rated, once.
func (h *WidgetChatHandler) Rate(w http.ResponseWriter, r *http.Request) {
	_, session, ok := h.chatByID(w, r)
	if !ok {
		return
	}
	if !chatEnded(session) {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "chat must be ended before rating", "message": "Chat must be ended before rating."})
		return
	}

	var in struct {
		Rating  int    `json:"rating"`
		Comment string `json:"comment"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Rating < 1 || in.Rating > 5 {
		writeWidgetValidation(w, "rating must be an integer between 1 and 5", map[string][]string{
			"rating": {"The rating must be between 1 and 5."},
		})
		return
	}
	comment := strings.TrimSpace(in.Comment)
	if len([]rune(comment)) > 1000 {
		comment = string([]rune(comment)[:1000])
	}

	err := h.sessions.RateChat(r.Context(), session, in.Rating, comment)
	if errors.Is(err, services.ErrChatAlreadyRated) {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error(), "message": "This chat has already been rated."})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Rating submitted."})
}

// SendMessage handles POST /widget/chat/sessions/{ref}/messages.
func (h *WidgetChatHandler) SendMessage(w http.ResponseWriter, r *http.Request) {
	if !h.config.Enabled {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "widget is disabled"})
		return
	}

	token := r.Header.Get("X-Guest-Token")
	if token == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "guest token required"})
		return
	}

	ref := chatRefFromPath(r)
	session, err := h.findSessionByRef(r, ref, token)
	if err != nil || session == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}

	h.send(w, r, session)
}

func (h *WidgetChatHandler) send(w http.ResponseWriter, r *http.Request, session *models.ChatSession) {
	var in struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || strings.TrimSpace(in.Body) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "body is required",
			"message": "body is required",
			"errors":  map[string][]string{"body": {"The body field is required."}},
		})
		return
	}

	if err := h.sessions.SendMessage(r.Context(), session, in.Body, nil, nil); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{"status": "sent", "message": "Message sent."})
}

// EndChat handles POST /widget/chat/sessions/{ref}/end.
func (h *WidgetChatHandler) EndChat(w http.ResponseWriter, r *http.Request) {
	if !h.config.Enabled {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "widget is disabled"})
		return
	}

	token := r.Header.Get("X-Guest-Token")
	if token == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "guest token required"})
		return
	}

	ref := chatRefFromPath(r)
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

// chatByID resolves the /chat/{id} path's token to a chat ticket and its
// session. Anything else (unknown token, a non-chat ticket's token, no
// session) is 404. It writes the error response and returns ok=false.
func (h *WidgetChatHandler) chatByID(w http.ResponseWriter, r *http.Request) (*models.Ticket, *models.ChatSession, bool) {
	if !h.config.Enabled {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "widget is disabled"})
		return nil, nil, false
	}
	notFound := func() (*models.Ticket, *models.ChatSession, bool) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "chat not found", "message": "Chat not found."})
		return nil, nil, false
	}

	token := pathParam(r, "id")
	lookup, ok := h.store.(guestTokenLookup)
	if token == "" || !ok {
		return notFound()
	}
	t, err := lookup.GetTicketByGuestToken(r.Context(), token)
	if err != nil || t == nil || !guestTokenMatches(t.GuestToken, token) {
		return notFound()
	}
	// The session is what makes a ticket a chat: the stores do not persist
	// the ticket's channel, so a guest token of an ordinary ticket finds no
	// session here and is 404.
	session, err := h.store.GetChatSessionByTicket(r.Context(), t.ID)
	if err != nil || session == nil {
		return notFound()
	}
	return t, session, true
}

// transcript maps the chat's opening message (the ticket description) and its
// public replies to the widget's message shape.
func (h *WidgetChatHandler) transcript(ctx context.Context, t *models.Ticket, replies []*models.Reply) []map[string]any {
	guest := "Visitor"
	if t.GuestName != nil && *t.GuestName != "" {
		guest = *t.GuestName
	}

	messages := []map[string]any{}
	if strings.TrimSpace(t.Description) != "" {
		messages = append(messages, map[string]any{
			"id":         "opening",
			"body":       t.Description,
			"is_agent":   false,
			"author":     map[string]string{"name": guest},
			"created_at": t.CreatedAt,
		})
	}

	if len(replies) > widgetChatMessageLimit {
		replies = replies[len(replies)-widgetChatMessageLimit:]
	}
	for _, reply := range replies {
		if reply == nil || reply.IsInternal {
			continue
		}
		isAgent := reply.AuthorType != nil
		name := guest
		if isAgent {
			name = h.agentName(ctx, reply.AuthorID, reply.AuthorName)
		}
		messages = append(messages, map[string]any{
			"id":         reply.ID,
			"body":       reply.Body,
			"is_agent":   isAgent,
			"author":     map[string]string{"name": name},
			"created_at": reply.CreatedAt,
		})
	}
	return messages
}

func (h *WidgetChatHandler) agent(ctx context.Context, session *models.ChatSession) map[string]string {
	if session == nil || session.AgentID == nil {
		return nil
	}
	return map[string]string{"name": h.agentName(ctx, session.AgentID, nil)}
}

func (h *WidgetChatHandler) agentName(ctx context.Context, id *models.UserID, known *string) string {
	if known != nil && *known != "" {
		return *known
	}
	if h.AgentName != nil && id != nil {
		if name := h.AgentName(ctx, *id); name != "" {
			return name
		}
	}
	return "Support"
}

func chatEnded(session *models.ChatSession) bool {
	return session.Status == models.ChatStatusEnded || session.Status == models.ChatStatusAbandoned
}

// chatRefFromPath reads {ref} from /chat/sessions/{ref}/... under either
// router. refFromPath's last-segment fallback would return "messages" or
// "end" under chi.
func chatRefFromPath(r *http.Request) string {
	if v := pathParam(r, "ref"); v != "" {
		return v
	}
	return refFromPath(r)
}

func (h *WidgetChatHandler) findSessionByRef(r *http.Request, ref, token string) (*models.ChatSession, error) {
	t, err := h.store.GetTicketByReference(r.Context(), ref)
	if err != nil || t == nil {
		return nil, err
	}
	if !guestTokenMatches(t.GuestToken, token) {
		return nil, nil
	}
	// A chat session for the ticket is what makes it a chat (the stores do
	// not persist the ticket's channel).
	return h.store.GetChatSessionByTicket(r.Context(), t.ID)
}

func (h *WidgetChatHandler) rateLimit(w http.ResponseWriter, r *http.Request) bool {
	return widgetAllow(w, r, h.limiter)
}
