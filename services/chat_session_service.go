package services

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/store"
)

// ChatSessionService handles live chat session lifecycle operations.
type ChatSessionService struct {
	store       store.Store
	routing     *ChatRoutingService
	broadcaster *Broadcaster
}

// NewChatSessionService creates a new ChatSessionService.
func NewChatSessionService(s store.Store, routing *ChatRoutingService, b *Broadcaster) *ChatSessionService {
	return &ChatSessionService{
		store:       s,
		routing:     routing,
		broadcaster: b,
	}
}

// StartSessionInput holds the fields needed to start a new chat session.
type StartSessionInput struct {
	GuestName    string
	GuestEmail   string
	Subject      string
	Message      string
	PageURL      string
	VisitorIP    string
	VisitorUA    string
	DepartmentID *int64
}

// StartSession creates a new ticket with channel=chat, status=live,
// and a ChatSession in waiting state. Attempts auto-routing.
func (cs *ChatSessionService) StartSession(ctx context.Context, in StartSessionInput) (*models.Ticket, *models.ChatSession, error) {
	channel := models.ChannelChat
	guestToken := models.GenerateReference("GT")

	subject := in.Subject
	if subject == "" {
		subject = "Live Chat"
	}

	chatMeta, _ := json.Marshal(map[string]any{
		"started_at": time.Now().Format(time.RFC3339),
		"page_url":   in.PageURL,
	})

	t := &models.Ticket{
		Subject:      subject,
		Description:  in.Message,
		Status:       models.StatusLive,
		Priority:     models.PriorityMedium,
		TicketType:   "question",
		Channel:      &channel,
		GuestName:    &in.GuestName,
		GuestEmail:   &in.GuestEmail,
		GuestToken:   &guestToken,
		DepartmentID: in.DepartmentID,
		ChatMetadata: chatMeta,
	}

	if err := cs.store.CreateTicket(ctx, t); err != nil {
		return nil, nil, fmt.Errorf("creating chat ticket: %w", err)
	}

	now := time.Now()
	session := &models.ChatSession{
		TicketID:         t.ID,
		Status:           models.ChatStatusWaiting,
		VisitorIP:        strPtr(in.VisitorIP),
		VisitorUserAgent: strPtr(in.VisitorUA),
		VisitorPageURL:   strPtr(in.PageURL),
		LastActivityAt:   &now,
		CreatedAt:        now,
	}

	if err := cs.store.CreateChatSession(ctx, session); err != nil {
		return nil, nil, fmt.Errorf("creating chat session: %w", err)
	}

	// Attempt auto-routing
	if agentID, err := cs.routing.FindAvailableAgent(ctx, t.DepartmentID); err == nil && agentID != nil {
		_ = cs.AssignAgent(ctx, session, *agentID)
	}

	if cs.broadcaster != nil {
		_, _ = cs.broadcaster.Publish(AgentChannel(), "chat.session_started", map[string]any{
			"session_id":       session.ID,
			"ticket_id":        t.ID,
			"ticket_reference": t.Reference,
			"guest_name":       in.GuestName,
			"status":           session.StatusString(),
		})
	}

	return t, session, nil
}

// AssignAgent assigns an agent to a waiting chat session.
func (cs *ChatSessionService) AssignAgent(ctx context.Context, session *models.ChatSession, agentID int64) error {
	now := time.Now()
	session.AgentID = &agentID
	session.Status = models.ChatStatusActive
	session.AgentJoinedAt = &now

	if err := cs.store.UpdateChatSession(ctx, session); err != nil {
		return fmt.Errorf("updating chat session: %w", err)
	}

	// Also assign the ticket
	t, err := cs.store.GetTicket(ctx, session.TicketID)
	if err == nil && t != nil {
		t.AssignedTo = &agentID
		_ = cs.store.UpdateTicket(ctx, t)
	}

	if cs.broadcaster != nil {
		_, _ = cs.broadcaster.Publish(TicketChannel(session.TicketID), "chat.agent_joined", map[string]any{
			"session_id": session.ID,
			"agent_id":   agentID,
		})
	}

	return nil
}

