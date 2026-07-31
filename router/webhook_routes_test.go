package router_test

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	_ "modernc.org/sqlite"

	escalated "github.com/escalated-dev/escalated-go"
	"github.com/escalated-dev/escalated-go/migrations"
	"github.com/escalated-dev/escalated-go/router"
)

// Regression guard for this port's known failure mode: features are coded but
// never mounted. The outbound-webhooks admin routes must be reachable through
// BOTH MountChi and MountStdlib — this exercises them end to end and fails if
// either helper forgets to register them (a 404 would surface here).
func newWebhookRouterEsc(t *testing.T) *escalated.Escalated {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	// Force a single connection so the in-memory schema persists across queries.
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	if err := migrations.MigrateSQLite(db, "escalated_"); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	cfg := escalated.DefaultConfig()
	cfg.DB = db
	cfg.UIEnabled = true
	cfg.AdminCheck = func(_ *http.Request) bool { return true }

	esc, err := escalated.NewSQLite(cfg)
	if err != nil {
		t.Fatalf("new escalated: %v", err)
	}
	return esc
}

func assertWebhookRoutesReachable(t *testing.T, h http.Handler) {
	t.Helper()

	do := func(method, path, body string) *httptest.ResponseRecorder {
		var r *http.Request
		if body != "" {
			r = httptest.NewRequest(method, path, strings.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
		} else {
			r = httptest.NewRequest(method, path, nil)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec
	}

	// POST /admin/webhooks — create (proves the collection route is mounted).
	rec := do(http.MethodPost, "/escalated/admin/webhooks",
		`{"url":"https://example.test/hook","events":["ticket.created"],"secret":"s"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /admin/webhooks: got %d, want 201 (route not mounted?): %s", rec.Code, rec.Body.String())
	}

	// GET /admin/webhooks — list.
	if rec := do(http.MethodGet, "/escalated/admin/webhooks", ""); rec.Code != http.StatusOK {
		t.Fatalf("GET /admin/webhooks: got %d, want 200: %s", rec.Code, rec.Body.String())
	}

	// PATCH /admin/webhooks/{id} — proves the item route + {id} param is mounted.
	if rec := do(http.MethodPatch, "/escalated/admin/webhooks/1", `{"active":false}`); rec.Code != http.StatusOK {
		t.Fatalf("PATCH /admin/webhooks/1: got %d, want 200: %s", rec.Code, rec.Body.String())
	}

	// GET /admin/webhooks/{id}/deliveries — proves the delivery-log route is mounted.
	if rec := do(http.MethodGet, "/escalated/admin/webhooks/1/deliveries", ""); rec.Code != http.StatusOK {
		t.Fatalf("GET /admin/webhooks/1/deliveries: got %d, want 200: %s", rec.Code, rec.Body.String())
	}
}

func TestMountChiRegistersWebhookRoutes(t *testing.T) {
	r := chi.NewRouter()
	router.MountChi(r, newWebhookRouterEsc(t))
	assertWebhookRoutesReachable(t, r)
}

func TestMountStdlibRegistersWebhookRoutes(t *testing.T) {
	mux := http.NewServeMux()
	router.MountStdlib(mux, newWebhookRouterEsc(t))
	assertWebhookRoutesReachable(t, mux)
}
