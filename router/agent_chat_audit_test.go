package router_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	escalated "github.com/escalated-dev/escalated-go"
	"github.com/escalated-dev/escalated-go/internal/testdb"
	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/router"
	"github.com/escalated-dev/escalated-go/services"
)

type consoleFixture struct {
	esc *escalated.Escalated
	h   http.Handler
}

func newConsoleFixture(t *testing.T, mount string, tweak func(*escalated.Config)) consoleFixture {
	t.Helper()
	cfg := escalated.DefaultConfig()
	cfg.DB = testdb.Open(t)
	cfg.UIEnabled = true
	cfg.EnableWidget = true
	cfg.Widget.Enabled = true
	cfg.GuestRateLimit.Disabled = true
	cfg.UserIDFunc = func(r *http.Request) models.UserID { return models.UserID(r.Header.Get("X-User")) }
	cfg.AgentCheck = func(r *http.Request) bool { return r.Header.Get("X-User") == "5" }
	cfg.AdminCheck = func(r *http.Request) bool { return r.Header.Get("X-User") == "9" }
	if tweak != nil {
		tweak(&cfg)
	}
	esc, err := escalated.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if mount == "chi" {
		r := chi.NewRouter()
		router.MountChi(r, esc)
		return consoleFixture{esc, r}
	}
	mux := http.NewServeMux()
	router.MountStdlib(mux, esc)
	return consoleFixture{esc, mux}
}

