package router_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	escalated "github.com/escalated-dev/escalated-go"
	"github.com/escalated-dev/escalated-go/internal/sqldialect"
	"github.com/escalated-dev/escalated-go/internal/testdb"
	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/services"
)

// The agent panel's data (escalated-developer-context ADR
// 2026-11-05-agent-panel-data, after escalated-laravel #292-#296): agent
// status, knowledge-base search, unread markers, customer presence and the
// agents' in-app notifications, through both routers.

const (
	panelAgent    = "900"
	panelOther    = "901"
	panelCustomer = "500"
	panelIndex    = "/escalated/agent/tickets"
)

type panelFixture struct {
	t       *testing.T
	esc     *escalated.Escalated
	tickets *services.TicketService
	mounts  map[string]http.Handler
}

func newPanelFixture(t *testing.T, configure ...func(*escalated.Config)) *panelFixture {
	t.Helper()
	cfg := escalated.DefaultConfig()
	cfg.DB = testdb.Open(t)
	cfg.UIEnabled = true
	cfg.AgentCheck = func(r *http.Request) bool {
		role := r.Header.Get(headerRole)
		return role == "agent" || role == "admin"
	}
	cfg.AdminCheck = func(r *http.Request) bool { return r.Header.Get(headerRole) == "admin" }
	cfg.UserIDFunc = func(r *http.Request) models.UserID { return models.UserID(r.Header.Get(headerUser)) }
	for _, c := range configure {
		c(&cfg)
	}
	esc, err := escalated.New(cfg)
	if err != nil {
		t.Fatalf("new escalated: %v", err)
	}
	f := &panelFixture{t: t, esc: esc, tickets: esc.NewTicketService()}
	f.mounts = (&uiFixture{esc: esc}).mounts()
	return f
}

// each runs fn against chi and the stdlib mux, on a fresh fixture each.
func eachPanelMount(t *testing.T, fn func(t *testing.T, f *panelFixture, h http.Handler), configure ...func(*escalated.Config)) {
	for _, name := range []string{"chi", "stdlib"} {
		t.Run(name, func(t *testing.T) {
			f := newPanelFixture(t, configure...)
			fn(t, f, f.mounts[name])
		})
	}
}

func (f *panelFixture) ticket(mods ...func(*services.CreateTicketInput)) *models.Ticket {
	f.t.Helper()
	userType := "User"
	requester := models.UserID(panelCustomer)
	in := services.CreateTicketInput{
		Subject: "Printer on fire", Description: "Help", Priority: models.PriorityLow,
		RequesterType: &userType, RequesterID: &requester,
	}
	for _, m := range mods {
		m(&in)
	}
	tk, err := f.tickets.Create(context.Background(), in)
	if err != nil {
		f.t.Fatalf("create ticket: %v", err)
	}
	return tk
}

func (f *panelFixture) exec(q string, args ...any) {
	f.t.Helper()
	db := f.esc.Config.DB
	if _, err := db.Exec(sqldialect.Rebind(db, q), args...); err != nil {
		f.t.Fatalf("%s: %v", q, err)
	}
}

func (f *panelFixture) count(q string, args ...any) int {
	f.t.Helper()
	db := f.esc.Config.DB
	var n int
	if err := db.QueryRow(sqldialect.Rebind(db, q), args...).Scan(&n); err != nil {
		f.t.Fatalf("%s: %v", q, err)
	}
	return n
}

func (f *panelFixture) reply(tk *models.Ticket, author string, internal bool) {
	f.t.Helper()
	var authorType *string
	var authorID *models.UserID
	if author != "" {
		ut := "User"
		id := models.UserID(author)
		authorType, authorID = &ut, &id
	}
	if _, err := f.tickets.AddReply(context.Background(), tk.ID, "A reply", authorType, authorID, internal); err != nil {
		f.t.Fatalf("reply: %v", err)
	}
}

