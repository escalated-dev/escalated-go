package services

import (
	"context"
	"testing"
	"time"

	"github.com/escalated-dev/escalated-go/models"
)

// snoozeMockStore extends mockStore with snooze-related methods.
type snoozeMockStore struct {
	*mockStore
}

func newSnoozeMockStore() *snoozeMockStore {
	return &snoozeMockStore{mockStore: newMockStore()}
}

func (m *snoozeMockStore) ListSnoozedDueBefore(_ context.Context, before time.Time) ([]*models.Ticket, error) {
	var result []*models.Ticket
	for _, t := range m.tickets {
		if t.Status == models.StatusSnoozed && t.SnoozedUntil != nil && !t.SnoozedUntil.After(before) {
			cp := *t
			result = append(result, &cp)
		}
	}
	return result, nil
}

func TestSnoozeTicket(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(ms *snoozeMockStore)
		id      int64
		until   time.Time
		wantErr bool
		check   func(t *testing.T, ms *snoozeMockStore)
	}{
		{
			name: "successful snooze",
			setup: func(ms *snoozeMockStore) {
				ms.tickets[1] = &models.Ticket{
					ID:     1,
					Status: models.StatusOpen,
				}
			},
			id:    1,
			until: time.Now().Add(24 * time.Hour),
			check: func(t *testing.T, ms *snoozeMockStore) {
				tk := ms.tickets[1]
				if tk.Status != models.StatusSnoozed {
					t.Errorf("expected status snoozed, got %d", tk.Status)
				}
				if tk.SnoozedUntil == nil {
					t.Error("expected SnoozedUntil to be set")
				}
				if tk.StatusBeforeSnooze == nil || *tk.StatusBeforeSnooze != models.StatusOpen {
					t.Error("expected StatusBeforeSnooze to be open")
				}
			},
		},
		{
			name: "cannot snooze resolved ticket",
			setup: func(ms *snoozeMockStore) {
				ms.tickets[2] = &models.Ticket{
					ID:     2,
					Status: models.StatusResolved,
				}
			},
			id:      2,
			until:   time.Now().Add(time.Hour),
			wantErr: true,
		},
		{
			name: "cannot snooze with past time",
			setup: func(ms *snoozeMockStore) {
				ms.tickets[3] = &models.Ticket{
					ID:     3,
					Status: models.StatusOpen,
				}
			},
			id:      3,
			until:   time.Now().Add(-time.Hour),
			wantErr: true,
		},
		{
			name:    "ticket not found",
			setup:   func(_ *snoozeMockStore) {},
			id:      999,
			until:   time.Now().Add(time.Hour),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ms := newSnoozeMockStore()
			tt.setup(ms)
			svc := NewSnoozeService(ms)

			agentID := int64(42)
			err := svc.SnoozeTicket(context.Background(), tt.id, tt.until, &agentID)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.check != nil {
				tt.check(t, ms)
			}
		})
	}
}

func TestUnsnoozeTicket(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(ms *snoozeMockStore)
		id      int64
		wantErr bool
		check   func(t *testing.T, ms *snoozeMockStore)
	}{
		{
			name: "successful unsnooze restores previous status",
			setup: func(ms *snoozeMockStore) {
				prevStatus := models.StatusInProgress
				until := time.Now().Add(time.Hour)
				ms.tickets[1] = &models.Ticket{
					ID:                 1,
					Status:             models.StatusSnoozed,
					SnoozedUntil:       &until,
					StatusBeforeSnooze: &prevStatus,
				}
			},
			id: 1,
			check: func(t *testing.T, ms *snoozeMockStore) {
				tk := ms.tickets[1]
				if tk.Status != models.StatusInProgress {
					t.Errorf("expected status in_progress, got %d", tk.Status)
				}
				if tk.SnoozedUntil != nil {
					t.Error("expected SnoozedUntil to be nil")
				}
				if tk.StatusBeforeSnooze != nil {
					t.Error("expected StatusBeforeSnooze to be nil")
				}
			},
		},
		{
			name: "unsnooze defaults to open if no previous status",
			setup: func(ms *snoozeMockStore) {
				until := time.Now().Add(time.Hour)
				ms.tickets[2] = &models.Ticket{
					ID:           2,
					Status:       models.StatusSnoozed,
					SnoozedUntil: &until,
				}
			},
			id: 2,
			check: func(t *testing.T, ms *snoozeMockStore) {
				if ms.tickets[2].Status != models.StatusOpen {
					t.Errorf("expected status open, got %d", ms.tickets[2].Status)
				}
			},
		},
		{
			name: "cannot unsnooze non-snoozed ticket",
			setup: func(ms *snoozeMockStore) {
				ms.tickets[3] = &models.Ticket{
					ID:     3,
					Status: models.StatusOpen,
				}
			},
			id:      3,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ms := newSnoozeMockStore()
			tt.setup(ms)
			svc := NewSnoozeService(ms)

			err := svc.UnsnoozeTicket(context.Background(), tt.id, nil)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.check != nil {
				tt.check(t, ms)
			}
		})
	}
}

func TestWakeExpiredSnoozes(t *testing.T) {
	ms := newSnoozeMockStore()

	// One expired snooze
	past := time.Now().Add(-time.Hour)
	prevStatus := models.StatusOpen
	ms.tickets[1] = &models.Ticket{
		ID:                 1,
		Status:             models.StatusSnoozed,
		SnoozedUntil:       &past,
		StatusBeforeSnooze: &prevStatus,
	}

	// One future snooze (should not be woken)
	future := time.Now().Add(24 * time.Hour)
	ms.tickets[2] = &models.Ticket{
		ID:                 2,
		Status:             models.StatusSnoozed,
		SnoozedUntil:       &future,
		StatusBeforeSnooze: &prevStatus,
	}

	svc := NewSnoozeService(ms)
	woken, err := svc.WakeExpiredSnoozes(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if woken != 1 {
		t.Errorf("expected 1 ticket woken, got %d", woken)
	}

	if ms.tickets[1].Status != models.StatusOpen {
		t.Error("expected ticket 1 to be unsnoozed")
	}
	if ms.tickets[2].Status != models.StatusSnoozed {
		t.Error("expected ticket 2 to still be snoozed")
	}
}
