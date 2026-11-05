package router_test

import (
	"context"
	"net/http"
	"testing"

	escalated "github.com/escalated-dev/escalated-go"
	"github.com/escalated-dev/escalated-go/models"
)

// Live chat goes only to agents whose own status is "online", as
// escalated-laravel's chat routing reads agent_profiles.chat_status (ADR
// 2026-11-05-agent-panel-data). Away and offline agents, and agents who never
// set a status, keep chat unavailable and get no chats.
func TestChatGoesOnlyToOnlineAgents(t *testing.T) {
	for _, mount := range []string{"chi", "stdlib"} {
		t.Run(mount, func(t *testing.T) {
			f := newConsoleFixture(t, mount, func(c *escalated.Config) { c.Mailer = newCodeMailer() })
			ctx := context.Background()
			if err := f.esc.Store.CreateChatRoutingRule(ctx, &models.ChatRoutingRule{
				Name: "All", Strategy: "round_robin", AgentIDs: []models.UserID{"5", "6", "7"}, MaxConcurrentChats: 3, IsActive: true,
			}); err != nil {
				t.Fatal(err)
			}
			panel := f.esc.AgentPanel()
			if err := panel.SetStatus(ctx, "5", "away"); err != nil {
				t.Fatal(err)
			}
			if err := panel.SetStatus(ctx, "6", "offline"); err != nil {
				t.Fatal(err)
			}

			if code, out := f.call(t, "", http.MethodGet, "/widget/chat/availability", nil); code != http.StatusOK || out["available"] != false {
				t.Fatalf("nobody online: availability %d %v", code, out)
			}
			if agent := startChat(t, f, "ada@example.com"); agent != nil {
				t.Fatalf("nobody online: chat went to %v", *agent)
			}

			if err := panel.SetStatus(ctx, "7", "online"); err != nil {
				t.Fatal(err)
			}
			if code, out := f.call(t, "", http.MethodGet, "/widget/chat/availability", nil); code != http.StatusOK || out["available"] != true {
				t.Fatalf("agent 7 online: availability %d %v", code, out)
			}
			if agent := startChat(t, f, "bea@example.com"); agent == nil || *agent != "7" {
				t.Fatalf("agent 7 online: chat went to %v", agent)
			}
		})
	}
}

// startChat starts a widget chat and answers the agent routing gave it.
func startChat(t *testing.T, f consoleFixture, email string) *models.UserID {
	t.Helper()
	code, out := f.call(t, "", http.MethodPost, "/widget/chat/start", withProof(t, f.esc, "chat", map[string]any{
		"name": "Guest", "email": email, "message": "Hello",
	}))
	if code != http.StatusCreated {
		t.Fatalf("start: %d %v", code, out)
	}
	ref, _ := out["ticket_reference"].(string)
	ticket, err := f.esc.Store.GetTicketByReference(context.Background(), ref)
	if err != nil || ticket == nil {
		t.Fatalf("ticket %q: %v", ref, err)
	}
	session, err := f.esc.Store.GetChatSessionByTicket(context.Background(), ticket.ID)
	if err != nil || session == nil {
		t.Fatalf("session for %q: %v", ref, err)
	}
	return session.AgentID
}
