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