// backdate moves the ticket's activity and the agent's read into the past:
// both are kept to the second, so a reply in the same second as a read would
// not be after it.
func (f *panelFixture) backdate(tk *models.Ticket, agent string) {
	f.exec(`UPDATE escalated_tickets SET last_customer_activity_at = ? WHERE id = ?`, time.Now().UTC().Add(-2*time.Minute).Truncate(time.Second), tk.ID)
	f.exec(`UPDATE escalated_ticket_reads SET last_read_at = ? WHERE ticket_id = ? AND user_id = ? AND last_read_at IS NOT NULL`, time.Now().UTC().Add(-time.Minute).Truncate(time.Second), tk.ID, agent)
}

func panelRows(t *testing.T, h http.Handler, agent string) map[string]map[string]any {
	t.Helper()
	page := visit(t, h, panelIndex, agent, "agent")
	out := map[string]map[string]any{}
	for _, row := range list(t, obj(t, page.Props, "tickets"), "data") {
		r := row.(map[string]any)
		out[r["reference"].(string)] = r
	}
	return out
}

func jsonOf(t *testing.T, code int, body []byte, want int) map[string]any {
	t.Helper()
	if code != want {
		t.Fatalf("status %d, want %d: %s", code, want, body)
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("not JSON: %s", body)
	}
	return out
}

func api(t *testing.T, h http.Handler, method, path, user string, body string, want int) map[string]any {
	t.Helper()
	rec := sendAPI(h, method, path, user, "agent", body)
	return jsonOf(t, rec.Code, rec.Body.Bytes(), want)
}

// ---- Agent status ---------------------------------------------------------------

func TestAgentPanelStatus(t *testing.T) {
	eachPanelMount(t, func(t *testing.T, f *panelFixture, h http.Handler) {
		shared := obj(t, visit(t, h, panelIndex, panelAgent, "agent").Props, "escalated")
		if shared["agent_status"] != "offline" {
			t.Fatalf("agent_status %v, want offline", shared["agent_status"])
		}
		wantJSON(t, "agent_statuses", shared["agent_statuses"], `["online","away","offline"]`)

		got := api(t, h, http.MethodPut, "/escalated/agent/status", panelAgent, `{"status":"away"}`, http.StatusOK)
		if got["agent_status"] != "away" {
			t.Fatalf("answer %v", got)
		}
		if s := obj(t, visit(t, h, panelIndex, panelAgent, "agent").Props, "escalated")["agent_status"]; s != "away" {
			t.Fatalf("shared agent_status %v after PUT", s)
		}
		api(t, h, http.MethodPost, "/escalated/agent/status", panelAgent, `{"status":"online"}`, http.StatusOK)
		if s := f.esc.AgentPanel().Status(context.Background(), panelAgent); s != "online" {
			t.Fatalf("stored status %q", s)
		}

		bad := api(t, h, http.MethodPut, "/escalated/agent/status", panelAgent, `{"status":"busy"}`, http.StatusUnprocessableEntity)
		if _, ok := bad["errors"].(map[string]any)["status"]; !ok {
			t.Fatalf("errors %v", bad)
		}

		// An Inertia visit goes back.
		rec := send(h, http.MethodPut, "/escalated/agent/status", panelAgent, "agent", `{"status":"away"}`)
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("Inertia PUT: %d", rec.Code)
		}

		// Customers have no status and cannot set one.
		if rec := sendAPI(h, http.MethodPut, "/escalated/agent/status", panelCustomer, "", `{"status":"away"}`); rec.Code == http.StatusOK {
			t.Fatal("a customer set an agent status")
		}
		customerPage := visit(t, h, "/escalated/tickets", panelCustomer, "")
		if shared, ok := customerPage.Props["escalated"].(map[string]any); ok {
			for _, k := range []string{"agent_status", "agent_statuses", "notifications"} {
				if _, ok := shared[k]; ok {
					t.Errorf("a customer page shares %q", k)
				}
			}
		}
	})
}

// ---- Knowledge base search ---------------------------------------------------------

