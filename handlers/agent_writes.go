package handlers

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/escalated-dev/escalated-go/actions"
	"github.com/escalated-dev/escalated-go/models"
)

// The agent ticket screen's writes (Laravel Agent\TicketController). Each one
// takes JSON or a form, and the ticket by reference or id. A page visit
// (X-Inertia) is answered with a 303 back to the page it came from, or the
// ticket screen again with an errors prop; an API client gets JSON.

func (h *AgentHandler) showURL(t *models.Ticket) string {
	prefix := h.RoutePrefix
	if prefix == "" {
		prefix = "/escalated"
	}
	return prefix + "/agent/tickets/" + url.PathEscape(t.Reference)
}

// done ends a successful write.
func (h *AgentHandler) done(w http.ResponseWriter, r *http.Request, t *models.Ticket, code int, body any) {
	if isInertia(r) {
		backTo(w, r, h.showURL(t))
		return
	}
	writeJSON(w, code, body)
}

// invalid ends a write that failed validation.
func (h *AgentHandler) invalid(w http.ResponseWriter, r *http.Request, t *models.Ticket, errs map[string]string) {
	validationErrors(w, r, errs, func(errs map[string]string) { h.renderShow(w, r, t, errs) })
}

// writeInput loads the ticket and reads the write's fields.
func (h *AgentHandler) writeInput(w http.ResponseWriter, r *http.Request) (*models.Ticket, url.Values, bool) {
	t, ok := h.pageTicket(w, r)
	if !ok {
		return nil, nil, false
	}
	in, err := readInput(r)
	if err != nil {
		http.Error(w, "invalid input", http.StatusBadRequest)
		return nil, nil, false
	}
	return t, in, true
}

func (h *AgentHandler) failed(w http.ResponseWriter, what string) {
	http.Error(w, "could not "+what, http.StatusInternalServerError)
}

// CustomAction handles POST /agent/tickets/{id}/actions/{action}.
func (h *AgentHandler) CustomAction(w http.ResponseWriter, r *http.Request) {
	if h.Actions == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	t, ok := h.pageTicket(w, r)
	if !ok {
		return
	}

	key := pathParam(r, "action")
	uid := h.userID(r)
	action, found := h.Actions.Find(key)
	if !found || !h.Actions.Visible(action, t, uid) {
		http.Error(w, "custom action not found", http.StatusNotFound)
		return
	}
	if !h.Actions.Enabled(action, t, uid) {
		http.Error(w, "custom action is not enabled", http.StatusForbidden)
		return
	}

	var payload map[string]any
	if in, err := readInput(r); err == nil && in.Get("payload") != "" {
		payload = decodePayload(in.Get("payload"))
	}

	// Record an internal note for auditability (authored by the agent).
	authorType := "User"
	_, _ = h.tickets.AddReply(r.Context(), t.ID, fmt.Sprintf("Custom action %q was triggered.", key), &authorType, &uid, true)

	// Hand off to the host's handler, if any.
	if h.OnCustomAction != nil {
		_ = h.OnCustomAction(r.Context(), actions.CustomActionEvent{
			Ticket:   t,
			Action:   key,
			UserID:   uid,
			Payload:  payload,
			Metadata: action.Metadata,
		})
	}

	h.done(w, r, t, http.StatusOK, map[string]any{"message": "Custom action dispatched.", "action": key})
}

// AssignTicket handles POST /agent/tickets/{id}/assign with agent_id. An empty
// agent_id unassigns.
func (h *AgentHandler) AssignTicket(w http.ResponseWriter, r *http.Request) {
	t, in, ok := h.writeInput(w, r)
	if !ok {
		return
	}
	uid := h.userID(r)
	agentID := models.UserID(strings.TrimSpace(in.Get("agent_id")))
	if agentID.Empty() || agentID == "null" {
		if isInertia(r) {
			h.invalid(w, r, t, map[string]string{"agent_id": requiredMessage("agent id")})
			return
		}
		if err := h.tickets.Unassign(r.Context(), t.ID, &uid); err != nil {
			h.failed(w, "unassign the ticket")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "unassigned"})
		return
	}
	if err := h.assignments.Reassign(r.Context(), t.ID, agentID, &uid); err != nil {
		h.failed(w, "assign the ticket")
		return
	}
	h.done(w, r, t, http.StatusOK, map[string]string{"status": "assigned"})
}

// Reply handles POST /agent/tickets/{id}/replies: a reply, or an internal note
// with is_internal.
func (h *AgentHandler) Reply(w http.ResponseWriter, r *http.Request) {
	h.addReply(w, r, nil)
}

// PageReply handles POST /agent/tickets/{id}/reply (escalated.agent.tickets.reply).
func (h *AgentHandler) PageReply(w http.ResponseWriter, r *http.Request) {
	internal := false
	h.addReply(w, r, &internal)
}

