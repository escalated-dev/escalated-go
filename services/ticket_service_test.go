package services

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/escalated-dev/escalated-go/models"
)

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
