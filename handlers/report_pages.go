package handlers

import (
	"context"
	"database/sql"
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

// The report pages (Laravel Admin\ReportController), built from the tickets
// and ratings in the period. Agents are named through Pages.Users; the
// numbers are this package's own: a ticket has one sla_breached flag, so the
// first-response / resolution split is read off the due dates.

const (
	reportFirstResponseTargetHours = 4
	reportResolutionTargetHours    = 24
)

// pageTicket is the projection the report pages read.
type pageTicket struct {
	ID              int64
	Reference       string
	Subject         string
	Status          int
	Priority        int
	TicketType      string
	AssignedTo      string
	DepartmentID    *int64
	SLAPolicyID     *int64
	SLABreached     bool
	FirstDue        *time.Time
	ResolutionDue   *time.Time
	FirstResponseAt *time.Time
	ResolvedAt      *time.Time
	CreatedAt       time.Time
	Tags            []string
}

type pageRating struct {
	TicketID   int64
	Rating     int
	AssignedTo string
	CreatedAt  time.Time
}

func nullTime(t sql.NullTime) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time
	return &v
}

func (h *ReportHandler) pageTickets(ctx context.Context, since time.Time) ([]pageTicket, error) {
	rows, err := ticketaccess.QueryContext(ctx, h.DB, sqldialect.Rebind(h.DB, fmt.Sprintf(
		`SELECT id, reference, subject, status, priority, ticket_type, assigned_to, department_id, sla_policy_id, sla_breached,
		        sla_first_response_due_at, sla_resolution_due_at, first_response_at, resolved_at, created_at
		   FROM %s WHERE created_at >= ? ORDER BY created_at ASC, id ASC`, h.t("tickets"))), since)
	if err != nil {
		return nil, err
	}
	var out []pageTicket
	index := map[int64]int{}
	for rows.Next() {
		var t pageTicket
		var ticketType, assigned sql.NullString
		var dept, policy sql.NullInt64
		var firstDue, resDue, first, resolved sql.NullTime
		if err := rows.Scan(&t.ID, &t.Reference, &t.Subject, &t.Status, &t.Priority, &ticketType, &assigned, &dept, &policy, &t.SLABreached,
			&firstDue, &resDue, &first, &resolved, &t.CreatedAt); err != nil {
			_ = rows.Close()
			return nil, err
		}
		t.TicketType, t.AssignedTo = ticketType.String, assigned.String
		if dept.Valid {
			t.DepartmentID = &dept.Int64
		}
		if policy.Valid {
			t.SLAPolicyID = &policy.Int64
		}
		t.FirstDue, t.ResolutionDue, t.FirstResponseAt, t.ResolvedAt = nullTime(firstDue), nullTime(resDue), nullTime(first), nullTime(resolved)
		index[t.ID] = len(out)
		out = append(out, t)
	}
	_ = rows.Close()

	tags, err := ticketaccess.QueryContext(ctx, h.DB, sqldialect.Rebind(h.DB, fmt.Sprintf(
		`SELECT tt.ticket_id, g.name FROM %s tt JOIN %s g ON g.id = tt.tag_id
		   JOIN %s t ON t.id = tt.ticket_id WHERE t.created_at >= ?`, h.t("ticket_tags"), h.t("tags"), h.t("tickets"))), since)
	if err == nil {
		for tags.Next() {
			var id int64
			var name string
			if tags.Scan(&id, &name) == nil {
				if i, ok := index[id]; ok {
					out[i].Tags = append(out[i].Tags, name)
				}
			}
		}
		_ = tags.Close()
	}
	return out, nil
}

func (h *ReportHandler) pageRatings(ctx context.Context, since time.Time) ([]pageRating, error) {
	rows, err := ticketaccess.QueryContext(ctx, h.DB, sqldialect.Rebind(h.DB, fmt.Sprintf(
		`SELECT r.ticket_id, r.rating, t.assigned_to, r.created_at FROM %s r JOIN %s t ON t.id = r.ticket_id
		  WHERE r.created_at >= ? AND r.rating > 0`, h.t("satisfaction_ratings"), h.t("tickets"))), since)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []pageRating
	for rows.Next() {
		var r pageRating
		var assigned sql.NullString
		if err := rows.Scan(&r.TicketID, &r.Rating, &assigned, &r.CreatedAt); err != nil {
			return nil, err
		}
		r.AssignedTo = assigned.String
		out = append(out, r)
	}
	return out, rows.Err()
}

