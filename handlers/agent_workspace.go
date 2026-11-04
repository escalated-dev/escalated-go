package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/escalated-dev/escalated-go/internal/sqldialect"
	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/services"
	"github.com/escalated-dev/escalated-go/ticketaccess"
)

// What the agent ticket page needs beyond the ticket itself (escalated-laravel
// Agent\TicketController): the agents the ticket can be reassigned to, the
// requester's other tickets, and snoozing from the ticket screen.

// assignableAgents is the agents prop of Escalated/Agent/TicketShow: the
// host's agent list (SkillAgentDirectory, as the admin pages use) less the
// agents who cannot see the ticket under the ticket visibility settings, as
// {id, name}.
func (h *AgentHandler) assignableAgents(r *http.Request, t *models.Ticket) []map[string]any {
	out := []map[string]any{}
	for _, a := range h.agents(r) {
		id := models.UserID(fmt.Sprint(a["id"]))
		if h.Access != nil && !h.Access.CanViewUser(r.Context(), id, t.ID, true) {
			continue
		}
		out = append(out, map[string]any{"id": a["id"], "name": a["name"]})
	}
	return out
}

// requesterTickets is the ticket's requester_tickets: the requester's five
// most recent other tickets the agent may see, newest first, matched as
// requester_ticket_count is (the requester, else a guest's email).
func (h *AgentHandler) requesterTickets(ctx context.Context, t *models.Ticket) []map[string]any {
	out := []map[string]any{}
	if h.DB == nil {
		return out
	}
	var where string
	var arg any
	switch {
	case t.RequesterID != nil && !t.RequesterID.Empty():
		where, arg = "requester_id = ?", string(*t.RequesterID)
	case t.GuestEmail != nil && *t.GuestEmail != "":
		where, arg = "guest_email = ?", *t.GuestEmail
	default:
		return out
	}
	// Through the visibility scope: the agent sees only tickets they may open.
	rows, err := ticketaccess.QueryContext(ctx, h.DB, sqldialect.Rebind(h.DB, fmt.Sprintf(
		`SELECT id FROM %s WHERE id <> ? AND %s ORDER BY created_at DESC, id DESC LIMIT 5`,
		h.queries().t("tickets"), where)), t.ID, arg)
	if err != nil {
		return out
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	_ = rows.Close()
	for _, id := range ids {
		other, err := h.store.GetTicket(ctx, id)
		if err != nil || other == nil {
			continue
		}
		out = append(out, map[string]any{
			"id":         other.ID,
			"reference":  other.Reference,
			"subject":    other.Subject,
			"status":     uiStatus(other.Status),
			"created_at": uiTimeValue(other.CreatedAt),
			"updated_at": uiTimeValue(other.UpdatedAt),
		})
	}
	return out
}

// snoozeLayouts are the date forms snoozed_until is read in: RFC 3339 (what
// the shared frontend's SnoozeButton sends), then the forms of a
// datetime-local input or a plain date, read as UTC.
var snoozeLayouts = []string{time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02"}

// parseSnoozeUntil validates snoozed_until as Laravel's SnoozeTicketRequest
// does (required, a date, after now).
func parseSnoozeUntil(raw string) (time.Time, string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, requiredMessage("snoozed until")
	}
	for _, layout := range snoozeLayouts {
		if until, err := time.Parse(layout, raw); err == nil {
			if !until.After(time.Now()) {
				return time.Time{}, "The snoozed until field must be a date after now."
			}
			return until, ""
		}
	}
	return time.Time{}, "The snoozed until field must be a valid date."
}

// Snooze handles POST /agent/tickets/{id}/snooze (and the admin route) with
// snoozed_until, a future date: the ticket is snoozed until then
// (services.SnoozeService), and wakes with its status restored.
func (h *AgentHandler) Snooze(w http.ResponseWriter, r *http.Request) {
	t, in, ok := h.writeInput(w, r)
	if !ok {
		return
	}
	until, msg := parseSnoozeUntil(in.Get("snoozed_until"))
	if msg == "" && !t.IsOpen() && t.Status != models.StatusSnoozed {
		msg = "A " + t.StatusString() + " ticket cannot be snoozed."
	}
	if msg != "" {
		h.invalid(w, r, t, map[string]string{"snoozed_until": msg})
		return
	}
	uid := h.userID(r)
	var by *models.UserID
	if !uid.Empty() {
		by = &uid
	}
	snooze := services.NewSnoozeService(h.store)
	if t.Status == models.StatusSnoozed {
		// Snoozed already: the new time replaces the old one, and the
		// status it wakes to is kept.
		if err := snooze.UnsnoozeTicket(r.Context(), t.ID, by); err != nil {
			h.failed(w, "snooze the ticket")
			return
		}
	}
	if err := snooze.SnoozeTicket(r.Context(), t.ID, until, by); err != nil {
		h.failed(w, "snooze the ticket")
		return
	}
	h.done(w, r, t, http.StatusOK, map[string]string{"status": "snoozed", "snoozed_until": until.UTC().Format(time.RFC3339)})
}

// Unsnooze handles POST /agent/tickets/{id}/unsnooze (and the admin route):
// the snooze ends now and the ticket goes back to the status it had. A
// ticket that is not snoozed is left as it is.
func (h *AgentHandler) Unsnooze(w http.ResponseWriter, r *http.Request) {
	t, ok := h.pageTicket(w, r)
	if !ok {
		return
	}
	if t.Status == models.StatusSnoozed {
		uid := h.userID(r)
		var by *models.UserID
		if !uid.Empty() {
			by = &uid
		}
		if err := services.NewSnoozeService(h.store).UnsnoozeTicket(r.Context(), t.ID, by); err != nil {
			h.failed(w, "unsnooze the ticket")
			return
		}
	}
	h.done(w, r, t, http.StatusOK, map[string]string{"status": "unsnoozed"})
}
