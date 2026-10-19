package router_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	escalated "github.com/escalated-dev/escalated-go"
	"github.com/escalated-dev/escalated-go/internal/testdb"
	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/router"
	"github.com/escalated-dev/escalated-go/services"
)

// The shared widget (@escalated-dev/escalated, src/widget/EscalatedWidget.vue)
// calls these paths relative to {RoutePrefix}/widget. Each test sends exactly
// the request the widget sends and asserts the fields it reads, through both
// routers, so a handler that exists but is never mounted fails here.

type widgetContractFixture struct {
	esc        *escalated.Escalated
	department int64
	inactive   int64
}

func newWidgetContractFixture(t *testing.T, enable bool) *widgetContractFixture {
	t.Helper()
	db := testdb.Open(t)

	cfg := escalated.DefaultConfig()
	cfg.DB = db
	cfg.UIEnabled = false
	cfg.EnableWidget = enable
	cfg.Widget.Enabled = true
	cfg.Widget.PrimaryColor = "#123456"
	cfg.Widget.WelcomeMessage = "Hi there"
	cfg.Widget.AllowedOrigins = "https://shop.example.com"

	esc, err := escalated.New(cfg)
	if err != nil {
		t.Fatalf("new escalated: %v", err)
	}

	ctx := context.Background()
	active := &models.Department{Name: "Billing", Slug: "billing", IsActive: true}
	if err := esc.Store.CreateDepartment(ctx, active); err != nil {
		t.Fatalf("create department: %v", err)
	}
	inactive := &models.Department{Name: "Old", Slug: "old", IsActive: false}
	if err := esc.Store.CreateDepartment(ctx, inactive); err != nil {
		t.Fatalf("create department: %v", err)
	}

	articles := services.NewArticleService(db, nil)
	cat := &models.ArticleCategory{Name: "Accounts", Slug: "accounts"}
	if err := articles.CreateCategory(cat); err != nil {
		t.Fatalf("create category: %v", err)
	}
	for _, a := range []*models.Article{
		{Title: "Reset your password", Slug: "reset-password", Body: "<p>Open settings and choose <b>Reset</b>.</p>", Status: models.ArticleStatusPublished, CategoryID: &cat.ID},
		{Title: "Password draft", Slug: "password-draft", Body: "Not published yet", Status: models.ArticleStatusDraft},
	} {
		if err := articles.CreateArticle(a); err != nil {
			t.Fatalf("create article: %v", err)
		}
	}

	return &widgetContractFixture{esc: esc, department: active.ID, inactive: inactive.ID}
}

func (f *widgetContractFixture) mounts() map[string]http.Handler {
	c := chi.NewRouter()
	router.MountChi(c, f.esc)
	m := http.NewServeMux()
	router.MountStdlib(m, f.esc)
	return map[string]http.Handler{"chi": c, "stdlib": m}
}

func widgetCall(t *testing.T, h http.Handler, method, path string, body any, headers ...string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, "/escalated/widget"+path, reader)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "10.9.8.7:1234"
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	out := map[string]any{}
	if strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
	}
	return rec, out
}

func TestWidgetRoutesNotMountedUnlessEnabled(t *testing.T) {
	f := newWidgetContractFixture(t, false)
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			rec, _ := widgetCall(t, h, http.MethodGet, "/config", nil)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("GET /widget/config with EnableWidget=false: got %d, want 404", rec.Code)
			}
		})
	}
}

func TestWidgetContractConfig(t *testing.T) {
	f := newWidgetContractFixture(t, true)
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			rec, cfg := widgetCall(t, h, http.MethodGet, "/config", nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("GET /config: %d %s", rec.Code, rec.Body.String())
			}
			want := map[string]any{
				"enabled":               true,
				"color":                 "#123456",
				"position":              "bottom-right",
				"greeting":              "Hi there",
				"kb_enabled":            true,
				"guest_tickets_enabled": true,
				"accepts_metadata":      true,
				"status_lookup_enabled": false,
				// Existing keys stay.
				"primary_color":   "#123456",
				"welcome_message": "Hi there",
			}
			for k, v := range want {
				if cfg[k] != v {
					t.Errorf("config[%q] = %#v, want %#v", k, cfg[k], v)
				}
			}
			if _, ok := cfg["guest_verification_required"]; ok {
				t.Errorf("guest_verification_required must be omitted: no /verification endpoint")
			}
			depts, _ := cfg["departments"].([]any)
			if len(depts) != 1 {
				t.Fatalf("departments = %#v, want only the active one", cfg["departments"])
			}
			d := depts[0].(map[string]any)
			if d["name"] != "Billing" || d["id"] != float64(f.department) {
				t.Fatalf("department = %#v", d)
			}
		})
	}
}