func (f *panelFixture) article(title, slug, body, status string, category any, updated time.Time) int64 {
	f.t.Helper()
	db := f.esc.Config.DB
	var id int64
	err := db.QueryRow(sqldialect.Rebind(db, `INSERT INTO escalated_articles (title, slug, body, status, category_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING id`), title, slug, body, status, category, updated, updated).Scan(&id)
	if err != nil {
		f.t.Fatalf("article: %v", err)
	}
	return id
}

func kbSearch(t *testing.T, h http.Handler, query string, want int) map[string]any {
	t.Helper()
	return api(t, h, http.MethodGet, "/escalated/agent/kb/search?"+query, panelAgent, "", want)
}

func kbIDs(t *testing.T, body map[string]any) []int64 {
	ids := []int64{}
	for _, item := range list(t, body, "data") {
		ids = append(ids, int64(item.(map[string]any)["id"].(float64)))
	}
	return ids
}

func TestAgentPanelKnowledgeBaseSearch(t *testing.T) {
	eachPanelMount(t, func(t *testing.T, f *panelFixture, h http.Handler) {
		db := f.esc.Config.DB
		var category int64
		if err := db.QueryRow(sqldialect.Rebind(db, `INSERT INTO escalated_article_categories (name, slug) VALUES (?, ?) RETURNING id`), "Accounts", "accounts").Scan(&category); err != nil {
			t.Fatal(err)
		}
		now := time.Now().UTC().Truncate(time.Second)
		bodyMatch := f.article("Account basics", "basics", "<p>To reset your password open   Settings.</p>", "published", category, now.Add(-10*time.Second))
		oldTitle := f.article("Password rules", "rules", "x", "published", nil, now.Add(-500*time.Second))
		newTitle := f.article("Password reset", "reset", "x", "published", nil, now.Add(-100*time.Second))
		f.article("Password draft", "draft", "x", "draft", nil, now)

		body := kbSearch(t, h, "q=password", http.StatusOK)
		wantJSON(t, "ids", kbIDs(t, body), fmt.Sprintf("[%d,%d,%d]", newTitle, oldTitle, bodyMatch))
		items := list(t, body, "data")
		last := items[len(items)-1].(map[string]any)
		hasKeys(t, "item", last, "id", "title", "excerpt", "url", "category", "updated_at")
		if len(last) != 6 {
			t.Errorf("item keys %v", keysOf(last))
		}
		wantJSON(t, "category", last["category"], fmt.Sprintf(`{"id":%d,"name":"Accounts"}`, category))
		if last["excerpt"] != "To reset your password open Settings." {
			t.Errorf("excerpt %q", last["excerpt"])
		}
		if last["url"] != "http://example.com/escalated/kb/basics" {
			t.Errorf("url %q", last["url"])
		}
		if items[0].(map[string]any)["category"] != nil {
			t.Errorf("uncategorised article has a category")
		}

		// Case-insensitive, with % and _ taken literally.
		percent := f.article("Save 50% today", "percent", "x", "published", nil, now)
		f.article("Save 50 dollars today", "dollars", "x", "published", nil, now)
		underscore := f.article("The snake_case guide", "snake", "x", "published", nil, now)
		f.article("The snakeXcase guide", "snakex", "x", "published", nil, now)
		wantJSON(t, "percent", kbIDs(t, kbSearch(t, h, url.Values{"q": {"50%"}}.Encode(), http.StatusOK)), fmt.Sprintf("[%d]", percent))
		wantJSON(t, "underscore", kbIDs(t, kbSearch(t, h, url.Values{"q": {"SNAKE_CASE"}}.Encode(), http.StatusOK)), fmt.Sprintf("[%d]", underscore))

		// Validation.
		for query, field := range map[string]string{
			"q=a":                           "q",
			"q=" + strings.Repeat("a", 201): "q",
			"q=abc&limit=0":                 "limit",
			"q=abc&limit=21":                "limit",
		} {
			errs := kbSearch(t, h, query, http.StatusUnprocessableEntity)["errors"].(map[string]any)
			if _, ok := errs[field]; !ok {
				t.Errorf("%s: errors %v", query, errs)
			}
		}
		if n := len(kbIDs(t, kbSearch(t, h, "q=password&limit=2", http.StatusOK))); n != 2 {
			t.Errorf("limit=2 answered %d", n)
		}

		// 404 while the knowledge base is off.
		if err := f.esc.Store.SetSetting(context.Background(), "knowledge_base_enabled", "0"); err != nil {
			t.Fatal(err)
		}
		kbSearch(t, h, "q=password", http.StatusNotFound)
	})
}