// SendMessage creates a reply on the chat ticket and broadcasts it.
func (cs *ChatSessionService) SendMessage(ctx context.Context, session *models.ChatSession, body string, authorType *string, authorID *int64) error {
	r := &models.Reply{
		TicketID:   session.TicketID,
		Body:       body,
		AuthorType: authorType,
		AuthorID:   authorID,
		IsInternal: false,
	}

	if err := cs.store.CreateReply(ctx, r); err != nil {
		return fmt.Errorf("creating chat message: %w", err)
	}

	now := time.Now()
	session.LastActivityAt = &now
	_ = cs.store.UpdateChatSession(ctx, session)

	isAgent := authorType != nil
	if cs.broadcaster != nil {
		_, _ = cs.broadcaster.Publish(TicketChannel(session.TicketID), "chat.message", map[string]any{
			"session_id": session.ID,
			"reply_id":   r.ID,
			"body":       r.Body,
			"is_agent":   isAgent,
			"created_at": r.CreatedAt,
		})
	}

	return nil
}

// EndSession ends a chat session and transitions the ticket to open.
func (cs *ChatSessionService) EndSession(ctx context.Context, session *models.ChatSession, causerID *int64) error {
	now := time.Now()
	session.Status = models.ChatStatusEnded
	session.EndedAt = &now

	if err := cs.store.UpdateChatSession(ctx, session); err != nil {
		return fmt.Errorf("ending chat session: %w", err)
	}

	t, err := cs.store.GetTicket(ctx, session.TicketID)
	if err == nil && t != nil {
		t.ChatEndedAt = &now
		t.Status = models.StatusOpen

		chatMeta := map[string]any{}
		if len(t.ChatMetadata) > 0 {
			_ = json.Unmarshal(t.ChatMetadata, &chatMeta)
		}
		chatMeta["ended_at"] = now.Format(time.RFC3339)
		if causerID != nil {
			chatMeta["ended_by"] = *causerID
		}
		if session.AgentJoinedAt != nil {
			chatMeta["duration_seconds"] = int(now.Sub(*session.AgentJoinedAt).Seconds())
		}
		t.ChatMetadata, _ = json.Marshal(chatMeta)
		_ = cs.store.UpdateTicket(ctx, t)
	}

	if cs.broadcaster != nil {
		_, _ = cs.broadcaster.Publish(TicketChannel(session.TicketID), "chat.session_ended", map[string]any{
			"session_id": session.ID,
			"ticket_id":  session.TicketID,
			"ended_by":   causerID,
		})
	}

	return nil
}

// CloseIdleSessions ends sessions that have had no activity for the given threshold.
func (cs *ChatSessionService) CloseIdleSessions(ctx context.Context, idleMinutes int) (int, error) {
	threshold := time.Now().Add(-time.Duration(idleMinutes) * time.Minute)
	sessions, err := cs.store.ListChatSessions(ctx, models.ChatSessionFilters{Active: true})
	if err != nil {
		return 0, err
	}

	count := 0
	for _, s := range sessions {
		if s.LastActivityAt != nil && s.LastActivityAt.Before(threshold) {
			if err := cs.EndSession(ctx, s, nil); err == nil {
				count++
			}
		}
	}
	return count, nil
}

// MarkAbandonedSessions marks sessions that have been waiting too long without an agent.
func (cs *ChatSessionService) MarkAbandonedSessions(ctx context.Context, waitMinutes int) (int, error) {
	threshold := time.Now().Add(-time.Duration(waitMinutes) * time.Minute)
	waiting := models.ChatStatusWaiting
	sessions, err := cs.store.ListChatSessions(ctx, models.ChatSessionFilters{Status: &waiting})
	if err != nil {
		return 0, err
	}

	count := 0
	for _, s := range sessions {
		if s.CreatedAt.Before(threshold) {
			now := time.Now()
			s.Status = models.ChatStatusAbandoned
			s.EndedAt = &now
			_ = cs.store.UpdateChatSession(ctx, s)

			t, _ := cs.store.GetTicket(ctx, s.TicketID)
			if t != nil {
				t.Status = models.StatusOpen
				t.ChatEndedAt = &now
				_ = cs.store.UpdateTicket(ctx, t)
			}

			if cs.broadcaster != nil {
				_, _ = cs.broadcaster.Publish(TicketChannel(s.TicketID), "chat.session_abandoned", map[string]any{
					"session_id": s.ID,
				})
			}
			count++
		}
	}
	return count, nil
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
