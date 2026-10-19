package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/services"
)

const embedShop = "https://shop.example.com"

type embedFixture struct {
	handler http.Handler
	store   *chatSessionStore
}

// newEmbedFixture wires the widget the way docs/embedding-the-widget.md tells
// a host to: embed.js on its own, every other widget route behind WidgetCORS.
func newEmbedFixture(t *testing.T, configure func(*WidgetConfig)) *embedFixture {
	t.Helper()
	cfg := DefaultWidgetConfig()
	cfg.Enabled = true
	cfg.AllowedOrigins = embedShop
	if configure != nil {
		configure(&cfg)
	}
	ms := &chatSessionStore{widgetMockStore: newWidgetMockStore(), sessions: map[int64]*models.ChatSession{}}
	h := NewWidgetHandler(cfg, ms, services.NewTicketService(ms))
	sessions := services.NewChatSessionService(ms, services.NewChatRoutingService(ms), services.NewBroadcaster(services.BroadcastConfig{Enabled: false}, nil))
	chat := NewWidgetChatHandler(cfg, ms, sessions, nil)

	widget := http.NewServeMux()
	widget.HandleFunc("GET /support/widget/config", h.Config)
	widget.HandleFunc("GET /support/widget/articles/{id}", h.ShowArticle)
	widget.HandleFunc("POST /support/widget/tickets", h.CreateTicket)
	widget.HandleFunc("GET /support/widget/tickets/lookup", h.LookupTicket)
	widget.HandleFunc("POST /support/widget/chat/start", chat.StartChat)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /support/widget/embed.js", WidgetScript)
	mux.Handle("/support/widget/", WidgetCORS(cfg, ms)(widget))
	return &embedFixture{handler: mux, store: ms}
}

func (f *embedFixture) do(method, path, origin string, body any, headers map[string]string) *httptest.ResponseRecorder {
	var reader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, "https://help.example.com"+path, reader)
	req.RemoteAddr = "10.1.2.3:4567"
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	return rec
}

func (f *embedFixture) onlyTicket(t *testing.T) *models.Ticket {
	t.Helper()
	if len(f.store.tickets) != 1 {
		t.Fatalf("expected one ticket, have %d", len(f.store.tickets))
	}
	for _, tk := range f.store.tickets {
		return tk
	}
	return nil
}

func ticketPayload(extra map[string]any) map[string]any {
	p := map[string]any{
		"name":        "Jane Doe",
		"email":       "jane@example.com",
		"subject":     "Where is my order?",
		"description": "It has not arrived yet.",
	}
	for k, v := range extra {
		p[k] = v
	}
	return p
}

func decodeMetadata(t *testing.T, raw models.JSONText) map[string]any {
	t.Helper()
	if len(raw) == 0 {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("metadata %q: %v", raw, err)
	}
	return out
}

func assertACAO(t *testing.T, rec *httptest.ResponseRecorder, want string) {
	t.Helper()
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != want {
		t.Fatalf("Access-Control-Allow-Origin = %q, want %q (status %d, body %s)", got, want, rec.Code, rec.Body)
	}
	if rec.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Fatal("Access-Control-Allow-Credentials must never be sent")
	}
}

func varyHas(rec *httptest.ResponseRecorder, value string) bool {
	for _, line := range rec.Header().Values("Vary") {
		for _, v := range strings.Split(line, ",") {
			if strings.EqualFold(strings.TrimSpace(v), value) {
				return true
			}
		}
	}
	return false
}

