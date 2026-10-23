package router_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/escalated-dev/escalated-go/models"
)

// escalated.admin.chat.transfer (escalated-laravel ChatController::transfer):
// the shared Agent/TicketShow page POSTs {"agent_id"} or {"department_id"} to
// route('escalated.admin.chat.transfer', ticket.chat_session_id), so hosts map
// that name to POST /agent/chat/{session}/transfer, which takes the chat
// SESSION id. The ticket-id console route stays as it was.

func TestChatTransferBySessionIDToAgent(t *testing.T) {
	for _, mount := range []string{"chi", "stdlib"} {
		t.Run(mount, func(t *testing.T) {
			f := chatFixture(t, mount)
			ticketID, session := sessionChat(t, f)
			path := fmt.Sprintf("/agent/chat/%d/transfer", session.ID)

			code, out := f.call(t, "5", http.MethodPost, path, map[string]any{"agent_id": 6}, "X-Requested-With", "XMLHttpRequest")
			if code != http.StatusOK || out["message"] != "Chat transferred." {
				t.Fatalf("transfer: %d %v", code, out)
			}
			moved, _ := f.esc.Store.GetChatSessionByTicket(context.Background(), ticketID)
			if moved.ID != session.ID || moved.AgentID == nil || *moved.AgentID != "6" {
				t.Errorf("session after transfer = id %d agent %v", moved.ID, moved.AgentID)
			}
		})
	}
}

func TestChatTransferBySessionIDToDepartment(t *testing.T) {
	for _, mount := range []string{"chi", "stdlib"} {
		t.Run(mount, func(t *testing.T) {
			f := chatFixture(t, mount)
			ticketID, session := sessionChat(t, f)
			dept := transferDepartment(t, f)

			code, out := f.call(t, "5", http.MethodPost, fmt.Sprintf("/agent/chat/%d/transfer", session.ID), map[string]any{"department_id": dept})
			if code != http.StatusOK {
				t.Fatalf("transfer: %d %v", code, out)
			}
			moved, _ := f.esc.Store.GetChatSessionByTicket(context.Background(), ticketID)
			if moved.AgentID != nil || moved.Status != models.ChatStatusWaiting {
				t.Errorf("session after department transfer = agent %v status %d", moved.AgentID, moved.Status)
			}
			ticket, _ := f.esc.Store.GetTicket(context.Background(), ticketID)
			if ticket.DepartmentID == nil || *ticket.DepartmentID != dept {
				t.Errorf("ticket department = %v, want %d", ticket.DepartmentID, dept)
			}
		})
	}
}

func TestChatTransferBySessionIDIsRefused(t *testing.T) {
	for _, mount := range []string{"chi", "stdlib"} {
		t.Run(mount, func(t *testing.T) {
			f := chatFixture(t, mount)
			ticketID, session := sessionChat(t, f)
			path := fmt.Sprintf("/agent/chat/%d/transfer", session.ID)

			if code, out := f.call(t, "7", http.MethodPost, path, map[string]any{"agent_id": 6}); code != http.StatusForbidden && code != http.StatusUnauthorized {
				t.Errorf("non-agent: %d %v", code, out)
			}
			if code, out := f.call(t, "5", http.MethodPost, path, map[string]any{}); code != http.StatusUnprocessableEntity {
				t.Errorf("no target: %d %v, want 422", code, out)
			}
			if code, out := f.call(t, "5", http.MethodPost, "/agent/chat/999999/transfer", map[string]any{"agent_id": 6}); code != http.StatusNotFound {
				t.Errorf("unknown session: %d %v, want 404", code, out)
			}
			// The ticket id is not a session id on this route.
			if code, out := f.call(t, "5", http.MethodPost, fmt.Sprintf("/agent/chat/%d/transfer", ticketID), map[string]any{"agent_id": 6}); code == http.StatusOK {
				if s, _ := f.esc.Store.GetChatSession(context.Background(), ticketID); s == nil {
					t.Errorf("ticket id accepted as a session id: %d %v", code, out)
				}
			}
			fresh, _ := f.esc.Store.GetChatSession(context.Background(), session.ID)
			if fresh.AgentID == nil || *fresh.AgentID != "5" {
				t.Errorf("a refused transfer moved the chat to %v", fresh.AgentID)
			}

			if code, out := f.call(t, "5", http.MethodPost, fmt.Sprintf("/agent/chat/%d/end", session.ID), map[string]any{}); code != http.StatusOK {
				t.Fatalf("end: %d %v", code, out)
			}
			if code, out := f.call(t, "5", http.MethodPost, path, map[string]any{"agent_id": 6}); code != http.StatusUnprocessableEntity {
				t.Errorf("ended chat: %d %v, want 422", code, out)
			}
		})
	}
}
