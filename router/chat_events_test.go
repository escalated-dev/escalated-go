package router_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	escalated "github.com/escalated-dev/escalated-go"
	"github.com/escalated-dev/escalated-go/internal/sqldialect"
	"github.com/escalated-dev/escalated-go/services"
)

// The live chat events this package produces (ADR 2026-10-21). There is no
// chat transfer here, so chat.transferred is neither offered nor sent.
var chatEvents = []string{"chat.started", "chat.assigned", "chat.message", "chat.ended"}

const chatVisitorEmail = "visitor@example.com"
const chatSecretMessage = "my order number is 4242"

type chatDelivery struct {
	event   string
	raw     string
	payload map[string]any
}

// chatDeliveries waits until want deliveries are recorded and returns them in
// order. The webhook points at a private address, so the dispatcher refuses
// to send, but every attempt is recorded with the payload it would carry.
func chatDeliveries(t *testing.T, db *sql.DB, want int) []chatDelivery {
	t.Helper()
	var out []chatDelivery
	deadline := time.Now().Add(5 * time.Second)
	for {
		out = nil
		rows, err := db.Query(`SELECT event, payload FROM escalated_webhook_deliveries ORDER BY id`)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var d chatDelivery
			if err := rows.Scan(&d.event, &d.raw); err != nil {
				t.Fatal(err)
			}
			_ = json.Unmarshal([]byte(d.raw), &d.payload)
			out = append(out, d)
		}
		rows.Close()
		if len(out) >= want || time.Now().After(deadline) {
			return out
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func onlyChatDelivery(t *testing.T, all []chatDelivery, event string) map[string]any {
	t.Helper()
	var found []chatDelivery
	for _, d := range all {
		if d.event == event {
			found = append(found, d)
		}
	}
	if len(found) != 1 {
		t.Fatalf("%s delivered %d time(s); all: %+v", event, len(found), all)
	}
	chat, ok := found[0].payload["chat"].(map[string]any)
	if !ok {
		t.Fatalf("%s payload has no chat object: %s", event, found[0].raw)
	}
	return chat
}

func assertChatPayloadPrivate(t *testing.T, all []chatDelivery, secrets ...string) {
	t.Helper()
	for _, d := range all {
		for _, s := range append(secrets, chatVisitorEmail, chatSecretMessage) {
			if s != "" && strings.Contains(d.raw, s) {
				t.Errorf("%s payload contains %q: %s", d.event, s, d.raw)
			}
		}
	}
}

func insertChatWebhook(t *testing.T, db *sql.DB, events []string) {
	t.Helper()
	ev, _ := json.Marshal(events)
	if _, err := sqldialect.ExecInsert(db, `INSERT INTO escalated_webhooks (url, events, secret, active, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		"https://10.0.0.1/hook", ev, nil, true, time.Now(), time.Now()); err != nil {
		t.Fatal(err)
	}
}

func startWidgetChat(t *testing.T, f consoleFixture) (token string, ticketID int64) {
	t.Helper()
	code, out := f.call(t, "", http.MethodPost, "/widget/chat/start", withProof(t, f.esc, "chat", map[string]any{
		"name": "Visitor", "email": chatVisitorEmail, "subject": "Help",
	}))
	if code != http.StatusCreated {
		t.Fatalf("chat start: %d %v", code, out)
	}
	token, _ = out["id"].(string)
	ref, _ := out["ticket_reference"].(string)
	tk, err := f.esc.Store.GetTicketByReference(t.Context(), ref)
	if err != nil || tk == nil {
		t.Fatalf("chat ticket %q: %v", ref, err)
	}
	return token, tk.ID
}

func chatFixture(t *testing.T, mount string) consoleFixture {
	return newConsoleFixture(t, mount, func(c *escalated.Config) { c.Mailer = newCodeMailer() })
}

func TestChatEventsAreOfferedToWebhooksAndWorkflows(t *testing.T) {
	offered := func(list []string, name string) bool {
		for _, e := range list {
			if e == name {
				return true
			}
		}
		return false
	}
	for _, e := range chatEvents {
		if !offered(services.SupportedWebhookEvents, e) {
			t.Errorf("webhook form does not offer %s", e)
		}
		if !offered(services.TriggerEvents, e) {
			t.Errorf("workflow builder does not offer %s", e)
		}
	}
	if offered(services.SupportedWebhookEvents, "chat.transferred") || offered(services.TriggerEvents, "chat.transferred") {
		t.Error("chat.transferred is offered, but nothing here can transfer a chat")
	}
}

func TestChatEventsReachWebhooksWithoutTheTokenEmailOrBody(t *testing.T) {
	for _, mount := range []string{"chi", "stdlib"} {
		t.Run(mount, func(t *testing.T) {
			f := chatFixture(t, mount)
			db := f.esc.Config.DB
			insertChatWebhook(t, db, chatEvents)

			token, ticketID := startWidgetChat(t, f)
			all := chatDeliveries(t, db, 1)
			started := onlyChatDelivery(t, all, "chat.started")
			if started["ticket_id"] != float64(ticketID) || started["status"] != "waiting" || started["agent_id"] != nil {
				t.Errorf("chat.started = %v", started)
			}
			if _, ok := started["session_id"]; !ok {
				t.Errorf("chat.started has no session_id: %v", started)
			}
			if _, ok := started["department_id"]; !ok {
				t.Errorf("chat.started has no department_id: %v", started)
			}

			base := fmt.Sprintf("/agent/chat/sessions/%d", ticketID)
			if code, _ := f.call(t, "5", http.MethodPost, base+"/accept", nil); code != http.StatusOK {
				t.Fatalf("accept: %d", code)
			}
			all = chatDeliveries(t, db, 2)
			assigned := onlyChatDelivery(t, all, "chat.assigned")
			if fmt.Sprint(assigned["agent_id"]) != "5" || assigned["status"] != "active" {
				t.Errorf("chat.assigned = %v", assigned)
			}

			if code, out := f.call(t, "", http.MethodPost, "/widget/chat/"+token+"/messages", map[string]any{"body": chatSecretMessage}); code != http.StatusCreated && code != http.StatusOK {
				t.Fatalf("visitor message: %d %v", code, out)
			}
			all = chatDeliveries(t, db, 3)
			message := onlyChatDelivery(t, all, "chat.message")
			if message["author_type"] != "visitor" || message["reply_id"] == nil {
				t.Errorf("chat.message = %v", message)
			}

			if code, out := f.call(t, "", http.MethodPost, "/widget/chat/"+token+"/end", nil); code != http.StatusOK {
				t.Fatalf("visitor end: %d %v", code, out)
			}
			all = chatDeliveries(t, db, 4)
			ended := onlyChatDelivery(t, all, "chat.ended")
			if ended["status"] != "ended" || ended["ended_by"] != "customer" {
				t.Errorf("chat.ended = %v", ended)
			}

			assertChatPayloadPrivate(t, all, token)
		})
	}
}

func TestAgentChatMessageAndEndAreSent(t *testing.T) {
	f := chatFixture(t, "chi")
	db := f.esc.Config.DB
	insertChatWebhook(t, db, []string{"chat.message", "chat.ended"})
	token, ticketID := startWidgetChat(t, f)
	base := fmt.Sprintf("/agent/chat/sessions/%d", ticketID)
	f.call(t, "5", http.MethodPost, base+"/accept", nil)

	if code, _ := f.call(t, "5", http.MethodPost, base+"/message", map[string]any{"body": "How can I help?"}); code != http.StatusCreated {
		t.Fatalf("agent message: %d", code)
	}
	if code, _ := f.call(t, "5", http.MethodPost, base+"/end", nil); code != http.StatusOK {
		t.Fatalf("agent end: %d", code)
	}
	all := chatDeliveries(t, db, 2)
	if m := onlyChatDelivery(t, all, "chat.message"); m["author_type"] != "agent" {
		t.Errorf("chat.message = %v", m)
	}
	if e := onlyChatDelivery(t, all, "chat.ended"); e["ended_by"] != "agent" {
		t.Errorf("chat.ended = %v", e)
	}
	assertChatPayloadPrivate(t, all, token, "How can I help?")
}

func TestLegacyChatSubscriptionsReceiveCanonicalEvents(t *testing.T) {
	f := chatFixture(t, "chi")
	db := f.esc.Config.DB
	insertChatWebhook(t, db, []string{"chat.session_started", "chat.session_ended"})
	token, _ := startWidgetChat(t, f)
	chatDeliveries(t, db, 1)
	f.call(t, "", http.MethodPost, "/widget/chat/"+token+"/end", nil)

	all := chatDeliveries(t, db, 2)
	var got []string
	for _, d := range all {
		got = append(got, d.event)
	}
	if strings.Join(got, ",") != "chat.started,chat.ended" {
		t.Errorf("delivered %v, want chat.started then chat.ended", got)
	}
}

func TestChatEventsRunWorkflows(t *testing.T) {
	f := chatFixture(t, "chi")
	db := f.esc.Config.DB
	ids := map[string]int64{}
	for _, e := range chatEvents {
		res, err := sqldialect.ExecInsert(db, `INSERT INTO escalated_workflows (name, trigger_event, conditions, actions, is_active, stop_on_match, position) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			e, e, `{}`, `[]`, true, false, 0)
		if err != nil {
			t.Fatal(err)
		}
		ids[e], _ = res.LastInsertId()
	}
	legacy, err := sqldialect.ExecInsert(db, `INSERT INTO escalated_workflows (name, trigger_event, conditions, actions, is_active, stop_on_match, position) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"legacy", "chat.session_started", `{}`, `[]`, true, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	ids["legacy chat.session_started"], _ = legacy.LastInsertId()

	token, ticketID := startWidgetChat(t, f)
	base := fmt.Sprintf("/agent/chat/sessions/%d", ticketID)
	f.call(t, "5", http.MethodPost, base+"/accept", nil)
	f.call(t, "", http.MethodPost, "/widget/chat/"+token+"/messages", map[string]any{"body": "hi"})
	f.call(t, "", http.MethodPost, "/widget/chat/"+token+"/end", nil)

	for name, id := range ids {
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
			t.Errorf("%s workflow ran %d time(s), want 1", name, n)
		}
	}
}
