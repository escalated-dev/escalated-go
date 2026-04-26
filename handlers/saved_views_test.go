package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/escalated-dev/escalated-go/models"
)

// savedViewMockStore implements the saved view methods needed for testing.
type savedViewMockStore struct {
	views  map[int64]*models.SavedView
	nextID int64
}

func newSavedViewMockStore() *savedViewMockStore {
	return &savedViewMockStore{
		views:  make(map[int64]*models.SavedView),
		nextID: 1,
	}
}

func (m *savedViewMockStore) CreateSavedView(_ context.Context, sv *models.SavedView) error {
	sv.ID = m.nextID
	m.nextID++
	now := time.Now()
	sv.CreatedAt = now
	sv.UpdatedAt = now
	cp := *sv
	m.views[sv.ID] = &cp
	return nil
}

func (m *savedViewMockStore) GetSavedView(_ context.Context, id int64) (*models.SavedView, error) {
	sv, ok := m.views[id]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	cp := *sv
	return &cp, nil
}

func (m *savedViewMockStore) ListSavedViews(_ context.Context, userID int64, includeShared bool) ([]*models.SavedView, error) {
	var result []*models.SavedView
	for _, sv := range m.views {
		if sv.UserID == userID || (includeShared && sv.IsShared) {
			cp := *sv
			result = append(result, &cp)
		}
	}
	return result, nil
}

func (m *savedViewMockStore) UpdateSavedView(_ context.Context, sv *models.SavedView) error {
	if _, ok := m.views[sv.ID]; !ok {
		return fmt.Errorf("not found")
	}
	cp := *sv
	m.views[sv.ID] = &cp
	return nil
}

func (m *savedViewMockStore) DeleteSavedView(_ context.Context, id int64) error {
	delete(m.views, id)
	return nil
}

func (m *savedViewMockStore) ReorderSavedViews(_ context.Context, _ int64, ids []int64) error {
	for i, id := range ids {
		if sv, ok := m.views[id]; ok {
			sv.Position = i
		}
	}
	return nil
}

