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
}

// NewAssignmentService creates a new AssignmentService.
func NewAssignmentService(s store.Store) *AssignmentService {
	return &AssignmentService{store: s}
}

// Unassign removes the assignee from a ticket.
func (as *AssignmentService) Unassign(ctx context.Context, ticketID int64, causerID *int64) error {
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
func (as *AssignmentService) Reassign(ctx context.Context, ticketID, newAgentID int64, causerID *int64) error {
	t, err := as.store.GetTicket(ctx, ticketID)
	if err != nil {
		return err
	}
	if t == nil {
		return fmt.Errorf("ticket %d not found", ticketID)
	}

	t.AssignedTo = &newAgentID
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
	return nil
}