func TestWidgetContractEmbedScriptAndCORS(t *testing.T) {
	f := newWidgetContractFixture(t, true)
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			rec, _ := widgetCall(t, h, http.MethodGet, "/embed.js", nil)
			if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/javascript") {
				t.Fatalf("embed.js: %d %v", rec.Code, rec.Header())
			}

			rec, _ = widgetCall(t, h, http.MethodOptions, "/chat/start", nil,
				"Origin", "https://shop.example.com", "Access-Control-Request-Method", "POST")
			if rec.Code != http.StatusNoContent || rec.Header().Get("Access-Control-Allow-Origin") != "https://shop.example.com" {
				t.Fatalf("preflight: %d %v", rec.Code, rec.Header())
			}

			rec, _ = widgetCall(t, h, http.MethodGet, "/config", nil, "Origin", "https://shop.example.com")
			if rec.Header().Get("Access-Control-Allow-Origin") != "https://shop.example.com" {
				t.Fatalf("config CORS: %v", rec.Header())
			}
		})
	}
}

func TestWidgetContractArticles(t *testing.T) {
	f := newWidgetContractFixture(t, true)
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			rec, out := widgetCall(t, h, http.MethodGet, "/articles?q=password", nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("search: %d %s", rec.Code, rec.Body.String())
			}
			list, _ := out["articles"].([]any)
			if len(list) != 1 {
				t.Fatalf("articles = %#v, want the one published match", out["articles"])
			}
			a := list[0].(map[string]any)
			if a["title"] != "Reset your password" || a["slug"] != "reset-password" {
				t.Fatalf("article = %#v", a)
			}
			if ex, _ := a["excerpt"].(string); ex == "" || strings.Contains(ex, "<") {
				t.Fatalf("excerpt = %#v, want plain text", a["excerpt"])
			}

			rec, out = widgetCall(t, h, http.MethodGet, "/articles?q=p", nil)
			if rec.Code != http.StatusOK || len(out["articles"].([]any)) != 0 {
				t.Fatalf("one-letter search: %d %v", rec.Code, out)
			}

			rec, out = widgetCall(t, h, http.MethodGet, "/articles/reset-password", nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("show: %d %s", rec.Code, rec.Body.String())
			}
			if out["title"] != "Reset your password" || out["slug"] != "reset-password" || out["category"] != "Accounts" {
				t.Fatalf("show = %#v", out)
			}
			if b, _ := out["body"].(string); !strings.Contains(b, "Reset") {
				t.Fatalf("body = %#v", out["body"])
			}

			if rec, _ := widgetCall(t, h, http.MethodGet, "/articles/password-draft", nil); rec.Code != http.StatusNotFound {
				t.Fatalf("draft article: %d, want 404", rec.Code)
			}
			if rec, _ := widgetCall(t, h, http.MethodGet, "/articles/nope", nil); rec.Code != http.StatusNotFound {
				t.Fatalf("missing article: %d, want 404", rec.Code)
			}
		})
	}
}