func TestWidgetEmbed_PreflightFromAllowedSite(t *testing.T) {
	f := newEmbedFixture(t, nil)
	rec := f.do(http.MethodOptions, "/support/widget/tickets", embedShop, nil, map[string]string{
		"Access-Control-Request-Method":  "POST",
		"Access-Control-Request-Headers": "content-type,authorization",
	})

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status %d, want 204", rec.Code)
	}
	assertACAO(t, rec, embedShop)
	h := rec.Header()
	if !strings.Contains(h.Get("Access-Control-Allow-Methods"), "POST") {
		t.Errorf("Allow-Methods = %q", h.Get("Access-Control-Allow-Methods"))
	}
	for _, want := range []string{"Content-Type", "Authorization", "Accept", "X-Requested-With"} {
		if !strings.Contains(h.Get("Access-Control-Allow-Headers"), want) {
			t.Errorf("Allow-Headers %q lacks %s", h.Get("Access-Control-Allow-Headers"), want)
		}
	}
	if h.Get("Access-Control-Max-Age") != "600" {
		t.Errorf("Max-Age = %q", h.Get("Access-Control-Max-Age"))
	}
	if !varyHas(rec, "Origin") {
		t.Errorf("Vary = %v", h.Values("Vary"))
	}
}

func TestWidgetEmbed_NoGrantForSitesNotAllowed(t *testing.T) {
	f := newEmbedFixture(t, nil)
	preflight := f.do(http.MethodOptions, "/support/widget/tickets", "https://evil.example.net", nil, map[string]string{
		"Access-Control-Request-Method": "POST",
	})
	if preflight.Code != http.StatusNoContent {
		t.Errorf("preflight status %d, want 204", preflight.Code)
	}
	assertACAO(t, preflight, "")

	config := f.do(http.MethodGet, "/support/widget/config", "https://evil.example.net", nil, nil)
	if config.Code != http.StatusOK {
		t.Errorf("config status %d, want 200", config.Code)
	}
	assertACAO(t, config, "")
}

// The widget used to answer every request with Access-Control-Allow-Origin: *
// (or the raw configured string). With no origin listed nothing is granted.
func TestWidgetEmbed_NoGrantByDefault(t *testing.T) {
	f := newEmbedFixture(t, func(c *WidgetConfig) { c.AllowedOrigins = "" })
	for _, origin := range []string{"", "https://help.example.com", embedShop, "https://evil.example.net"} {
		rec := f.do(http.MethodGet, "/support/widget/config", origin, nil, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("origin %q: status %d", origin, rec.Code)
		}
		assertACAO(t, rec, "")
	}

	// Without the middleware the handlers grant nothing themselves either.
	cfg := DefaultWidgetConfig()
	cfg.Enabled = true
	cfg.AllowedOrigins = embedShop
	ms := newWidgetMockStore()
	h := NewWidgetHandler(cfg, ms, services.NewTicketService(ms))
	req := httptest.NewRequest(http.MethodGet, "/widget/config", nil)
	req.Header.Set("Origin", "https://evil.example.net")
	rec := httptest.NewRecorder()
	h.Config(rec, req)
	assertACAO(t, rec, "")
}

func TestWidgetEmbed_AllowedSiteCreatesTicket(t *testing.T) {
	f := newEmbedFixture(t, nil)
	rec := f.do(http.MethodPost, "/support/widget/tickets", embedShop, ticketPayload(nil), map[string]string{"Content-Type": "application/json"})

	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	assertACAO(t, rec, embedShop)
	if !strings.Contains(rec.Header().Get("Access-Control-Expose-Headers"), "Retry-After") {
		t.Errorf("Expose-Headers = %q", rec.Header().Get("Access-Control-Expose-Headers"))
	}
	if !varyHas(rec, "Origin") {
		t.Errorf("Vary = %v", rec.Header().Values("Vary"))
	}
	tk := f.onlyTicket(t)
	if tk.Subject != "Where is my order?" || tk.RequesterID != nil {
		t.Errorf("ticket = %+v", tk)
	}
}

func TestWidgetEmbed_SameOriginAndNoOriginUntouched(t *testing.T) {
	f := newEmbedFixture(t, nil)
	for _, origin := range []string{"", "https://help.example.com"} {
		rec := f.do(http.MethodPost, "/support/widget/tickets", origin, ticketPayload(nil), nil)
		if rec.Code != http.StatusCreated {
			t.Fatalf("origin %q: status %d", origin, rec.Code)
		}
		assertACAO(t, rec, "")
	}
}

