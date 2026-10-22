package handlers

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/escalated-dev/escalated-go/internal/sqldialect"
	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/services"
	"github.com/escalated-dev/escalated-go/ticketaccess"
)

// AgentDetailPage renders Escalated/Admin/Reports/AgentDetail
// (GET /admin/reports/agent/{id}/detail, escalated.admin.reports.agent-detail,
// which the agent ranking links to): one agent's tickets created in the last
// `days` days against the `days` before.
//
// The props are the ones the page declares. escalated-laravel's controller
// passes agent_id, metrics and percentiles, which the page does not read.
func (h *ReportHandler) AgentDetailPage(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		http.NotFound(w, r)
		return
	}
	days := reportDays(r)
	now := time.Now()
	currentStart := now.AddDate(0, 0, -days)
	previousStart := now.AddDate(0, 0, -2*days)

	h.reportPage(w, r, previousStart, func(all []pageTicket, ratings []pageRating) (string, map[string]any) {
		var theirs []pageTicket
		for _, t := range all {
			if t.AssignedTo == id {
				theirs = append(theirs, t)
			}
		}
		var rated []pageRating
		for _, rt := range ratings {
			if rt.AssignedTo == id {
				rated = append(rated, rt)
			}
		}

		current := agentFigures(within(theirs, currentStart, now.Add(time.Second)), ratingsWithin(rated, currentStart, now.Add(time.Second)))
		previous := agentFigures(within(theirs, previousStart, currentStart), ratingsWithin(rated, previousStart, currentStart))

		name := h.agentName(r.Context(), id)
		email := h.Pages.Users.email(r.Context(), models.UserID(id))
		if name == id && email != "" {
			name = email
		}

		props := map[string]any{
			"period_days": days,
			"agent":       map[string]any{"id": agentID(id), "name": name, "email": email},
		}
		for key, value := range current.figures {
			props[key] = value
		}
		for _, key := range []string{"total_tickets", "resolved_tickets", "avg_frt", "avg_resolution", "csat_score"} {
			props["prev_"+key] = previous.figures[key]
		}

		p := services.CalculatePercentiles(current.frt)
		props["response_percentiles"] = map[string]any{
			"p50": round1(p.P50), "p75": round1(p.P75), "p90": round1(p.P90), "p95": round1(p.P95), "p99": round1(p.P99),
		}

		// Replies and notes written, and tickets resolved, per day.
		written := h.agentReplyDays(r.Context(), id, currentStart, now.Add(time.Second))
		resolved := map[string]int{}
		for _, t := range within(theirs, currentStart, now.Add(time.Second)) {
			if t.ResolvedAt != nil && !t.ResolvedAt.Before(currentStart) && !t.ResolvedAt.After(now) {
				resolved[t.ResolvedAt.UTC().Format("2006-01-02")]++
			}
		}
		dates := map[string]bool{}
		for d := range written {
			dates[d] = true
		}
		for d := range resolved {
			dates[d] = true
		}
		productivity := []map[string]any{}
		for _, d := range sortedKeys(dates) {
			productivity = append(productivity, map[string]any{
				"label": d, "value": written[d] + resolved[d], "values": []int{written[d], resolved[d]},
			})
		}
		props["productivity"] = productivity
		props["workload"] = volumeByDate(theirs, currentStart, now.Add(time.Second))

		stars := map[int]int{}
		for _, rt := range ratingsWithin(rated, currentStart, now.Add(time.Second)) {
			stars[rt.Rating]++
		}
		levels := make([]int, 0, len(stars))
		for rating := range stars {
			levels = append(levels, rating)
		}
		sort.Ints(levels)
		breakdown := []map[string]any{}
		for _, rating := range levels {
			breakdown = append(breakdown, labelValue(strconv.Itoa(rating)+" stars", stars[rating]))
		}
		props["csat_breakdown"] = breakdown

		return "Escalated/Admin/Reports/AgentDetail", props
	})
}

type agentPeriod struct {
	figures map[string]any
	frt     []float64
}

// agentFigures is one period's totals for an agent; csat_score is nil
// without ratings, which the page shows as a dash.
func agentFigures(tickets []pageTicket, ratings []pageRating) agentPeriod {
	resolved := 0
	var frt, res []float64
	for _, t := range tickets {
		if v, ok := frtOf(t); ok {
			frt = append(frt, v)
		}
		if v, ok := resolutionOf(t); ok {
			resolved++
			res = append(res, v)
		}
	}
	var csat any
	if len(ratings) > 0 {
		csat = ratingsAverage(ratings)
	}
	return agentPeriod{
		figures: map[string]any{
			"total_tickets": len(tickets), "resolved_tickets": resolved, "avg_frt": mean(frt), "avg_resolution": mean(res),
			"csat_score": csat,
		},
		frt: frt,
	}
}

func ratingsWithin(ratings []pageRating, from, to time.Time) []pageRating {
	var out []pageRating
	for _, rt := range ratings {
		if !rt.CreatedAt.Before(from) && rt.CreatedAt.Before(to) {
			out = append(out, rt)
		}
	}
	return out
}

// agentReplyDays counts the replies and notes the agent wrote in [from, to)
// per UTC day.
func (h *ReportHandler) agentReplyDays(ctx context.Context, id string, from, to time.Time) map[string]int {
	out := map[string]int{}
	rows, err := ticketaccess.QueryContext(ctx, h.DB, sqldialect.Rebind(h.DB, fmt.Sprintf(
		`SELECT created_at FROM %s WHERE author_id = ? AND created_at >= ? AND created_at < ?`, h.t("replies"))),
		id, from, to)
	if err != nil {
		return out
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var at time.Time
		if rows.Scan(&at) == nil {
			out[at.UTC().Format("2006-01-02")]++
		}
	}
	return out
}

func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
