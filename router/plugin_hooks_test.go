package router_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	escalated "github.com/escalated-dev/escalated-go"
	"github.com/escalated-dev/escalated-go/internal/pluginruntimetest"
	"github.com/escalated-dev/escalated-go/internal/sqldialect"
	"github.com/escalated-dev/escalated-go/models"
)

// The hooks-and-context half of the plugin contract's port test checklist:
// canonical events reaching active plugins, the plugin context's domain
// methods, and escalated.plugins on every page.

// eventually polls until check passes: actions are delivered in the
// background.
func eventually(t *testing.T, what string, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !check() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// seen collects the stand-in's recorded actions by hook.
type seen struct {
	rec    *recorder
	events []string
}

func (s *seen) poll() {
	s.events = append(s.events, s.rec.take()...)
}

func (s *seen) payloads(hook string) []map[string]any {
	var out []map[string]any
	for _, e := range s.events {
		rest, ok := strings.CutPrefix(e, "action:"+hook+":")
		if !ok {
			continue
		}
		var m map[string]any
		if json.Unmarshal([]byte(rest), &m) == nil {
			out = append(out, m)
		}
	}
	return out
}

func (s *seen) hooks() []string {
	var out []string
	for _, e := range s.events {
		rest := strings.TrimPrefix(e, "action:")
		hook, _, _ := strings.Cut(rest, ":{")
		out = append(out, hook)
	}
	return out
}

func createTicketOverHTTP(t *testing.T, h http.Handler) int64 {
	t.Helper()
	rec := preq(h, http.MethodPost, "/escalated/api/tickets", "agent", `{"subject":"Printer on fire","description":"Smoke","priority":2}`, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create ticket: %d %s", rec.Code, rec.Body.String())
	}
	id := obj(t, bodyOf(t, rec), "ticket")["id"].(float64)
	return int64(id)
}

func TestPluginsReceiveTheCanonicalEvents(t *testing.T) {
	f := newPluginFixture(t)
	h := f.mounts()["chi"]
	got := &seen{rec: f.rec}

	// Inactive: a ticket created over HTTP reaches no plugin.
	createTicketOverHTTP(t, h)
	time.Sleep(200 * time.Millisecond)
	got.poll()
	if len(got.events) != 0 {
		t.Fatalf("an inactive plugin received %v", got.events)
	}

	lifecycleCall(t, h, http.MethodPost, "/escalated/admin/plugins/"+crmSlug+"/activate")
	f.rec.take()

	id := createTicketOverHTTP(t, h)
	eventually(t, "ticket.created", func() bool { got.poll(); return len(got.payloads("ticket.created")) == 1 })
	created := got.payloads("ticket.created")[0]
	ticket := obj(t, created, "ticket")
	for _, payload := range []map[string]any{created, ticket} {
		hasKeys(t, "ticket payload", payload, "id", "reference", "subject", "title", "status", "priority", "department_id", "assigned_to", "requester_id", "metadata", "created_at", "updated_at")
		if payload["id"] != float64(id) || payload["subject"] != "Printer on fire" || payload["title"] != "Printer on fire" || payload["status"] != "open" || payload["priority"] != "high" {
			t.Errorf("ticket.created payload %v", payload)
		}
	}

	// A reply delivers reply.created with the reply flat, reply and ticket.
	rec := preq(h, http.MethodPost, fmt.Sprintf("/escalated/api/tickets/%d/replies", id), "agent", `{"body":"On our way"}`, nil)
	if rec.Code >= 300 {
		t.Fatalf("reply: %d %s", rec.Code, rec.Body.String())
	}
	eventually(t, "reply.created", func() bool { got.poll(); return len(got.payloads("reply.created")) == 1 })
	reply := got.payloads("reply.created")[0]
	if reply["body"] != "On our way" || obj(t, reply, "reply")["body"] != "On our way" || obj(t, reply, "ticket")["id"] != float64(id) || reply["ticket_id"] != float64(id) {
		t.Errorf("reply.created payload %v", reply)
	}

	// A status change carries old and new status.
	rec = preq(h, http.MethodPatch, fmt.Sprintf("/escalated/api/tickets/%d", id), "agent", `{"status":"resolved"}`, nil)
	if rec.Code >= 300 {
		t.Fatalf("status: %d %s", rec.Code, rec.Body.String())
	}
	// Each event is delivered on its own, in no particular order.
	eventually(t, "ticket.resolved and ticket.status_changed", func() bool {
		got.poll()
		return len(got.payloads("ticket.resolved")) == 1 && len(got.payloads("ticket.status_changed")) == 1
	})
	changed := got.payloads("ticket.status_changed")
	if len(changed) != 1 || changed[0]["old_status"] != "open" || changed[0]["new_status"] != "resolved" || changed[0]["status"] != "resolved" {
		t.Errorf("ticket.status_changed %v", changed)
	}

	// Never a legacy name, whatever the plugin listens for.
	time.Sleep(100 * time.Millisecond)
	got.poll()
	for _, hook := range got.hooks() {
		if _, legacy := models.LegacyEventNames[hook]; legacy {
			t.Errorf("legacy event %s was delivered", hook)
		}
	}
}

func TestAFailingPluginActionDoesNotFailTheRequest(t *testing.T) {
	f := newPluginFixture(t)
	f.crm.panicOnCreated = true
	h := f.mounts()["stdlib"]
	lifecycleCall(t, h, http.MethodPost, "/escalated/admin/plugins/"+crmSlug+"/activate")
	createTicketOverHTTP(t, h)
	got := &seen{rec: f.rec}
	eventually(t, "the panicking action ran", func() bool { got.poll(); return len(got.payloads("ticket.created")) == 1 })
	// And the host is still serving.
	createTicketOverHTTP(t, h)
}

func TestPluginContextWritesThroughTheTicketService(t *testing.T) {
	f := newPluginFixture(t)
	h := f.mounts()["chi"]
	ctx := context.Background()
	lifecycleCall(t, h, http.MethodPost, "/escalated/admin/plugins/"+crmSlug+"/activate")
	f.rec.take()
	got := &seen{rec: f.rec}
	dept := &models.Department{Name: "Billing", Slug: "billing", IsActive: true}
	if err := f.esc.Store.CreateDepartment(ctx, dept); err != nil {
		t.Fatal(err)
	}

	// Through an endpoint: the writes act as the signed-in admin.
	rec := preq(h, http.MethodPost, "/escalated/api/plugins/"+crmSlug+"/ticket", "admin",
		fmt.Sprintf(`{"title":"Renewal","body":"Contract ends","priority":"urgent","status":"in_progress","department_id":%d,"ticket_type":"task","channel":"api","metadata":{"deal":7},"requester_email":"Buyer@Example.com","requester_name":"Buyer"}`, dept.ID),
		map[string]string{"X-Requested-With": "XMLHttpRequest"})
	if rec.Code != http.StatusOK {
		t.Fatalf("ctx.tickets.create: %d %s", rec.Code, rec.Body.String())
	}
	out := bodyOf(t, rec)
	ticket := obj(t, out, "ticket")
	if ticket["subject"] != "Renewal" || ticket["description"] != "Contract ends" || ticket["priority"] != "urgent" || ticket["status"] != "in_progress" ||
		ticket["department_id"] != float64(dept.ID) || ticket["ticket_type"] != "task" || obj(t, ticket, "metadata")["deal"] != float64(7) || ticket["contact_id"] == nil {
		t.Errorf("created %v", ticket)
	}
	note := obj(t, out, "note")
	if note["type"] != "note" || note["is_internal"] != true || note["user_id"] != float64(900) {
		t.Errorf("note %v (it must act as the signed-in user)", note)
	}
	eventually(t, "ticket.created and note.created", func() bool {
		got.poll()
		return len(got.payloads("ticket.created")) == 1 && len(got.payloads("note.created")) == 1
	})

	pc := pluginContext(t, f, crmSlug)
	id := ticket["id"]

	// query filters on the contract's keys only.
	found, err := pc.Tickets().Query(ctx, map[string]any{"status": "in_progress", "priority": "urgent"})
	if err != nil || len(found) != 1 || found[0]["id"] != id {
		t.Errorf("query: %v %v", found, err)
	}
	if _, err := pc.Tickets().Query(ctx, map[string]any{"subject; DROP TABLE escalated_tickets": "x"}); err == nil {
		t.Error("a query on an unknown key was accepted")
	}
	if _, err := pc.Tickets().Create(ctx, map[string]any{"subject": "x", "requester_id": 1}); err == nil {
		t.Error("ctx.tickets.create let the plugin name the requester")
	}

	// update goes through the service: priority_changed fires.
	updated, err := pc.Tickets().Update(ctx, id, map[string]any{"priority": "low", "subject": "Renewal (Q3)"})
	if err != nil || updated["priority"] != "low" || updated["title"] != "Renewal (Q3)" {
		t.Errorf("update: %v %v", updated, err)
	}
	if _, err := pc.Tickets().Update(ctx, id, map[string]any{"requester_email": "x@example.com"}); err == nil {
		t.Error("ctx.tickets.update accepted a requester")
	}
	eventually(t, "ticket.priority_changed", func() bool {
		got.poll()
		p := got.payloads("ticket.priority_changed")
		return len(p) == 1 && p[0]["old_priority"] == "urgent" && p[0]["new_priority"] == "low"
	})

	replies, err := pc.Replies().Query(ctx, map[string]any{"ticket_id": id, "type": "note"})
	if err != nil || len(replies) != 1 {
		t.Errorf("replies query: %v %v", replies, err)
	}
	if _, err := pc.Replies().Query(ctx, map[string]any{"body": "x"}); err == nil {
		t.Error("ctx.replies.query accepted an unknown key")
	}
	if _, err := pc.Replies().Create(ctx, map[string]any{"ticket_id": id, "body": "x", "author_id": 1}); err == nil {
		t.Error("ctx.replies.create accepted an author")
	}

	contact, err := pc.Contacts().FindByEmail(ctx, "buyer@example.com")
	if err != nil || contact == nil || contact["name"] != "Buyer" {
		t.Errorf("contact %v %v", contact, err)
	}
	again, err := pc.Contacts().Create(ctx, map[string]any{"email": "buyer@example.com", "name": "Other"})
	if err != nil || again["id"] != contact["id"] {
		t.Errorf("contacts.create did not find the existing contact: %v %v", again, err)
	}
	if byID, err := pc.Contacts().Find(ctx, contact["id"]); err != nil || byID["email"] != "buyer@example.com" {
		t.Errorf("contacts.find %v %v", byID, err)
	}
	tag, err := pc.Tags().Create(ctx, map[string]any{"name": "Hot Lead"})
	if err != nil || tag["slug"] != "hot-lead" {
		t.Errorf("tags.create %v %v", tag, err)
	}
	if tags, err := pc.Tags().All(ctx); err != nil || len(tags) != 1 {
		t.Errorf("tags.all %v %v", tags, err)
	}
	if deps, err := pc.Departments().All(ctx); err != nil || len(deps) != 1 || deps[0]["name"] != "Billing" {
		t.Errorf("departments.all %v %v", deps, err)
	}
	if agents, err := pc.Agents().All(ctx); err != nil || len(agents) != 0 {
		t.Errorf("agents without a directory: %v %v", agents, err)
	}
	if err := pc.Broadcast().ToTicket(ctx, id, "crm.synced", map[string]any{}); err != nil {
		t.Errorf("broadcast without a broadcaster: %v", err)
	}
}

func TestSDKPluginsGetEventsAndContextOverTheBridge(t *testing.T) {
	f := newPluginFixture(t, withRuntime, func(cfg *escalated.Config) {
		cfg.SkillAgentDirectory = uiAgents{}
	})
	h := f.mounts()["chi"]
	lifecycleCall(t, h, http.MethodPost, "/escalated/admin/plugins/"+slackSlug+"/activate")

	id := createTicketOverHTTP(t, h)
	eventually(t, "ticket.created at the runtime", func() bool { return len(f.received(t, slackName, "ticket.created")) == 1 })
	event := obj(t, f.received(t, slackName, "ticket.created")[0], "event")
	if event["id"] != float64(id) || event["subject"] != "Printer on fire" || obj(t, event, "ticket")["title"] != "Printer on fire" {
		t.Errorf("SDK ticket.created %v", event)
	}
	if got := f.received(t, jiraName, "ticket.created"); len(got) != 0 {
		t.Errorf("inactive jira received %v", got)
	}

	call := func(method, params string) map[string]any {
		t.Helper()
		rec := preq(h, http.MethodPost, "/escalated/api/plugins/"+slackSlug+"/ctx", "admin", `{"method":"`+method+`","params":`+params+`}`, map[string]string{"X-Requested-With": "XMLHttpRequest"})
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", method, rec.Code, rec.Body.String())
		}
		return bodyOf(t, rec)
	}
	found := call("ctx.tickets.find", fmt.Sprintf(`{"id":%d}`, id))
	if obj(t, found, "result")["subject"] != "Printer on fire" {
		t.Errorf("ctx.tickets.find over the bridge %v", found)
	}
	made := call("ctx.replies.create", fmt.Sprintf(`{"data":{"ticket_id":%d,"body":"From Slack"}}`, id))
	if obj(t, made, "result")["user_id"] != float64(900) {
		t.Errorf("ctx.replies.create over the bridge must act as the signed-in user: %v", made)
	}
	if bad := call("ctx.tickets.query", `{"filter":{"subject":"x"}}`); bad["error"] == nil {
		t.Errorf("an unknown query key over the bridge: %v", bad)
	}
	agents := call("ctx.agents.all", `{}`)
	if list, _ := agents["result"].([]any); len(list) != 1 {
		t.Errorf("ctx.agents.all %v", agents)
	}
	if user := call("ctx.currentUser", `{}`); obj(t, user, "result")["id"] != float64(900) {
		t.Errorf("ctx.currentUser %v", user)
	}
}