func (f consoleFixture) call(t *testing.T, user, method, path string, body any, headers ...string) (int, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, "/escalated"+path, &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if user != "" {
		req.Header.Set("X-User", user)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, req)
	out := map[string]any{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

// The agent live-chat console (handlers/chat.go) was built but mounted by
// neither router, so visitors' chats could not be taken.
func TestAgentChatConsoleIsMounted(t *testing.T) {
	for _, mount := range []string{"chi", "stdlib"} {
		t.Run(mount, func(t *testing.T) {
			f := newConsoleFixture(t, mount, func(c *escalated.Config) {
				c.ChatAgentName = func(_ context.Context, id models.UserID) string { return "Agent " + string(id) }
			})
			ctx := context.Background()
			sessions := services.NewChatSessionService(f.esc.Store, services.NewChatRoutingService(f.esc.Store), nil)
			ticket, _, err := sessions.StartSession(ctx, services.StartSessionInput{GuestName: "Vi", GuestEmail: "vi@example.com", Message: "Hello"})
			if err != nil {
				t.Fatal(err)
			}

			if code, _ := f.call(t, "7", http.MethodGet, "/agent/chat/sessions", nil); code != http.StatusForbidden && code != http.StatusUnauthorized {
				t.Errorf("non-agent listing chats: %d", code)
			}
			code, out := f.call(t, "5", http.MethodGet, "/agent/chat/sessions", nil)
			if list, _ := out["sessions"].([]any); code != http.StatusOK || len(list) != 1 {
				t.Fatalf("list: %d %v", code, out)
			}
			base := fmt.Sprintf("/agent/chat/sessions/%d", ticket.ID)
			if code, _ := f.call(t, "5", http.MethodPost, base+"/accept", nil); code != http.StatusOK {
				t.Fatalf("accept: %d", code)
			}
			if code, _ := f.call(t, "5", http.MethodPost, base+"/accept", nil); code != http.StatusConflict {
				t.Errorf("accept twice: %d", code)
			}
			if code, _ := f.call(t, "5", http.MethodPost, base+"/message", map[string]any{"body": "Hi Vi"}); code != http.StatusCreated {
				t.Fatalf("message: %d", code)
			}

			// The visitor sees the agent's message under ChatAgentName.
			code, out = f.call(t, "", http.MethodGet, "/widget/chat/"+*ticket.GuestToken+"/messages", nil)
			msgs, _ := out["messages"].([]any)
			found := false
			for _, m := range msgs {
				mm := m.(map[string]any)
				if mm["body"] == "Hi Vi" && mm["author"].(map[string]any)["name"] == "Agent 5" {
					found = true
				}
			}
			if code != http.StatusOK || !found {
				t.Errorf("widget transcript: %d %v", code, msgs)
			}

			if code, _ := f.call(t, "5", http.MethodPost, base+"/end", nil); code != http.StatusOK {
				t.Fatalf("end: %d", code)
			}
		})
	}
}

// Config.ChatAgentOnline reaches the widget's availability check, which
// otherwise counted every listed agent as online around the clock.
func TestChatAgentOnlineHookDrivesAvailability(t *testing.T) {
	for _, online := range []bool{true, false} {
		f := newConsoleFixture(t, "chi", func(c *escalated.Config) {
			c.ChatAgentOnline = func(context.Context, models.UserID) (bool, error) { return online, nil }
		})
		if err := f.esc.Store.CreateChatRoutingRule(context.Background(), &models.ChatRoutingRule{
			Name: "All", Strategy: "round_robin", AgentIDs: []models.UserID{"5"}, MaxConcurrentChats: 3, IsActive: true,
		}); err != nil {
			t.Fatal(err)
		}
		code, out := f.call(t, "", http.MethodGet, "/widget/chat/availability", nil)
		if code != http.StatusOK || out["available"] != online {
			t.Errorf("agent online=%v: availability %d %v", online, code, out)
		}
	}
}

// Admin changes to departments, tags and SLA policies are recorded and listed
// on /admin/audit-log with Laravel's filters.
func TestAdminAuditLog(t *testing.T) {
	for _, mount := range []string{"chi", "stdlib"} {
		t.Run(mount, func(t *testing.T) {
			f := newConsoleFixture(t, mount, nil)
			inertia := []string{"X-Inertia", "true", "User-Agent", "audit-test"}
			// The writes go as an API client: a page visit gets a 303, not JSON.
			api := []string{"User-Agent", "audit-test"}

			code, out := f.call(t, "9", http.MethodPost, "/admin/departments", map[string]any{"name": "Billing"}, api...)
			if code != http.StatusCreated {
				t.Fatalf("create department: %d %v", code, out)
			}
			id := int64(out["department"].(map[string]any)["id"].(float64))
			if code, _ := f.call(t, "9", http.MethodPatch, fmt.Sprintf("/admin/departments/%d", id), map[string]any{"name": "Billing & Payments"}, api...); code != http.StatusOK {
				t.Fatalf("update department: %d", code)
			}
			if code, _ := f.call(t, "9", http.MethodPost, "/admin/tags", map[string]any{"name": "vip", "secret_token": "x"}, api...); code != http.StatusCreated {
				t.Fatalf("create tag: %d", code)
			}
			if code, _ := f.call(t, "9", http.MethodDelete, fmt.Sprintf("/admin/departments/%d", id), nil, api...); code != http.StatusOK {
				t.Fatalf("delete department: %d", code)
			}

			if code, _ := f.call(t, "5", http.MethodGet, "/admin/audit-log", nil, inertia...); code == http.StatusOK {
				t.Errorf("non-admin read the audit log")
			}
			code, page := f.call(t, "9", http.MethodGet, "/admin/audit-log", nil, inertia...)
			if code != http.StatusOK || page["component"] != "Escalated/Admin/AuditLog/Index" {
				t.Fatalf("audit log: %d %v", code, page)
			}
			props := page["props"].(map[string]any)
			logs := props["logs"].(map[string]any)
			data := logs["data"].([]any)
			if logs["total"] != float64(4) || len(data) != 4 {
				t.Fatalf("logs = %v", logs)
			}
			newest := data[0].(map[string]any)
			if newest["action"] != "deleted" || newest["auditable_type"] != "department" || newest["auditable_id"] != float64(id) || fmt.Sprint(newest["user_id"]) != "9" || newest["user_agent"] != "audit-test" {
				t.Errorf("newest entry = %v", newest)
			}
			update := data[2].(map[string]any)
			if update["action"] != "updated" || update["old_values"].(map[string]any)["name"] != "Billing" || update["new_values"].(map[string]any)["name"] != "Billing & Payments" {
				t.Errorf("update entry = %v", update)
			}
			if types := props["resourceTypes"].([]any); len(types) != 2 {
				t.Errorf("resourceTypes = %v", types)
			}

			_, page = f.call(t, "9", http.MethodGet, "/admin/audit-log?action=created&auditable_type=tag", nil, inertia...)
			logs = page["props"].(map[string]any)["logs"].(map[string]any)
			if logs["total"] != float64(1) {
				t.Errorf("filtered total = %v", logs["total"])
			}
		})
	}
}
