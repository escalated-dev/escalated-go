package plugins

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/escalated-dev/escalated-go/models"
)

// TicketEvent delivers a canonical ticket event (services.PluginEventSink)
// to active plugins' actions, in the background: the ticket flat, plus
// "ticket" (the same object) and the event's own keys. chat.* events carry
// {session} instead. A slow or failing plugin never reaches the request that
// caused the event.
func (h *Host) TicketEvent(event string, t *models.Ticket, extra map[string]any) {
	if h == nil || t == nil || !h.hasPlugins() {
		return
	}
	var payload map[string]any
	if strings.HasPrefix(event, "chat.") {
		payload = map[string]any{"session": extra["chat"]}
	} else {
		ticket := ticketObject(t)
		payload = flatWith(ticket, "ticket", ticket)
		for k, v := range extra {
			payload[k] = v
		}
	}
	go h.DispatchAction(context.Background(), event, payload)
}

// ReplyEvent delivers reply.created or note.created: the reply flat, plus
// "reply" and "ticket".
func (h *Host) ReplyEvent(event string, t *models.Ticket, r *models.Reply) {
	if h == nil || t == nil || r == nil || !h.hasPlugins() {
		return
	}
	reply := replyObject(r)
	payload := flatWith(reply, "reply", reply)
	payload["ticket"] = ticketObject(t)
	go h.DispatchAction(context.Background(), event, payload)
}

func (h *Host) hasPlugins() bool {
	return len(h.native) > 0 || h.bridge != nil
}

func flatWith(object map[string]any, key string, value any) map[string]any {
	out := make(map[string]any, len(object)+1)
	for k, v := range object {
		out[k] = v
	}
	out[key] = value
	return out
}

func isoTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}

func userIDValue(id *models.UserID) any {
	if id == nil || id.Empty() {
		return nil
	}
	return *id
}

// jsonShaped gives an object the shape plugins get over JSON: numbers as
// float64, no Go pointers.
func jsonShaped(m map[string]any) map[string]any {
	v, err := roundTrip(m)
	if err != nil {
		return m
	}
	if out := asRecord(v); out != nil {
		return out
	}
	return m
}

// ticketObject is a ticket as plugins see it: snake_case, with status and
// priority as slugs and title beside subject (the SDK's Ticket reads title).
func ticketObject(t *models.Ticket) map[string]any {
	var metadata any = map[string]any{}
	if len(t.Metadata) > 0 {
		var m any
		if json.Unmarshal(t.Metadata, &m) == nil && m != nil {
			metadata = m
		}
	}
	var channel any
	if t.Channel != nil {
		channel = *t.Channel
	}
	tags := []any{}
	for _, tag := range t.Tags {
		tags = append(tags, map[string]any{"id": tag.ID, "name": tag.Name, "slug": tag.Slug})
	}
	return jsonShaped(map[string]any{
		"id":             t.ID,
		"reference":      t.Reference,
		"subject":        t.Subject,
		"title":          t.Subject,
		"description":    t.Description,
		"status":         models.StatusName[t.Status],
		"priority":       models.PriorityName[t.Priority],
		"ticket_type":    t.TicketType,
		"channel":        channel,
		"department_id":  t.DepartmentID,
		"assigned_to":    userIDValue(t.AssignedTo),
		"requester_id":   userIDValue(t.RequesterID),
		"requester_type": t.RequesterType,
		"contact_id":     t.ContactID,
		"metadata":       metadata,
		"tags":           tags,
		"created_at":     isoTime(t.CreatedAt),
		"updated_at":     isoTime(t.UpdatedAt),
	})
}

// replyObject is a reply as plugins see it.
func replyObject(r *models.Reply) map[string]any {
	kind := "reply"
	switch {
	case r.IsSystem:
		kind = "system"
	case r.IsInternal:
		kind = "note"
	}
	return jsonShaped(map[string]any{
		"id":          r.ID,
		"ticket_id":   r.TicketID,
		"body":        r.Body,
		"type":        kind,
		"is_internal": r.IsInternal,
		"user_id":     userIDValue(r.AuthorID),
		"created_at":  isoTime(r.CreatedAt),
		"updated_at":  isoTime(r.UpdatedAt),
	})
}
