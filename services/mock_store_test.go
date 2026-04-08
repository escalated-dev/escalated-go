package services

import (
	"context"
	"fmt"
	"time"

	"github.com/escalated-dev/escalated-go/models"
)

// mockStore implements store.Store for testing (minus ListSnoozedDueBefore).
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

func (m *mockStore) ListSnoozedDueBefore(_ context.Context, _ time.Time) ([]*models.Ticket, error) {
	return nil, nil
}

func (m *mockStore) CreateActivity(_ context.Context, a *models.Activity) error {
	a.ID = m.nextID
	m.nextID++
	m.activities = append(m.activities, a)
	return nil
}

func (m *mockStore) ListActivities(_ context.Context, _ int64, _ int) ([]*models.Activity, error) {
	return m.activities, nil
}
