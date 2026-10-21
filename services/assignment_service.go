package services

import (
	"context"
	"fmt"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/store"
)

// AssignmentService handles ticket assignment logic.
type AssignmentService struct {
	store store.Store

	// Tickets, when set, announces a reassignment like TicketService.Assign
	// does (ticket.assigned to webhooks and workflows, the assignee's email,
	// ticket.updated). The routers set it.
	Tickets *TicketService
}

// NewAssignmentService creates a new AssignmentService.
func NewAssignmentService(s store.Store) *AssignmentService {
	return &AssignmentService{store: s}
}

// Unassign removes the assignee from a ticket.
func (as *AssignmentService) Unassign(ctx context.Context, ticketID int64, causerID *models.UserID) error {
	t, err := as.store.GetTicket(ctx, ticketID)
	if err != nil {
		return err
	}
	if t == nil {
		return fmt.Errorf("ticket %d not found", ticketID)
	}

	t.AssignedTo = nil
	if err := as.store.UpdateTicket(ctx, t); err != nil {
		return err
	}

	var causerType *string
	if causerID != nil {
		ct := "User"
		causerType = &ct
	}
	_ = as.store.CreateActivity(ctx, &models.Activity{
		TicketID:   ticketID,
		Action:     models.ActionTicketUnassigned,
		CauserType: causerType,
		CauserID:   causerID,
	})
	return nil
}

// Reassign changes a ticket's assignee.
func (as *AssignmentService) Reassign(ctx context.Context, ticketID int64, newAgentID models.UserID, causerID *models.UserID) error {
	if access := store.AccessFor(as.store); access != nil && !access.CanViewUser(ctx, newAgentID, ticketID, true) {
		return fmt.Errorf("agent cannot access ticket")
	}
	t, err := as.store.GetTicket(ctx, ticketID)
	if err != nil {
		return err
	}
	if t == nil {
		return fmt.Errorf("ticket %d not found", ticketID)
	}

	agentID := newAgentID
	t.AssignedTo = &agentID
	if err := as.store.UpdateTicket(ctx, t); err != nil {
		return err
	}

	var causerType *string
	if causerID != nil {
		ct := "User"
		causerType = &ct
	}
	_ = as.store.CreateActivity(ctx, &models.Activity{
		TicketID:   ticketID,
		Action:     models.ActionTicketAssigned,
		CauserType: causerType,
		CauserID:   causerID,
	})
	if as.Tickets != nil {
		as.Tickets.announceAssigned(t, agentID, causerID)
	}
	return nil
}