func TestEscalatedPluginsIsSharedWithEveryPage(t *testing.T) {
	f := newPluginFixture(t)
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			shared := func(role string, headers map[string]string) map[string]any {
				t.Helper()
				all := map[string]string{"X-Inertia": "true"}
				for k, v := range headers {
					all[k] = v
				}
				page := pageOf(t, preq(h, http.MethodGet, "/escalated/agent/tickets", role, "", all))
				return obj(t, obj(t, page.Props, "escalated"), "plugins")
			}
			if got := shared("admin", nil); len(got) != 0 {
				t.Errorf("no active plugin: %v, want {}", got)
			}

			lifecycleCall(t, h, http.MethodPost, "/escalated/admin/plugins/"+crmSlug+"/activate")
			defer lifecycleCall(t, h, http.MethodPost, "/escalated/admin/plugins/"+crmSlug+"/deactivate")

			got := shared("admin", nil)
			menu := list(t, got, "menuItems")
			if len(menu) != 1 {
				t.Fatalf("menuItems %v", menu)
			}
			item := menu[0].(map[string]any)
			if item["label"] != "CRM" || item["route"] != "/escalated/admin/plugins/"+crmSlug+"/settings" || item["position"] != float64(100) || item["section"] != "admin" || item["plugin"] != crmSlug {
				t.Errorf("menu item %v", item)
			}
			slots := obj(t, got, "pageComponents")
			sidebar, _ := slots["ticket.show.sidebar"].([]any)
			if len(sidebar) != 1 || sidebar[0].(map[string]any)["component"] != "CrmCard" || sidebar[0].(map[string]any)["plugin"] != crmSlug {
				t.Errorf("pageComponents %v", slots)
			}
			widgets := list(t, got, "dashboardWidgets")
			if len(widgets) != 1 {
				t.Fatalf("dashboardWidgets %v", widgets)
			}
			w := widgets[0].(map[string]any)
			if w["id"] != crmSlug+".CrmWidget" || w["title"] != "CRM" || w["width"] != "half" || w["position"] != float64(100) {
				t.Errorf("widget %v", w)
			}

			// An item whose capability the viewer holds is shared too.
			withCap := list(t, shared("admin", map[string]string{capHeader: "crm.secret"}), "dashboardWidgets")
			if len(withCap) != 2 || withCap[1].(map[string]any)["width"] != "full" || withCap[1].(map[string]any)["position"] != float64(5) {
				t.Errorf("widgets with the capability %v", withCap)
			}

			// An agent gets no admin menu items.
			if menu := list(t, shared("agent", nil), "menuItems"); len(menu) != 0 {
				t.Errorf("agent menu %v", menu)
			}
			if strings.Contains(jsonString(got), "api_key") {
				t.Error("plugin config reached the shared props")
			}
		})
	}
}