// ---- Unread markers ------------------------------------------------------------------

func TestAgentPanelUnreadMarkers(t *testing.T) {
	eachPanelMount(t, func(t *testing.T, f *panelFixture, h http.Handler) {
		tk := f.ticket()
		if panelRows(t, h, panelAgent)[tk.Reference]["is_unread"] != true {
			t.Fatal("a new ticket is not unread")
		}
		show := visit(t, h, panelIndex+"/"+tk.Reference, panelAgent, "agent")
		if obj(t, show.Props, "ticket")["is_unread"] != false {
			t.Fatal("the ticket page says unread")
		}
		if panelRows(t, h, panelAgent)[tk.Reference]["is_unread"] != false {
			t.Fatal("opening the ticket did not read it")
		}
		if panelRows(t, h, panelOther)[tk.Reference]["is_unread"] != true {
			t.Fatal("markers are not per agent")
		}

		// Agents' replies and notes are not customer activity.
		f.backdate(tk, panelAgent)
		f.reply(tk, panelOther, false)
		f.reply(tk, panelOther, true)
		if panelRows(t, h, panelAgent)[tk.Reference]["is_unread"] != false {
			t.Fatal("an agent's reply made the ticket unread")
		}
		// The requester's reply is.
		f.reply(tk, panelCustomer, false)
		if panelRows(t, h, panelAgent)[tk.Reference]["is_unread"] != true {
			t.Fatal("the customer's reply did not make the ticket unread")
		}
		// A guest or inbound email (no author) is the customer side too.
		visit(t, h, panelIndex+"/"+tk.Reference, panelAgent, "agent")
		f.backdate(tk, panelAgent)
		f.reply(tk, "", false)
		if panelRows(t, h, panelAgent)[tk.Reference]["is_unread"] != true {
			t.Fatal("an inbound reply did not make the ticket unread")
		}

		// The read and unread routes.
		path := panelIndex + "/" + tk.Reference
		wantJSON(t, "read", api(t, h, http.MethodPost, path+"/read", panelAgent, "", http.StatusOK), `{"is_unread":false}`)
		if panelRows(t, h, panelAgent)[tk.Reference]["is_unread"] != false {
			t.Fatal("read route")
		}
		wantJSON(t, "unread", api(t, h, http.MethodPost, path+"/unread", panelAgent, "", http.StatusOK), `{"is_unread":true}`)
		if panelRows(t, h, panelAgent)[tk.Reference]["is_unread"] != true {
			t.Fatal("unread route")
		}
		if rec := sendAPI(h, http.MethodPost, panelIndex+"/NOPE-1/read", panelAgent, "agent", ""); rec.Code != http.StatusNotFound {
			t.Fatalf("unknown ticket: %d", rec.Code)
		}
	})
}

func TestAgentPanelBulkMarkers(t *testing.T) {
	eachPanelMount(t, func(t *testing.T, f *panelFixture, h http.Handler) {
		a, b := f.ticket(), f.ticket()
		api(t, h, http.MethodPost, panelIndex+"/bulk", panelAgent, fmt.Sprintf(`{"ticket_ids":[%d,%d],"action":"mark_read"}`, a.ID, b.ID), http.StatusOK)
		rows := panelRows(t, h, panelAgent)
		if rows[a.Reference]["is_unread"] != false || rows[b.Reference]["is_unread"] != false {
			t.Fatal("mark_read")
		}
		api(t, h, http.MethodPost, panelIndex+"/bulk", panelAgent, fmt.Sprintf(`{"ticket_ids":[%d],"action":"mark_unread"}`, a.ID), http.StatusOK)
		rows = panelRows(t, h, panelAgent)
		if rows[a.Reference]["is_unread"] != true || rows[b.Reference]["is_unread"] != false {
			t.Fatal("mark_unread")
		}
	})
}

