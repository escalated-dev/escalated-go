package services

import (
	"context"
	"time"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/store"
)

// SLAService handles SLA breach detection and policy management.
type SLAService struct {
	store store.Store
}

// NewSLAService creates a new SLAService.
func NewSLAService(s store.Store) *SLAService {
	return &SLAService{store: s}
}

// CheckBreaches scans open tickets for SLA breaches and marks them.
// Call this periodically (e.g., every minute via a ticker goroutine).
func (ss *SLAService) CheckBreaches(ctx context.Context) (int, error) {
	// Fetch open tickets that have SLA deadlines but aren't already marked breached
	breached := 0
	f := models.TicketFilters{Limit: 200}
	// We'll scan all open statuses
	for _, status := range []int{
		models.StatusOpen, models.StatusInProgress,
		models.StatusWaitingOnCustomer, models.StatusWaitingOnAgent,
		models.StatusEscalated, models.StatusReopened,
	} {
		s := status
		f.Status = &s
		tickets, _, err := ss.store.ListTickets(ctx, f)
		if err != nil {
			return breached, err
		}
		for _, t := range tickets {
			if t.SLABreached {
				continue
			}
			now := time.Now()
			firstBreached := t.SLAFirstResponseDueAt != nil && t.FirstResponseAt == nil && now.After(*t.SLAFirstResponseDueAt)
			resBreached := t.SLAResolutionDueAt != nil && t.ResolvedAt == nil && now.After(*t.SLAResolutionDueAt)

			if firstBreached || resBreached {
				t.SLABreached = true
				if err := ss.store.UpdateTicket(ctx, t); err != nil {
					continue
				}
				breachType := "first_response"
				if resBreached {
					breachType = "resolution"
				}
				details := []byte(`{"breach_type":"` + breachType + `"}`)
				_ = ss.store.CreateActivity(ctx, &models.Activity{
					TicketID: t.ID,
					Action:   models.ActionSLABreached,
					Details:  details,
				})
				breached++
			}
		}
	}
	return breached, nil
}

// ApplyPolicy applies an SLA policy to a ticket based on its priority.
func (ss *SLAService) ApplyPolicy(ctx context.Context, ticketID, policyID int64) error {
	t, err := ss.store.GetTicket(ctx, ticketID)
	if err != nil || t == nil {
		return err
	}
	policy, err := ss.store.GetSLAPolicy(ctx, policyID)
	if err != nil || policy == nil {
		return err
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

	return ss.store.UpdateTicket(ctx, t)
}
