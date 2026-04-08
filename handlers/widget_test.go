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
	"github.com/escalated-dev/escalated-go/services"
)

// widgetMockStore implements store.Store for widget handler tests.
type widgetMockStore struct {
	tickets map[int64]*models.Ticket
	replies map[int64]*models.Reply
	nextID  int64
}

func newWidgetMockStore() *widgetMockStore {
	return &widgetMockStore{
		tickets: make(map[int64]*models.Ticket),
		replies: make(map[int64]*models.Reply),
		nextID:  1,
	}
}

func (m *widgetMockStore) CreateTicket(_ context.Context, t *models.Ticket) error {
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
func (m *widgetMockStore) GetTicket(_ context.Context, id int64) (*models.Ticket, error) {
	t, ok := m.tickets[id]
	if !ok {
		return nil, nil
	}
	cp := *t
	return &cp, nil
}
func (m *widgetMockStore) GetTicketByReference(_ context.Context, ref string) (*models.Ticket, error) {
	for _, t := range m.tickets {
		if t.Reference == ref {
			cp := *t
			return &cp, nil
		}
	}
	return nil, nil
}
func (m *widgetMockStore) UpdateTicket(_ context.Context, t *models.Ticket) error {
	cp := *t
	m.tickets[t.ID] = &cp
	return nil
}
func (m *widgetMockStore) ListTickets(_ context.Context, _ models.TicketFilters) ([]*models.Ticket, int, error) {
	return nil, 0, nil
}
func (m *widgetMockStore) DeleteTicket(_ context.Context, _ int64) error        { return nil }
func (m *widgetMockStore) CreateReply(_ context.Context, r *models.Reply) error { return nil }
func (m *widgetMockStore) GetReply(_ context.Context, id int64) (*models.Reply, error) {
	r, ok := m.replies[id]
	if !ok {
		return nil, nil
	}
	cp := *r
	return &cp, nil
}
func (m *widgetMockStore) ListReplies(_ context.Context, f models.ReplyFilters) ([]*models.Reply, error) {
	var result []*models.Reply
	for _, r := range m.replies {
		if r.TicketID == f.TicketID {
			cp := *r
			result = append(result, &cp)
		}
	}
	return result, nil
}
func (m *widgetMockStore) UpdateReply(_ context.Context, _ *models.Reply) error { return nil }
func (m *widgetMockStore) DeleteReply(_ context.Context, _ int64) error         { return nil }
func (m *widgetMockStore) CreateDepartment(_ context.Context, _ *models.Department) error {
	return nil
}
func (m *widgetMockStore) GetDepartment(_ context.Context, _ int64) (*models.Department, error) {
	return nil, nil
}
func (m *widgetMockStore) ListDepartments(_ context.Context, _ bool) ([]*models.Department, error) {
	return nil, nil
}
func (m *widgetMockStore) UpdateDepartment(_ context.Context, _ *models.Department) error {
	return nil
}
func (m *widgetMockStore) DeleteDepartment(_ context.Context, _ int64) error { return nil }
func (m *widgetMockStore) CreateTag(_ context.Context, _ *models.Tag) error  { return nil }
func (m *widgetMockStore) GetTag(_ context.Context, _ int64) (*models.Tag, error) {
	return nil, nil
}
func (m *widgetMockStore) ListTags(_ context.Context) ([]*models.Tag, error)  { return nil, nil }
func (m *widgetMockStore) UpdateTag(_ context.Context, _ *models.Tag) error   { return nil }
func (m *widgetMockStore) DeleteTag(_ context.Context, _ int64) error         { return nil }
func (m *widgetMockStore) AddTagToTicket(_ context.Context, _, _ int64) error { return nil }
func (m *widgetMockStore) RemoveTagFromTicket(_ context.Context, _, _ int64) error {
	return nil
}
func (m *widgetMockStore) GetTicketTags(_ context.Context, _ int64) ([]*models.Tag, error) {
	return nil, nil
}
func (m *widgetMockStore) CreateSLAPolicy(_ context.Context, _ *models.SLAPolicy) error {
	return nil
}
func (m *widgetMockStore) GetSLAPolicy(_ context.Context, _ int64) (*models.SLAPolicy, error) {
	return nil, nil
}
func (m *widgetMockStore) GetDefaultSLAPolicy(_ context.Context) (*models.SLAPolicy, error) {
	return nil, nil
}
func (m *widgetMockStore) ListSLAPolicies(_ context.Context, _ bool) ([]*models.SLAPolicy, error) {
	return nil, nil
}
func (m *widgetMockStore) UpdateSLAPolicy(_ context.Context, _ *models.SLAPolicy) error {
	return nil
}
func (m *widgetMockStore) DeleteSLAPolicy(_ context.Context, _ int64) error { return nil }
func (m *widgetMockStore) CreateActivity(_ context.Context, _ *models.Activity) error {
	return nil
}
func (m *widgetMockStore) ListActivities(_ context.Context, _ int64, _ int) ([]*models.Activity, error) {
	return nil, nil
}

func TestWidgetHandler_Config(t *testing.T) {
	tests := []struct {
		name       string
		enabled    bool
		wantStatus int
	}{
		{
			name:       "widget enabled",
			enabled:    true,
			wantStatus: http.StatusOK,
		},
		{
			name:       "widget disabled",
			enabled:    false,
			wantStatus: http.StatusServiceUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultWidgetConfig()
			cfg.Enabled = tt.enabled
			ms := newWidgetMockStore()
			svc := services.NewTicketService(ms)
			h := NewWidgetHandler(cfg, ms, svc)

			req := httptest.NewRequest(http.MethodGet, "/widget/config", nil)
			rec := httptest.NewRecorder()
			h.Config(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("expected %d, got %d", tt.wantStatus, rec.Code)
			}

			if tt.enabled {
				var resp map[string]any
				_ = json.Unmarshal(rec.Body.Bytes(), &resp)
				if resp["title"] != cfg.Title {
					t.Errorf("title = %v, want %v", resp["title"], cfg.Title)
				}
			}
		})
	}
}