func TestAgentPanelInboxUnreadCounts(t *testing.T) {
	eachPanelMount(t, func(t *testing.T, f *panelFixture, h http.Handler) {
		agent := models.UserID(panelAgent)
		mine := f.ticket()
		other := f.ticket()
		for _, tk := range []*models.Ticket{mine, other} {
			if err := f.tickets.Assign(context.Background(), tk.ID, agent, nil); err != nil {
				t.Fatal(err)
			}
		}
		unassigned := f.ticket()
		resolved := f.ticket()
		if err := f.tickets.ChangeStatus(context.Background(), resolved.ID, models.StatusResolved, nil); err != nil {
			t.Fatal(err)
		}
		visit(t, h, panelIndex+"/"+mine.Reference, panelAgent, "agent")
		visit(t, h, panelIndex+"/"+unassigned.Reference, panelAgent, "agent")

		inbox := obj(t, visit(t, h, panelIndex, panelAgent, "agent").Props, "inbox")
		wantJSON(t, "unread", inbox["unread"], `{"all":1,"mine":1,"unassigned":0}`)
	})
}

func TestAgentPanelOnlyVisibleTicketsAreCountedAndMarked(t *testing.T) {
	eachPanelMount(t, func(t *testing.T, f *panelFixture, h http.Handler) {
		ctx := context.Background()
		own := &models.Department{Name: "Own", Slug: "own", IsActive: true}
		hiddenDept := &models.Department{Name: "Hidden", Slug: "hidden", IsActive: true}
		for _, d := range []*models.Department{own, hiddenDept} {
			if err := f.esc.Store.CreateDepartment(ctx, d); err != nil {
				t.Fatal(err)
			}
		}
		f.exec(`INSERT INTO escalated_department_agent (department_id, agent_id) VALUES (?, ?)`, own.ID, panelAgent)
		visible := f.ticket(func(in *services.CreateTicketInput) { in.DepartmentID = &own.ID })
		hidden := f.ticket(func(in *services.CreateTicketInput) { in.DepartmentID = &hiddenDept.ID })

		rows := panelRows(t, h, panelAgent)
		if _, ok := rows[hidden.Reference]; ok || rows[visible.Reference] == nil {
			t.Fatalf("rows %v", rows)
		}
		inbox := obj(t, visit(t, h, panelIndex, panelAgent, "agent").Props, "inbox")
		if obj(t, inbox, "unread")["all"] != float64(1) {
			t.Fatalf("unread %v", inbox["unread"])
		}
		if rec := sendAPI(h, http.MethodPost, panelIndex+"/"+hidden.Reference+"/read", panelAgent, "agent", ""); rec.Code != http.StatusNotFound {
			t.Fatalf("hidden ticket read: %d", rec.Code)
		}
		if n := f.count(`SELECT COUNT(*) FROM escalated_ticket_reads WHERE ticket_id = ?`, hidden.ID); n != 0 {
			t.Fatalf("%d read rows for the hidden ticket", n)
		}
	}, func(cfg *escalated.Config) { cfg.Visibility = "department" })
}

// ---- Customer presence ------------------------------------------------------------------