// Stub the remaining store.Store methods needed to compile.
func (m *savedViewMockStore) CreateTicket(_ context.Context, _ *models.Ticket) error { return nil }
func (m *savedViewMockStore) GetTicket(_ context.Context, _ int64) (*models.Ticket, error) {
	return nil, nil
}
func (m *savedViewMockStore) GetTicketByReference(_ context.Context, _ string) (*models.Ticket, error) {
	return nil, nil
}
func (m *savedViewMockStore) UpdateTicket(_ context.Context, _ *models.Ticket) error { return nil }
func (m *savedViewMockStore) ListTickets(_ context.Context, _ models.TicketFilters) ([]*models.Ticket, int, error) {
	return nil, 0, nil
}
func (m *savedViewMockStore) DeleteTicket(_ context.Context, _ int64) error        { return nil }
func (m *savedViewMockStore) CreateReply(_ context.Context, _ *models.Reply) error { return nil }
func (m *savedViewMockStore) GetReply(_ context.Context, _ int64) (*models.Reply, error) {
	return nil, nil
}
func (m *savedViewMockStore) ListReplies(_ context.Context, _ models.ReplyFilters) ([]*models.Reply, error) {
	return nil, nil
}
func (m *savedViewMockStore) UpdateReply(_ context.Context, _ *models.Reply) error { return nil }
func (m *savedViewMockStore) DeleteReply(_ context.Context, _ int64) error         { return nil }
func (m *savedViewMockStore) CreateDepartment(_ context.Context, _ *models.Department) error {
	return nil
}
func (m *savedViewMockStore) GetDepartment(_ context.Context, _ int64) (*models.Department, error) {
	return nil, nil
}
func (m *savedViewMockStore) ListDepartments(_ context.Context, _ bool) ([]*models.Department, error) {
	return nil, nil
}
func (m *savedViewMockStore) UpdateDepartment(_ context.Context, _ *models.Department) error {
	return nil
}
func (m *savedViewMockStore) DeleteDepartment(_ context.Context, _ int64) error { return nil }
func (m *savedViewMockStore) CreateTag(_ context.Context, _ *models.Tag) error  { return nil }
func (m *savedViewMockStore) GetTag(_ context.Context, _ int64) (*models.Tag, error) {
	return nil, nil
}
func (m *savedViewMockStore) ListTags(_ context.Context) ([]*models.Tag, error)  { return nil, nil }
func (m *savedViewMockStore) UpdateTag(_ context.Context, _ *models.Tag) error   { return nil }
func (m *savedViewMockStore) DeleteTag(_ context.Context, _ int64) error         { return nil }
func (m *savedViewMockStore) AddTagToTicket(_ context.Context, _, _ int64) error { return nil }
func (m *savedViewMockStore) RemoveTagFromTicket(_ context.Context, _, _ int64) error {
	return nil
}
func (m *savedViewMockStore) GetTicketTags(_ context.Context, _ int64) ([]*models.Tag, error) {
	return nil, nil
}
func (m *savedViewMockStore) CreateSLAPolicy(_ context.Context, _ *models.SLAPolicy) error {
	return nil
}
func (m *savedViewMockStore) GetSLAPolicy(_ context.Context, _ int64) (*models.SLAPolicy, error) {
	return nil, nil
}
func (m *savedViewMockStore) GetDefaultSLAPolicy(_ context.Context) (*models.SLAPolicy, error) {
	return nil, nil
}
func (m *savedViewMockStore) ListSLAPolicies(_ context.Context, _ bool) ([]*models.SLAPolicy, error) {
	return nil, nil
}
func (m *savedViewMockStore) UpdateSLAPolicy(_ context.Context, _ *models.SLAPolicy) error {
	return nil
}
func (m *savedViewMockStore) DeleteSLAPolicy(_ context.Context, _ int64) error { return nil }
func (m *savedViewMockStore) ListSnoozedDueBefore(_ context.Context, _ time.Time) ([]*models.Ticket, error) {
	return nil, nil
}
func (m *savedViewMockStore) CreateActivity(_ context.Context, _ *models.Activity) error {
	return nil
}
func (m *savedViewMockStore) ListActivities(_ context.Context, _ int64, _ int) ([]*models.Activity, error) {
	return nil, nil
}
func (m *savedViewMockStore) CreateChatSession(_ context.Context, _ *models.ChatSession) error {
	return nil
}
func (m *savedViewMockStore) GetChatSession(_ context.Context, _ int64) (*models.ChatSession, error) {
	return nil, nil
}
func (m *savedViewMockStore) GetChatSessionByTicket(_ context.Context, _ int64) (*models.ChatSession, error) {
	return nil, nil
}
func (m *savedViewMockStore) UpdateChatSession(_ context.Context, _ *models.ChatSession) error {
	return nil
}
func (m *savedViewMockStore) ListChatSessions(_ context.Context, _ models.ChatSessionFilters) ([]*models.ChatSession, error) {
	return nil, nil
}
func (m *savedViewMockStore) CreateChatRoutingRule(_ context.Context, _ *models.ChatRoutingRule) error {
	return nil
}
func (m *savedViewMockStore) GetChatRoutingRule(_ context.Context, _ int64) (*models.ChatRoutingRule, error) {
	return nil, nil
}
func (m *savedViewMockStore) ListActiveChatRoutingRules(_ context.Context, _ *int64) ([]*models.ChatRoutingRule, error) {
	return nil, nil
}
func (m *savedViewMockStore) UpdateChatRoutingRule(_ context.Context, _ *models.ChatRoutingRule) error {
	return nil
}
func (m *savedViewMockStore) DeleteChatRoutingRule(_ context.Context, _ int64) error { return nil }
func (m *savedViewMockStore) CountActiveChatsForAgent(_ context.Context, _ int64) (int, error) {
	return 0, nil
}
func (m *savedViewMockStore) CreateChatMessage(_ context.Context, _ *models.ChatMessage) error {
	return nil
}
func (m *savedViewMockStore) ListChatMessages(_ context.Context, _ int64) ([]models.ChatMessage, error) {
	return nil, nil
}
func (m *savedViewMockStore) CountTicketsByRequester(_ context.Context, _ string, _ int64) (int, error) {
	return 0, nil
}
func (m *savedViewMockStore) ListRelatedTickets(_ context.Context, _ int64) ([]models.RelatedTicket, error) {
	return nil, nil
}
func (m *savedViewMockStore) CreateAttachment(_ context.Context, _ *models.Attachment) error {
	return nil
}
func (m *savedViewMockStore) GetAttachmentByID(_ context.Context, _ int64) (*models.Attachment, error) {
	return nil, nil
}
func (m *savedViewMockStore) GetAttachmentsByTicketID(_ context.Context, _ int64) ([]*models.Attachment, error) {
	return nil, nil
}
func (m *savedViewMockStore) GetAttachmentsByReplyID(_ context.Context, _ int64) ([]*models.Attachment, error) {
	return nil, nil
}
func (m *savedViewMockStore) GetContactByEmail(_ context.Context, _ string) (*models.Contact, error) {
	return nil, nil
}
func (m *savedViewMockStore) CreateContact(_ context.Context, _ *models.Contact) error {
	return nil
}
func (m *savedViewMockStore) UpdateContactName(_ context.Context, _ int64, _ string) error {
	return nil
}