func TestWidgetHandler_CreateTicket(t *testing.T) {
	tests := []struct {
		name       string
		body       map[string]any
		wantStatus int
	}{
		{
			name: "valid guest ticket",
			body: map[string]any{
				"name":        "John Doe",
				"email":       "john@example.com",
				"subject":     "Help needed",
				"description": "I need assistance with...",
			},
			wantStatus: http.StatusCreated,
		},
		{
			name: "missing email",
			body: map[string]any{
				"subject":     "Help",
				"description": "Text",
			},
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name: "missing subject",
			body: map[string]any{
				"email":       "test@example.com",
				"description": "Text",
			},
			wantStatus: http.StatusUnprocessableEntity,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultWidgetConfig()
			cfg.Enabled = true
			ms := newWidgetMockStore()
			svc := services.NewTicketService(ms)
			h := NewWidgetHandler(cfg, ms, svc)

			bodyBytes, _ := json.Marshal(tt.body)
			req := httptest.NewRequest(http.MethodPost, "/widget/tickets", bytes.NewReader(bodyBytes))
			req.RemoteAddr = "127.0.0.1:1234"
			rec := httptest.NewRecorder()
			h.CreateTicket(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("expected %d, got %d; body: %s", tt.wantStatus, rec.Code, rec.Body.String())
			}

			if tt.wantStatus == http.StatusCreated {
				var resp map[string]any
				_ = json.Unmarshal(rec.Body.Bytes(), &resp)
				if resp["ticket_reference"] == nil {
					t.Error("expected ticket_reference in response")
				}
				if resp["guest_token"] == nil {
					t.Error("expected guest_token in response")
				}
			}
		})
	}
}

func TestWidgetHandler_LookupTicket(t *testing.T) {
	cfg := DefaultWidgetConfig()
	cfg.Enabled = true
	ms := newWidgetMockStore()
	svc := services.NewTicketService(ms)
	h := NewWidgetHandler(cfg, ms, svc)

	// Seed a ticket with guest token
	token := "GT-TEST-TOKEN"
	ms.tickets[1] = &models.Ticket{
		ID:         1,
		Reference:  "ESC-0001",
		Subject:    "Test",
		Status:     models.StatusOpen,
		GuestToken: &token,
	}

	tests := []struct {
		name       string
		ref        string
		token      string
		wantStatus int
	}{
		{
			name:       "valid lookup",
			ref:        "ESC-0001",
			token:      "GT-TEST-TOKEN",
			wantStatus: http.StatusOK,
		},
		{
			name:       "wrong token",
			ref:        "ESC-0001",
			token:      "wrong",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "not found",
			ref:        "ESC-9999",
			token:      "any",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "missing params",
			ref:        "",
			token:      "",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := fmt.Sprintf("/widget/tickets/lookup?ref=%s&token=%s", tt.ref, tt.token)
			req := httptest.NewRequest(http.MethodGet, url, nil)
			req.RemoteAddr = "127.0.0.1:1234"
			rec := httptest.NewRecorder()
			h.LookupTicket(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("expected %d, got %d; body: %s", tt.wantStatus, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestWidgetHandler_RateLimit(t *testing.T) {
	cfg := DefaultWidgetConfig()
	cfg.Enabled = true
	cfg.RateLimitPerMin = 3
	ms := newWidgetMockStore()
	svc := services.NewTicketService(ms)
	h := NewWidgetHandler(cfg, ms, svc)

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/widget/articles?q=test", nil)
		req.RemoteAddr = "10.0.0.1:1234"
		rec := httptest.NewRecorder()
		h.SearchArticles(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i, rec.Code)
		}
	}

	// 4th request should be rate limited
	req := httptest.NewRequest(http.MethodGet, "/widget/articles?q=test", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	rec := httptest.NewRecorder()
	h.SearchArticles(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", rec.Code)
	}

	// Different IP should not be rate limited
	req = httptest.NewRequest(http.MethodGet, "/widget/articles?q=test", nil)
	req.RemoteAddr = "10.0.0.2:1234"
	rec = httptest.NewRecorder()
	h.SearchArticles(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("different IP: expected 200, got %d", rec.Code)
	}
}

func TestWidgetHandler_CORS(t *testing.T) {
	cfg := DefaultWidgetConfig()
	cfg.Enabled = true
	cfg.AllowedOrigins = "https://example.com"
	ms := newWidgetMockStore()
	svc := services.NewTicketService(ms)
	h := NewWidgetHandler(cfg, ms, svc)

	req := httptest.NewRequest(http.MethodGet, "/widget/config", nil)
	rec := httptest.NewRecorder()
	h.Config(rec, req)

	origin := rec.Header().Get("Access-Control-Allow-Origin")
	if origin != "https://example.com" {
		t.Errorf("CORS origin = %q, want 'https://example.com'", origin)
	}
}