func TestAgentPanelCustomerPresence(t *testing.T) {
	eachPanelMount(t, func(t *testing.T, f *panelFixture, h http.Handler) {
		tk := f.ticket()
		row := panelRows(t, h, panelAgent)[tk.Reference]
		if row["requester_online"] != false || row["requester_last_seen_at"] != nil {
			t.Fatalf("presence before any visit: %v", row)
		}

		customerPage := send(h, http.MethodGet, "/escalated/tickets/"+fmt.Sprint(tk.ID), panelCustomer, "", "")
		for _, k := range []string{"requester_online", "requester_last_seen_at", "is_unread"} {
			if strings.Contains(customerPage.Body.String(), k) {
				t.Errorf("a customer page carries %q", k)
			}
		}
		visit(t, h, "/escalated/tickets", panelCustomer, "")
		row = panelRows(t, h, panelAgent)[tk.Reference]
		if row["requester_online"] != true || row["requester_last_seen_at"] == nil {
			t.Fatalf("presence after a visit: %v", row)
		}
		show := visit(t, h, panelIndex+"/"+tk.Reference, panelAgent, "agent")
		if obj(t, show.Props, "ticket")["requester_online"] != true {
			t.Fatal("the ticket page does not show the customer online")
		}

		// Throttled: a visit within 30 seconds writes nothing.
		recent := time.Now().UTC().Add(-10 * time.Second).Truncate(time.Second)
		f.exec(`UPDATE escalated_customer_presence SET last_seen_at = ?`, recent)
		visit(t, h, "/escalated/tickets", panelCustomer, "")
		if n := f.count(`SELECT COUNT(*) FROM escalated_customer_presence WHERE last_seen_at = ?`, recent); n != 1 {
			t.Fatal("a visit within the throttle wrote presence")
		}
		f.exec(`UPDATE escalated_customer_presence SET last_seen_at = ?`, time.Now().UTC().Add(-40*time.Second).Truncate(time.Second))
		visit(t, h, "/escalated/tickets", panelCustomer, "")
		if n := f.count(`SELECT COUNT(*) FROM escalated_customer_presence WHERE last_seen_at > ?`, time.Now().UTC().Add(-5*time.Second)); n != 1 {
			t.Fatal("a visit after the throttle wrote nothing")
		}
		if n := f.count(`SELECT COUNT(*) FROM escalated_customer_presence`); n != 1 {
			t.Fatalf("%d presence rows", n)
		}

		// Seen outside the window: offline, with the time.
		f.exec(`UPDATE escalated_customer_presence SET last_seen_at = ?`, time.Now().UTC().Add(-10*time.Minute).Truncate(time.Second))
		row = panelRows(t, h, panelAgent)[tk.Reference]
		if row["requester_online"] != false || row["requester_last_seen_at"] == nil {
			t.Fatalf("presence outside the window: %v", row)
		}

		// Staff browsing the portal are not recorded.
		f.exec(`DELETE FROM escalated_customer_presence`)
		send(h, http.MethodGet, "/escalated/tickets", panelAgent, "agent", "")
		if n := f.count(`SELECT COUNT(*) FROM escalated_customer_presence`); n != 0 {
			t.Fatal("an agent on the portal was recorded")
		}
	})
}

// ---- Notifications ------------------------------------------------------------------------

func notificationsOf(t *testing.T, h http.Handler, agent, query string) map[string]any {
	t.Helper()
	return api(t, h, http.MethodGet, "/escalated/agent/notifications"+query, agent, "", http.StatusOK)
}

