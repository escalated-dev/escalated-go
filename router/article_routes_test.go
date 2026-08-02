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

// Regression guard for this port's known failure mode: a feature is coded but
// never mounted. The knowledge-base admin authoring routes (articles +
// categories CRUD) must be reachable through BOTH MountChi and MountStdlib —
// this exercises them end to end and fails with a 404 if either helper forgets
// to register them.
func newArticleRouterEsc(t *testing.T) *escalated.Escalated {
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
	cfg.AgentCheck = func(_ *http.Request) bool { return true }

	esc, err := escalated.NewSQLite(cfg)
	if err != nil {
		t.Fatalf("new escalated: %v", err)
	}
	return esc
}

func assertArticleRoutesReachable(t *testing.T, h http.Handler) {
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

	// POST /admin/kb/categories — create (proves the category collection route is mounted).
	if rec := do(http.MethodPost, "/escalated/admin/kb/categories", `{"name":"Billing"}`); rec.Code != http.StatusCreated {
		t.Fatalf("POST /admin/kb/categories: got %d, want 201 (route not mounted?): %s", rec.Code, rec.Body.String())
	}

	// GET /admin/kb/categories — list.
	if rec := do(http.MethodGet, "/escalated/admin/kb/categories", ""); rec.Code != http.StatusOK {
		t.Fatalf("GET /admin/kb/categories: got %d, want 200: %s", rec.Code, rec.Body.String())
	}

	// PATCH /admin/kb/categories/{id} — item route + {id} param.
	if rec := do(http.MethodPatch, "/escalated/admin/kb/categories/1", `{"name":"Support"}`); rec.Code != http.StatusOK {
		t.Fatalf("PATCH /admin/kb/categories/1: got %d, want 200: %s", rec.Code, rec.Body.String())
	}

	// POST /admin/kb/articles — create, filed under the category above.
	if rec := do(http.MethodPost, "/escalated/admin/kb/articles",
		`{"title":"Getting Started","body":"Hello","status":"published","category_id":1}`); rec.Code != http.StatusCreated {
		t.Fatalf("POST /admin/kb/articles: got %d, want 201 (route not mounted?): %s", rec.Code, rec.Body.String())
	}

	// GET /admin/kb/articles — list.
	if rec := do(http.MethodGet, "/escalated/admin/kb/articles", ""); rec.Code != http.StatusOK {
		t.Fatalf("GET /admin/kb/articles: got %d, want 200: %s", rec.Code, rec.Body.String())
	}

	// GET /admin/kb/articles/{id} — show a single article (drafts included).
	if rec := do(http.MethodGet, "/escalated/admin/kb/articles/1", ""); rec.Code != http.StatusOK {
		t.Fatalf("GET /admin/kb/articles/1: got %d, want 200: %s", rec.Code, rec.Body.String())
	}

	// PATCH /admin/kb/articles/{id} — update.
	if rec := do(http.MethodPatch, "/escalated/admin/kb/articles/1", `{"title":"Updated"}`); rec.Code != http.StatusOK {
		t.Fatalf("PATCH /admin/kb/articles/1: got %d, want 200: %s", rec.Code, rec.Body.String())
	}

	// DELETE /admin/kb/articles/{id} — proves the delete route is mounted.
	if rec := do(http.MethodDelete, "/escalated/admin/kb/articles/1", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE /admin/kb/articles/1: got %d, want 204: %s", rec.Code, rec.Body.String())
	}

	// DELETE /admin/kb/categories/{id} — proves the category delete route is mounted.
	if rec := do(http.MethodDelete, "/escalated/admin/kb/categories/1", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE /admin/kb/categories/1: got %d, want 204: %s", rec.Code, rec.Body.String())
	}
}

func TestMountChiRegistersArticleRoutes(t *testing.T) {
	r := chi.NewRouter()
	router.MountChi(r, newArticleRouterEsc(t))
	assertArticleRoutesReachable(t, r)
}

func TestMountStdlibRegistersArticleRoutes(t *testing.T) {
	mux := http.NewServeMux()
	router.MountStdlib(mux, newArticleRouterEsc(t))
	assertArticleRoutesReachable(t, mux)
}