func TestWidgetEmbed_AllowedSitesFromAdminSetting(t *testing.T) {
	f := newEmbedFixture(t, func(c *WidgetConfig) { c.AllowedOrigins = "" })
	f.store.settings[WidgetAllowedOriginsSetting] = "https://*.brand.test\nhttp://localhost:5173"

	cases := map[string]string{
		"https://help.brand.test": "https://help.brand.test",
		"https://a.b.brand.test":  "https://a.b.brand.test",
		"http://localhost:5173":   "http://localhost:5173",
		"https://brand.test":      "",
		"http://help.brand.test":  "",
		"https://evilbrand.test":  "",
		"http://localhost:5174":   "",
	}
	for origin, want := range cases {
		rec := f.do(http.MethodGet, "/support/widget/config", origin, nil, nil)
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != want {
			t.Errorf("origin %s: ACAO = %q, want %q", origin, got, want)
		}
	}
}

func TestWidgetEmbed_CORSOnErrorAnswers(t *testing.T) {
	f := newEmbedFixture(t, nil)

	invalid := f.do(http.MethodPost, "/support/widget/tickets", embedShop, map[string]any{"name": "x"}, nil)
	if invalid.Code != http.StatusUnprocessableEntity {
		t.Fatalf("validation status %d", invalid.Code)
	}
	assertACAO(t, invalid, embedShop)

	token := "GT-RIGHT"
	f.store.tickets[50] = &models.Ticket{ID: 50, Reference: "ESC-0050", GuestToken: &token}
	forbidden := f.do(http.MethodGet, "/support/widget/tickets/lookup?ref=ESC-0050&token=wrong", embedShop, nil, nil)
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("lookup status %d", forbidden.Code)
	}
	assertACAO(t, forbidden, embedShop)

	missing := f.do(http.MethodGet, "/support/widget/articles/9", embedShop, nil, nil)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("article status %d", missing.Code)
	}
	assertACAO(t, missing, embedShop)
}

func TestWidgetEmbed_CORSOnRateLimitAnswers(t *testing.T) {
	f := newEmbedFixture(t, func(c *WidgetConfig) { c.RateLimitPerMin = 1 })
	if rec := f.do(http.MethodGet, "/support/widget/tickets/lookup?ref=x&token=y", embedShop, nil, nil); rec.Code == http.StatusTooManyRequests {
		t.Fatal("first request limited")
	}
	limited := f.do(http.MethodGet, "/support/widget/tickets/lookup?ref=x&token=y", embedShop, nil, nil)
	if limited.Code != http.StatusTooManyRequests {
		t.Fatalf("status %d, want 429", limited.Code)
	}
	assertACAO(t, limited, embedShop)
	if !strings.Contains(limited.Header().Get("Access-Control-Expose-Headers"), "Retry-After") {
		t.Errorf("Expose-Headers = %q", limited.Header().Get("Access-Control-Expose-Headers"))
	}
}

func TestWidgetEmbed_VaryIsAppendedNotReplaced(t *testing.T) {
	cfg := DefaultWidgetConfig()
	cfg.AllowedOrigins = embedShop
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Add("Vary", "Accept-Language")
		w.WriteHeader(http.StatusOK)
	})
	outer := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Vary", "Accept-Encoding")
			next.ServeHTTP(w, r)
		})
	}
	h := outer(WidgetCORS(cfg, newWidgetMockStore())(inner))
	req := httptest.NewRequest(http.MethodGet, "https://help.example.com/support/widget/config", nil)
	req.Header.Set("Origin", embedShop)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	for _, want := range []string{"Accept-Encoding", "Origin", "Accept-Language"} {
		if !varyHas(rec, want) {
			t.Errorf("Vary %v lacks %s", rec.Header().Values("Vary"), want)
		}
	}
}