func TestAgentPanelNotifications(t *testing.T) {
	eachPanelMount(t, func(t *testing.T, f *panelFixture, h http.Handler) {
		ctx := context.Background()
		tk := f.ticket()
		agent, other := models.UserID(panelAgent), models.UserID(panelOther)

		// Assigning yourself tells you nothing.
		if err := f.tickets.Assign(ctx, tk.ID, agent, &agent); err != nil {
			t.Fatal(err)
		}
		if n := f.count(`SELECT COUNT(*) FROM escalated_notifications WHERE user_id = ?`, panelAgent); n != 0 {
			t.Fatalf("self-assignment stored %d notifications", n)
		}
		if err := f.tickets.Assign(ctx, tk.ID, agent, &other); err != nil {
			t.Fatal(err)
		}
		body := notificationsOf(t, h, panelAgent, "")
		wantJSON(t, "meta", body["meta"], `{"current_page":1,"last_page":1,"per_page":20,"total":1}`)
		item := list(t, body, "data")[0].(map[string]any)
		if keys := sorted(keysOf(item)); strings.Join(keys, ",") != "body,created_at,id,read_at,ticket,title,type,url" {
			t.Fatalf("item keys %v", keys)
		}
		if item["type"] != "ticket_assigned" || item["title"] != tk.Reference+" was assigned to you" || item["body"] != tk.Subject {
			t.Fatalf("item %v", item)
		}
		wantJSON(t, "ticket", item["ticket"], fmt.Sprintf(`{"id":%d,"reference":%q,"subject":%q}`, tk.ID, tk.Reference, tk.Subject))
		if item["url"] != "/escalated/agent/tickets/"+tk.Reference || item["read_at"] != nil {
			t.Fatalf("item %v", item)
		}
		wantJSON(t, "shared", obj(t, visit(t, h, panelIndex, panelAgent, "agent").Props, "escalated")["notifications"], `{"unread_count":1}`)

		// Only customer-side replies notify the assignee and followers.
		f.exec(`INSERT INTO escalated_ticket_followers (ticket_id, user_id) VALUES (?, ?)`, tk.ID, panelOther)
		f.reply(tk, panelOther, false)
		f.reply(tk, panelOther, true)
		if n := f.count(`SELECT COUNT(*) FROM escalated_notifications WHERE type = 'ticket_reply'`); n != 0 {
			t.Fatalf("agents' replies notified %d times", n)
		}
		f.reply(tk, panelCustomer, false)
		for _, id := range []string{panelAgent, panelOther} {
			if n := f.count(`SELECT COUNT(*) FROM escalated_notifications WHERE type = 'ticket_reply' AND user_id = ?`, id); n != 1 {
				t.Fatalf("%s got %d reply notifications", id, n)
			}
		}

		// SLA warnings and breaches, and chat transfers.
		panel := f.esc.AgentPanel()
		assigned, _ := f.esc.Store.GetTicket(ctx, tk.ID)
		panel.TicketEvent(ctx, "sla.warning", assigned, map[string]any{"type": "resolution"}, nil)
		panel.TicketEvent(ctx, "sla.breached", assigned, map[string]any{"type": "resolution"}, nil)
		panel.TicketEvent(ctx, "chat.transferred", assigned, map[string]any{"chat": map[string]any{"session_id": 1, "from_agent_id": other, "to_agent_id": agent}}, nil)
		types := map[string]map[string]any{}
		for _, it := range list(t, notificationsOf(t, h, panelAgent, ""), "data") {
			types[it.(map[string]any)["type"].(string)] = it.(map[string]any)
		}
		for _, kind := range []string{"ticket_assigned", "ticket_reply", "sla_warning", "sla_breached", "chat_transferred"} {
			if types[kind] == nil {
				t.Errorf("no %s notification", kind)
			}
		}
		if types["sla_breached"]["title"] != "SLA breached on "+tk.Reference {
			t.Errorf("sla title %v", types["sla_breached"]["title"])
		}

		// read, read-all, the unread filter and paging.
		var first string
		db := f.esc.Config.DB
		if err := db.QueryRow(sqldialect.Rebind(db, `SELECT id FROM escalated_notifications WHERE user_id = ? ORDER BY created_at LIMIT 1`), panelAgent).Scan(&first); err != nil {
			t.Fatal(err)
		}
		wantJSON(t, "read", api(t, h, http.MethodPost, "/escalated/agent/notifications/"+first+"/read", panelAgent, "", http.StatusOK), `{"unread_count":4}`)
		api(t, h, http.MethodPost, "/escalated/agent/notifications/"+first+"/read", panelOther, "", http.StatusNotFound)
		if total := obj(t, notificationsOf(t, h, panelAgent, "?unread=1"), "meta")["total"]; total != float64(4) {
			t.Fatalf("unread total %v", total)
		}
		paged := notificationsOf(t, h, panelAgent, "?per_page=2&page=3")
		wantJSON(t, "paged meta", paged["meta"], `{"current_page":3,"last_page":3,"per_page":2,"total":5}`)
		if n := len(list(t, paged, "data")); n != 1 {
			t.Fatalf("page 3 has %d items", n)
		}
		if pp := obj(t, notificationsOf(t, h, panelAgent, "?per_page=500"), "meta")["per_page"]; pp != float64(50) {
			t.Fatalf("per_page %v", pp)
		}
		wantJSON(t, "read-all", api(t, h, http.MethodPost, "/escalated/agent/notifications/read-all", panelAgent, "", http.StatusOK), `{"unread_count":0}`)
		if n := panel.UnreadCount(ctx, agent); n != 0 {
			t.Fatalf("unread after read-all: %d", n)
		}
		if n := panel.UnreadCount(ctx, other); n != 1 {
			t.Fatalf("the other agent's unread count changed: %d", n)
		}
	})
}

