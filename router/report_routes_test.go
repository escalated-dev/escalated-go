package router_test

import (
	"bytes"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	_ "modernc.org/sqlite"

	escalated "github.com/escalated-dev/escalated-go"
	"github.com/escalated-dev/escalated-go/migrations"
	"github.com/escalated-dev/escalated-go/router"
)

// Regression guard for this port's known failure mode: a feature is coded but
// never mounted. The reporting analytics live in services/reporting_service.go
// but shipped with no handler or route, so they were unreachable. Every
// /admin/reports/* endpoint must be reachable through BOTH MountChi and
// MountStdlib — this exercises them end to end and fails with a 404 if either
// helper forgets to register them.
func newReportRouterEsc(t *testing.T) *escalated.Escalated {
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

	now := time.Now()
	seed := func(ref string, status, priority int, assigned, slaPolicy any, breached bool, created time.Time, firstResp, resolved any) {
		if _, err := db.Exec(
			`INSERT INTO escalated_tickets
			   (reference, subject, description, status, priority, assigned_to, sla_policy_id,
			    sla_breached, first_response_at, resolved_at, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			ref, ref, "body", status, priority, assigned, slaPolicy, breached, firstResp, resolved, created, created,
		); err != nil {
			t.Fatalf("seed %s: %v", ref, err)
		}
	}
	seed("R-1", 5, 2, "3", 1, false, now.AddDate(0, 0, -2), now.AddDate(0, 0, -2).Add(time.Hour), now.AddDate(0, 0, -2).Add(3*time.Hour))
	seed("R-2", 6, 1, "3", 1, true, now.AddDate(0, 0, -1), now.AddDate(0, 0, -1).Add(2*time.Hour), now.AddDate(0, 0, -1).Add(4*time.Hour))
	if _, err := db.Exec(`INSERT INTO escalated_satisfaction_ratings (ticket_id, rating, created_at) VALUES (1, 5, ?)`, now); err != nil {
		t.Fatalf("seed rating: %v", err)
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

func assertReportRoutesReachable(t *testing.T, h http.Handler) {
	t.Helper()

	do := func(path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec
	}

	cases := []struct {
		path string
		want string // a report-specific key that must appear in the JSON body
	}{
		{"/escalated/admin/reports", `"total_tickets"`},
		{"/escalated/admin/reports/first-response-time", `"percentiles"`},
		{"/escalated/admin/reports/resolution-time", `"distribution"`},
		{"/escalated/admin/reports/agent-ranking", `"ranking"`},
		{"/escalated/admin/reports/sla", `"compliance_rate"`},
		{"/escalated/admin/reports/csat", `"response_rate"`},
		{"/escalated/admin/reports/period-comparison", `"changes"`},
	}
	for _, c := range cases {
		rec := do(c.path)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: got %d, want 200 (route not mounted?): %s", c.path, rec.Code, rec.Body.String())
		}
		body := rec.Body.Bytes()
		if !bytes.Contains(body, []byte(`"period_days"`)) || !bytes.Contains(body, []byte(c.want)) {
			t.Fatalf("GET %s: body missing expected keys %q: %s", c.path, c.want, string(body))
		}
	}
}

func TestMountChiRegistersReportRoutes(t *testing.T) {
	r := chi.NewRouter()
	router.MountChi(r, newReportRouterEsc(t))
	assertReportRoutesReachable(t, r)
}

func TestMountStdlibRegistersReportRoutes(t *testing.T) {
	mux := http.NewServeMux()
	router.MountStdlib(mux, newReportRouterEsc(t))
	assertReportRoutesReachable(t, mux)
}
