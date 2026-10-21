package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/escalated-dev/escalated-go/models"
)

// ErrInvalidPriority is returned for a priority outside models.PriorityName.
var ErrInvalidPriority = fmt.Errorf("invalid priority")

// ErrInvalidStatus is returned for a status outside models.StatusName.
var ErrInvalidStatus = fmt.Errorf("invalid status")

// ErrInvalidDepartment is returned when a department does not exist.
var ErrInvalidDepartment = fmt.Errorf("invalid department")

// ChangePriority updates a ticket's priority, records the activity and fires
// ticket.priority_changed and ticket.updated (Laravel Ticket::changePriority).
func (ts *TicketService) ChangePriority(ctx context.Context, ticketID int64, priority int, causerID *models.UserID) error {
	if _, ok := models.PriorityName[priority]; !ok {
		return ErrInvalidPriority
	}
	t, err := ts.load(ctx, ticketID)
	if err != nil {
		return err
	}
	old := t.Priority
	if old == priority {
		return nil
	}
	t.Priority = priority
	if err := ts.store.UpdateTicket(ctx, t); err != nil {
		return err
	}

	ts.logActivity(ctx, ticketID, models.ActionPriorityChanged, causerID, map[string]any{
		"old_priority": models.PriorityName[old],
		"new_priority": models.PriorityName[priority],
	})

	payload := ticketWebhookPayload(t)
	payload["old_priority"] = models.PriorityName[old]
	payload["new_priority"] = models.PriorityName[priority]
	ts.emit("ticket.priority_changed", payload, t)
	ts.emitUpdated(t)
	return nil
}

// ChangeDepartment moves a ticket to another department, records the activity
// and fires ticket.department_changed and ticket.updated (Laravel
// Ticket::changeDepartment).
func (ts *TicketService) ChangeDepartment(ctx context.Context, ticketID int64, departmentID int64, causerID *models.UserID) error {
	d, err := ts.store.GetDepartment(ctx, departmentID)
	if err != nil {
		return err
	}
	if d == nil {
		return ErrInvalidDepartment
	}
	t, err := ts.load(ctx, ticketID)
	if err != nil {
		return err
	}
	var old *int64
	if t.DepartmentID != nil {
		v := *t.DepartmentID
		old = &v
	}
	if old != nil && *old == departmentID {
		return nil
	}
	newID := departmentID
	t.DepartmentID = &newID
	if err := ts.store.UpdateTicket(ctx, t); err != nil {
		return err
	}

	ts.logActivity(ctx, ticketID, models.ActionDeptChanged, causerID, map[string]any{
		"old_department_id": old,
		"new_department_id": departmentID,
	})

	payload := ticketWebhookPayload(t)
	payload["old_department_id"] = old
	payload["new_department_id"] = departmentID
	ts.emit("ticket.department_changed", payload, t)
	ts.emitUpdated(t)
	return nil
}

// Unassign clears a ticket's agent, records the activity and fires
// ticket.unassigned and ticket.updated (Laravel Ticket::unassignTicket).
func (ts *TicketService) Unassign(ctx context.Context, ticketID int64, causerID *models.UserID) error {
	t, err := ts.load(ctx, ticketID)
	if err != nil {
		return err
	}
	if t.AssignedTo == nil {
		return nil
	}
	previous := *t.AssignedTo
	t.AssignedTo = nil
	if err := ts.store.UpdateTicket(ctx, t); err != nil {
		return err
	}

	ts.logActivity(ctx, ticketID, models.ActionTicketUnassigned, causerID, map[string]any{
		"previous_agent_id": string(previous),
	})

	payload := ticketWebhookPayload(t)
	payload["previous_agent_id"] = string(previous)
	ts.emit("ticket.unassigned", payload, t)
	ts.emitUpdated(t)
	return nil
}

// AddTag attaches a tag, records the activity and fires ticket.tag_added
// (Laravel TagAddedToTicket). A tag the ticket already has is left alone and
// announces nothing.
func (ts *TicketService) AddTag(ctx context.Context, ticketID, tagID int64, causerID *models.UserID) error {
	return ts.changeTag(ctx, ticketID, tagID, causerID, true)
}

// RemoveTag detaches a tag, records the activity and fires ticket.tag_removed
// (Laravel TagRemovedFromTicket). A tag the ticket does not have announces
// nothing.
func (ts *TicketService) RemoveTag(ctx context.Context, ticketID, tagID int64, causerID *models.UserID) error {
	return ts.changeTag(ctx, ticketID, tagID, causerID, false)
}