func TestAgentPanelNotificationsRedactTicketsOutOfSight(t *testing.T) {
	eachPanelMount(t, func(t *testing.T, f *panelFixture, h http.Handler) {
		ctx := context.Background()
		hidden := &models.Department{Name: "Hidden", Slug: "hidden", IsActive: true}
		if err := f.esc.Store.CreateDepartment(ctx, hidden); err != nil {
			t.Fatal(err)
		}
		tk := f.ticket(func(in *services.CreateTicketInput) { in.DepartmentID = &hidden.ID })
		f.esc.AgentPanel().Notify(ctx, panelAgent, "ticket_assigned", tk, nil, nil)

		item := list(t, notificationsOf(t, h, panelAgent, ""), "data")[0].(map[string]any)
		if item["title"] != "A ticket you can no longer view" || item["body"] != nil || item["ticket"] != nil || item["url"] != nil {
			t.Fatalf("item %v", item)
		}
		encoded, _ := json.Marshal(item)
		if strings.Contains(string(encoded), tk.Subject) || strings.Contains(string(encoded), tk.Reference) {
			t.Fatalf("redacted item leaks the ticket: %s", encoded)
		}
	}, func(cfg *escalated.Config) { cfg.Visibility = "department" })
}

func TestAgentPanelNotificationsAreBroadcast(t *testing.T) {
	broadcastCfg := services.DefaultBroadcastConfig()
	broadcastCfg.Enabled = true
	broadcaster := services.NewBroadcaster(broadcastCfg, func(string, string) bool { return true })
	f := newPanelFixture(t, func(cfg *escalated.Config) { cfg.AgentPanel.Broadcaster = broadcaster })
	sub, err := broadcaster.Subscribe("user-"+panelAgent, []string{services.UserChannel(panelAgent)})
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	tk := f.ticket()
	other := models.UserID(panelOther)
	if err := f.tickets.Assign(context.Background(), tk.ID, panelAgent, &other); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-sub.Events:
		if event.EventType != "notification.created" || !strings.Contains(string(event.Data), tk.Reference) {
			t.Fatalf("event %+v", event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no notification.created broadcast")
	}
}

func TestAgentPanelNotificationsSwitchedOff(t *testing.T) {
	eachPanelMount(t, func(t *testing.T, f *panelFixture, h http.Handler) {
		tk := f.ticket()
		other := models.UserID(panelOther)
		if err := f.tickets.Assign(context.Background(), tk.ID, panelAgent, &other); err != nil {
			t.Fatal(err)
		}
		if n := f.count(`SELECT COUNT(*) FROM escalated_notifications`); n != 0 {
			t.Fatalf("%d notifications stored while off", n)
		}
		for _, r := range []struct{ method, path string }{
			{http.MethodGet, "/escalated/agent/notifications"},
			{http.MethodPost, "/escalated/agent/notifications/read-all"},
		} {
			if rec := sendAPI(h, r.method, r.path, panelAgent, "agent", ""); rec.Code != http.StatusNotFound {
				t.Errorf("%s %s: %d", r.method, r.path, rec.Code)
			}
		}
		if _, ok := obj(t, visit(t, h, panelIndex, panelAgent, "agent").Props, "escalated")["notifications"]; ok {
			t.Error("the unread count is shared while the feed is off")
		}
	}, func(cfg *escalated.Config) { cfg.AgentPanel.DisableNotifications = true })
}
