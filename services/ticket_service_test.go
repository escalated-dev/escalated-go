package services

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/escalated-dev/escalated-go/models"
)

// mockStore implements store.Store for testing.
type mockStore struct {
	tickets    map[int64]*models.Ticket
	replies    map[int64]*models.Reply
	activities []*models.Activity
	nextID     int64
}

func newMockStore() *mockStore {
	return &mockStore{
		tickets: make(map[int64]*models.Ticket),
		replies: make(map[int64]*models.Reply),
		nextID:  1,
	}
}

func (m *mockStore) CreateTicket(_ context.Context, t *models.Ticket) error {
	t.ID = m.nextID
	m.nextID++
	if t.Reference == "" {
		t.Reference = models.GenerateReference("")
	}
	now := time.Now()
	t.CreatedAt = now
	t.UpdatedAt = now
	cp := *t
	m.tickets[t.ID] = &cp
	return nil
}

func (m *mockStore) GetTicket(_ context.Context, id int64) (*models.Ticket, error) {
	t, ok := m.tickets[id]
	if !ok {
		return nil, nil
	}
	cp := *t
	return &cp, nil
}

func (m *mockStore) GetTicketByReference(_ context.Context, ref string) (*models.Ticket, error) {
	for _, t := range m.tickets {
		if t.Reference == ref {
			cp := *t
			return &cp, nil
		}
	}
	return nil, nil
}

func (m *mockStore) UpdateTicket(_ context.Context, t *models.Ticket) error {
	if _, ok := m.tickets[t.ID]; !ok {
		return fmt.Errorf("ticket %d not found", t.ID)
	}
	cp := *t
	m.tickets[t.ID] = &cp
	return nil
}

func (m *mockStore) ListTickets(_ context.Context, _ models.TicketFilters) ([]*models.Ticket, int, error) {
	var result []*models.Ticket
	for _, t := range m.tickets {
		cp := *t
		result = append(result, &cp)
	}
	return result, len(result), nil
}

func (m *mockStore) DeleteTicket(_ context.Context, id int64) error {
	delete(m.tickets, id)
	return nil
}

func (m *mockStore) CreateReply(_ context.Context, r *models.Reply) error {
	r.ID = m.nextID
	m.nextID++
	now := time.Now()
	r.CreatedAt = now
	r.UpdatedAt = now
	cp := *r
	m.replies[r.ID] = &cp
	return nil
}

func (m *mockStore) GetReply(_ context.Context, id int64) (*models.Reply, error) {
	r, ok := m.replies[id]
	if !ok {
		return nil, nil
	}
	cp := *r
	return &cp, nil
}

func (m *mockStore) ListReplies(_ context.Context, _ models.ReplyFilters) ([]*models.Reply, error) {
	var result []*models.Reply
	for _, r := range m.replies {
		cp := *r
		result = append(result, &cp)
	}
	return result, nil
}

func (m *mockStore) UpdateReply(_ context.Context, r *models.Reply) error {
	if _, ok := m.replies[r.ID]; !ok {
		return fmt.Errorf("reply %d not found", r.ID)
	}
	cp := *r
	m.replies[r.ID] = &cp
	return nil
}

func (m *mockStore) DeleteReply(_ context.Context, id int64) error {
	delete(m.replies, id)
	return nil
}

func (m *mockStore) CreateDepartment(_ context.Context, _ *models.Department) error { return nil }
func (m *mockStore) GetDepartment(_ context.Context, _ int64) (*models.Department, error) {
	return nil, nil
}
func (m *mockStore) ListDepartments(_ context.Context, _ bool) ([]*models.Department, error) {
	return nil, nil
}
func (m *mockStore) UpdateDepartment(_ context.Context, _ *models.Department) error { return nil }
func (m *mockStore) DeleteDepartment(_ context.Context, _ int64) error              { return nil }

func (m *mockStore) CreateTag(_ context.Context, _ *models.Tag) error        { return nil }
func (m *mockStore) GetTag(_ context.Context, _ int64) (*models.Tag, error)  { return nil, nil }
func (m *mockStore) ListTags(_ context.Context) ([]*models.Tag, error)       { return nil, nil }
func (m *mockStore) UpdateTag(_ context.Context, _ *models.Tag) error        { return nil }
func (m *mockStore) DeleteTag(_ context.Context, _ int64) error              { return nil }
func (m *mockStore) AddTagToTicket(_ context.Context, _, _ int64) error      { return nil }
func (m *mockStore) RemoveTagFromTicket(_ context.Context, _, _ int64) error { return nil }
func (m *mockStore) GetTicketTags(_ context.Context, _ int64) ([]*models.Tag, error) {
	return nil, nil
}

func (m *mockStore) CreateSLAPolicy(_ context.Context, _ *models.SLAPolicy) error { return nil }
func (m *mockStore) GetSLAPolicy(_ context.Context, _ int64) (*models.SLAPolicy, error) {
	return nil, nil
}
func (m *mockStore) GetDefaultSLAPolicy(_ context.Context) (*models.SLAPolicy, error) {
	return nil, nil
}
func (m *mockStore) ListSLAPolicies(_ context.Context, _ bool) ([]*models.SLAPolicy, error) {
	return nil, nil
}
func (m *mockStore) UpdateSLAPolicy(_ context.Context, _ *models.SLAPolicy) error { return nil }
func (m *mockStore) DeleteSLAPolicy(_ context.Context, _ int64) error             { return nil }