func TestWidgetContractCreateTicket(t *testing.T) {
	f := newWidgetContractFixture(t, true)
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			rec, out := widgetCall(t, h, http.MethodPost, "/tickets", map[string]any{
				"name":          "Ada",
				"email":         "ada@example.com",
				"subject":       "Billing question",
				"description":   "Why was I charged twice?",
				"department_id": f.department,
				"metadata":      map[string]any{"plan": "pro"},
			})
			if rec.Code != http.StatusCreated {
				t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
			}
			ref, _ := out["reference"].(string)
			if ref == "" || out["ticket_reference"] != ref || out["message"] == nil || out["guest_token"] == nil {
				t.Fatalf("create response = %#v", out)
			}
			tk, err := f.esc.Store.GetTicketByReference(context.Background(), ref)
			if err != nil || tk == nil {
				t.Fatalf("ticket %s not stored: %v", ref, err)
			}
			if tk.DepartmentID == nil || *tk.DepartmentID != f.department {
				t.Fatalf("department_id not stored: %#v", tk.DepartmentID)
			}

			// The widget sends department_id: null when none is picked.
			rec, _ = widgetCall(t, h, http.MethodPost, "/tickets", map[string]any{
				"name": "Ada", "email": "ada@example.com", "subject": "S", "description": "D", "department_id": nil,
			})
			if rec.Code != http.StatusCreated {
				t.Fatalf("create with null department: %d %s", rec.Code, rec.Body.String())
			}

			for _, dept := range []int64{f.inactive, 99999} {
				rec, out = widgetCall(t, h, http.MethodPost, "/tickets", map[string]any{
					"name": "Ada", "email": "ada@example.com", "subject": "S", "description": "D", "department_id": dept,
				})
				errs, _ := out["errors"].(map[string]any)
				if rec.Code != http.StatusUnprocessableEntity || errs["department_id"] == nil {
					t.Fatalf("department %d: %d %s", dept, rec.Code, rec.Body.String())
				}
			}

			rec, out = widgetCall(t, h, http.MethodPost, "/tickets", map[string]any{"name": "Ada"})
			errs, _ := out["errors"].(map[string]any)
			if rec.Code != http.StatusUnprocessableEntity || out["message"] == nil || errs["email"] == nil {
				t.Fatalf("validation: %d %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestWidgetContractChatFlow(t *testing.T) {
	f := newWidgetContractFixture(t, true)
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			rec, out := widgetCall(t, h, http.MethodGet, "/chat/availability", nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("availability: %d", rec.Code)
			}
			if _, ok := out["available"].(bool); !ok {
				t.Fatalf("availability = %#v", out)
			}

			rec, out = widgetCall(t, h, http.MethodPost, "/chat/start", map[string]any{
				"name":          "Grace",
				"email":         "grace@example.com",
				"department_id": f.department,
				"message":       "Hello, anyone there?",
				"metadata":      map[string]any{"page_url": "https://help.example.com/pricing"},
			})
			if rec.Code != http.StatusCreated {
				t.Fatalf("start: %d %s", rec.Code, rec.Body.String())
			}
			id, _ := out["id"].(string)
			if len(id) < 32 {
				t.Fatalf("chat id = %#v, want an unguessable token", out["id"])
			}
			if out["session_id"] == nil || out["status"] != "waiting" {
				t.Fatalf("start response = %#v", out)
			}
			if _, ok := out["messages"].([]any); !ok {
				t.Fatalf("messages = %#v", out["messages"])
			}

			rec, _ = widgetCall(t, h, http.MethodPost, "/chat/"+id+"/messages", map[string]any{"body": "My order is late"})
			if rec.Code/100 != 2 {
				t.Fatalf("send: %d %s", rec.Code, rec.Body.String())
			}
			rec, _ = widgetCall(t, h, http.MethodPost, "/chat/"+id+"/typing", nil)
			if rec.Code/100 != 2 {
				t.Fatalf("typing: %d %s", rec.Code, rec.Body.String())
			}

			rec, out = widgetCall(t, h, http.MethodGet, "/chat/"+id+"/messages", nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("messages: %d %s", rec.Code, rec.Body.String())
			}
			msgs, _ := out["messages"].([]any)
			if len(msgs) != 2 {
				t.Fatalf("messages = %#v, want the opening message and the reply", out["messages"])
			}
			last := msgs[1].(map[string]any)
			author, _ := last["author"].(map[string]any)
			if last["body"] != "My order is late" || last["is_agent"] != false || author["name"] != "Grace" || last["id"] == nil || last["created_at"] == nil {
				t.Fatalf("message = %#v", last)
			}
			if out["ended"] != false {
				t.Fatalf("ended = %#v", out["ended"])
			}

			// An agent reply shows as is_agent.
			tk, _ := f.esc.Store.GetTicketByReference(context.Background(), refForChat(t, f, id))
			session, _ := f.esc.Store.GetChatSessionByTicket(context.Background(), tk.ID)
			agentType := "User"
			agentID := models.UserID("7")
			if err := services.NewChatSessionService(f.esc.Store, services.NewChatRoutingService(f.esc.Store), nil).SendMessage(context.Background(), session, "On it", &agentType, &agentID); err != nil {
				t.Fatalf("agent message: %v", err)
			}
			_, out = widgetCall(t, h, http.MethodGet, "/chat/"+id+"/messages", nil)
			msgs, _ = out["messages"].([]any)
			if m := msgs[len(msgs)-1].(map[string]any); m["is_agent"] != true || m["body"] != "On it" {
				t.Fatalf("agent message = %#v", m)
			}

			// Rating before the chat ends is refused.
			if rec, _ := widgetCall(t, h, http.MethodPost, "/chat/"+id+"/rate", map[string]any{"rating": 5}); rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("rate before end: %d", rec.Code)
			}

			rec, _ = widgetCall(t, h, http.MethodPost, "/chat/"+id+"/end", nil)
			if rec.Code/100 != 2 {
				t.Fatalf("end: %d %s", rec.Code, rec.Body.String())
			}
			_, out = widgetCall(t, h, http.MethodGet, "/chat/"+id+"/messages", nil)
			if out["ended"] != true {
				t.Fatalf("ended after end = %#v", out["ended"])
			}
			if rec, _ := widgetCall(t, h, http.MethodPost, "/chat/"+id+"/messages", map[string]any{"body": "late"}); rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("send after end: %d", rec.Code)
			}

			if rec, _ := widgetCall(t, h, http.MethodPost, "/chat/"+id+"/rate", map[string]any{"rating": 9}); rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("rate 9: %d", rec.Code)
			}
			rec, _ = widgetCall(t, h, http.MethodPost, "/chat/"+id+"/rate", map[string]any{"rating": 4, "comment": "Quick"})
			if rec.Code/100 != 2 {
				t.Fatalf("rate: %d %s", rec.Code, rec.Body.String())
			}
			tk, _ = f.esc.Store.GetTicket(context.Background(), tk.ID)
			var meta map[string]any
			_ = json.Unmarshal(tk.Metadata, &meta)
			rating, _ := meta["chat_rating"].(map[string]any)
			if rating["rating"] != float64(4) || rating["comment"] != "Quick" || meta["page_url"] == nil {
				t.Fatalf("rating not stored next to the chat metadata: %s", tk.Metadata)
			}
			if rec, _ := widgetCall(t, h, http.MethodPost, "/chat/"+id+"/rate", map[string]any{"rating": 2}); rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("second rating: %d", rec.Code)
			}
		})
	}
}

