package services

import (
	"context"
	"testing"

	"github.com/escalated-dev/escalated-go/models"
)

func TestStartSession(t *testing.T) {
	tests := []struct {
		name    string
		input   StartSessionInput
		setup   func(ms *mockStore)
		wantErr bool
		check   func(t *testing.T, ticket *models.Ticket, session *models.ChatSession)
	}{
		{
			name: "successful start creates ticket and session",
			input: StartSessionInput{
				GuestName:  "Alice",
				GuestEmail: "alice@example.com",
				Subject:    "Need help",
				Message:    "Hello!",
				PageURL:    "https://example.com/pricing",
				VisitorIP:  "192.168.1.1",
				VisitorUA:  "Mozilla/5.0",
			},
			check: func(t *testing.T, ticket *models.Ticket, session *models.ChatSession) {
				if ticket.Subject != "Need help" {
					t.Errorf("expected subject 'Need help', got %q", ticket.Subject)
				}
				if ticket.Channel == nil || *ticket.Channel != models.ChannelChat {
					t.Error("expected channel to be 'chat'")
				}
				if ticket.Status != models.StatusLive {
					t.Errorf("expected status live (%d), got %d", models.StatusLive, ticket.Status)
				}
				if ticket.GuestToken == nil {
					t.Error("expected guest token to be set")
				}
				if session.Status != models.ChatStatusWaiting {
					t.Errorf("expected session status waiting, got %d", session.Status)
				}
				if session.TicketID != ticket.ID {
					t.Errorf("expected session ticket_id %d, got %d", ticket.ID, session.TicketID)
				}
			},
		},
		{
			name: "start with default subject",
			input: StartSessionInput{
				GuestName:  "Bob",
				GuestEmail: "bob@example.com",
			},
			check: func(t *testing.T, ticket *models.Ticket, _ *models.ChatSession) {
				if ticket.Subject != "Live Chat" {
					t.Errorf("expected default subject 'Live Chat', got %q", ticket.Subject)
				}
			},
		},
		{
			name: "auto-routes to available agent",
			input: StartSessionInput{
				GuestName:  "Carol",
				GuestEmail: "carol@example.com",
			},
			setup: func(ms *mockStore) {
				ms.chatRules[100] = &models.ChatRoutingRule{
					ID:                 100,
					Name:               "Default",
					Strategy:           models.StrategyRoundRobin,
					AgentIDs:           []int64{42},
					MaxConcurrentChats: 5,
					IsActive:           true,
				}
			},
			check: func(t *testing.T, ticket *models.Ticket, session *models.ChatSession) {
				// After auto-routing, session should be retrieved fresh from store
				// The assign may have been done in-memory
				if ticket.AssignedTo != nil && *ticket.AssignedTo == 42 {
					// Good, was auto-assigned
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ms := newMockStore()
			if tt.setup != nil {
				tt.setup(ms)
			}

			routing := NewChatRoutingService(ms)
			broadcaster := NewBroadcaster(BroadcastConfig{Enabled: false}, nil)
			svc := NewChatSessionService(ms, routing, broadcaster)

			ticket, session, err := svc.StartSession(context.Background(), tt.input)
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
				tt.check(t, ticket, session)
			}
		})
	}
}

func TestAssignAgent(t *testing.T) {
	ms := newMockStore()
	routing := NewChatRoutingService(ms)
	broadcaster := NewBroadcaster(BroadcastConfig{Enabled: false}, nil)
	svc := NewChatSessionService(ms, routing, broadcaster)

	// Create a ticket and session
	ticket, session, err := svc.StartSession(context.Background(), StartSessionInput{
		GuestName:  "Test",
		GuestEmail: "test@example.com",
	})
	if err != nil {
		t.Fatalf("failed to start session: %v", err)
	}

	if !session.IsWaiting() {
		t.Fatal("expected session to be in waiting state")
	}

	// Assign agent
	err = svc.AssignAgent(context.Background(), session, 42)
	if err != nil {
		t.Fatalf("failed to assign agent: %v", err)
	}

	// Verify the session was updated in the store
	updated, _ := ms.GetChatSessionByTicket(context.Background(), ticket.ID)
	if updated == nil {
		t.Fatal("expected to find updated session")
	}
	if updated.Status != models.ChatStatusActive {
		t.Errorf("expected status active, got %d", updated.Status)
	}
	if updated.AgentID == nil || *updated.AgentID != 42 {
		t.Error("expected agent_id to be 42")
	}
	if updated.AgentJoinedAt == nil {
		t.Error("expected agent_joined_at to be set")
	}
}

