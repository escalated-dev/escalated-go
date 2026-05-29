package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/escalated-dev/escalated-go/actions"
	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/renderer"
	"github.com/escalated-dev/escalated-go/services"
	"github.com/escalated-dev/escalated-go/store"
)

// AgentHandler serves the agent dashboard and ticket management views.
type AgentHandler struct {
	store       store.Store
	tickets     *services.TicketService
	assignments *services.AssignmentService
	renderer    renderer.Renderer
	userID      func(r *http.Request) models.UserID

	// Actions, OnCustomAction, and RoutePrefix are wired by the router to
	// support host-defined custom ticket actions. They may be nil/empty.
	Actions        *actions.Registry
	OnCustomAction func(ctx context.Context, e actions.CustomActionEvent) error
	RoutePrefix    string
}

// NewAgentHandler creates a new AgentHandler.
func NewAgentHandler(s store.Store, ts *services.TicketService, as *services.AssignmentService, rend renderer.Renderer, userIDFunc func(r *http.Request) models.UserID) *AgentHandler {
	return &AgentHandler{
		store:       s,
		tickets:     ts,
		assignments: as,
		renderer:    rend,
		userID:      userIDFunc,
	}
}

// Dashboard shows the agent dashboard with ticket overview.
func (h *AgentHandler) Dashboard(w http.ResponseWriter, r *http.Request) {
	uid := h.userID(r)

	// My open tickets
	myF := models.TicketFilters{AssignedTo: &uid, Limit: 20}
	myTickets, myTotal, _ := h.store.ListTickets(r.Context(), myF)

	// Unassigned tickets
	unassF := models.TicketFilters{Unassigned: true, Limit: 20}
	unassigned, unassTotal, _ := h.store.ListTickets(r.Context(), unassF)

	for _, t := range myTickets {
		t.PopulateComputed(nil)
	}
	for _, t := range unassigned {
		t.PopulateComputed(nil)
	}

	_ = h.renderer.Render(w, r, "Agent/Dashboard", map[string]any{
		"my_tickets":       myTickets,
		"my_total":         myTotal,
		"unassigned":       unassigned,
		"unassigned_total": unassTotal,
	})
}

// ListTickets shows the full ticket queue for agents.
func (h *AgentHandler) ListTickets(w http.ResponseWriter, r *http.Request) {
	f := models.TicketFilters{Limit: 50}
	f.Search = r.URL.Query().Get("search")
	f.SortBy = r.URL.Query().Get("sort_by")
	f.SortOrder = r.URL.Query().Get("sort_order")

	tickets, total, err := h.store.ListTickets(r.Context(), f)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	depts, _ := h.store.ListDepartments(r.Context(), true)
	tags, _ := h.store.ListTags(r.Context())

	for _, t := range tickets {
		t.PopulateComputed(nil)
	}

	_ = h.renderer.Render(w, r, "Agent/Tickets/Index", map[string]any{
		"tickets":     tickets,
		"total":       total,
		"departments": depts,
		"tags":        tags,
	})
}

// ShowTicket shows a single ticket with full details for agents.
func (h *AgentHandler) ShowTicket(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	t, err := h.tickets.Get(r.Context(), id)
	if err != nil || t == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	replies, _ := h.store.ListReplies(r.Context(), models.ReplyFilters{TicketID: id})
	activities, _ := h.store.ListActivities(r.Context(), id, 50)
	depts, _ := h.store.ListDepartments(r.Context(), true)
	tags, _ := h.store.ListTags(r.Context())

	// Load attachments for the ticket
	ticketAttachments, _ := h.store.GetAttachmentsByTicketID(r.Context(), id)
	populateAttachmentURLs(ticketAttachments, "/escalated")

	// Attach per-reply attachments
	for _, rpl := range replies {
		replyAtts, _ := h.store.GetAttachmentsByReplyID(r.Context(), rpl.ID)
		populateAttachmentURLs(replyAtts, "/escalated")
		if len(replyAtts) > 0 {
			rpl.Attachments = make([]models.Attachment, len(replyAtts))
			for i, a := range replyAtts {
				rpl.Attachments[i] = *a
			}
		}
	}

	t.PopulateComputed(replies)

	_ = h.renderer.Render(w, r, "Agent/Tickets/Show", map[string]any{
		"ticket":        t,
		"replies":       replies,
		"activities":    activities,
		"departments":   depts,
		"tags":          tags,
		"attachments":   ticketAttachments,
		"customActions": h.customActionsFor(t, h.userID(r), "agent"),
	})
}

// customActionsFor serializes the visible custom actions for a ticket, adding
// url + method. surface is "agent" or "api" (selects the URL prefix segment).
func (h *AgentHandler) customActionsFor(t *models.Ticket, userID models.UserID, surface string) []map[string]any {
	if h.Actions == nil {
		return []map[string]any{}
	}
	prefix := h.RoutePrefix
	if prefix == "" {
		prefix = "/escalated"
	}
	out := h.Actions.ForTicket(t, userID)
	for _, a := range out {
		a["url"] = fmt.Sprintf("%s/%s/tickets/%d/actions/%v", prefix, surface, t.ID, a["key"])
		a["method"] = "post"
	}
	return out
}

// CustomAction handles POST /agent/tickets/{id}/actions/{action}.
func (h *AgentHandler) CustomAction(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if h.Actions == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	t, err := h.tickets.Get(r.Context(), id)
	if err != nil || t == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	key := r.PathValue("action")
	uid := h.userID(r)
	action, ok := h.Actions.Find(key)
	if !ok || !h.Actions.Visible(action, t, uid) {
		http.Error(w, "custom action not found", http.StatusNotFound)
		return
	}
	if !h.Actions.Enabled(action, t, uid) {
		http.Error(w, "custom action is not enabled", http.StatusForbidden)
		return
	}

	var in struct {
		Payload map[string]any `json:"payload"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)

	// Record an internal note for auditability (authored by the agent).
	authorType := "User"
	_, _ = h.tickets.AddReply(r.Context(), id, fmt.Sprintf("Custom action %q was triggered.", key), &authorType, &uid, true)

	// Hand off to the host's handler, if any.
	if h.OnCustomAction != nil {
		_ = h.OnCustomAction(r.Context(), actions.CustomActionEvent{
			Ticket:   t,
			Action:   key,
			UserID:   uid,
			Payload:  in.Payload,
			Metadata: action.Metadata,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"message": "Custom action dispatched.", "action": key})
}

// AssignTicket handles assigning a ticket to an agent.
func (h *AgentHandler) AssignTicket(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	var in struct {
		AgentID models.UserID `json:"agent_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid input", http.StatusBadRequest)
		return
	}

	uid := h.userID(r)
	if err := h.assignments.Reassign(r.Context(), id, in.AgentID, &uid); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "assigned"})
}

// Reply handles an agent adding a reply or internal note.
func (h *AgentHandler) Reply(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	var in struct {
		Body       string `json:"body"`
		IsInternal bool   `json:"is_internal"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Body == "" {
		http.Error(w, "body is required", http.StatusBadRequest)
		return
	}

	uid := h.userID(r)
	authorType := "User"

	reply, err := h.tickets.AddReply(r.Context(), id, in.Body, &authorType, &uid, in.IsInternal)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"reply": reply})
}

// ChangeStatus handles updating a ticket's status.
func (h *AgentHandler) ChangeStatus(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	var in struct {
		Status int `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid input", http.StatusBadRequest)
		return
	}

	uid := h.userID(r)
	if err := h.tickets.ChangeStatus(r.Context(), id, in.Status, &uid); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}