// PageNote handles POST /agent/tickets/{id}/note (escalated.agent.tickets.note).
func (h *AgentHandler) PageNote(w http.ResponseWriter, r *http.Request) {
	internal := true
	h.addReply(w, r, &internal)
}

func (h *AgentHandler) addReply(w http.ResponseWriter, r *http.Request, internal *bool) {
	t, in, ok := h.writeInput(w, r)
	if !ok {
		return
	}
	if errs := rejectUploads(r); errs != nil {
		h.invalid(w, r, t, errs)
		return
	}
	body := strings.TrimSpace(in.Get("body"))
	if body == "" {
		h.invalid(w, r, t, map[string]string{"body": requiredMessage("body")})
		return
	}
	isInternal := truthy(in.Get("is_internal")) || truthy(in.Get("is_internal_note"))
	if internal != nil {
		isInternal = *internal
	}

	uid := h.userID(r)
	authorType := "User"
	reply, err := h.tickets.AddReply(r.Context(), t.ID, body, &authorType, &uid, isInternal)
	if err != nil {
		h.failed(w, "add the reply")
		return
	}
	h.done(w, r, t, http.StatusCreated, map[string]any{"reply": reply})
}

// ChangeStatus handles POST /agent/tickets/{id}/status with status as a slug
// ("resolved") or the integer.
func (h *AgentHandler) ChangeStatus(w http.ResponseWriter, r *http.Request) {
	t, in, ok := h.writeInput(w, r)
	if !ok {
		return
	}
	status, valid := parseStatus(in.Get("status"))
	if !valid {
		h.invalid(w, r, t, map[string]string{"status": "The selected status is invalid."})
		return
	}
	uid := h.userID(r)
	if err := h.tickets.ChangeStatus(r.Context(), t.ID, status, &uid); err != nil {
		h.failed(w, "change the status")
		return
	}
	h.done(w, r, t, http.StatusOK, map[string]string{"status": "updated"})
}

// ChangePriority handles POST /agent/tickets/{id}/priority with priority as a
// slug ("urgent") or the integer.
func (h *AgentHandler) ChangePriority(w http.ResponseWriter, r *http.Request) {
	t, in, ok := h.writeInput(w, r)
	if !ok {
		return
	}
	priority, valid := parsePriority(in.Get("priority"))
	if !valid {
		h.invalid(w, r, t, map[string]string{"priority": "The selected priority is invalid."})
		return
	}
	uid := h.userID(r)
	if err := h.tickets.ChangePriority(r.Context(), t.ID, priority, &uid); err != nil {
		h.failed(w, "change the priority")
		return
	}
	h.done(w, r, t, http.StatusOK, map[string]string{"status": "updated"})
}