func (ts *TicketService) changeTag(ctx context.Context, ticketID, tagID int64, causerID *models.UserID, add bool) error {
	tag, err := ts.store.GetTag(ctx, tagID)
	if err != nil {
		return err
	}
	if tag == nil {
		return fmt.Errorf("tag %d not found", tagID)
	}
	t, err := ts.load(ctx, ticketID)
	if err != nil {
		return err
	}
	current, err := ts.store.GetTicketTags(ctx, ticketID)
	if err != nil {
		return err
	}
	has := false
	for _, tg := range current {
		if tg.ID == tagID {
			has = true
			break
		}
	}
	if has == add {
		return nil
	}

	action, event := models.ActionTagsAdded, "ticket.tag_added"
	if add {
		err = ts.store.AddTagToTicket(ctx, ticketID, tagID)
	} else {
		action, event = models.ActionTagsRemoved, "ticket.tag_removed"
		err = ts.store.RemoveTagFromTicket(ctx, ticketID, tagID)
	}
	if err != nil {
		return err
	}

	ts.logActivity(ctx, ticketID, action, causerID, map[string]any{"tag_id": tagID})
	payload := ticketWebhookPayload(t)
	payload["tag"] = map[string]any{"id": tag.ID, "name": tag.Name}
	ts.emit(event, payload, t)
	return nil
}

// UpdateDetails saves subject, description and ticket type edits (nil leaves
// a field alone) and fires ticket.updated when anything changed.
func (ts *TicketService) UpdateDetails(ctx context.Context, ticketID int64, subject, description, ticketType *string, causerID *models.UserID) (*models.Ticket, error) {
	t, err := ts.load(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	changed := false
	set := func(dst *string, v *string) {
		if v != nil && *dst != *v {
			*dst = *v
			changed = true
		}
	}
	set(&t.Subject, subject)
	set(&t.Description, description)
	set(&t.TicketType, ticketType)
	if !changed {
		return t, nil
	}
	if err := ts.store.UpdateTicket(ctx, t); err != nil {
		return nil, err
	}
	ts.logActivity(ctx, ticketID, models.ActionTicketUpdated, causerID, nil)
	ts.emitUpdated(t)
	return t, nil
}

func (ts *TicketService) load(ctx context.Context, ticketID int64) (*models.Ticket, error) {
	t, err := ts.store.GetTicket(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, fmt.Errorf("ticket %d not found", ticketID)
	}
	return t, nil
}

func (ts *TicketService) logActivity(ctx context.Context, ticketID int64, action string, causerID *models.UserID, details map[string]any) {
	var causerType *string
	if causerID != nil {
		ct := "User"
		causerType = &ct
	}
	var raw models.JSONText
	if details != nil {
		raw, _ = json.Marshal(details)
	}
	_ = ts.store.CreateActivity(ctx, &models.Activity{
		TicketID:   ticketID,
		Action:     action,
		CauserType: causerType,
		CauserID:   causerID,
		Details:    raw,
	})
}

// emit fires an event to both sinks: webhooks and event-driven workflows.
func (ts *TicketService) emit(event string, payload map[string]any, t *models.Ticket) {
	ts.dispatchWebhook(event, payload)
	ts.runWorkflows(event, t)
}

// emitUpdated fires ticket.updated, which Laravel raises on every ticket save.
func (ts *TicketService) emitUpdated(t *models.Ticket) {
	ts.emit("ticket.updated", ticketWebhookPayload(t), t)
}

// StatusFromValue reads a status given as a number or as its slug
// ("resolved"), reporting false for anything unknown. A slug used to scan as
// 0 (open) and silently reopen the ticket.
func StatusFromValue(v any) (int, bool) {
	return enumFromValue(v, models.StatusName)
}

// PriorityFromValue reads a priority given as a number or as its slug.
func PriorityFromValue(v any) (int, bool) {
	return enumFromValue(v, models.PriorityName)
}

func enumFromValue(v any, names map[int]string) (int, bool) {
	var n int
	switch x := v.(type) {
	case int:
		n = x
	case int64:
		n = int(x)
	case float64:
		if x != float64(int(x)) {
			return 0, false
		}
		n = int(x)
	case string:
		s := strings.TrimSpace(x)
		if parsed, err := strconv.Atoi(s); err == nil {
			n = parsed
			break
		}
		for k, name := range names {
			if strings.EqualFold(name, s) {
				return k, true
			}
		}
		return 0, false
	default:
		return 0, false
	}
	_, ok := names[n]
	return n, ok
}
