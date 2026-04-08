package handlers

import (
	"context"
	"fmt"
	"time"

	"github.com/escalated-dev/escalated-go/models"
)

// handlerMockStore implements store.Store for handler tests.
type handlerMockStore struct {
	tickets    map[int64]*models.Ticket
	replies    map[int64]*models.Reply
	activities []*models.Activity
	nextID     int64
}

func newHandlerMockStore() *handlerMockStore {
	return &handlerMockStore{
		tickets: make(map[int64]*models.Ticket),
		replies: make(map[int64]*models.Reply),
		nextID:  100,
	}
}

func (m *handlerMockStore) CreateTicket(_ context.Context, t *models.Ticket) error {
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

func (m *handlerMockStore) GetTicket(_ context.Context, id int64) (*models.Ticket, error) {
	t, ok := m.tickets[id]
	if !ok {
		return nil, nil
	}
	cp := *t
	return &cp, nil
}

func (m *handlerMockStore) GetTicketByReference(_ context.Context, ref string) (*models.Ticket, error) {
	for _, t := range m.tickets {
		if t.Reference == ref {
			cp := *t
			return &cp, nil
		}
	}
	return nil, nil
}

func (m *handlerMockStore) UpdateTicket(_ context.Context, t *models.Ticket) error {
	if _, ok := m.tickets[t.ID]; !ok {
		return fmt.Errorf("ticket %d not found", t.ID)
	}
	cp := *t
	m.tickets[t.ID] = &cp
	return nil
}

func (m *handlerMockStore) ListTickets(_ context.Context, _ models.TicketFilters) ([]*models.Ticket, int, error) {
	var result []*models.Ticket
	for _, t := range m.tickets {
		cp := *t
		result = append(result, &cp)
	}
	return result, len(result), nil
}

func (m *handlerMockStore) DeleteTicket(_ context.Context, id int64) error {
	delete(m.tickets, id)
	return nil
}

func (m *handlerMockStore) CreateReply(_ context.Context, r *models.Reply) error {
	r.ID = m.nextID
	m.nextID++
	now := time.Now()
	r.CreatedAt = now
	r.UpdatedAt = now
	cp := *r
	m.replies[r.ID] = &cp
	return nil
}

func (m *handlerMockStore) GetReply(_ context.Context, id int64) (*models.Reply, error) {
	r, ok := m.replies[id]
	if !ok {
		return nil, nil
	}
	cp := *r
	return &cp, nil
}

func (m *handlerMockStore) ListReplies(_ context.Context, _ models.ReplyFilters) ([]*models.Reply, error) {
	var result []*models.Reply
	for _, r := range m.replies {
		cp := *r
		result = append(result, &cp)
	}
	return result, nil
}

func (m *handlerMockStore) UpdateReply(_ context.Context, r *models.Reply) error {
	cp := *r
	m.replies[r.ID] = &cp
	return nil
}

func (m *handlerMockStore) DeleteReply(_ context.Context, id int64) error {
	delete(m.replies, id)
	return nil
}

func (m *handlerMockStore) CreateDepartment(_ context.Context, _ *models.Department) error {
	return nil
}
func (m *handlerMockStore) GetDepartment(_ context.Context, _ int64) (*models.Department, error) {
	return nil, nil
}
func (m *handlerMockStore) ListDepartments(_ context.Context, _ bool) ([]*models.Department, error) {
	return nil, nil
}
func (m *handlerMockStore) UpdateDepartment(_ context.Context, _ *models.Department) error {
	return nil
}
func (m *handlerMockStore) DeleteDepartment(_ context.Context, _ int64) error { return nil }

func (m *handlerMockStore) CreateTag(_ context.Context, _ *models.Tag) error       { return nil }
func (m *handlerMockStore) GetTag(_ context.Context, _ int64) (*models.Tag, error) { return nil, nil }
func (m *handlerMockStore) ListTags(_ context.Context) ([]*models.Tag, error)      { return nil, nil }
func (m *handlerMockStore) UpdateTag(_ context.Context, _ *models.Tag) error       { return nil }
func (m *handlerMockStore) DeleteTag(_ context.Context, _ int64) error             { return nil }
func (m *handlerMockStore) AddTagToTicket(_ context.Context, _, _ int64) error     { return nil }
func (m *handlerMockStore) RemoveTagFromTicket(_ context.Context, _, _ int64) error {
	return nil
}
func (m *handlerMockStore) GetTicketTags(_ context.Context, _ int64) ([]*models.Tag, error) {
	return nil, nil
}

func (m *handlerMockStore) CreateSLAPolicy(_ context.Context, _ *models.SLAPolicy) error {
	return nil
}
func (m *handlerMockStore) GetSLAPolicy(_ context.Context, _ int64) (*models.SLAPolicy, error) {
	return nil, nil
}
func (m *handlerMockStore) GetDefaultSLAPolicy(_ context.Context) (*models.SLAPolicy, error) {
	return nil, nil
}
func (m *handlerMockStore) ListSLAPolicies(_ context.Context, _ bool) ([]*models.SLAPolicy, error) {
	return nil, nil
}
func (m *handlerMockStore) UpdateSLAPolicy(_ context.Context, _ *models.SLAPolicy) error {
	return nil
}
func (m *handlerMockStore) DeleteSLAPolicy(_ context.Context, _ int64) error { return nil }

func (m *handlerMockStore) ListSnoozedDueBefore(_ context.Context, _ time.Time) ([]*models.Ticket, error) {
	return nil, nil
}

func (m *handlerMockStore) CreateSavedView(_ context.Context, _ *models.SavedView) error {
	return nil
}
func (m *handlerMockStore) GetSavedView(_ context.Context, _ int64) (*models.SavedView, error) {
	return nil, nil
}
func (m *handlerMockStore) ListSavedViews(_ context.Context, _ int64, _ bool) ([]*models.SavedView, error) {
	return nil, nil
}
func (m *handlerMockStore) UpdateSavedView(_ context.Context, _ *models.SavedView) error {
	return nil
}
func (m *handlerMockStore) DeleteSavedView(_ context.Context, _ int64) error { return nil }
func (m *handlerMockStore) ReorderSavedViews(_ context.Context, _ int64, _ []int64) error {
	return nil
}

func (m *handlerMockStore) CreateActivity(_ context.Context, a *models.Activity) error {
	a.ID = m.nextID
	m.nextID++
	m.activities = append(m.activities, a)
	return nil
}

func (m *handlerMockStore) ListActivities(_ context.Context, _ int64, _ int) ([]*models.Activity, error) {
	return m.activities, nil
}

func (m *handlerMockStore) CreateChatSession(_ context.Context, _ *models.ChatSession) error {
	return nil
}
func (m *handlerMockStore) GetChatSession(_ context.Context, _ int64) (*models.ChatSession, error) {
	return nil, nil
}
func (m *handlerMockStore) GetChatSessionByTicket(_ context.Context, _ int64) (*models.ChatSession, error) {
	return nil, nil
}
func (m *handlerMockStore) UpdateChatSession(_ context.Context, _ *models.ChatSession) error {
	return nil
}
func (m *handlerMockStore) ListChatSessions(_ context.Context, _ models.ChatSessionFilters) ([]*models.ChatSession, error) {
	return nil, nil
}
func (m *handlerMockStore) CreateChatRoutingRule(_ context.Context, _ *models.ChatRoutingRule) error {
	return nil
}
func (m *handlerMockStore) GetChatRoutingRule(_ context.Context, _ int64) (*models.ChatRoutingRule, error) {
	return nil, nil
}
func (m *handlerMockStore) ListActiveChatRoutingRules(_ context.Context, _ *int64) ([]*models.ChatRoutingRule, error) {
	return nil, nil
}
func (m *handlerMockStore) UpdateChatRoutingRule(_ context.Context, _ *models.ChatRoutingRule) error {
	return nil
}
func (m *handlerMockStore) DeleteChatRoutingRule(_ context.Context, _ int64) error { return nil }
func (m *handlerMockStore) CountActiveChatsForAgent(_ context.Context, _ int64) (int, error) {
	return 0, nil
}
