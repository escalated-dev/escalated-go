package router_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	escalated "github.com/escalated-dev/escalated-go"
	"github.com/escalated-dev/escalated-go/handlers"
	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/services"
)

type mentionDirectory struct{}

func (mentionDirectory) ListAgentsForSkillForm(context.Context) ([]handlers.SkillFormAgent, error) {
	return []handlers.SkillFormAgent{
		{ID: 5, Name: "Agent Five", Email: "five@example.com"},
		{ID: 6, Name: "Alice Smith", Email: "alice@example.com"},
		{ID: 9, Name: "Boss", Email: "boss@example.com"},
	}, nil
}

func newMentionFixture(t *testing.T, mount string) consoleFixture {
	return newConsoleFixture(t, mount, func(c *escalated.Config) {
		c.SkillAgentDirectory = mentionDirectory{}
		c.AgentCheck = func(r *http.Request) bool { u := r.Header.Get("X-User"); return u == "5" || u == "6" }
	})
}

func mentionTicket(t *testing.T, f consoleFixture) (*services.TicketService, *models.Ticket) {
	t.Helper()
	tickets := f.esc.NewTicketService()
	requester := models.UserID("300")
	userType := "user"
	ticket, err := tickets.Create(context.Background(), services.CreateTicketInput{
		Subject: "Printer on fire", Description: "Help", RequesterType: &userType, RequesterID: &requester,
	})
	if err != nil {
		t.Fatal(err)
	}
	return tickets, ticket
}

func feedOf(t *testing.T, f consoleFixture, user string) []any {
	t.Helper()
	code, out := f.call(t, user, http.MethodGet, "/agent/notifications", nil)
	if code != http.StatusOK {
		t.Fatalf("feed for %s: %d %v", user, code, out)
	}
	data, _ := out["data"].([]any)
	return data
}

// Agents mentioned in an agent's reply or note get a mention notification,
// as escalated-laravel's MentionService: @{Full Name} and @name, matched to
// the agent directory by name or email, ignoring case.
func TestAgentMentionsNotify(t *testing.T) {
	for _, mount := range []string{"chi", "stdlib"} {
		t.Run(mount, func(t *testing.T) {
			f := newMentionFixture(t, mount)
			tickets, ticket := mentionTicket(t, f)
			agentType := "agent"
			author := models.UserID("5")
			if _, err := tickets.AddReply(context.Background(), ticket.ID, "Can @{alice smith} look?", &agentType, &author, true); err != nil {
				t.Fatal(err)
			}

			items := feedOf(t, f, "6")
			if len(items) != 1 {
				t.Fatalf("alice's feed = %v", items)
			}
			item := items[0].(map[string]any)
			if item["type"] != "mention" || item["title"] != "Agent Five mentioned you on "+ticket.Reference {
				t.Fatalf("mention item = %v", item)
			}

			// An agent reply, by email.
			if _, err := tickets.AddReply(context.Background(), ticket.ID, "cc @{ALICE@example.com}", &agentType, &author, false); err != nil {
				t.Fatal(err)
			}
			if items := feedOf(t, f, "6"); len(items) != 2 {
				t.Fatalf("after the reply, alice's feed = %v", items)
			}
		})
	}
}

func TestAgentMentionsNeverNotifyTheAuthorOrComeFromCustomers(t *testing.T) {
	f := newMentionFixture(t, "chi")
	tickets, ticket := mentionTicket(t, f)
	agentType, userType := "agent", "user"
	author, customer := models.UserID("5"), models.UserID("300")
	if _, err := tickets.AddReply(context.Background(), ticket.ID, "@{Agent Five} @nobody", &agentType, &author, true); err != nil {
		t.Fatal(err)
	}
	if _, err := tickets.AddReply(context.Background(), ticket.ID, "Hi @{Alice Smith}", &userType, &customer, false); err != nil {
		t.Fatal(err)
	}
	for _, user := range []string{"5", "6"} {
		for _, item := range feedOf(t, f, user) {
			if item.(map[string]any)["type"] == "mention" {
				t.Errorf("%s got a mention: %v", user, item)
			}
		}
	}
}

// The composer's mention picker (escalated.admin.agents.search, Laravel's
// AgentSearchController): admins only, {id, name, email} by name or email.
func TestAgentMentionPicker(t *testing.T) {
	for _, mount := range []string{"chi", "stdlib"} {
		t.Run(mount, func(t *testing.T) {
			f := newMentionFixture(t, mount)
			search := func(user, q string) (int, []any) {
				t.Helper()
				req := "/admin/agents/search?q=" + q
				code, _ := f.call(t, user, http.MethodGet, req, nil)
				var rows []any
				if code == http.StatusOK {
					rows = callArray(t, f, user, req)
				}
				return code, rows
			}
			code, rows := search("9", "ALICE")
			if code != http.StatusOK || len(rows) != 1 {
				t.Fatalf("search alice: %d %v", code, rows)
			}
			row := rows[0].(map[string]any)
			if fmt.Sprint(row["id"]) != "6" || row["name"] != "Alice Smith" || row["email"] != "alice@example.com" {
				t.Fatalf("row = %v", row)
			}
			if _, rows := search("9", ""); len(rows) != 0 {
				t.Fatalf("empty query = %v", rows)
			}
			if code, _ := search("5", "alice"); code == http.StatusOK {
				t.Fatalf("an agent reached the admin picker")
			}
		})
	}
}

// callArray is f.call for a JSON array answer.
func callArray(t *testing.T, f consoleFixture, user, path string) []any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/escalated"+path, nil)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-User", user)
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, req)
	var rows []any
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatalf("%s: %v %s", path, err, rec.Body.String())
	}
	return rows
}
