package handlers

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/services"
	"github.com/escalated-dev/escalated-go/store"
)

// AgentPanelHandler serves the agent panel's own endpoints (escalated-
// developer-context ADR 2026-11-05-agent-panel-data): the agent's status,
// knowledge-base search for the reply composer and the agent's in-app
// notifications.
type AgentPanelHandler struct {
	Panel       *services.AgentPanel
	Store       store.Store
	UserID      func(r *http.Request) models.UserID
	RoutePrefix string
}

const (
	kbSearchDefaultLimit = 10
	kbSearchMaxLimit     = 20
	notificationsPerPage = 20
	notificationsMaxPage = 50
)

func (h *AgentPanelHandler) agentTickets() string {
	prefix := h.RoutePrefix
	if prefix == "" {
		prefix = "/escalated"
	}
	return prefix + "/agent/tickets"
}

// Status handles PUT/POST /agent/status (escalated.agent.status) with
// {status: online|away|offline}: {agent_status}, or back for an Inertia
// visit. Anything else is a 422.
func (h *AgentPanelHandler) Status(w http.ResponseWriter, r *http.Request) {
	in, err := readInput(r)
	if err != nil {
		http.Error(w, "invalid input", http.StatusBadRequest)
		return
	}
	status := strings.TrimSpace(in.Get("status"))
	if !services.ValidAgentStatus(status) {
		validationErrors(w, r, map[string]string{"status": "The selected status is invalid."}, func(errs map[string]string) {
			backTo(w, r, h.agentTickets())
		})
		return
	}
	if err := h.Panel.SetStatus(r.Context(), h.UserID(r), status); err != nil {
		http.Error(w, "could not save status", http.StatusInternalServerError)
		return
	}
	if isInertia(r) {
		backTo(w, r, h.agentTickets())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"message": "Status updated.", "agent_status": status})
}

// KBSearch handles GET /agent/kb/search?q=&limit= (escalated.agent.kb.search):
// published articles whose title or body contains q (2 to 200 characters),
// title matches first; limit 1 to 20, default 10. 404 while the knowledge
// base is off.
func (h *AgentPanelHandler) KBSearch(w http.ResponseWriter, r *http.Request) {
	if !KBSettingsFromStore(r.Context(), h.Store).KnowledgeBaseEnabled {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Knowledge base is not enabled"})
		return
	}
	query := r.URL.Query()
	term := strings.TrimSpace(query.Get("q"))
	errs := map[string]string{}
	switch n := utf8.RuneCountInString(term); {
	case n < 2:
		errs["q"] = "The q field must be at least 2 characters."
	case n > 200:
		errs["q"] = "The q field must not be greater than 200 characters."
	}
	limit := kbSearchDefaultLimit
	if raw := strings.TrimSpace(query.Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > kbSearchMaxLimit {
			errs["limit"] = "The limit field must be between 1 and 20."
		} else {
			limit = n
		}
	}
	if len(errs) > 0 {
		validationErrors(w, r, errs, nil)
		return
	}
	articles, err := h.Panel.KBSearch(r.Context(), term, limit)
	if err != nil {
		http.Error(w, "could not search the knowledge base", http.StatusInternalServerError)
		return
	}
	data := make([]map[string]any, 0, len(articles))
	for _, a := range articles {
		slug, _ := a["slug"].(string)
		body, _ := a["body"].(string)
		data = append(data, map[string]any{
			"id":         a["id"],
			"title":      a["title"],
			"excerpt":    articleExcerpt(body),
			"url":        absoluteURL(r, h.prefix()+"/kb/"+slug),
			"category":   a["category"],
			"updated_at": a["updated_at"],
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data})
}

func (h *AgentPanelHandler) prefix() string {
	if h.RoutePrefix == "" {
		return "/escalated"
	}
	return h.RoutePrefix
}

// absoluteURL is path on the request's own origin.
func absoluteURL(r *http.Request, path string) string {
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return scheme + "://" + r.Host + path
}

// Notifications handles GET /agent/notifications?page=&per_page=&unread=1
// (escalated.agent.notifications.index): {data, meta}, newest first.
func (h *AgentPanelHandler) Notifications(w http.ResponseWriter, r *http.Request) {
	if !h.feed(w) {
		return
	}
	query := r.URL.Query()
	perPage := clampInt(query.Get("per_page"), notificationsPerPage, 1, notificationsMaxPage)
	page := clampInt(query.Get("page"), 1, 1, 1<<20)
	items, meta, err := h.Panel.NotificationPage(r.Context(), h.UserID(r), page, perPage, truthy(query.Get("unread")))
	if err != nil {
		http.Error(w, "could not load notifications", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items, "meta": meta})
}

// ReadNotification handles POST /agent/notifications/{id}/read
// (escalated.agent.notifications.read): {unread_count}. Another agent's
// notification is 404.
func (h *AgentPanelHandler) ReadNotification(w http.ResponseWriter, r *http.Request) {
	if !h.feed(w) {
		return
	}
	uid := h.UserID(r)
	found, err := h.Panel.ReadNotification(r.Context(), uid, pathParam(r, "id"))
	if err != nil {
		http.Error(w, "could not update the notification", http.StatusInternalServerError)
		return
	}
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Notification not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"unread_count": h.Panel.UnreadCount(r.Context(), uid)})
}

