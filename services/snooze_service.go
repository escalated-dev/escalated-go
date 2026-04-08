package services

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/store"
)

// SnoozeService handles ticket snooze and unsnooze operations.
type SnoozeService struct {
	store store.Store
}

// NewSnoozeService creates a new SnoozeService.
func NewSnoozeService(s store.Store) *SnoozeService {
	return &SnoozeService{store: s}
}

// SnoozeTicket snoozes a ticket until the given time. The ticket's current status
// is saved so it can be restored when the snooze expires.
func (ss *SnoozeService) SnoozeTicket(ctx context.Context, ticketID int64, until time.Time, snoozedBy *int64) error {
	t, err := ss.store.GetTicket(ctx, ticketID)
	if err != nil {
		return fmt.Errorf("fetching ticket: %w", err)
	}
	if t == nil {
		return fmt.Errorf("ticket %d not found", ticketID)
	}

	if !t.IsOpen() {
		return fmt.Errorf("cannot snooze a ticket with status %q", t.StatusString())
	}

	if until.Before(time.Now()) {
		return fmt.Errorf("snooze time must be in the future")
	}

	prevStatus := t.Status
	t.StatusBeforeSnooze = &prevStatus
	t.Status = models.StatusSnoozed
	t.SnoozedUntil = &until
	t.SnoozedBy = snoozedBy

	if err := ss.store.UpdateTicket(ctx, t); err != nil {
		return fmt.Errorf("updating ticket: %w", err)
	}

	var causerType *string
	if snoozedBy != nil {
		ct := "User"
		causerType = &ct
	}
	details, _ := json.Marshal(map[string]any{
		"snoozed_until":        until.Format(time.RFC3339),
		"status_before_snooze": models.StatusName[prevStatus],
	})
	_ = ss.store.CreateActivity(ctx, &models.Activity{
		TicketID:   ticketID,
		Action:     models.ActionTicketSnoozed,
		CauserType: causerType,
		CauserID:   snoozedBy,
		Details:    details,
	})

	return nil
}

// UnsnoozeTicket restores a snoozed ticket to its previous status.
func (ss *SnoozeService) UnsnoozeTicket(ctx context.Context, ticketID int64, causerID *int64) error {
	t, err := ss.store.GetTicket(ctx, ticketID)
	if err != nil {
		return fmt.Errorf("fetching ticket: %w", err)
	}
	if t == nil {
		return fmt.Errorf("ticket %d not found", ticketID)
	}

	if t.Status != models.StatusSnoozed {
		return fmt.Errorf("ticket %d is not snoozed", ticketID)
	}

	if t.StatusBeforeSnooze != nil {
		t.Status = *t.StatusBeforeSnooze
	} else {
		t.Status = models.StatusOpen
	}
	t.SnoozedUntil = nil
	t.SnoozedBy = nil
	t.StatusBeforeSnooze = nil

	if err := ss.store.UpdateTicket(ctx, t); err != nil {
		return fmt.Errorf("updating ticket: %w", err)
	}

	var causerType *string
	if causerID != nil {
		ct := "User"
		causerType = &ct
	}
	_ = ss.store.CreateActivity(ctx, &models.Activity{
		TicketID:   ticketID,
		Action:     models.ActionTicketUnsnoozed,
		CauserType: causerType,
		CauserID:   causerID,
	})

	return nil
}

// WakeExpiredSnoozes finds all tickets snoozed until before now and unsnoozes them.
// Returns the number of tickets woken up.
func (ss *SnoozeService) WakeExpiredSnoozes(ctx context.Context) (int, error) {
	tickets, err := ss.store.ListSnoozedDueBefore(ctx, time.Now())
	if err != nil {
		return 0, fmt.Errorf("listing snoozed tickets: %w", err)
	}

	woken := 0
	for _, t := range tickets {
		if err := ss.UnsnoozeTicket(ctx, t.ID, nil); err == nil {
			woken++
		}
	}
	return woken, nil
}

// StartSnoozeWaker starts a background goroutine that periodically wakes
// expired snoozed tickets. It stops when the context is cancelled.
func (ss *SnoozeService) StartSnoozeWaker(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Minute
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_, _ = ss.WakeExpiredSnoozes(ctx)
			}
		}
	}()
}