func (h *ReportHandler) names(ctx context.Context, table string) map[int64]string {
	out := map[int64]string{}
	rows, err := ticketaccess.QueryContext(ctx, h.DB, fmt.Sprintf(`SELECT id, name FROM %s`, h.t(table)))
	if err != nil {
		return out
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id int64
		var name string
		if rows.Scan(&id, &name) == nil {
			out[id] = name
		}
	}
	return out
}

func (h *ReportHandler) agentName(ctx context.Context, id string) string {
	if name := h.Pages.Users.name(ctx, models.UserID(id)); name != "" {
		return name
	}
	return id
}

// agentID is the agent's id as the pages show it: a number when numeric.
func agentID(id string) any {
	if n, err := strconv.ParseInt(id, 10, 64); err == nil {
		return n
	}
	return id
}

func hoursBetween(from time.Time, to *time.Time) (float64, bool) {
	if to == nil {
		return 0, false
	}
	return to.Sub(from).Hours(), true
}

func frtOf(t pageTicket) (float64, bool)        { return hoursBetween(t.CreatedAt, t.FirstResponseAt) }
func resolutionOf(t pageTicket) (float64, bool) { return hoursBetween(t.CreatedAt, t.ResolvedAt) }

func mean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return round1(sum / float64(len(values)))
}

func pct(part, whole int) float64 {
	if whole == 0 {
		return 0
	}
	return round1(float64(part) / float64(whole) * 100)
}

// firstResponseBreach / resolutionBreach read a breach off the due dates.
func firstResponseBreach(t pageTicket, now time.Time) bool {
	if t.FirstDue == nil {
		return false
	}
	if t.FirstResponseAt != nil {
		return t.FirstResponseAt.After(*t.FirstDue)
	}
	return now.After(*t.FirstDue)
}

func resolutionBreach(t pageTicket, now time.Time) bool {
	if t.ResolutionDue == nil {
		return false
	}
	if t.ResolvedAt != nil {
		return t.ResolvedAt.After(*t.ResolutionDue)
	}
	return now.After(*t.ResolutionDue)
}

func breached(t pageTicket, now time.Time) bool {
	return t.SLABreached || firstResponseBreach(t, now) || resolutionBreach(t, now)
}

func labelValue(label string, value any) map[string]any {
	return map[string]any{"label": label, "value": value}
}

