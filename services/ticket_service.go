// Package services contains business logic that sits between handlers and the store.
package services

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/store"
)

// TicketService handles ticket lifecycle operations.
type TicketService struct {
	store store.Store
}

// NewTicketService creates a new TicketService.
func NewTicketService(s store.Store) *TicketService {
	return &TicketService{store: s}
}

// CreateTicketInput holds the fields needed to create a ticket.
type CreateTicketInput struct {
	Subject       string
	Description   string
	Priority      int
	TicketType    string
	RequesterType *string
	RequesterID   *int64
	GuestName     *string
	GuestEmail    *string
	DepartmentID  *int64
}

// Create creates a new ticket, applies SLA targets, and records a creation activity.
func (ts *TicketService) Create(ctx context.Context, in CreateTicketInput) (*models.Ticket, error) {
	t := &models.Ticket{
		Subject:       in.Subject,
		Description:   in.Description,
		Status:        models.StatusOpen,
		Priority:      in.Priority,
		TicketType:    in.TicketType,
		RequesterType: in.RequesterType,
		RequesterID:   in.RequesterID,
		GuestName:     in.GuestName,
		GuestEmail:    in.GuestEmail,
		DepartmentID:  in.DepartmentID,
	}

	if t.TicketType == "" {
		t.TicketType = "question"
	}

	// Generate guest token for unauthenticated tickets
	if t.RequesterType == nil && t.GuestEmail != nil {
		token := models.GenerateReference("GT")
		t.GuestToken = &token
	}

	// Dedupe repeat guests by email (Pattern B): ensure a Contact row
	// exists for this email. Inline guest_* fields remain set for the
	// backwards-compat dual-read period. Any store error here is
	// non-fatal — ticket creation must never block on the Contact
	// lookup.
	//
	// TODO: once the Ticket CRUD SQL is updated to project contact_id
	// through every SELECT/INSERT, set t.ContactID = &contact.ID here.
	// Tracked as a follow-up alongside the inline-columns deprecation.
	if t.RequesterType == nil && t.GuestEmail != nil && *t.GuestEmail != "" {
		name := ""
		if t.GuestName != nil {
			name = *t.GuestName
		}
		_, _ = ts.resolveContact(ctx, *t.GuestEmail, name)
	}

	// Apply SLA policy
	if err := ts.applySLA(ctx, t); err != nil {
		// SLA errors are non-fatal — log and continue
		_ = err
	}

	if err := ts.store.CreateTicket(ctx, t); err != nil {
		return nil, fmt.Errorf("creating ticket: %w", err)
	}

	// Record activity
	_ = ts.store.CreateActivity(ctx, &models.Activity{
		TicketID: t.ID,
		Action:   models.ActionTicketCreated,
	})

	return t, nil
}

// Get retrieves a ticket by ID with its tags loaded.
func (ts *TicketService) Get(ctx context.Context, id int64) (*models.Ticket, error) {
	t, err := ts.store.GetTicket(ctx, id)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, nil
	}
	tags, err := ts.store.GetTicketTags(ctx, id)
	if err == nil {
		deref := make([]models.Tag, len(tags))
		for i, tg := range tags {
			deref[i] = *tg
		}
		t.Tags = deref
	}
	return t, nil
}

// Assign assigns a ticket to an agent and records the activity.
func (ts *TicketService) Assign(ctx context.Context, ticketID, agentID int64, causerID *int64) error {
	t, err := ts.store.GetTicket(ctx, ticketID)
	if err != nil {
		return err
	}
	if t == nil {
		return fmt.Errorf("ticket %d not found", ticketID)
	}

	t.AssignedTo = &agentID
	if t.Status == models.StatusOpen {
		t.Status = models.StatusInProgress
	}

	if err := ts.store.UpdateTicket(ctx, t); err != nil {
		return err
	}

	var causerType *string
	if causerID != nil {
		ct := "User"
		causerType = &ct
	}
	_ = ts.store.CreateActivity(ctx, &models.Activity{
		TicketID:   ticketID,
		Action:     models.ActionTicketAssigned,
		CauserType: causerType,
		CauserID:   causerID,
	})
	return nil
}