func TestWidgetContractChatRejectsUnknownIDs(t *testing.T) {
	f := newWidgetContractFixture(t, true)
	// A ticket's guest token is not a chat id unless the ticket is a chat.
	tk, err := services.NewTicketService(f.esc.Store).Create(context.Background(), services.CreateTicketInput{
		Subject: "Email ticket", Description: "x", GuestName: strPtr("A"), GuestEmail: strPtr("a@example.com"),
	})
	if err != nil {
		t.Fatalf("create ticket: %v", err)
	}
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			for _, id := range []string{"not-a-token", "1", *tk.GuestToken} {
				for _, call := range []struct{ method, path string }{
					{http.MethodGet, "/messages"},
					{http.MethodPost, "/messages"},
					{http.MethodPost, "/typing"},
					{http.MethodPost, "/end"},
					{http.MethodPost, "/rate"},
				} {
					rec, _ := widgetCall(t, h, call.method, "/chat/"+id+call.path, map[string]any{"body": "x", "rating": 5})
					if rec.Code != http.StatusNotFound {
						t.Fatalf("%s /chat/%s%s: %d, want 404", call.method, id, call.path, rec.Code)
					}
				}
			}
		})
	}
}

func refForChat(t *testing.T, f *widgetContractFixture, token string) string {
	t.Helper()
	lookup, ok := f.esc.Store.(interface {
		GetTicketByGuestToken(context.Context, string) (*models.Ticket, error)
	})
	if !ok {
		t.Fatal("store has no guest token lookup")
	}
	tk, err := lookup.GetTicketByGuestToken(context.Background(), token)
	if err != nil || tk == nil {
		t.Fatalf("chat ticket for token: %v", err)
	}
	return tk.Reference
}

func strPtr(s string) *string { return &s }
