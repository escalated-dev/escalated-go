package router_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/escalated-dev/escalated-go/internal/sqldialect"
	"github.com/escalated-dev/escalated-go/services"
)

// The report pages (Laravel Admin\ReportController) with every prop the Vue
// screens read, on a fixture with known times: the fixture ticket answered in
// 30 minutes and resolved in 2 hours against a 1-hour resolution target, the
// guest ticket past its first-response due date, and a third ticket due
// within the at-risk window.
func reportFixture(t *testing.T) *uiFixture {
	t.Helper()
	f := newUIFixture(t)
	db := f.esc.Config.DB
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(sqldialect.Rebind(db, q), args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	created := time.Now().Add(-5 * time.Hour).UTC().Truncate(time.Second)
	exec(`UPDATE escalated_tickets SET created_at = ?, first_response_at = ?, resolved_at = ?, status = 5,
		sla_first_response_due_at = ?, sla_resolution_due_at = ? WHERE id = ?`,
		created, created.Add(30*time.Minute), created.Add(2*time.Hour), created.Add(time.Hour), created.Add(time.Hour), f.ticket.ID)
	exec(`UPDATE escalated_tickets SET first_response_at = NULL, sla_first_response_due_at = ? WHERE id = ?`, time.Now().Add(-time.Hour).UTC(), f.other.ID)
	third, err := services.NewTicketService(f.esc.Store).Create(context.Background(), services.CreateTicketInput{Subject: "Due soon", Description: "x", GuestName: ptr("S"), GuestEmail: ptr("s@example.com")})
	if err != nil {
		t.Fatal(err)
	}
	exec(`UPDATE escalated_tickets SET sla_resolution_due_at = ?, sla_first_response_due_at = NULL WHERE id = ?`, time.Now().Add(2*time.Hour).UTC(), third.ID)
	return f
}

func TestReportPages(t *testing.T) {
	f := reportFixture(t)
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			get := func(path, component string) map[string]any {
				t.Helper()
				page := visit(t, h, "/escalated/admin/reports"+path, uiAgent, "admin")
				if page.Component != component {
					t.Fatalf("%s: component %q, want %s", path, page.Component, component)
				}
				return page.Props
			}

			overview := get("", "Escalated/Admin/Reports")
			hasKeys(t, "overview", overview, "period_days", "total_tickets", "resolved_tickets", "avg_first_response_hours", "sla_breach_count", "by_status", "by_priority", "csat")
			csat := obj(t, overview, "csat")
			if csat["average"].(float64) != 4 || csat["total"].(float64) != 1 || obj(t, csat, "breakdown")["4"].(float64) != 1 {
				t.Errorf("csat %v", csat)
			}
			if obj(t, overview, "by_status")["resolved"].(float64) != 1 {
				t.Errorf("by_status %v", overview["by_status"])
			}

			dash := get("/dashboard", "Escalated/Admin/Reports/Dashboard")
			hasKeys(t, "dashboard", dash, "period_days", "volume", "by_status", "by_priority", "avg_response_hours", "avg_resolution_hours", "sla_compliance", "csat_average", "agent_performance")
			hasKeys(t, "volume point", first(t, list(t, dash, "volume"), "volume"), "label", "value")
			agent := first(t, list(t, dash, "agent_performance"), "agent_performance")
			hasKeys(t, "agent", agent, "agent_id", "agent_name", "total_tickets", "resolved_tickets", "avg_response_hours", "avg_resolution_hours")
			if agent["agent_name"] != "Ada Agent" || agent["avg_response_hours"].(float64) != 0.5 || agent["avg_resolution_hours"].(float64) != 2 {
				t.Errorf("agent %v", agent)
			}

			hasKeys(t, "agent metric", first(t, list(t, get("/agents", "Escalated/Admin/Reports/AgentMetrics"), "agents"), "agents"),
				"agent_id", "agent_name", "total_tickets", "resolved_tickets", "avg_response_hours", "avg_resolution_hours")

			sla := get("/sla", "Escalated/Admin/Reports/SlaReport")
			hasKeys(t, "sla", sla, "period_days", "compliance_rate", "compliance_by_policy", "breaches")
			policy := first(t, list(t, sla, "compliance_by_policy"), "compliance_by_policy")
			hasKeys(t, "policy", policy, "policy_name", "total", "met", "breached")
			if policy["policy_name"] != "Standard" || policy["breached"].(float64) < 1 {
				t.Errorf("policy %v", policy)
			}
			hasKeys(t, "breach", first(t, list(t, sla, "breaches"), "breaches"), "id", "reference", "subject", "sla_policy", "sla_first_response_breached", "sla_resolution_breached", "created_at")

			csatPage := get("/csat", "Escalated/Admin/Reports/CsatReport")
			hasKeys(t, "csat page", csatPage, "period_days", "csat_average", "response_rate", "total_ratings", "by_agent", "over_time")
			byAgent := first(t, list(t, csatPage, "by_agent"), "by_agent")
			hasKeys(t, "csat agent", byAgent, "agent_id", "agent_name", "avg_rating", "total_ratings")
			hasKeys(t, "csat point", first(t, list(t, csatPage, "over_time"), "over_time"), "label", "value")

			for _, path := range []string{"/response-times", "/frt"} {
				frt := get(path, "Escalated/Admin/Reports/ResponseTimes")
				hasKeys(t, "response times", frt, "period_days", "avg_frt", "median_frt", "p90_frt", "pct_under_target", "target_hours", "distribution", "trend", "by_agent", "by_department", "by_priority")
				if len(list(t, frt, "distribution")) != 5 || frt["target_hours"].(float64) != 4 {
					t.Errorf("%s: %v / %v", path, frt["distribution"], frt["target_hours"])
				}
				hasKeys(t, "frt agent", first(t, list(t, frt, "by_agent"), "by_agent"), "agent_id", "agent_name", "count", "avg", "median", "p90")
				hasKeys(t, "frt department", first(t, list(t, frt, "by_department"), "by_department"), "label", "value")
			}
			for _, path := range []string{"/resolution-times", "/resolution"} {
				res := get(path, "Escalated/Admin/Reports/ResolutionTimes")
				hasKeys(t, "resolution times", res, "period_days", "avg_resolution", "median_resolution", "p90_resolution", "pct_under_target", "target_hours", "distribution", "trend", "by_agent", "by_department", "by_channel")
				if len(list(t, res, "distribution")) != 6 || res["avg_resolution"].(float64) != 2 {
					t.Errorf("%s: %v / %v", path, res["distribution"], res["avg_resolution"])
				}
			}

			ranked := first(t, list(t, get("/agent-ranking", "Escalated/Admin/Reports/AgentRanking"), "agents"), "agents")
			hasKeys(t, "ranked agent", ranked, "agent_id", "agent_name", "volume", "resolution_rate", "avg_frt", "avg_resolution", "csat", "composite_score")
			if ranked["resolution_rate"].(float64) != 100 || ranked["csat"].(float64) != 4 {
				t.Errorf("ranked %v", ranked)
			}

			cohorts := get("/cohorts", "Escalated/Admin/Reports/Cohorts")
			hasKeys(t, "cohorts", cohorts, "period_days", "by_tag", "by_department", "by_channel", "by_type", "by_priority")
			tag := first(t, list(t, cohorts, "by_tag"), "by_tag")
			hasKeys(t, "cohort", tag, "name", "volume", "avg_resolution", "breach_rate", "csat")
			if tag["name"] != "vip" {
				t.Errorf("by_tag %v", tag)
			}

			cmp := get("/comparison", "Escalated/Admin/Reports/Comparison")
			for _, side := range []string{"current", "previous"} {
				hasKeys(t, side, obj(t, cmp, side), "total_tickets", "resolved_tickets", "avg_frt", "avg_resolution", "sla_compliance", "csat", "breach_count", "volume_trend")
			}
			if obj(t, cmp, "current")["total_tickets"].(float64) != 3 {
				t.Errorf("current %v", cmp["current"])
			}

			trends := get("/sla-trends", "Escalated/Admin/Reports/SlaTrends")
			hasKeys(t, "trends", trends, "period_days", "breach_trend", "breach_by_type_trend", "breach_by_department", "breach_by_priority", "at_risk_tickets", "total_breaches", "breach_rate", "first_response_breaches", "resolution_breaches")
			if trends["total_breaches"].(float64) != 2 || trends["first_response_breaches"].(float64) != 1 || trends["resolution_breaches"].(float64) != 1 {
				t.Errorf("breaches %v / %v / %v", trends["total_breaches"], trends["first_response_breaches"], trends["resolution_breaches"])
			}
			risk := first(t, list(t, trends, "at_risk_tickets"), "at_risk_tickets")
			hasKeys(t, "at risk", risk, "id", "reference", "subject", "hours_remaining")
			hasKeys(t, "by type", first(t, list(t, trends, "breach_by_type_trend"), "breach_by_type_trend"), "label", "values")

			// API clients keep the JSON report.
			rec := sendAPI(h, http.MethodGet, "/escalated/admin/reports", uiAgent, "admin", "")
			var out map[string]any
			if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &out) != nil || out["total_tickets"] == nil {
				t.Errorf("API report: %d %s", rec.Code, rec.Body.String())
			}
		})
	}
}

