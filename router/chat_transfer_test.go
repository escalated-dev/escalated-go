package router_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/escalated-dev/escalated-go/internal/sqldialect"
	"github.com/escalated-dev/escalated-go/models"
)

// Chat transfer (escalated-laravel ChatController::transfer and
// ChatSessionService::transferChat): an agent hands a live chat to another
// agent and/or department, and chat.transferred reaches webhooks.

func acceptedChat(t *testing.T, f consoleFixture) (token string, ticketID int64, base string) {
	t.Helper()
	token, ticketID = startWidgetChat(t, f)
	base = fmt.Sprintf("/agent/chat/sessions/%d", ticketID)
	if code, out := f.call(t, "5", http.MethodPost, base+"/accept", nil); code != http.StatusOK {
		t.Fatalf("accept: %d %v", code, out)
	}
	return token, ticketID, base
}

func transferDepartment(t *testing.T, f consoleFixture) int64 {
	t.Helper()
	d := &models.Department{Name: "Billing", Slug: "billing", IsActive: true}
	if err := f.esc.Store.CreateDepartment(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	return d.ID
}

func TestChatTransferToAnotherAgent(t *testing.T) {
	for _, mount := range []string{"chi", "stdlib"} {
		t.Run(mount, func(t *testing.T) {
			f := chatFixture(t, mount)
			db := f.esc.Config.DB
			insertChatWebhook(t, db, []string{"chat.transferred"})
			token, ticketID, base := acceptedChat(t, f)

			code, out := f.call(t, "5", http.MethodPost, base+"/transfer", map[string]any{"agent_id": 6})
			if code != http.StatusOK {
				t.Fatalf("transfer: %d %v", code, out)
			}
			if out["message"] != "Chat transferred." {
				t.Errorf("transfer response = %v", out)
			}

			ctx := context.Background()
			session, _ := f.esc.Store.GetChatSessionByTicket(ctx, ticketID)
			if session.AgentID == nil || *session.AgentID != "6" {
				t.Errorf("session agent = %v, want 6", session.AgentID)
			}
			ticket, _ := f.esc.Store.GetTicket(ctx, ticketID)
			if ticket.AssignedTo == nil || *ticket.AssignedTo != "6" {
				t.Errorf("ticket assignee = %v, want 6", ticket.AssignedTo)
			}

			all := chatDeliveries(t, db, 1)
			chat := onlyChatDelivery(t, all, "chat.transferred")
			if fmt.Sprint(chat["from_agent_id"]) != "5" || fmt.Sprint(chat["to_agent_id"]) != "6" || chat["to_department_id"] != nil {
				t.Errorf("chat.transferred = %v", chat)
			}
			if fmt.Sprint(chat["agent_id"]) != "6" || chat["ticket_id"] != float64(ticketID) {
				t.Errorf("chat.transferred = %v", chat)
			}
			assertChatPayloadPrivate(t, all, token)

			// A system note records the transfer on the ticket, out of the
			// visitor's transcript.
			replies, _ := f.esc.Store.ListReplies(ctx, models.ReplyFilters{TicketID: ticketID})
			found := false
			for _, r := range replies {
				if r.IsSystem && r.IsInternal && strings.Contains(r.Body, "transferred") {
					found = true
				}
			}
			if !found {
				t.Errorf("no system note for the transfer: %+v", replies)
			}
			_, transcript := f.call(t, "", http.MethodGet, "/widget/chat/"+token+"/messages", nil)
			if strings.Contains(fmt.Sprint(transcript), "transferred") {
				t.Errorf("visitor transcript shows the transfer note: %v", transcript)
			}
		})
	}
}

func TestChatTransferToDepartmentReturnsItToTheQueue(t *testing.T) {
	f := chatFixture(t, "chi")
	db := f.esc.Config.DB
	insertChatWebhook(t, db, []string{"chat.transferred"})
	_, ticketID, base := acceptedChat(t, f)
	dept := transferDepartment(t, f)

	if code, out := f.call(t, "5", http.MethodPost, base+"/transfer", map[string]any{"department_id": dept}); code != http.StatusOK {
		t.Fatalf("transfer: %d %v", code, out)
	}
	ctx := context.Background()
	session, _ := f.esc.Store.GetChatSessionByTicket(ctx, ticketID)
	if session.AgentID != nil || session.Status != models.ChatStatusWaiting {
		t.Errorf("session = agent %v status %v, want unassigned and waiting", session.AgentID, session.Status)
	}
	ticket, _ := f.esc.Store.GetTicket(ctx, ticketID)
	if ticket.AssignedTo != nil || ticket.DepartmentID == nil || *ticket.DepartmentID != dept {
		t.Errorf("ticket = assignee %v department %v", ticket.AssignedTo, ticket.DepartmentID)
	}
	chat := onlyChatDelivery(t, chatDeliveries(t, db, 1), "chat.transferred")
	if fmt.Sprint(chat["from_agent_id"]) != "5" || chat["to_agent_id"] != nil || chat["to_department_id"] != float64(dept) {
		t.Errorf("chat.transferred = %v", chat)
	}
	if chat["status"] != "waiting" || chat["department_id"] != float64(dept) {
		t.Errorf("chat.transferred = %v", chat)
	}
}

func TestChatTransferToAgentAndDepartment(t *testing.T) {
	f := chatFixture(t, "chi")
	_, ticketID, base := acceptedChat(t, f)
	dept := transferDepartment(t, f)

	if code, out := f.call(t, "5", http.MethodPost, base+"/transfer", map[string]any{"agent_id": "6", "department_id": dept}); code != http.StatusOK {
		t.Fatalf("transfer: %d %v", code, out)
	}
	ctx := context.Background()
	session, _ := f.esc.Store.GetChatSessionByTicket(ctx, ticketID)
	ticket, _ := f.esc.Store.GetTicket(ctx, ticketID)
	if session.AgentID == nil || *session.AgentID != "6" || ticket.DepartmentID == nil || *ticket.DepartmentID != dept {
		t.Errorf("session agent %v, ticket department %v", session.AgentID, ticket.DepartmentID)
	}
}

// The shared ChatActionBar posts {type, id}.
func TestChatTransferAcceptsTheChatActionBarBody(t *testing.T) {
	f := chatFixture(t, "chi")
	_, ticketID, base := acceptedChat(t, f)
	if code, out := f.call(t, "5", http.MethodPost, base+"/transfer", map[string]any{"type": "agent", "id": 6}); code != http.StatusOK {
		t.Fatalf("transfer: %d %v", code, out)
	}
	session, _ := f.esc.Store.GetChatSessionByTicket(context.Background(), ticketID)
	if session.AgentID == nil || *session.AgentID != "6" {
		t.Errorf("session agent = %v", session.AgentID)
	}
}

func TestChatTransferIsRefused(t *testing.T) {
	f := chatFixture(t, "chi")
	db := f.esc.Config.DB
	insertChatWebhook(t, db, []string{"chat.transferred", "chat.ended"})
	token, ticketID, base := acceptedChat(t, f)

	cases := []struct {
		name string
		user string
		path string
		body any
		want int
	}{
		{"non-agent", "7", base + "/transfer", map[string]any{"agent_id": 6}, http.StatusForbidden},
		{"no target", "5", base + "/transfer", map[string]any{}, http.StatusUnprocessableEntity},
		{"unknown department", "5", base + "/transfer", map[string]any{"department_id": 999}, http.StatusUnprocessableEntity},
		{"unknown session", "5", "/agent/chat/sessions/999999/transfer", map[string]any{"agent_id": 6}, http.StatusNotFound},
	}
	for _, c := range cases {
		code, out := f.call(t, c.user, http.MethodPost, c.path, c.body)
		if c.name == "non-agent" && code == http.StatusUnauthorized {
			continue
		}
		if code != c.want {
			t.Errorf("%s: %d %v, want %d", c.name, code, out, c.want)
		}
	}

	f.call(t, "", http.MethodPost, "/widget/chat/"+token+"/end", nil)
	if code, out := f.call(t, "5", http.MethodPost, base+"/transfer", map[string]any{"agent_id": 6}); code != http.StatusUnprocessableEntity {
		t.Errorf("ended chat: %d %v", code, out)
	}

	session, _ := f.esc.Store.GetChatSessionByTicket(context.Background(), ticketID)
	if session.AgentID == nil || *session.AgentID != "5" {
		t.Errorf("a refused transfer moved the chat to %v", session.AgentID)
	}
	// Deliveries are recorded asynchronously; wait for chat.ended, then look
	// for any chat.transferred.
	for _, d := range chatDeliveries(t, db, 1) {
		if d.event == "chat.transferred" {
			t.Errorf("a refused transfer sent chat.transferred: %s", d.raw)
		}
	}
}

func TestChatTransferredRunsWorkflows(t *testing.T) {
	f := chatFixture(t, "chi")
	db := f.esc.Config.DB
	id := insertWorkflow(t, db, "chat.transferred")
	_, ticketID, base := acceptedChat(t, f)
	f.call(t, "5", http.MethodPost, base+"/transfer", map[string]any{"agent_id": 6})
	waitWorkflowRan(t, db, id, ticketID)
}

func insertWorkflow(t *testing.T, db *sql.DB, trigger string) int64 {
	t.Helper()
	res, err := sqldialect.ExecInsert(db, `INSERT INTO escalated_workflows (name, trigger_event, conditions, actions, is_active, stop_on_match, position) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		trigger, trigger, `{}`, `[]`, true, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

func waitWorkflowRan(t *testing.T, db *sql.DB, id, ticketID int64) {
	t.Helper()
	var n int
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_ = db.QueryRow(sqldialect.Rebind(db, `SELECT COUNT(*) FROM escalated_workflow_logs WHERE workflow_id = ? AND ticket_id = ?`), id, ticketID).Scan(&n)
		if n > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if n != 1 {
		t.Errorf("workflow %d ran %d time(s), want 1", id, n)
	}
}
