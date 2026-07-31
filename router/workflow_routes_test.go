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
// never mounted. The workflow-engine port shipped with NO route at all, so a
// workflow could not be created. The admin Workflow routes must be reachable
// through BOTH MountChi and MountStdlib — this exercises them end to end and
// fails with a 404 if either helper forgets to register them.
func newWorkflowRouterEsc(t *testing.T) *escalated.Escalated {
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

func assertWorkflowRoutesReachable(t *testing.T, h http.Handler) {
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

	// POST /admin/workflows — create (proves the collection route is mounted).
	rec := do(http.MethodPost, "/escalated/admin/workflows",
		`{"name":"Auto triage","trigger_event":"ticket.created","conditions":{"all":[]},"actions":[{"type":"add_tag","value":"triage"}]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /admin/workflows: got %d, want 201 (route not mounted?): %s", rec.Code, rec.Body.String())
	}

	// GET /admin/workflows — list.
	if rec := do(http.MethodGet, "/escalated/admin/workflows", ""); rec.Code != http.StatusOK {
		t.Fatalf("GET /admin/workflows: got %d, want 200: %s", rec.Code, rec.Body.String())
	}

	// PATCH /admin/workflows/{id} — proves the item route + {id} param is mounted.
	if rec := do(http.MethodPatch, "/escalated/admin/workflows/1", `{"is_active":false}`); rec.Code != http.StatusOK {
		t.Fatalf("PATCH /admin/workflows/1: got %d, want 200: %s", rec.Code, rec.Body.String())
	}

	// GET /admin/workflows/{id}/logs — proves the run-log route is mounted.
	if rec := do(http.MethodGet, "/escalated/admin/workflows/1/logs", ""); rec.Code != http.StatusOK {
		t.Fatalf("GET /admin/workflows/1/logs: got %d, want 200: %s", rec.Code, rec.Body.String())
	}

	// DELETE /admin/workflows/{id} — proves the delete route is mounted.
	if rec := do(http.MethodDelete, "/escalated/admin/workflows/1", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE /admin/workflows/1: got %d, want 204: %s", rec.Code, rec.Body.String())
	}
}

func TestMountChiRegistersWorkflowRoutes(t *testing.T) {
	r := chi.NewRouter()
	router.MountChi(r, newWorkflowRouterEsc(t))
	assertWorkflowRoutesReachable(t, r)
}

func TestMountStdlibRegistersWorkflowRoutes(t *testing.T) {
	mux := http.NewServeMux()
	router.MountStdlib(mux, newWorkflowRouterEsc(t))
	assertWorkflowRoutesReachable(t, mux)
}