// ChangeDepartment handles POST /agent/tickets/{id}/department with
// department_id.
func (h *AgentHandler) ChangeDepartment(w http.ResponseWriter, r *http.Request) {
	t, in, ok := h.writeInput(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(strings.TrimSpace(in.Get("department_id")), 10, 64)
	var dept *models.Department
	if err == nil {
		dept, _ = h.store.GetDepartment(r.Context(), id)
	}
	if dept == nil {
		h.invalid(w, r, t, map[string]string{"department_id": "The selected department is invalid."})
		return
	}
	uid := h.userID(r)
	if err := h.tickets.ChangeDepartment(r.Context(), t.ID, id, &uid); err != nil {
		h.failed(w, "change the department")
		return
	}
	h.done(w, r, t, http.StatusOK, map[string]string{"status": "updated"})
}

// UpdateTags handles POST /agent/tickets/{id}/tags: tag_ids is the full set,
// so tags not in it are removed (Laravel TicketController::tags).
func (h *AgentHandler) UpdateTags(w http.ResponseWriter, r *http.Request) {
	t, in, ok := h.writeInput(w, r)
	if !ok {
		return
	}
	want := map[int64]bool{}
	for _, key := range []string{"tag_ids", "tag_ids[]"} {
		for _, v := range in[key] {
			for _, part := range strings.Split(strings.Trim(v, "[]"), ",") {
				part = strings.TrimSpace(part)
				if part == "" {
					continue
				}
				id, err := strconv.ParseInt(part, 10, 64)
				if err != nil {
					h.invalid(w, r, t, map[string]string{"tag_ids": "The selected tags are invalid."})
					return
				}
				if tag, _ := h.store.GetTag(r.Context(), id); tag == nil {
					h.invalid(w, r, t, map[string]string{"tag_ids": "The selected tags are invalid."})
					return
				}
				want[id] = true
			}
		}
	}

	current, err := h.store.GetTicketTags(r.Context(), t.ID)
	if err != nil {
		h.failed(w, "load the tags")
		return
	}
	uid := h.userID(r)
	have := map[int64]bool{}
	for _, tag := range current {
		have[tag.ID] = true
		if !want[tag.ID] {
			if err := h.tickets.RemoveTag(r.Context(), t.ID, tag.ID, &uid); err != nil {
				h.failed(w, "remove a tag")
				return
			}
		}
	}
	for id := range want {
		if !have[id] {
			if err := h.tickets.AddTag(r.Context(), t.ID, id, &uid); err != nil {
				h.failed(w, "add a tag")
				return
			}
		}
	}
	h.done(w, r, t, http.StatusOK, map[string]string{"status": "updated"})
}

// ApplyMacro handles POST /agent/tickets/{id}/macro with macro_id: one of the
// macros this agent may use.
func (h *AgentHandler) ApplyMacro(w http.ResponseWriter, r *http.Request) {
	t, in, ok := h.writeInput(w, r)
	if !ok {
		return
	}
	if h.Macros == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	uid := h.userID(r)
	id, err := strconv.ParseInt(strings.TrimSpace(in.Get("macro_id")), 10, 64)
	var macro *models.Macro
	if err == nil {
		if mine, err := h.Macros.ListForAgent(uid); err == nil {
			for i := range mine {
				if mine[i].ID == id {
					macro = &mine[i]
				}
			}
		}
	}
	if macro == nil {
		h.invalid(w, r, t, map[string]string{"macro_id": "The selected macro is invalid."})
		return
	}
	if err := h.Macros.ApplyContext(r.Context(), macro, t.ID, uid); err != nil {
		h.failed(w, "apply the macro")
		return
	}
	h.done(w, r, t, http.StatusOK, map[string]any{"status": "applied", "macro": macro.Name})
}

// ToggleFollow handles POST /agent/tickets/{id}/follow: follows the ticket,
// or stops following it.
func (h *AgentHandler) ToggleFollow(w http.ResponseWriter, r *http.Request) {
	t, ok := h.pageTicket(w, r)
	if !ok {
		return
	}
	uid := h.userID(r)
	if uid.Empty() {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	q := h.queries()
	following := q.isFollowing(r.Context(), t.ID, uid)
	var err error
	if following {
		err = q.unfollow(r.Context(), t.ID, uid)
	} else {
		err = q.follow(r.Context(), t.ID, uid)
	}
	if err != nil {
		h.failed(w, "update following")
		return
	}
	h.done(w, r, t, http.StatusOK, map[string]any{"is_following": !following})
}

// TogglePin handles POST /agent/tickets/{id}/replies/{reply}/pin: pins an
// internal note, or unpins it. Only internal notes can be pinned.
func (h *AgentHandler) TogglePin(w http.ResponseWriter, r *http.Request) {
	t, ok := h.pageTicket(w, r)
	if !ok {
		return
	}
	replyID, err := strconv.ParseInt(pathParam(r, "reply"), 10, 64)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	reply, err := h.store.GetReply(r.Context(), replyID)
	if err != nil || reply == nil || reply.TicketID != t.ID {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if !reply.IsInternal {
		h.invalid(w, r, t, map[string]string{"reply": "Only internal notes can be pinned."})
		return
	}
	reply.IsPinned = !reply.IsPinned
	if err := h.store.UpdateReply(r.Context(), reply); err != nil {
		h.failed(w, "pin the note")
		return
	}
	h.done(w, r, t, http.StatusOK, map[string]any{"is_pinned": reply.IsPinned})
}

// UpdateTicket handles PUT /agent/tickets/{id}: subject, description and
// ticket_type (Laravel TicketController::update).
func (h *AgentHandler) UpdateTicket(w http.ResponseWriter, r *http.Request) {
	t, in, ok := h.writeInput(w, r)
	if !ok {
		return
	}
	field := func(key string) *string {
		if !in.Has(key) {
			return nil
		}
		v := strings.TrimSpace(in.Get(key))
		return &v
	}
	subject, description, ticketType := field("subject"), field("description"), field("ticket_type")
	errs := map[string]string{}
	if subject != nil && (*subject == "" || len(*subject) > 255) {
		errs["subject"] = "The subject must be between 1 and 255 characters."
	}
	if ticketType != nil && !validTicketType(*ticketType) {
		errs["ticket_type"] = "The selected ticket type is invalid."
	}
	if len(errs) > 0 {
		h.invalid(w, r, t, errs)
		return
	}
	uid := h.userID(r)
	updated, err := h.tickets.UpdateDetails(r.Context(), t.ID, subject, description, ticketType, &uid)
	if err != nil {
		h.failed(w, "update the ticket")
		return
	}
	h.done(w, r, t, http.StatusOK, map[string]any{"ticket": updated})
}

func validTicketType(v string) bool {
	for _, t := range models.TicketTypes {
		if t == v {
			return true
		}
	}
	return false
}
