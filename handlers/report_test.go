package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/escalated-dev/escalated-go/migrations"
	"github.com/escalated-dev/escalated-go/models"
)

func openReportTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	// Single connection so the in-memory schema persists across queries.
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if err := migrations.MigrateSQLite(db, "escalated_"); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func seedReportTicket(t *testing.T, db *sql.DB, ref, subject string, status, priority int, assignedTo, slaPolicyID any, slaBreached bool, createdAt time.Time, firstResponseAt, resolvedAt any) int64 {
	t.Helper()
	res, err := db.Exec(
		`INSERT INTO escalated_tickets
		   (reference, subject, description, status, priority, assigned_to, sla_policy_id,
		    sla_breached, first_response_at, resolved_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ref, subject, "body", status, priority, assignedTo, slaPolicyID,
		slaBreached, firstResponseAt, resolvedAt, createdAt, createdAt,
	)
	if err != nil {
		t.Fatalf("seed ticket %s: %v", ref, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("seed ticket %s id: %v", ref, err)
	}
	return id
}

func seedReportRating(t *testing.T, db *sql.DB, ticketID int64, rating int, createdAt time.Time) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO escalated_satisfaction_ratings (ticket_id, rating, created_at) VALUES (?, ?, ?)`,
		ticketID, rating, createdAt,
	); err != nil {
		t.Fatalf("seed rating: %v", err)
	}
}

// TestReportHandler_SeededData exercises every report endpoint over a fixed
// seed and asserts the computed analytics — proving the handler correctly
// drives the reporting service's helpers, not merely that routes are wired.
func TestReportHandler_SeededData(t *testing.T) {
	db := openReportTestDB(t)
	h := NewReportHandler(db, "escalated_")

	now := time.Now()
	t1 := now.AddDate(0, 0, -3)
	t2 := now.AddDate(0, 0, -2)
	t3 := now.AddDate(0, 0, -1)
	tOld := now.AddDate(0, 0, -20)

	// Agent "7": two tickets, both resolved, one SLA breach.
	id1 := seedReportTicket(t, db, "T-1", "Login broken", models.StatusResolved, models.PriorityHigh,
		"7", 1, false, t1, t1.Add(2*time.Hour), t1.Add(5*time.Hour))
	id2 := seedReportTicket(t, db, "T-2", "Billing issue", models.StatusClosed, models.PriorityMedium,
		"7", 1, true, t2, t2.Add(1*time.Hour), t2.Add(3*time.Hour))
	// Agent "9": one open ticket, no policy, slow first response, unresolved.
	seedReportTicket(t, db, "T-3", "Question", models.StatusOpen, models.PriorityLow,
		"9", nil, false, t3, t3.Add(10*time.Hour), nil)
	// Older-than-a-week ticket used only to prove the ?days window filters.
	seedReportTicket(t, db, "T-OLD", "Stale", models.StatusOpen, models.PriorityLow,
		nil, nil, false, tOld, nil, nil)

	seedReportRating(t, db, id1, 5, now)
	seedReportRating(t, db, id2, 3, now)

	get := func(fn http.HandlerFunc, target string) map[string]any {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, target, nil)
		rec := httptest.NewRecorder()
		fn(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: got %d: %s", target, rec.Code, rec.Body.String())
		}
		var out map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode %s: %v", target, err)
		}
		return out
	}
	eq := func(label string, got, want any) {
		t.Helper()
		if got != want {
			t.Errorf("%s: got %v (%T), want %v", label, got, got, want)
		}
	}

	// ---- Overview ----
	ov := get(h.Index, "/admin/reports")
	eq("overview.total_tickets", ov["total_tickets"], float64(4))
	eq("overview.resolved_tickets", ov["resolved_tickets"], float64(2))
	eq("overview.avg_first_response_hours", ov["avg_first_response_hours"], 4.3) // avg(2,1,10)
	eq("overview.avg_resolution_hours", ov["avg_resolution_hours"], float64(4))  // avg(5,3)
	eq("overview.sla_compliance_rate", ov["sla_compliance_rate"], float64(50))   // 1 of 2 policy tickets breached
	eq("overview.csat_average", ov["csat_average"], float64(4))                  // avg(5,3)
	volSum := 0.0
	for _, e := range ov["volume"].([]any) {
		volSum += e.(map[string]any)["value"].(float64)
	}
	eq("overview.volume sum", volSum, float64(4))

	// ?days window must exclude the 20-day-old ticket.
	ov7 := get(h.Index, "/admin/reports?days=7")
	eq("overview?days=7.total_tickets", ov7["total_tickets"], float64(3))

	// ---- First response time ----
	frt := get(h.FirstResponseTime, "/admin/reports/first-response-time")
	eq("frt.sample_size", frt["sample_size"], float64(3))
	if _, ok := frt["percentiles"]; !ok {
		t.Error("frt: missing percentiles")
	}
	if frt["distribution"].(map[string]any)["stats"].(map[string]any)["count"] != float64(3) {
		t.Errorf("frt.distribution.stats.count: got %v, want 3", frt["distribution"])
	}

	// ---- Resolution time ----
	res := get(h.ResolutionTime, "/admin/reports/resolution-time")
	eq("resolution.sample_size", res["sample_size"], float64(2))

	// ---- Agent ranking ----
	ar := get(h.AgentRanking, "/admin/reports/agent-ranking")
	ranking := ar["ranking"].([]any)
	if len(ranking) != 2 {
		t.Fatalf("agent ranking: got %d rows, want 2", len(ranking))
	}
	first := ranking[0].(map[string]any)
	eq("ranking[0].agent_id", first["agent_id"], "7")
	eq("ranking[0].rank", first["rank"], float64(1))
	eq("ranking[0].resolution_rate", first["resolution_rate"], float64(100))
	eq("ranking[0].csat_average", first["csat_average"], float64(4))
	second := ranking[1].(map[string]any)
	eq("ranking[1].agent_id", second["agent_id"], "9")
	eq("ranking[1].csat_average (null)", second["csat_average"], nil)

	// ---- SLA ----
	sla := get(h.SLA, "/admin/reports/sla")
	eq("sla.total", sla["total"], float64(2))
	eq("sla.breached", sla["breached"], float64(1))
	eq("sla.compliance_rate", sla["compliance_rate"], float64(50))
	breaches := sla["breaches"].([]any)
	if len(breaches) != 1 {
		t.Fatalf("sla breaches: got %d, want 1", len(breaches))
	}
	eq("sla.breaches[0].reference", breaches[0].(map[string]any)["reference"], "T-2")

	// ---- CSAT ----
	csat := get(h.CSAT, "/admin/reports/csat")
	eq("csat.total_ratings", csat["total_ratings"], float64(2))
	eq("csat.csat_average", csat["csat_average"], float64(4))
	eq("csat.response_rate", csat["response_rate"], float64(50)) // 2 ratings / 4 tickets
	if len(csat["breakdown"].([]any)) != 2 {
		t.Errorf("csat.breakdown: got %v, want 2 buckets", csat["breakdown"])
	}

	// ---- Period comparison ----
	pc := get(h.PeriodComparison, "/admin/reports/period-comparison")
	cur := pc["current"].(map[string]any)
	eq("period.current.total_created", cur["total_created"], float64(4))
	eq("period.current.total_resolved", cur["total_resolved"], float64(2))
	prev := pc["previous"].(map[string]any)
	eq("period.previous.total_created", prev["total_created"], float64(0))
	changes := pc["changes"].(map[string]any)
	eq("period.changes.total_created", changes["total_created"], float64(100))
}
