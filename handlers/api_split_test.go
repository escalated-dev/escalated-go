package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/renderer"
	"github.com/escalated-dev/escalated-go/services"
)

func TestSplitTicketHandler(t *testing.T) {
	tests := []struct {
		name       string
		body       map[string]any
		ticketID   string
		wantStatus int
	}{
		{
			name:       "missing reply_id",
			body:       map[string]any{"subject": "New ticket"},
			ticketID:   "1",
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:       "invalid request body",
			body:       nil,
			ticketID:   "1",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "valid split request",
			body:       map[string]any{"reply_id": 10, "subject": "Split ticket"},
			ticketID:   "1",
			wantStatus: http.StatusCreated,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ms := newHandlerMockStore()
			// Seed data for valid case
			ms.tickets[1] = &models.Ticket{
				ID:         1,
				Reference:  "ESC-0001",
				Subject:    "Original",
				Priority:   models.PriorityMedium,
				TicketType: "question",
				Status:     models.StatusOpen,
			}
			ms.replies[10] = &models.Reply{
				ID:       10,
				TicketID: 1,
				Body:     "Reply body to split",
			}

			svc := services.NewTicketService(ms)
			rend := renderer.NewJSONRenderer()
			h := NewAPIHandler(ms, svc, rend, func(_ *http.Request) int64 { return 1 })

			var bodyBytes []byte
			if tt.body != nil {
				bodyBytes, _ = json.Marshal(tt.body)
			} else {
				bodyBytes = []byte("invalid json{{{")
			}

			req := httptest.NewRequest(http.MethodPost, "/api/tickets/"+tt.ticketID+"/split", bytes.NewReader(bodyBytes))
			req.SetPathValue("id", tt.ticketID)
			rec := httptest.NewRecorder()

			h.SplitTicket(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("expected status %d, got %d; body: %s", tt.wantStatus, rec.Code, rec.Body.String())
			}

			if tt.wantStatus == http.StatusCreated {
				var resp map[string]any
				_ = json.Unmarshal(rec.Body.Bytes(), &resp)
				ticket, ok := resp["ticket"].(map[string]any)
				if !ok {
					t.Fatal("expected ticket in response")
				}
				if ticket["subject"] != "Split ticket" {
					t.Errorf("expected subject 'Split ticket', got %v", ticket["subject"])
				}
			}
		})
	}
}