func TestSavedViewHandler_Create(t *testing.T) {
	tests := []struct {
		name       string
		body       map[string]any
		wantStatus int
	}{
		{
			name:       "valid create",
			body:       map[string]any{"name": "My Queue", "filters": map[string]any{"status": 0}, "is_shared": false},
			wantStatus: http.StatusCreated,
		},
		{
			name:       "missing name",
			body:       map[string]any{"filters": map[string]any{}},
			wantStatus: http.StatusUnprocessableEntity,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ms := newSavedViewMockStore()
			h := NewSavedViewHandler(ms, func(_ *http.Request) int64 { return 1 })

			bodyBytes, _ := json.Marshal(tt.body)
			req := httptest.NewRequest(http.MethodPost, "/api/saved-views", bytes.NewReader(bodyBytes))
			rec := httptest.NewRecorder()

			h.Create(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("expected status %d, got %d; body: %s", tt.wantStatus, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestSavedViewHandler_ListAndShow(t *testing.T) {
	ms := newSavedViewMockStore()
	ms.views[1] = &models.SavedView{ID: 1, Name: "My View", UserID: 1, Filters: json.RawMessage(`{}`)}
	ms.views[2] = &models.SavedView{ID: 2, Name: "Shared View", UserID: 2, IsShared: true, Filters: json.RawMessage(`{}`)}
	ms.views[3] = &models.SavedView{ID: 3, Name: "Private Other", UserID: 2, Filters: json.RawMessage(`{}`)}

	h := NewSavedViewHandler(ms, func(_ *http.Request) int64 { return 1 })

	// List should include own and shared views
	req := httptest.NewRequest(http.MethodGet, "/api/saved-views", nil)
	rec := httptest.NewRecorder()
	h.List(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("List: expected 200, got %d", rec.Code)
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	views := resp["saved_views"].([]any)
	if len(views) != 2 {
		t.Errorf("expected 2 views (own + shared), got %d", len(views))
	}

	// Show own view
	req = httptest.NewRequest(http.MethodGet, "/api/saved-views/1", nil)
	req.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	h.Show(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("Show own: expected 200, got %d", rec.Code)
	}

	// Show shared view
	req = httptest.NewRequest(http.MethodGet, "/api/saved-views/2", nil)
	req.SetPathValue("id", "2")
	rec = httptest.NewRecorder()
	h.Show(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("Show shared: expected 200, got %d", rec.Code)
	}

	// Show private other's view should be forbidden
	req = httptest.NewRequest(http.MethodGet, "/api/saved-views/3", nil)
	req.SetPathValue("id", "3")
	rec = httptest.NewRecorder()
	h.Show(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("Show private: expected 403, got %d", rec.Code)
	}
}

func TestSavedViewHandler_Reorder(t *testing.T) {
	ms := newSavedViewMockStore()
	ms.views[1] = &models.SavedView{ID: 1, Name: "A", UserID: 1, Position: 0}
	ms.views[2] = &models.SavedView{ID: 2, Name: "B", UserID: 1, Position: 1}

	h := NewSavedViewHandler(ms, func(_ *http.Request) int64 { return 1 })

	body, _ := json.Marshal(map[string]any{"ids": []int64{2, 1}})
	req := httptest.NewRequest(http.MethodPost, "/api/saved-views/reorder", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.Reorder(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Reorder: expected 200, got %d", rec.Code)
	}

	if ms.views[2].Position != 0 {
		t.Errorf("view 2 position = %d, want 0", ms.views[2].Position)
	}
	if ms.views[1].Position != 1 {
		t.Errorf("view 1 position = %d, want 1", ms.views[1].Position)
	}
}