// Two plugin calls in flight at once, from different signed-in users: each
// ctx.* call the runtime makes carries host_call_id, so each is answered for
// its own request's user, for ctx.currentUser and as the actor of a write.
func TestSDKPluginCallsInFlightAtOnceKeepTheirOwnUser(t *testing.T) {
	f := newPluginFixture(t, withRuntime, func(cfg *escalated.Config) {
		cfg.SkillAgentDirectory = uiAgents{}
	})
	h := f.mounts()["chi"]
	lifecycleCall(t, h, http.MethodPost, "/escalated/admin/plugins/"+slackSlug+"/activate")
	id := createTicketOverHTTP(t, h)

	together := func(method, params string) map[string]map[string]any {
		t.Helper()
		out := map[string]map[string]any{}
		var mu sync.Mutex
		var wg sync.WaitGroup
		for _, user := range []string{"900", "901"} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				rec := preq(h, http.MethodPost, "/escalated/api/plugins/"+slackSlug+"/ctx-together", "admin",
					`{"method":"`+method+`","params":`+params+`}`,
					map[string]string{"X-Requested-With": "XMLHttpRequest", headerUser: user})
				var body map[string]any
				_ = json.Unmarshal(rec.Body.Bytes(), &body)
				mu.Lock()
				out[user] = body
				mu.Unlock()
			}()
		}
		wg.Wait()
		return out
	}

	who := together("ctx.currentUser", `{}`)
	for user, want := range map[string]float64{"900": 900, "901": 901} {
		result, _ := who[user]["result"].(map[string]any)
		if result == nil || result["id"] != want {
			t.Errorf("ctx.currentUser for %s while another call is in flight: %v", user, who[user])
		}
	}

	wrote := together("ctx.replies.create", fmt.Sprintf(`{"data":{"ticket_id":%d,"body":"From Slack"}}`, id))
	for user, want := range map[string]float64{"900": 900, "901": 901} {
		result, _ := wrote[user]["result"].(map[string]any)
		if result == nil || result["user_id"] != want {
			t.Errorf("ctx.replies.create for %s must act as that user: %v", user, wrote[user])
		}
	}
}