// ReadAllNotifications handles POST /agent/notifications/read-all
// (escalated.agent.notifications.read-all): {unread_count: 0}.
func (h *AgentPanelHandler) ReadAllNotifications(w http.ResponseWriter, r *http.Request) {
	if !h.feed(w) {
		return
	}
	if err := h.Panel.ReadAllNotifications(r.Context(), h.UserID(r)); err != nil {
		http.Error(w, "could not update the notifications", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"unread_count": 0})
}

// feed answers 404 while the notifications feed is off.
func (h *AgentPanelHandler) feed(w http.ResponseWriter) bool {
	if h.Panel.NotificationsEnabled() {
		return true
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
	return false
}

func clampInt(raw string, def, minimum, maximum int) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return def
	}
	if n < minimum {
		return minimum
	}
	if n > maximum {
		return maximum
	}
	return n
}

// MarkRead handles POST /agent/tickets/{id}/read (escalated.agent.tickets.read):
// {is_unread: false}; an Inertia visit goes back.
func (h *AgentHandler) MarkRead(w http.ResponseWriter, r *http.Request) { h.mark(w, r, false) }

// MarkUnread handles POST /agent/tickets/{id}/unread
// (escalated.agent.tickets.unread): {is_unread: true}; an Inertia visit goes
// back.
func (h *AgentHandler) MarkUnread(w http.ResponseWriter, r *http.Request) { h.mark(w, r, true) }

func (h *AgentHandler) mark(w http.ResponseWriter, r *http.Request, unread bool) {
	t, ok := h.pageTicket(w, r)
	if !ok {
		return
	}
	if h.Panel != nil {
		var err error
		if unread {
			err = h.Panel.MarkUnread(r.Context(), h.userID(r), t.ID)
		} else {
			err = h.Panel.MarkRead(r.Context(), h.userID(r), t.ID)
		}
		if err != nil {
			http.Error(w, "could not update the ticket", http.StatusInternalServerError)
			return
		}
	}
	if isInertia(r) {
		backTo(w, r, h.agentTicketURL(t))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"is_unread": unread})
}

func (h *AgentHandler) agentTicketURL(t *models.Ticket) string {
	prefix := h.RoutePrefix
	if prefix == "" {
		prefix = "/escalated"
	}
	return prefix + "/agent/tickets/" + t.Reference
}

// withPanelFields adds is_unread and the requester's presence to each list
// row (ADR 2026-11-05-agent-panel-data), one query each for the page.
func (h *AgentHandler) withPanelFields(ctx context.Context, uid models.UserID, page any) {
	paginator, _ := page.(map[string]any)
	rows, _ := paginator["data"].([]map[string]any)
	if h.Panel == nil || len(rows) == 0 {
		return
	}
	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		if id, ok := row["id"].(int64); ok {
			ids = append(ids, id)
		}
	}
	fields := h.Panel.RowFields(ctx, uid, ids)
	for _, row := range rows {
		id, _ := row["id"].(int64)
		for k, v := range fields[id] {
			row[k] = v
		}
	}
}

// seeTicketCustomer records that the ticket's customer is on the site (the
// guest pages and the widget). Only agent pages ever show it.
func seeTicketCustomer(ctx context.Context, tickets *services.TicketService, t *models.Ticket) {
	if tickets != nil && tickets.Panel != nil {
		tickets.Panel.SeeTicketCustomer(ctx, t)
	}
}
