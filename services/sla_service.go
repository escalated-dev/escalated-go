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

	// Tickets, when set, announces sla.breached and sla.warning to webhooks
	// and workflows through the ticket service's sinks. Without it the checks
	// still mark breaches but announce nothing.
	Tickets *TicketService

	// Now returns the current time (time.Now when nil). Tests override it.
	Now func() time.Time
}

// DefaultSLAWarningMinutes is how far ahead of a deadline sla.warning fires
// (Laravel SlaService::checkWarnings default).
const DefaultSLAWarningMinutes = 30

// NewSLAService creates a new SLAService.
func NewSLAService(s store.Store) *SLAService {
	return &SLAService{store: s}
}

// slaOpenStatuses are the statuses an SLA clock runs in.
var slaOpenStatuses = []int{
	models.StatusOpen, models.StatusInProgress,
	models.StatusWaitingOnCustomer, models.StatusWaitingOnAgent,
	models.StatusEscalated, models.StatusReopened,
}

// Check runs CheckBreaches and then CheckWarnings with the given window
// (DefaultSLAWarningMinutes when <= 0). It is the entry point a host
// scheduler calls, every minute being the convention (Laravel
// escalated:check-sla). Escalated.CheckSLA and Escalated.StartSLAChecker wrap
// it with the package's webhook and workflow sinks wired.
func (ss *SLAService) Check(ctx context.Context, warningMinutes int) (breaches, warnings int, err error) {
	breaches, err = ss.CheckBreaches(ctx)
	if err != nil {
		return breaches, 0, err
	}
	warnings, err = ss.CheckWarnings(ctx, warningMinutes)
	return breaches, warnings, err
}

func (ss *SLAService) now() time.Time {
	if ss.Now != nil {
		return ss.Now()
	}
	return time.Now()
}

func (ss *SLAService) openTickets(ctx context.Context, fn func(*models.Ticket) error) error {
	for _, status := range slaOpenStatuses {
		s := status
		tickets, _, err := ss.store.ListTickets(ctx, models.TicketFilters{Status: &s, Limit: 200})
		if err != nil {
			return err
		}
		for _, t := range tickets {
			if err := fn(t); err != nil {
				return err
			}
		}
	}
	return nil
}

// CheckBreaches scans open tickets for SLA breaches, marks them, records an
// activity and fires sla.breached once per ticket (the breach flag keeps a
// ticket from being announced twice).
func (ss *SLAService) CheckBreaches(ctx context.Context) (int, error) {
	breached := 0
	now := ss.now()
	err := ss.openTickets(ctx, func(t *models.Ticket) error {
		if t.SLABreached {
			return nil
		}
		firstBreached := t.SLAFirstResponseDueAt != nil && t.FirstResponseAt == nil && now.After(*t.SLAFirstResponseDueAt)
		resBreached := t.SLAResolutionDueAt != nil && t.ResolvedAt == nil && now.After(*t.SLAResolutionDueAt)
		if !firstBreached && !resBreached {
			return nil
		}
		t.SLABreached = true
		if err := ss.store.UpdateTicket(ctx, t); err != nil {
			return nil
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
		if ss.Tickets != nil {
			payload := ticketWebhookPayload(t)
			payload["type"] = breachType
			ss.Tickets.emit("sla.breached", payload, t)
			if ss.Tickets.Notifications != nil {
				ss.Tickets.Notifications.SLABreached(t, breachType)
			}
		}
		breached++
		return nil
	})
	return breached, err
}

// CheckWarnings fires sla.warning for every open, unbreached ticket whose
// first-response or resolution deadline falls within the next warningMinutes
// (DefaultSLAWarningMinutes when <= 0). Like Laravel's checkWarnings it keeps
// no record of having warned: each run inside the window warns again, so pick
// the schedule with that in mind.
func (ss *SLAService) CheckWarnings(ctx context.Context, warningMinutes int) (int, error) {
	if warningMinutes <= 0 {
		warningMinutes = DefaultSLAWarningMinutes
	}
	warned := 0
	now := ss.now()
	threshold := now.Add(time.Duration(warningMinutes) * time.Minute)
	within := func(due *time.Time) bool {
		return due != nil && !due.Before(now) && !due.After(threshold)
	}
	err := ss.openTickets(ctx, func(t *models.Ticket) error {
		if t.SLABreached {
			return nil
		}
		for _, c := range []struct {
			kind string
			due  *time.Time
			ok   bool
		}{
			{"first_response", t.SLAFirstResponseDueAt, t.FirstResponseAt == nil},
			{"resolution", t.SLAResolutionDueAt, t.ResolvedAt == nil},
		} {
			if !c.ok || !within(c.due) {
				continue
			}
			if ss.Tickets != nil {
				payload := ticketWebhookPayload(t)
				payload["type"] = c.kind
				payload["minutes_remaining"] = int(c.due.Sub(now).Minutes())
				ss.Tickets.emit("sla.warning", payload, t)
			}
			warned++
		}
		return nil
	})
	return warned, err
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
	base := time.Now()
	if access := store.AccessFor(ss.store); access != nil && access.Options.TargetedSLAs {
		base = t.CreatedAt
		t.SLAFirstResponseDueAt = nil
		t.SLAResolutionDueAt = nil
	}

	if hours, ok := policy.FirstResponseHoursFor(priority); ok {
		due := base.Add(time.Duration(hours * float64(time.Hour)))
		t.SLAFirstResponseDueAt = &due
	}
	if hours, ok := policy.ResolutionHoursFor(priority); ok {
		due := base.Add(time.Duration(hours * float64(time.Hour)))
		t.SLAResolutionDueAt = &due
	}

	return ss.store.UpdateTicket(ctx, t)
}

// ApplyDefaultPolicy explicitly reselects a policy, retaining the creation clock in targeted mode.
func (ss *SLAService) ApplyDefaultPolicy(ctx context.Context, ticketID int64) error {
	ticket, err := ss.store.GetTicket(ctx, ticketID)
	if err != nil || ticket == nil {
		return err
	}
	if err := NewTicketService(ss.store).applySLA(ctx, ticket); err != nil {
		return err
	}
	return ss.store.UpdateTicket(ctx, ticket)
}