// An older runtime stamps no host_call_id: a lone call still acts for its
// request's user, as before.
func TestSDKPluginCallsFromAnOlderRuntimeStillActForTheRequest(t *testing.T) {
	f := newPluginFixture(t, withRuntime, func(cfg *escalated.Config) {
		cfg.Plugins.Runtime.Env = append(cfg.Plugins.Runtime.Env, pluginruntimetest.EnvNoHostCallIDs+"=1")
	})
	h := f.mounts()["chi"]
	lifecycleCall(t, h, http.MethodPost, "/escalated/admin/plugins/"+slackSlug+"/activate")
	rec := preq(h, http.MethodPost, "/escalated/api/plugins/"+slackSlug+"/ctx", "admin",
		`{"method":"ctx.currentUser","params":{}}`, map[string]string{"X-Requested-With": "XMLHttpRequest"})
	if user := obj(t, bodyOf(t, rec), "result"); user["id"] != float64(900) {
		t.Errorf("ctx.currentUser from an older runtime %v", user)
	}
}

// The cron schedules an active SDK plugin declares run when due, through the
// runtime's cron method, and an inactive plugin's never do.
func TestSDKPluginCronSchedulesRunWhenDue(t *testing.T) {
	f := newPluginFixture(t, withRuntime)
	h := f.mounts()["chi"]
	lifecycleCall(t, h, http.MethodPost, "/escalated/admin/plugins/"+slackSlug+"/activate")

	schedules := func(plugin string) []string {
		t.Helper()
		db := f.esc.Config.DB
		rows, err := db.Query(sqldialect.Rebind(db, `SELECT data FROM escalated_plugin_store WHERE plugin = ? AND collection = 'cron' ORDER BY id`), plugin)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = rows.Close() }()
		var out []string
		for rows.Next() {
			var raw string
			if err := rows.Scan(&raw); err != nil {
				t.Fatal(err)
			}
			var m map[string]any
			_ = json.Unmarshal([]byte(raw), &m)
			out = append(out, fmt.Sprint(m["schedule"]))
		}
		sort.Strings(out)
		return out
	}

	nine := time.Date(2026, 10, 29, 9, 0, 20, 0, time.UTC)
	f.esc.Plugins.RunDueCron(context.Background(), nine)
	if got := schedules(slackName); strings.Join(got, "|") != "* * * * *|0 9 * * *" {
		t.Errorf("at 09:00 the slack plugin ran %v", got)
	}
	f.esc.Plugins.RunDueCron(context.Background(), nine.Add(time.Minute))
	if got := schedules(slackName); len(got) != 3 {
		t.Errorf("at 09:01 only the every-minute schedule is due: %v", got)
	}
	if got := schedules(jiraName); len(got) != 0 {
		t.Errorf("the inactive jira plugin ran %v", got)
	}

	// The scheduler entry point runs the same check for the current minute.
	f.esc.RunPluginCron(context.Background())
	if got := schedules(slackName); len(got) < 4 {
		t.Errorf("RunPluginCron did not run the every-minute schedule: %v", got)
	}
}