func TestWidgetEmbed_CrossOriginRequestIsMarked(t *testing.T) {
	cfg := DefaultWidgetConfig()
	cfg.AllowedOrigins = embedShop
	var seen []string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, WidgetCrossOrigin(r.Context()))
	})
	h := WidgetCORS(cfg, newWidgetMockStore())(inner)
	for _, origin := range []string{embedShop, "https://evil.example.net", ""} {
		req := httptest.NewRequest(http.MethodGet, "https://help.example.com/support/widget/config", nil)
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		h.ServeHTTP(httptest.NewRecorder(), req)
	}
	if want := []string{embedShop, "", ""}; strings.Join(seen, "|") != strings.Join(want, "|") {
		t.Errorf("WidgetCrossOrigin = %q, want %q", seen, want)
	}
}

func TestWidgetEmbed_ConfigAcceptsMetadata(t *testing.T) {
	f := newEmbedFixture(t, nil)
	rec := f.do(http.MethodGet, "/support/widget/config", "", nil, nil)
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["accepts_metadata"] != true {
		t.Errorf("accepts_metadata = %v", body["accepts_metadata"])
	}
	for _, key := range []string{"title", "subtitle", "primary_color", "logo_url", "welcome_message"} {
		if _, ok := body[key]; !ok {
			t.Errorf("config lost key %s", key)
		}
	}
}

func TestWidgetEmbed_StoresTicketMetadata(t *testing.T) {
	f := newEmbedFixture(t, nil)
	rec := f.do(http.MethodPost, "/support/widget/tickets", embedShop, ticketPayload(map[string]any{
		"metadata": map[string]any{
			"order_number": "A-1001",
			"plan":         " pro ",
			"items":        3,
			"page_url":     "https://shop.example.com/orders/A-1001",
			"embedded_on":  "https://spoofed.example",
			"nested":       map[string]any{"not": "kept"},
			"list":         []any{1, 2},
			"nothing":      nil,
			"bad key!":     "dropped",
			"note":         strings.Repeat("é", 600),
		},
	}), nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}

	got := decodeMetadata(t, f.onlyTicket(t).Metadata)
	want := map[string]any{
		"order_number": "A-1001",
		"plan":         "pro",
		"items":        float64(3),
		"page_url":     "https://shop.example.com/orders/A-1001",
		"embedded_on":  embedShop,
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("metadata[%s] = %#v, want %#v", k, got[k], v)
		}
	}
	for _, k := range []string{"nested", "list", "nothing", "bad key!"} {
		if _, ok := got[k]; ok {
			t.Errorf("metadata kept %s", k)
		}
	}
	if note, _ := got["note"].(string); len([]rune(note)) != 500 {
		t.Errorf("note has %d characters, want 500", len([]rune(note)))
	}
}