// ChangeStatus updates a ticket's status and records the activity.
func (ts *TicketService) ChangeStatus(ctx context.Context, ticketID int64, newStatus int, causerID *int64) error {
	t, err := ts.store.GetTicket(ctx, ticketID)
	if err != nil {
		return err
	}
	if t == nil {
		return fmt.Errorf("ticket %d not found", ticketID)
	}

	oldStatus := t.Status
	t.Status = newStatus

	now := time.Now()
	if newStatus == models.StatusResolved && t.ResolvedAt == nil {
		t.ResolvedAt = &now
	}
	if newStatus == models.StatusClosed && t.ClosedAt == nil {
		t.ClosedAt = &now
	}

	if err := ts.store.UpdateTicket(ctx, t); err != nil {
		return err
	}

	details, _ := json.Marshal(map[string]string{
		"from": models.StatusName[oldStatus],
		"to":   models.StatusName[newStatus],
	})
	var causerType *string
	if causerID != nil {
		ct := "User"
		causerType = &ct
	}
	_ = ts.store.CreateActivity(ctx, &models.Activity{
		TicketID:   ticketID,
		Action:     models.ActionStatusChanged,
		CauserType: causerType,
		CauserID:   causerID,
		Details:    details,
	})
	return nil
}

// AddReply creates a reply on a ticket and records the activity.
func (ts *TicketService) AddReply(ctx context.Context, ticketID int64, body string, authorType *string, authorID *int64, internal bool) (*models.Reply, error) {
	t, err := ts.store.GetTicket(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, fmt.Errorf("ticket %d not found", ticketID)
	}

	r := &models.Reply{
		TicketID:   ticketID,
		Body:       body,
		AuthorType: authorType,
		AuthorID:   authorID,
		IsInternal: internal,
	}

	if err := ts.store.CreateReply(ctx, r); err != nil {
		return nil, err
	}

	// Track first response
	if t.FirstResponseAt == nil && !internal && authorType != nil {
		now := time.Now()
		t.FirstResponseAt = &now
		_ = ts.store.UpdateTicket(ctx, t)
	}

	action := models.ActionReplyAdded
	if internal {
		action = models.ActionInternalNote
	}
	_ = ts.store.CreateActivity(ctx, &models.Activity{
		TicketID:   ticketID,
		Action:     action,
		CauserType: authorType,
		CauserID:   authorID,
	})

	return r, nil
}

// SplitTicketInput holds the fields needed to split a ticket from a reply.
type SplitTicketInput struct {
	TicketID int64
	ReplyID  int64
	Subject  string
	CauserID *int64
}

