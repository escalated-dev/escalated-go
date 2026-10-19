package router_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/escalated-dev/escalated-go/handlers"
	"github.com/escalated-dev/escalated-go/services"
)

// The chi wiring shown in docs/embedding-the-widget.md: preflights reach
// WidgetCORS even though no OPTIONS route is registered, and embed.js keeps
// its public grant.
func TestWidgetEmbedChiWiring(t *testing.T) {
	f := newGuardFixture(t)
	cfg := handlers.DefaultWidgetConfig()
	cfg.Enabled = true
	cfg.AllowedOrigins = "https://shop.example.com"
	widget := handlers.NewWidgetHandler(cfg, f.esc.Store, services.NewTicketService(f.esc.Store))

	r := chi.NewRouter()
	r.Get("/support/widget/embed.js", handlers.WidgetScript)
	r.Route("/support/widget", func(r chi.Router) {
		r.Use(handlers.WidgetCORS(cfg, f.esc.Store))
		r.Get("/config", widget.Config)
		r.Post("/tickets", widget.CreateTicket)
	})

	request := func(method, path string, headers map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	preflight := request(http.MethodOptions, "/support/widget/tickets", map[string]string{
		"Origin":                        "https://shop.example.com",
		"Access-Control-Request-Method": "POST",
	})
	if preflight.Code != http.StatusNoContent || preflight.Header().Get("Access-Control-Allow-Origin") != "https://shop.example.com" {
		t.Fatalf("preflight: %d %v", preflight.Code, preflight.Header())
	}

	config := request(http.MethodGet, "/support/widget/config", map[string]string{"Origin": "https://shop.example.com"})
	if config.Code != http.StatusOK || config.Header().Get("Access-Control-Allow-Origin") != "https://shop.example.com" {
		t.Fatalf("config: %d %v", config.Code, config.Header())
	}

	script := request(http.MethodGet, "/support/widget/embed.js", map[string]string{"Origin": "https://shop.example.com"})
	if script.Code != http.StatusOK || script.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("embed.js: %d %v", script.Code, script.Header())
	}
}