func TestWidgetEmbed_DropsPageURLThatIsNotAWebAddress(t *testing.T) {
	f := newEmbedFixture(t, nil)
	rec := f.do(http.MethodPost, "/support/widget/tickets", embedShop, ticketPayload(map[string]any{
		"metadata": map[string]any{"page_url": "javascript:alert(1)", "plan": "pro"},
	}), nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	got := decodeMetadata(t, f.onlyTicket(t).Metadata)
	if len(got) != 2 || got["plan"] != "pro" || got["embedded_on"] != embedShop {
		t.Errorf("metadata = %v", got)
	}

	for _, bad := range []string{"ftp://shop.example.com/file", "/orders/1", "https://"} {
		f := newEmbedFixture(t, nil)
		f.do(http.MethodPost, "/support/widget/tickets", "", ticketPayload(map[string]any{
			"metadata": map[string]any{"page_url": bad, "plan": "pro"},
		}), nil)
		if got := decodeMetadata(t, f.onlyTicket(t).Metadata); len(got) != 1 || got["plan"] != "pro" {
			t.Errorf("page_url %q: metadata = %v", bad, got)
		}
	}
}

func TestWidgetEmbed_SameOriginCannotClaimEmbeddedOn(t *testing.T) {
	f := newEmbedFixture(t, nil)
	f.do(http.MethodPost, "/support/widget/tickets", "", ticketPayload(map[string]any{
		"metadata": map[string]any{"embedded_on": "https://spoofed.example"},
	}), nil)
	if md := f.onlyTicket(t).Metadata; len(md) != 0 {
		t.Errorf("metadata = %s, want none", md)
	}
}

func TestWidgetEmbed_KeepsAtMost25Entries(t *testing.T) {
	f := newEmbedFixture(t, nil)
	md := map[string]any{}
	for i := 0; i < 40; i++ {
		md[fmt.Sprintf("k%02d", i)] = i
	}
	f.do(http.MethodPost, "/support/widget/tickets", embedShop, ticketPayload(map[string]any{"metadata": md}), nil)
	got := decodeMetadata(t, f.onlyTicket(t).Metadata)
	// 25 visitor entries plus embedded_on, which the server records.
	if len(got) != 26 {
		t.Errorf("kept %d entries, want 26", len(got))
	}
}

func TestWidgetEmbed_RejectsMalformedMetadata(t *testing.T) {
	tooMany := map[string]any{}
	for i := 0; i < 51; i++ {
		tooMany[fmt.Sprintf("k%d", i)] = i
	}
	for name, md := range map[string]any{"string": "plan=pro", "array": []any{"a"}, "number": 5, "51 keys": tooMany} {
		t.Run(name, func(t *testing.T) {
			f := newEmbedFixture(t, nil)
			rec := f.do(http.MethodPost, "/support/widget/tickets", embedShop, ticketPayload(map[string]any{"metadata": md}), nil)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status %d, want 422", rec.Code)
			}
			assertACAO(t, rec, embedShop)
			if len(f.store.tickets) != 0 {
				t.Error("ticket created")
			}
		})
	}
}

func TestWidgetEmbed_TicketWithoutMetadataUnchanged(t *testing.T) {
	f := newEmbedFixture(t, nil)
	for _, body := range []map[string]any{ticketPayload(nil), ticketPayload(map[string]any{"metadata": nil})} {
		f.store.tickets = map[int64]*models.Ticket{}
		rec := f.do(http.MethodPost, "/support/widget/tickets", "", body, nil)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status %d: %s", rec.Code, rec.Body)
		}
		if md := f.onlyTicket(t).Metadata; md != nil {
			t.Errorf("metadata = %s, want nil", md)
		}
	}
}