// SplitTicket creates a new ticket from a reply on an existing ticket.
// The new ticket copies metadata from the original and records a link via SplitFromID.
func (ts *TicketService) SplitTicket(ctx context.Context, in SplitTicketInput) (*models.Ticket, error) {
	original, err := ts.store.GetTicket(ctx, in.TicketID)
	if err != nil {
		return nil, fmt.Errorf("fetching original ticket: %w", err)
	}
	if original == nil {
		return nil, fmt.Errorf("ticket %d not found", in.TicketID)
	}

	reply, err := ts.store.GetReply(ctx, in.ReplyID)
	if err != nil {
		return nil, fmt.Errorf("fetching reply: %w", err)
	}
	if reply == nil {
		return nil, fmt.Errorf("reply %d not found", in.ReplyID)
	}
	if reply.TicketID != in.TicketID {
		return nil, fmt.Errorf("reply %d does not belong to ticket %d", in.ReplyID, in.TicketID)
	}

	subject := in.Subject
	if subject == "" {
		subject = "Split from: " + original.Subject
	}

	newTicket := &models.Ticket{
		Subject:       subject,
		Description:   reply.Body,
		Status:        models.StatusOpen,
		Priority:      original.Priority,
		TicketType:    original.TicketType,
		RequesterType: original.RequesterType,
		RequesterID:   original.RequesterID,
		GuestName:     original.GuestName,
		GuestEmail:    original.GuestEmail,
		DepartmentID:  original.DepartmentID,
		SplitFromID:   &original.ID,
	}

	if err := ts.store.CreateTicket(ctx, newTicket); err != nil {
		return nil, fmt.Errorf("creating split ticket: %w", err)
	}

	// Record activity on original ticket
	var causerType *string
	if in.CauserID != nil {
		ct := "User"
		causerType = &ct
	}
	details, _ := json.Marshal(map[string]any{
		"new_ticket_id":   newTicket.ID,
		"new_reference":   newTicket.Reference,
		"source_reply_id": in.ReplyID,
	})
	_ = ts.store.CreateActivity(ctx, &models.Activity{
		TicketID:   original.ID,
		Action:     models.ActionTicketSplit,
		CauserType: causerType,
		CauserID:   in.CauserID,
		Details:    details,
	})

	// Record activity on new ticket
	_ = ts.store.CreateActivity(ctx, &models.Activity{
		TicketID: newTicket.ID,
		Action:   models.ActionTicketCreated,
		Details:  details,
	})

	return newTicket, nil
}

func (ts *TicketService) applySLA(ctx context.Context, t *models.Ticket) error {
	// Try department default SLA, then global default
	var policy *models.SLAPolicy
	if t.DepartmentID != nil {
		dept, err := ts.store.GetDepartment(ctx, *t.DepartmentID)
		if err == nil && dept != nil && dept.DefaultSLAPolicyID != nil {
			policy, _ = ts.store.GetSLAPolicy(ctx, *dept.DefaultSLAPolicyID)
		}
	}
	if policy == nil {
		policy, _ = ts.store.GetDefaultSLAPolicy(ctx)
	}
	if policy == nil {
		return nil
	}

	t.SLAPolicyID = &policy.ID
	priority := models.PriorityName[t.Priority]

	if hours, ok := policy.FirstResponseHoursFor(priority); ok {
		due := time.Now().Add(time.Duration(hours * float64(time.Hour)))
		t.SLAFirstResponseDueAt = &due
	}
	if hours, ok := policy.ResolutionHoursFor(priority); ok {
		due := time.Now().Add(time.Duration(hours * float64(time.Hour)))
		t.SLAResolutionDueAt = &due
	}

	return nil
}

// resolveContact finds or creates a Contact by email. The email is
// normalized (trim + lowercase) before lookup. If an existing row has
// a blank name and a non-blank name is provided, the existing row's
// name is updated in place.
//
// Mirrors the Pattern B reference impl used across the other
// framework PRs (see contact model's DecideContactAction helper).
func (ts *TicketService) resolveContact(ctx context.Context, email, name string) (*models.Contact, error) {
	normalized := models.NormalizeEmail(email)
	if normalized == "" {
		return nil, nil
	}

	existing, err := ts.store.GetContactByEmail(ctx, normalized)
	if err != nil {
		return nil, err
	}

	action := models.DecideContactAction(existing, name)
	switch action {
	case models.ContactActionReturnExisting:
		return existing, nil
	case models.ContactActionUpdateName:
		if err := ts.store.UpdateContactName(ctx, existing.ID, name); err != nil {
			return existing, nil // non-fatal
		}
		existing.Name = &name
		return existing, nil
	default: // create
		c := &models.Contact{Email: normalized}
		if name != "" {
			c.Name = &name
		}
		if err := ts.store.CreateContact(ctx, c); err != nil {
			return nil, err
		}
		return c, nil
	}
}
