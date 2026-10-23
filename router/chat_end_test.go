package router_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/escalated-dev/escalated-go/models"
)

// escalated.admin.chat.end (escalated-laravel ChatController::end): the shared
// Agent/TicketShow page POSTs {} to route('escalated.admin.chat.end',
// ticket.chat_session_id), so hosts map that name to
// POST /agent/chat/{session}/end, which takes the chat SESSION id.

// sessionChat starts and accepts a widget chat whose session id differs from
// its ticket id, so a route reading the wrong id cannot pass by accident.
func sessionChat(t *testing.T, f consoleFixture) (ticketID int64, session *models.ChatSession) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		filler := &models.Ticket{Subject: "Filler", Description: "Not a chat", TicketType: "question"}
		if err := f.esc.Store.CreateTicket(ctx, filler); err != nil {
			t.Fatal(err)
		}
	}
	_, ticketID, _ = acceptedChat(t, f)
	session, err := f.esc.Store.GetChatSessionByTicket(ctx, ticketID)
	if err != nil || session == nil {
		t.Fatalf("chat session for ticket %d: %v", ticketID, err)
	}
	if session.ID == ticketID {
		t.Fatalf("session id %d equals ticket id; the fixture cannot tell them apart", session.ID)
	}
	return ticketID, session
}

func TestChatEndBySessionID(t *testing.T) {
	for _, mount := range []string{"chi", "stdlib"} {
		t.Run(mount, func(t *testing.T) {
			f := chatFixture(t, mount)
			ticketID, session := sessionChat(t, f)
			path := fmt.Sprintf("/agent/chat/%d/end", session.ID)

			code, out := f.call(t, "5", http.MethodPost, path, map[string]any{}, "X-Requested-With", "XMLHttpRequest")
			if code != http.StatusOK || out["message"] != "Chat ended." {
				t.Fatalf("end: %d %v", code, out)
			}
			ended, _ := f.esc.Store.GetChatSessionByTicket(context.Background(), ticketID)
			if ended.Status != models.ChatStatusEnded || ended.EndedAt == nil {
				t.Errorf("session after end = status %d ended_at %v", ended.Status, ended.EndedAt)
			}

			code, out = f.call(t, "5", http.MethodPost, path, map[string]any{})
			if code != http.StatusUnprocessableEntity || out["message"] != "Chat is already ended." {
				t.Errorf("end again: %d %v, want 422", code, out)
			}
		})
	}
}

func TestChatEndBySessionIDIsRefused(t *testing.T) {
	for _, mount := range []string{"chi", "stdlib"} {
		t.Run(mount, func(t *testing.T) {
			f := chatFixture(t, mount)
			_, session := sessionChat(t, f)
			path := fmt.Sprintf("/agent/chat/%d/end", session.ID)

			if code, out := f.call(t, "7", http.MethodPost, path, map[string]any{}); code != http.StatusForbidden && code != http.StatusUnauthorized {
				t.Errorf("non-agent: %d %v", code, out)
			}
			if code, out := f.call(t, "5", http.MethodPost, "/agent/chat/999999/end", map[string]any{}); code != http.StatusNotFound {
				t.Errorf("unknown session: %d %v, want 404", code, out)
			}
			fresh, _ := f.esc.Store.GetChatSession(context.Background(), session.ID)
			if fresh.Status == models.ChatStatusEnded {
				t.Errorf("a refused end ended the chat")
			}
		})
	}
}

// The ticket-id console route keeps its shape.
func TestChatEndByTicketIDStillWorks(t *testing.T) {
	for _, mount := range []string{"chi", "stdlib"} {
		t.Run(mount, func(t *testing.T) {
			f := chatFixture(t, mount)
			ticketID, _ := sessionChat(t, f)
			code, out := f.call(t, "5", http.MethodPost, fmt.Sprintf("/agent/chat/sessions/%d/end", ticketID), nil)
			if code != http.StatusOK || out["status"] != "ended" {
				t.Fatalf("end by ticket: %d %v", code, out)
			}
			ended, _ := f.esc.Store.GetChatSessionByTicket(context.Background(), ticketID)
			if ended.Status != models.ChatStatusEnded {
				t.Errorf("session status = %d", ended.Status)
			}
		})
	}
}