func TestWidgetEmbed_ChatKeepsHostParameters(t *testing.T) {
	f := newEmbedFixture(t, nil)
	rec := f.do(http.MethodPost, "/support/widget/chat/start", embedShop, map[string]any{
		"name":     "John Doe",
		"email":    "john@example.com",
		"message":  "Hello",
		"metadata": map[string]any{"page_url": "https://shop.example.com/cart", "cart_total": 129.5, "deep": map[string]any{"x": 1}},
	}, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	assertACAO(t, rec, embedShop)

	tk := f.onlyTicket(t)
	got := decodeMetadata(t, tk.Metadata)
	if len(got) != 3 || got["page_url"] != "https://shop.example.com/cart" || got["cart_total"] != 129.5 || got["embedded_on"] != embedShop {
		t.Errorf("ticket metadata = %v", got)
	}
	chat := decodeMetadata(t, tk.ChatMetadata)
	if chat["embedded_on"] != embedShop || chat["cart_total"] != 129.5 || chat["started_at"] == nil {
		t.Errorf("chat metadata = %v", chat)
	}
}

func TestWidgetEmbed_ChatRejectsMalformedMetadata(t *testing.T) {
	f := newEmbedFixture(t, nil)
	rec := f.do(http.MethodPost, "/support/widget/chat/start", embedShop, map[string]any{
		"name": "John", "email": "john@example.com", "metadata": "nope",
	}, nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422", rec.Code)
	}
	assertACAO(t, rec, embedShop)
}

func TestWidgetEmbed_ServesScript(t *testing.T) {
	f := newEmbedFixture(t, nil)
	rec := f.do(http.MethodGet, "/support/widget/embed.js", embedShop, nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	want, err := os.ReadFile("assets/widget/escalated-widget.js")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(rec.Body.Bytes(), want) {
		t.Error("embed.js body differs from the vendored bundle")
	}
	h := rec.Header()
	checks := map[string]string{
		"Content-Type":                 "application/javascript; charset=utf-8",
		"Cache-Control":                "public, max-age=3600",
		"X-Content-Type-Options":       "nosniff",
		"Cross-Origin-Resource-Policy": "cross-origin",
		"Access-Control-Allow-Origin":  "*",
	}
	for k, v := range checks {
		if h.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, h.Get(k), v)
		}
	}
	etag := h.Get("ETag")
	if !strings.HasPrefix(etag, `"`) {
		t.Fatalf("ETag = %q", etag)
	}

	again := f.do(http.MethodGet, "/support/widget/embed.js", "", nil, map[string]string{"If-None-Match": etag})
	if again.Code != http.StatusNotModified || again.Body.Len() != 0 {
		t.Errorf("If-None-Match: status %d, %d bytes", again.Code, again.Body.Len())
	}
	if again.Header().Get("ETag") != etag {
		t.Errorf("304 ETag = %q", again.Header().Get("ETag"))
	}
	stale := f.do(http.MethodGet, "/support/widget/embed.js", "", nil, map[string]string{"If-None-Match": `"other"`})
	if stale.Code != http.StatusOK {
		t.Errorf("stale ETag: status %d", stale.Code)
	}
}

func TestAdminHandler_WidgetSettings(t *testing.T) {
	h, ms := newAdminHandlerForTest()

	get := httptest.NewRecorder()
	h.GetWidgetSettings(get, httptest.NewRequest(http.MethodGet, "/admin/settings/widget", nil))
	if get.Code != http.StatusOK || decodeSettings(t, get.Body)["widget_allowed_origins"] != "" {
		t.Fatalf("empty GET: %d %s", get.Code, get.Body)
	}

	value := "https://shop.example.com\nhttps://*.brand.test"
	body, _ := json.Marshal(map[string]string{"widget_allowed_origins": value})
	put := httptest.NewRecorder()
	h.UpdateWidgetSettings(put, httptest.NewRequest(http.MethodPut, "/admin/settings/widget", bytes.NewReader(body)))
	if put.Code != http.StatusOK {
		t.Fatalf("PUT: %d %s", put.Code, put.Body)
	}
	if got := decodeSettings(t, put.Body)["widget_allowed_origins"]; got != value {
		t.Errorf("response = %v", got)
	}
	if ms.settings[WidgetAllowedOriginsSetting] != value {
		t.Errorf("stored = %q", ms.settings[WidgetAllowedOriginsSetting])
	}

	bad, _ := json.Marshal(map[string]string{"widget_allowed_origins": "https://shop.example.com/checkout\n*"})
	rejected := httptest.NewRecorder()
	h.UpdateWidgetSettings(rejected, httptest.NewRequest(http.MethodPut, "/admin/settings/widget", bytes.NewReader(bad)))
	if rejected.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid PUT: %d %s", rejected.Code, rejected.Body)
	}
	if !strings.Contains(rejected.Body.String(), "widget_allowed_origins") {
		t.Errorf("error body = %s", rejected.Body)
	}
	if ms.settings[WidgetAllowedOriginsSetting] != value {
		t.Errorf("invalid value stored: %q", ms.settings[WidgetAllowedOriginsSetting])
	}
}