func (m *mockStore) CreateActivity(_ context.Context, a *models.Activity) error {
	a.ID = m.nextID
	m.nextID++
	m.activities = append(m.activities, a)
	return nil
}

func (m *mockStore) ListActivities(_ context.Context, _ int64, _ int) ([]*models.Activity, error) {
	return m.activities, nil
}

// --- Tests ---

func TestSplitTicket(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(ms *mockStore) SplitTicketInput
		wantErr bool
		check   func(t *testing.T, ms *mockStore, result *models.Ticket)
	}{
		{
			name: "successful split with custom subject",
			setup: func(ms *mockStore) SplitTicketInput {
				ms.tickets[1] = &models.Ticket{
					ID:         1,
					Reference:  "ESC-0001",
					Subject:    "Original ticket",
					Priority:   models.PriorityHigh,
					TicketType: "problem",
					Status:     models.StatusOpen,
				}
				ms.replies[10] = &models.Reply{
					ID:       10,
					TicketID: 1,
					Body:     "This should be a separate issue",
				}
				causerID := int64(42)
				return SplitTicketInput{
					TicketID: 1,
					ReplyID:  10,
					Subject:  "Separated issue",
					CauserID: &causerID,
				}
			},
			check: func(t *testing.T, ms *mockStore, result *models.Ticket) {
				if result.Subject != "Separated issue" {
					t.Errorf("expected subject 'Separated issue', got %q", result.Subject)
				}
				if result.Description != "This should be a separate issue" {
					t.Errorf("expected description from reply body, got %q", result.Description)
				}
				if result.Priority != models.PriorityHigh {
					t.Errorf("expected priority %d, got %d", models.PriorityHigh, result.Priority)
				}
				if result.TicketType != "problem" {
					t.Errorf("expected ticket_type 'problem', got %q", result.TicketType)
				}
				if result.SplitFromID == nil || *result.SplitFromID != 1 {
					t.Error("expected SplitFromID to be 1")
				}
				if result.Status != models.StatusOpen {
					t.Errorf("expected status open, got %d", result.Status)
				}
				// Check activities were recorded
				if len(ms.activities) < 2 {
					t.Fatalf("expected at least 2 activities, got %d", len(ms.activities))
				}
				found := false
				for _, a := range ms.activities {
					if a.Action == models.ActionTicketSplit {
						found = true
						var details map[string]any
						_ = json.Unmarshal(a.Details, &details)
						if details["source_reply_id"].(float64) != 10 {
							t.Error("expected source_reply_id=10 in activity details")
						}
					}
				}
				if !found {
					t.Error("expected a ticket_split activity")
				}
			},
		},
		{
			name: "split with default subject",
			setup: func(ms *mockStore) SplitTicketInput {
				ms.tickets[2] = &models.Ticket{
					ID:         2,
					Reference:  "ESC-0002",
					Subject:    "Parent ticket",
					Priority:   models.PriorityLow,
					TicketType: "question",
					Status:     models.StatusOpen,
				}
				ms.replies[20] = &models.Reply{
					ID:       20,
					TicketID: 2,
					Body:     "Side topic",
				}
				return SplitTicketInput{
					TicketID: 2,
					ReplyID:  20,
					Subject:  "",
				}
			},
			check: func(t *testing.T, _ *mockStore, result *models.Ticket) {
				if result.Subject != "Split from: Parent ticket" {
					t.Errorf("expected default subject, got %q", result.Subject)
				}
			},
		},
		{
			name: "split with nonexistent ticket",
			setup: func(_ *mockStore) SplitTicketInput {
				return SplitTicketInput{
					TicketID: 999,
					ReplyID:  1,
				}
			},
			wantErr: true,
		},
		{
			name: "split with nonexistent reply",
			setup: func(ms *mockStore) SplitTicketInput {
				ms.tickets[3] = &models.Ticket{
					ID:      3,
					Subject: "Ticket",
					Status:  models.StatusOpen,
				}
				return SplitTicketInput{
					TicketID: 3,
					ReplyID:  999,
				}
			},
			wantErr: true,
		},
		{
			name: "split with reply from different ticket",
			setup: func(ms *mockStore) SplitTicketInput {
				ms.tickets[4] = &models.Ticket{
					ID:      4,
					Subject: "Ticket A",
					Status:  models.StatusOpen,
				}
				ms.replies[40] = &models.Reply{
					ID:       40,
					TicketID: 99, // different ticket
					Body:     "Wrong ticket reply",
				}
				return SplitTicketInput{
					TicketID: 4,
					ReplyID:  40,
				}
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ms := newMockStore()
			svc := NewTicketService(ms)
			input := tt.setup(ms)

			result, err := svc.SplitTicket(context.Background(), input)
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
				tt.check(t, ms, result)
			}
		})
	}
}