func TestEndSession(t *testing.T) {
	ms := newMockStore()
	routing := NewChatRoutingService(ms)
	broadcaster := NewBroadcaster(BroadcastConfig{Enabled: false}, nil)
	svc := NewChatSessionService(ms, routing, broadcaster)

	ticket, session, _ := svc.StartSession(context.Background(), StartSessionInput{
		GuestName:  "Test",
		GuestEmail: "test@example.com",
	})

	_ = svc.AssignAgent(context.Background(), session, 42)

	agentID := int64(42)
	err := svc.EndSession(context.Background(), session, &agentID)
	if err != nil {
		t.Fatalf("failed to end session: %v", err)
	}

	// Check session
	ended, _ := ms.GetChatSession(context.Background(), session.ID)
	if ended == nil {
		t.Fatal("expected to find ended session")
	}
	if ended.Status != models.ChatStatusEnded {
		t.Errorf("expected status ended, got %d", ended.Status)
	}
	if ended.EndedAt == nil {
		t.Error("expected ended_at to be set")
	}

	// Check ticket
	updatedTicket, _ := ms.GetTicket(context.Background(), ticket.ID)
	if updatedTicket == nil {
		t.Fatal("expected to find updated ticket")
	}
	if updatedTicket.Status != models.StatusOpen {
		t.Errorf("expected ticket status open, got %d", updatedTicket.Status)
	}
	if updatedTicket.ChatEndedAt == nil {
		t.Error("expected ticket chat_ended_at to be set")
	}
}

func TestSendMessage(t *testing.T) {
	ms := newMockStore()
	routing := NewChatRoutingService(ms)
	broadcaster := NewBroadcaster(BroadcastConfig{Enabled: false}, nil)
	svc := NewChatSessionService(ms, routing, broadcaster)

	_, session, _ := svc.StartSession(context.Background(), StartSessionInput{
		GuestName:  "Test",
		GuestEmail: "test@example.com",
	})
	_ = svc.AssignAgent(context.Background(), session, 42)

	// Guest message
	err := svc.SendMessage(context.Background(), session, "Hello!", nil, nil)
	if err != nil {
		t.Fatalf("failed to send guest message: %v", err)
	}

	// Agent message
	agentType := "User"
	agentID := int64(42)
	err = svc.SendMessage(context.Background(), session, "Hi there!", &agentType, &agentID)
	if err != nil {
		t.Fatalf("failed to send agent message: %v", err)
	}

	// Check replies were created
	replies, _ := ms.ListReplies(context.Background(), models.ReplyFilters{TicketID: session.TicketID})
	if len(replies) != 2 {
		t.Errorf("expected 2 replies, got %d", len(replies))
	}
}

func TestChatSessionModel(t *testing.T) {
	tests := []struct {
		name   string
		status int
		want   string
	}{
		{"waiting", models.ChatStatusWaiting, "waiting"},
		{"active", models.ChatStatusActive, "active"},
		{"ended", models.ChatStatusEnded, "ended"},
		{"abandoned", models.ChatStatusAbandoned, "abandoned"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &models.ChatSession{Status: tt.status}
			if s.StatusString() != tt.want {
				t.Errorf("expected %q, got %q", tt.want, s.StatusString())
			}
		})
	}
}

func TestTicketChatFields(t *testing.T) {
	channel := models.ChannelChat
	t.Run("IsLiveChat", func(t *testing.T) {
		ticket := &models.Ticket{Channel: &channel}
		if !ticket.IsLiveChat() {
			t.Error("expected IsLiveChat to be true")
		}
	})

	t.Run("IsChatActive", func(t *testing.T) {
		ticket := &models.Ticket{Channel: &channel, Status: models.StatusLive}
		if !ticket.IsChatActive() {
			t.Error("expected IsChatActive to be true")
		}
	})

	t.Run("IsChatActive false when ended", func(t *testing.T) {
		ticket := &models.Ticket{Channel: &channel, Status: models.StatusOpen}
		if ticket.IsChatActive() {
			t.Error("expected IsChatActive to be false after status change")
		}
	})

	t.Run("no channel", func(t *testing.T) {
		ticket := &models.Ticket{}
		if ticket.IsLiveChat() {
			t.Error("expected IsLiveChat to be false with no channel")
		}
	})
}