// Reports/AgentDetail: the fixture agent's one ticket (answered in 30
// minutes, resolved in 2 hours, rated 4) and the reply and note they wrote,
// against an empty period before. The props are the ones the page declares.
func TestReportAgentDetailPage(t *testing.T) {
	f := reportFixture(t)
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			page := visit(t, h, "/escalated/admin/reports/agent/"+uiAgent+"/detail?days=7", uiAgent, "admin")
			if page.Component != "Escalated/Admin/Reports/AgentDetail" {
				t.Fatalf("component %q", page.Component)
			}
			p := page.Props
			hasKeys(t, "agent detail", p, "period_days", "agent", "total_tickets", "resolved_tickets", "avg_frt",
				"avg_resolution", "csat_score", "response_percentiles", "productivity", "workload", "csat_breakdown",
				"prev_total_tickets", "prev_resolved_tickets", "prev_avg_frt", "prev_avg_resolution", "prev_csat_score")
			agent := obj(t, p, "agent")
			hasKeys(t, "agent", agent, "id", "name", "email")
			if agent["id"].(float64) != 900 || agent["name"] != "Ada Agent" || agent["email"] != "ada@example.com" {
				t.Errorf("agent %v", agent)
			}
			if p["period_days"].(float64) != 7 || p["total_tickets"].(float64) != 1 || p["resolved_tickets"].(float64) != 1 ||
				p["avg_frt"].(float64) != 0.5 || p["avg_resolution"].(float64) != 2 || p["csat_score"].(float64) != 4 {
				t.Errorf("figures %v", p)
			}
			percentiles := obj(t, p, "response_percentiles")
			hasKeys(t, "percentiles", percentiles, "p50", "p75", "p90", "p95", "p99")
			if percentiles["p90"].(float64) != 0.5 {
				t.Errorf("percentiles %v", percentiles)
			}
			written, resolved := 0.0, 0.0
			for _, item := range list(t, p, "productivity") {
				day := item.(map[string]any)
				hasKeys(t, "productivity", day, "label", "value", "values")
				values := day["values"].([]any)
				written += values[0].(float64)
				resolved += values[1].(float64)
			}
			if written != 2 || resolved != 1 {
				t.Errorf("productivity %v", p["productivity"])
			}
			hasKeys(t, "workload", first(t, list(t, p, "workload"), "workload"), "label", "value")
			breakdown := first(t, list(t, p, "csat_breakdown"), "csat_breakdown")
			if breakdown["label"] != "4 stars" || breakdown["value"].(float64) != 1 {
				t.Errorf("csat_breakdown %v", p["csat_breakdown"])
			}
			if p["prev_total_tickets"].(float64) != 0 || p["prev_avg_frt"].(float64) != 0 || p["prev_csat_score"] != nil {
				t.Errorf("previous %v %v %v", p["prev_total_tickets"], p["prev_avg_frt"], p["prev_csat_score"])
			}

			if rec := send(h, http.MethodGet, "/escalated/admin/reports/agent/"+uiAgent+"/detail", uiAgent, "agent", ""); rec.Code != http.StatusForbidden {
				t.Errorf("an agent who is not an admin: %d", rec.Code)
			}
		})
	}
}