// countBy counts tickets by a key, in key order.
func countBy(tickets []pageTicket, key func(pageTicket) string) []map[string]any {
	counts := map[string]int{}
	for _, t := range tickets {
		counts[key(t)]++
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := []map[string]any{}
	for _, k := range keys {
		out = append(out, labelValue(k, counts[k]))
	}
	return out
}

func volumeByDate(tickets []pageTicket, from, to time.Time) []map[string]any {
	counts := map[string]int{}
	for _, t := range tickets {
		if !t.CreatedAt.Before(from) && t.CreatedAt.Before(to) {
			counts[t.CreatedAt.UTC().Format("2006-01-02")]++
		}
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := []map[string]any{}
	for _, k := range keys {
		out = append(out, labelValue(k, counts[k]))
	}
	return out
}

func within(tickets []pageTicket, from, to time.Time) []pageTicket {
	var out []pageTicket
	for _, t := range tickets {
		if !t.CreatedAt.Before(from) && t.CreatedAt.Before(to) {
			out = append(out, t)
		}
	}
	return out
}

func slaCompliance(tickets []pageTicket, now time.Time) float64 {
	bound, met := 0, 0
	for _, t := range tickets {
		if t.SLAPolicyID == nil {
			continue
		}
		bound++
		if !breached(t, now) {
			met++
		}
	}
	if bound == 0 {
		return 100
	}
	return pct(met, bound)
}

func ratingsAverage(ratings []pageRating) float64 {
	if len(ratings) == 0 {
		return 0
	}
	sum := 0
	for _, r := range ratings {
		sum += r.Rating
	}
	return round1(float64(sum) / float64(len(ratings)))
}

// agentPerformance is Laravel's getAgentPerformance: per assignee.
func (h *ReportHandler) agentPerformance(ctx context.Context, tickets []pageTicket) []map[string]any {
	type agg struct {
		total, resolved int
		frt, res        []float64
	}
	by := map[string]*agg{}
	for _, t := range tickets {
		if t.AssignedTo == "" {
			continue
		}
		a := by[t.AssignedTo]
		if a == nil {
			a = &agg{}
			by[t.AssignedTo] = a
		}
		a.total++
		if v, ok := frtOf(t); ok {
			a.frt = append(a.frt, v)
		}
		if v, ok := resolutionOf(t); ok {
			a.resolved++
			a.res = append(a.res, v)
		}
	}
	ids := make([]string, 0, len(by))
	for id := range by {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		return by[ids[i]].total > by[ids[j]].total || (by[ids[i]].total == by[ids[j]].total && ids[i] < ids[j])
	})
	out := []map[string]any{}
	for _, id := range ids {
		a := by[id]
		out = append(out, map[string]any{
			"agent_id":             agentID(id),
			"agent_name":           h.agentName(ctx, id),
			"total_tickets":        a.total,
			"resolved_tickets":     a.resolved,
			"avg_response_hours":   mean(a.frt),
			"avg_resolution_hours": mean(a.res),
		})
	}
	return out
}

// reportPage loads the period's tickets and ratings and renders a page.
func (h *ReportHandler) reportPage(w http.ResponseWriter, r *http.Request, since time.Time, build func(tickets []pageTicket, ratings []pageRating) (string, map[string]any)) {
	tickets, err := h.pageTickets(r.Context(), since)
	if err != nil {
		http.Error(w, "could not load the report", http.StatusInternalServerError)
		return
	}
	ratings, err := h.pageRatings(r.Context(), since)
	if err != nil {
		http.Error(w, "could not load the report", http.StatusInternalServerError)
		return
	}
	component, props := build(tickets, ratings)
	h.Pages.render(w, r, component, props, nil)
}

// OverviewPage renders Escalated/Admin/Reports (GET /admin/reports for a page
// visit).
func (h *ReportHandler) OverviewPage(w http.ResponseWriter, r *http.Request) {
	days := reportDays(r)
	since := time.Now().AddDate(0, 0, -days)
	h.reportPage(w, r, since, func(tickets []pageTicket, ratings []pageRating) (string, map[string]any) {
		now := time.Now()
		resolved, breaches := 0, 0
		var frt []float64
		status, priority := map[string]int{}, map[string]int{}
		for _, t := range tickets {
			if t.ResolvedAt != nil {
				resolved++
			}
			if breached(t, now) {
				breaches++
			}
			if v, ok := frtOf(t); ok {
				frt = append(frt, v)
			}
			status[statusLabel(t.Status)]++
			priority[priorityLabel(t.Priority)]++
		}
		breakdown := map[string]int{}
		for _, rt := range ratings {
			breakdown[strconv.Itoa(rt.Rating)]++
		}
		return "Escalated/Admin/Reports", map[string]any{
			"period_days":              days,
			"total_tickets":            len(tickets),
			"resolved_tickets":         resolved,
			"avg_first_response_hours": mean(frt),
			"sla_breach_count":         breaches,
			"by_status":                status,
			"by_priority":              priority,
			"csat":                     map[string]any{"average": ratingsAverage(ratings), "total": len(ratings), "breakdown": breakdown},
		}
	})
}

// DashboardPage renders Escalated/Admin/Reports/Dashboard
// (GET /admin/reports/dashboard).
func (h *ReportHandler) DashboardPage(w http.ResponseWriter, r *http.Request) {
	days := reportDays(r)
	since := time.Now().AddDate(0, 0, -days)
	h.reportPage(w, r, since, func(tickets []pageTicket, ratings []pageRating) (string, map[string]any) {
		now := time.Now()
		var frt, res []float64
		for _, t := range tickets {
			if v, ok := frtOf(t); ok {
				frt = append(frt, v)
			}
			if v, ok := resolutionOf(t); ok {
				res = append(res, v)
			}
		}
		return "Escalated/Admin/Reports/Dashboard", map[string]any{
			"period_days":          days,
			"volume":               volumeByDate(tickets, since, now.Add(time.Second)),
			"by_status":            countBy(tickets, func(t pageTicket) string { return statusLabel(t.Status) }),
			"by_priority":          countBy(tickets, func(t pageTicket) string { return priorityLabel(t.Priority) }),
			"avg_response_hours":   mean(frt),
			"avg_resolution_hours": mean(res),
			"sla_compliance":       slaCompliance(tickets, now),
			"csat_average":         ratingsAverage(ratings),
			"agent_performance":    h.agentPerformance(r.Context(), tickets),
		}
	})
}

// AgentsPage renders Escalated/Admin/Reports/AgentMetrics
// (GET /admin/reports/agents).
func (h *ReportHandler) AgentsPage(w http.ResponseWriter, r *http.Request) {
	days := reportDays(r)
	h.reportPage(w, r, time.Now().AddDate(0, 0, -days), func(tickets []pageTicket, _ []pageRating) (string, map[string]any) {
		return "Escalated/Admin/Reports/AgentMetrics", map[string]any{"period_days": days, "agents": h.agentPerformance(r.Context(), tickets)}
	})
}

// SLAPage renders Escalated/Admin/Reports/SlaReport (GET /admin/reports/sla
// for a page visit).
func (h *ReportHandler) SLAPage(w http.ResponseWriter, r *http.Request) {
	days := reportDays(r)
	h.reportPage(w, r, time.Now().AddDate(0, 0, -days), func(tickets []pageTicket, _ []pageRating) (string, map[string]any) {
		now := time.Now()
		policies := h.names(r.Context(), "sla_policies")
		type agg struct{ total, met, breached int }
		by := map[int64]*agg{}
		breaches := []map[string]any{}
		for _, t := range tickets {
			if t.SLAPolicyID != nil {
				a := by[*t.SLAPolicyID]
				if a == nil {
					a = &agg{}
					by[*t.SLAPolicyID] = a
				}
				a.total++
				if breached(t, now) {
					a.breached++
				} else {
					a.met++
				}
			}
			if breached(t, now) {
				var policy any
				if t.SLAPolicyID != nil {
					policy = map[string]any{"id": *t.SLAPolicyID, "name": policies[*t.SLAPolicyID]}
				}
				breaches = append(breaches, map[string]any{
					"id":                          t.ID,
					"reference":                   t.Reference,
					"subject":                     t.Subject,
					"sla_policy":                  policy,
					"sla_first_response_breached": firstResponseBreach(t, now),
					"sla_resolution_breached":     resolutionBreach(t, now) || (t.SLABreached && !firstResponseBreach(t, now)),
					"created_at":                  uiTimeValue(t.CreatedAt),
				})
			}
		}
		ids := make([]int64, 0, len(by))
		for id := range by {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		byPolicy := []map[string]any{}
		for _, id := range ids {
			a := by[id]
			byPolicy = append(byPolicy, map[string]any{"policy_id": id, "policy_name": policies[id], "total": a.total, "met": a.met, "breached": a.breached})
		}
		return "Escalated/Admin/Reports/SlaReport", map[string]any{
			"period_days":          days,
			"compliance_rate":      slaCompliance(tickets, now),
			"compliance_by_policy": byPolicy,
			"breaches":             breaches,
		}
	})
}

// CSATPage renders Escalated/Admin/Reports/CsatReport (GET /admin/reports/csat
// for a page visit).
func (h *ReportHandler) CSATPage(w http.ResponseWriter, r *http.Request) {
	days := reportDays(r)
	h.reportPage(w, r, time.Now().AddDate(0, 0, -days), func(tickets []pageTicket, ratings []pageRating) (string, map[string]any) {
		type agg struct{ sum, n int }
		by := map[string]*agg{}
		byDay := map[string]*agg{}
		for _, rt := range ratings {
			if rt.AssignedTo != "" {
				a := by[rt.AssignedTo]
				if a == nil {
					a = &agg{}
					by[rt.AssignedTo] = a
				}
				a.sum += rt.Rating
				a.n++
			}
			day := rt.CreatedAt.UTC().Format("2006-01-02")
			d := byDay[day]
			if d == nil {
				d = &agg{}
				byDay[day] = d
			}
			d.sum += rt.Rating
			d.n++
		}
		agents := []map[string]any{}
		ids := make([]string, 0, len(by))
		for id := range by {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			a := by[id]
			agents = append(agents, map[string]any{"agent_id": agentID(id), "agent_name": h.agentName(r.Context(), id), "avg_rating": round1(float64(a.sum) / float64(a.n)), "total_ratings": a.n})
		}
		days2 := make([]string, 0, len(byDay))
		for d := range byDay {
			days2 = append(days2, d)
		}
		sort.Strings(days2)
		overTime := []map[string]any{}
		for _, d := range days2 {
			overTime = append(overTime, labelValue(d, round1(float64(byDay[d].sum)/float64(byDay[d].n))))
		}
		return "Escalated/Admin/Reports/CsatReport", map[string]any{
			"period_days":   days,
			"csat_average":  ratingsAverage(ratings),
			"response_rate": pct(len(ratings), len(tickets)),
			"total_ratings": len(ratings),
			"by_agent":      agents,
			"over_time":     overTime,
		}
	})
}

// histogram counts hours into Laravel's labelled buckets.
func histogram(values []float64, buckets []struct {
	label    string
	from, to float64
}) []map[string]any {
	out := []map[string]any{}
	for _, b := range buckets {
		n := 0
		for _, v := range values {
			if v >= b.from && (b.to < 0 || v < b.to) {
				n++
			}
		}
		out = append(out, labelValue(b.label, n))
	}
	return out
}

var frtBuckets = []struct {
	label    string
	from, to float64
}{{"<1h", 0, 1}, {"1-4h", 1, 4}, {"4-8h", 4, 8}, {"8-24h", 8, 24}, {">24h", 24, -1}}

var resolutionBuckets = []struct {
	label    string
	from, to float64
}{{"<4h", 0, 4}, {"4-8h", 4, 8}, {"8-24h", 8, 24}, {"1-3d", 24, 72}, {"3-7d", 72, 168}, {">7d", 168, -1}}

// timeScreen builds ResponseTimes or ResolutionTimes: summary, distribution,
// daily trend, and the averages by agent, department and a third grouping.
func (h *ReportHandler) timeScreen(ctx context.Context, tickets []pageTicket, measure func(pageTicket) (float64, bool), target float64, buckets []struct {
	label    string
	from, to float64
}, third func(pageTicket) string) (map[string]any, []map[string]any) {
	var all []float64
	byDay := map[string][]float64{}
	byAgent := map[string][]float64{}
	byDept := map[string][]float64{}
	byThird := map[string][]float64{}
	departments := h.names(ctx, "departments")
	under := 0
	for _, t := range tickets {
		v, ok := measure(t)
		if !ok {
			continue
		}
		all = append(all, v)
		if v <= target {
			under++
		}
		day := t.CreatedAt.UTC().Format("2006-01-02")
		byDay[day] = append(byDay[day], v)
		if t.AssignedTo != "" {
			byAgent[t.AssignedTo] = append(byAgent[t.AssignedTo], v)
		}
		dept := "Unassigned"
		if t.DepartmentID != nil {
			if name, ok := departments[*t.DepartmentID]; ok {
				dept = name
			}
		}
		byDept[dept] = append(byDept[dept], v)
		byThird[third(t)] = append(byThird[third(t)], v)
	}
	p := services.CalculatePercentiles(all)
	summary := map[string]any{
		"avg":              mean(all),
		"median":           round1(p.P50),
		"p90":              round1(p.P90),
		"pct_under_target": pct(under, len(all)),
		"distribution":     histogram(all, buckets),
		"trend":            averages(byDay),
		"by_department":    averages(byDept),
		"by_third":         averages(byThird),
	}
	agents := []map[string]any{}
	ids := make([]string, 0, len(byAgent))
	for id := range byAgent {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		vals := byAgent[id]
		ap := services.CalculatePercentiles(vals)
		agents = append(agents, map[string]any{
			"agent_id": agentID(id), "agent_name": h.agentName(ctx, id), "count": len(vals),
			"avg": mean(vals), "median": round1(ap.P50), "p90": round1(ap.P90),
		})
	}
	return summary, agents
}

// averages is [{label, value: mean}] in label order.
func averages(groups map[string][]float64) []map[string]any {
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := []map[string]any{}
	for _, k := range keys {
		out = append(out, labelValue(k, mean(groups[k])))
	}
	return out
}

func channelLabel(pageTicket) string { return "unknown" }

// ResponseTimesPage renders Escalated/Admin/Reports/ResponseTimes
// (GET /admin/reports/response-times and /frt).
func (h *ReportHandler) ResponseTimesPage(w http.ResponseWriter, r *http.Request) {
	days := reportDays(r)
	h.reportPage(w, r, time.Now().AddDate(0, 0, -days), func(tickets []pageTicket, _ []pageRating) (string, map[string]any) {
		s, agents := h.timeScreen(r.Context(), tickets, frtOf, reportFirstResponseTargetHours, frtBuckets,
			func(t pageTicket) string { return priorityLabel(t.Priority) })
		return "Escalated/Admin/Reports/ResponseTimes", map[string]any{
			"period_days": days, "avg_frt": s["avg"], "median_frt": s["median"], "p90_frt": s["p90"],
			"pct_under_target": s["pct_under_target"], "target_hours": reportFirstResponseTargetHours,
			"distribution": s["distribution"], "trend": s["trend"], "by_agent": agents,
			"by_department": s["by_department"], "by_priority": s["by_third"],
		}
	})
}

// ResolutionTimesPage renders Escalated/Admin/Reports/ResolutionTimes
// (GET /admin/reports/resolution-times and /resolution). Tickets keep no
// channel, so by_channel has one "unknown" group.
func (h *ReportHandler) ResolutionTimesPage(w http.ResponseWriter, r *http.Request) {
	days := reportDays(r)
	h.reportPage(w, r, time.Now().AddDate(0, 0, -days), func(tickets []pageTicket, _ []pageRating) (string, map[string]any) {
		s, agents := h.timeScreen(r.Context(), tickets, resolutionOf, reportResolutionTargetHours, resolutionBuckets, channelLabel)
		return "Escalated/Admin/Reports/ResolutionTimes", map[string]any{
			"period_days": days, "avg_resolution": s["avg"], "median_resolution": s["median"], "p90_resolution": s["p90"],
			"pct_under_target": s["pct_under_target"], "target_hours": reportResolutionTargetHours,
			"distribution": s["distribution"], "trend": s["trend"], "by_agent": agents,
			"by_department": s["by_department"], "by_channel": s["by_third"],
		}
	})
}

// AgentRankingPage renders Escalated/Admin/Reports/AgentRanking
// (GET /admin/reports/agent-ranking for a page visit).
func (h *ReportHandler) AgentRankingPage(w http.ResponseWriter, r *http.Request) {
	days := reportDays(r)
	h.reportPage(w, r, time.Now().AddDate(0, 0, -days), func(tickets []pageTicket, ratings []pageRating) (string, map[string]any) {
		type agg struct {
			total, resolved int
			frt, res        []float64
			csat            []float64
		}
		by := map[string]*agg{}
		for _, t := range tickets {
			if t.AssignedTo == "" {
				continue
			}
			a := by[t.AssignedTo]
			if a == nil {
				a = &agg{}
				by[t.AssignedTo] = a
			}
			a.total++
			if v, ok := frtOf(t); ok {
				a.frt = append(a.frt, v)
			}
			if v, ok := resolutionOf(t); ok {
				a.resolved++
				a.res = append(a.res, v)
			}
		}
		for _, rt := range ratings {
			if a := by[rt.AssignedTo]; a != nil {
				a.csat = append(a.csat, float64(rt.Rating))
			}
		}
		agents := []map[string]any{}
		for id, a := range by {
			rate := pct(a.resolved, a.total)
			var frt, res, csat *float64
			if len(a.frt) > 0 {
				v := mean(a.frt)
				frt = &v
			}
			if len(a.res) > 0 {
				v := mean(a.res)
				res = &v
			}
			if len(a.csat) > 0 {
				v := mean(a.csat)
				csat = &v
			}
			deref := func(p *float64) float64 {
				if p == nil {
					return 0
				}
				return *p
			}
			agents = append(agents, map[string]any{
				"agent_id": agentID(id), "agent_name": h.agentName(r.Context(), id), "volume": a.total,
				"resolution_rate": rate, "avg_frt": deref(frt), "avg_resolution": deref(res), "csat": deref(csat),
				"composite_score": services.CompositeScore(rate, frt, res, csat),
			})
		}
		sort.SliceStable(agents, func(i, j int) bool {
			return agents[i]["composite_score"].(float64) > agents[j]["composite_score"].(float64)
		})
		return "Escalated/Admin/Reports/AgentRanking", map[string]any{"period_days": days, "agents": agents}
	})
}

// CohortsPage renders Escalated/Admin/Reports/Cohorts
// (GET /admin/reports/cohorts).
func (h *ReportHandler) CohortsPage(w http.ResponseWriter, r *http.Request) {
	days := reportDays(r)
	h.reportPage(w, r, time.Now().AddDate(0, 0, -days), func(tickets []pageTicket, ratings []pageRating) (string, map[string]any) {
		now := time.Now()
		departments := h.names(r.Context(), "departments")
		ratingOf := map[int64]int{}
		for _, rt := range ratings {
			ratingOf[rt.TicketID] = rt.Rating
		}
		cohort := func(keys func(pageTicket) []string) []map[string]any {
			type agg struct {
				volume, breaches int
				res, csat        []float64
			}
			by := map[string]*agg{}
			for _, t := range tickets {
				for _, k := range keys(t) {
					a := by[k]
					if a == nil {
						a = &agg{}
						by[k] = a
					}
					a.volume++
					if breached(t, now) {
						a.breaches++
					}
					if v, ok := resolutionOf(t); ok {
						a.res = append(a.res, v)
					}
					if rating, ok := ratingOf[t.ID]; ok {
						a.csat = append(a.csat, float64(rating))
					}
				}
			}
			names := make([]string, 0, len(by))
			for k := range by {
				names = append(names, k)
			}
			sort.Strings(names)
			out := []map[string]any{}
			for _, k := range names {
				a := by[k]
				out = append(out, map[string]any{"name": k, "volume": a.volume, "avg_resolution": mean(a.res), "breach_rate": pct(a.breaches, a.volume), "csat": mean(a.csat)})
			}
			return out
		}
		return "Escalated/Admin/Reports/Cohorts", map[string]any{
			"period_days":   days,
			"by_tag":        cohort(func(t pageTicket) []string { return t.Tags }),
			"by_department": cohort(func(t pageTicket) []string { return []string{departmentLabel(departments, t.DepartmentID)} }),
			"by_channel":    cohort(func(t pageTicket) []string { return []string{"unknown"} }),
			"by_type":       cohort(func(t pageTicket) []string { return []string{orUnknown(t.TicketType)} }),
			"by_priority":   cohort(func(t pageTicket) []string { return []string{priorityLabel(t.Priority)} }),
		}
	})
}

func departmentLabel(names map[int64]string, id *int64) string {
	if id == nil {
		return "unknown"
	}
	if name, ok := names[*id]; ok {
		return name
	}
	return "unknown"
}

func orUnknown(v string) string {
	if strings.TrimSpace(v) == "" {
		return "unknown"
	}
	return v
}

// ComparisonPage renders Escalated/Admin/Reports/Comparison
// (GET /admin/reports/comparison): this period against the one before.
func (h *ReportHandler) ComparisonPage(w http.ResponseWriter, r *http.Request) {
	days := reportDays(r)
	now := time.Now()
	currentStart := now.AddDate(0, 0, -days)
	previousStart := now.AddDate(0, 0, -2*days)
	h.reportPage(w, r, previousStart, func(all []pageTicket, ratings []pageRating) (string, map[string]any) {
		side := func(from, to time.Time) map[string]any {
			tickets := within(all, from, to)
			resolved, breaches := 0, 0
			var frt, res []float64
			for _, t := range tickets {
				if v, ok := resolutionOf(t); ok {
					resolved++
					res = append(res, v)
				}
				if v, ok := frtOf(t); ok {
					frt = append(frt, v)
				}
				if breached(t, now) {
					breaches++
				}
			}
			var inside []pageRating
			for _, rt := range ratings {
				if !rt.CreatedAt.Before(from) && rt.CreatedAt.Before(to) {
					inside = append(inside, rt)
				}
			}
			return map[string]any{
				"total_tickets": len(tickets), "resolved_tickets": resolved, "avg_frt": mean(frt), "avg_resolution": mean(res),
				"sla_compliance": slaCompliance(tickets, now), "csat": ratingsAverage(inside), "breach_count": breaches,
				"volume_trend": volumeByDate(tickets, from, to),
			}
		}
		return "Escalated/Admin/Reports/Comparison", map[string]any{
			"period_days": days,
			"current":     side(currentStart, now.Add(time.Second)),
			"previous":    side(previousStart, currentStart),
		}
	})
}

// SLATrendsPage renders Escalated/Admin/Reports/SlaTrends
// (GET /admin/reports/sla-trends, ?group_by=day|week|month).
func (h *ReportHandler) SLATrendsPage(w http.ResponseWriter, r *http.Request) {
	days := reportDays(r)
	groupBy := r.URL.Query().Get("group_by")
	h.reportPage(w, r, time.Now().AddDate(0, 0, -days), func(tickets []pageTicket, _ []pageRating) (string, map[string]any) {
		now := time.Now()
		departments := h.names(r.Context(), "departments")
		period := func(t time.Time) string {
			t = t.UTC()
			switch groupBy {
			case "week":
				y, w := t.ISOWeek()
				return fmt.Sprintf("%d-W%02d", y, w)
			case "month":
				return t.Format("2006-01")
			}
			return t.Format("2006-01-02")
		}
		type split struct{ first, resolution, total int }
		byPeriod := map[string]*split{}
		deptTotal, deptBreach := map[string]int{}, map[string]int{}
		prioTotal, prioBreach := map[string]int{}, map[string]int{}
		first, resolution, total := 0, 0, 0
		atRisk := []map[string]any{}
		for _, t := range tickets {
			fb, rb := firstResponseBreach(t, now), resolutionBreach(t, now)
			if t.SLABreached && !fb && !rb {
				rb = true
			}
			dept, prio := departmentLabel(departments, t.DepartmentID), priorityLabel(t.Priority)
			deptTotal[dept]++
			prioTotal[prio]++
			if fb || rb {
				total++
				deptBreach[dept]++
				prioBreach[prio]++
				p := byPeriod[period(t.CreatedAt)]
				if p == nil {
					p = &split{}
					byPeriod[period(t.CreatedAt)] = p
				}
				p.total++
				if fb {
					first++
					p.first++
				}
				if rb {
					resolution++
					p.resolution++
				}
				continue
			}
			if !(&models.Ticket{Status: t.Status}).IsOpen() {
				continue
			}
			var due *time.Time
			for _, d := range []*time.Time{dueIfPending(t.FirstDue, t.FirstResponseAt), dueIfPending(t.ResolutionDue, t.ResolvedAt)} {
				if d != nil && d.After(now) && d.Before(now.Add(8*time.Hour)) && (due == nil || d.Before(*due)) {
					due = d
				}
			}
			if due != nil {
				atRisk = append(atRisk, map[string]any{"id": t.ID, "reference": t.Reference, "subject": t.Subject, "hours_remaining": round1(due.Sub(now).Hours())})
			}
		}
		sort.Slice(atRisk, func(i, j int) bool {
			return atRisk[i]["hours_remaining"].(float64) < atRisk[j]["hours_remaining"].(float64)
		})
		periods := make([]string, 0, len(byPeriod))
		for p := range byPeriod {
			periods = append(periods, p)
		}
		sort.Strings(periods)
		trend, byType := []map[string]any{}, []map[string]any{}
		for _, p := range periods {
			trend = append(trend, labelValue(p, byPeriod[p].total))
			byType = append(byType, map[string]any{"label": p, "values": []int{byPeriod[p].first, byPeriod[p].resolution}})
		}
		rates := func(totals, breaches map[string]int) []map[string]any {
			keys := make([]string, 0, len(totals))
			for k := range totals {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			out := []map[string]any{}
			for _, k := range keys {
				out = append(out, labelValue(k, pct(breaches[k], totals[k])))
			}
			return out
		}
		return "Escalated/Admin/Reports/SlaTrends", map[string]any{
			"period_days":             days,
			"breach_trend":            trend,
			"breach_by_type_trend":    byType,
			"breach_by_department":    rates(deptTotal, deptBreach),
			"breach_by_priority":      rates(prioTotal, prioBreach),
			"at_risk_tickets":         atRisk,
			"total_breaches":          total,
			"breach_rate":             pct(total, len(tickets)),
			"first_response_breaches": first,
			"resolution_breaches":     resolution,
		}
	})
}

func dueIfPending(due, done *time.Time) *time.Time {
	if done != nil {
		return nil
	}
	return due
}