// An SDK plugin's widget badges are asked as the viewer while the shared
// props are built; a failing badge is null and a widget without one has none.
func TestSDKPluginWidgetBadgesAreAskedAsTheViewer(t *testing.T) {
	f := newPluginFixture(t, withRuntime)
	h := f.mounts()["chi"]
	lifecycleCall(t, h, http.MethodPost, "/escalated/admin/plugins/"+slackSlug+"/activate")

	page := pageOf(t, preq(h, http.MethodGet, "/escalated/agent/tickets", "admin", "", map[string]string{"X-Inertia": "true"}))
	widgets := list(t, obj(t, obj(t, page.Props, "escalated"), "plugins"), "dashboardWidgets")
	byComponent := map[string]map[string]any{}
	for _, w := range widgets {
		item := w.(map[string]any)
		byComponent[fmt.Sprint(item["component"])] = item
	}
	if got := byComponent["Inbox"]["badge"]; got != "for 900" {
		t.Errorf("Inbox badge %v, want the viewer's", got)
	}
	if got := byComponent["Count"]["badge"]; got != float64(7) {
		t.Errorf("Count badge %v", got)
	}
	if got, ok := byComponent["Broken"]["badge"]; !ok || got != nil {
		t.Errorf("a failing badge must be null: %v", byComponent["Broken"])
	}
	if _, ok := byComponent["Plain"]["badge"]; ok {
		t.Errorf("a widget without a badge function carries none: %v", byComponent["Plain"])
	}
}
