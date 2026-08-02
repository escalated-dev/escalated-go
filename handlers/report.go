package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/services"
)

// ReportHandler exposes the analytics computed by the reporting service
// (services/reporting_service.go) as read-only admin JSON endpoints.
//
// The report surface mirrors the canonical Laravel reference
// (Escalated\Laravel\Http\Controllers\Admin\ReportController plus its API
// sibling) and the sibling ports (symfony/spring/dotnet): an overview
// summary, first-response-time and resolution-time distributions, an agent
// performance ranking, SLA compliance, CSAT analytics, and a
// current-vs-previous period comparison.
//
// All endpoints sit under /admin/reports, so the host app's admin gate (the
// same middleware that protects every other admin route) governs access. The
// handler reads tickets/ratings straight from *sql.DB — like automation.go —
// and feeds them to the reporting service's pure helpers (CalculatePercentiles,
// BuildDistribution, CompositeScore, DateSeries, CalculateChanges), which is
// where the analytics actually live.
type ReportHandler struct {
	DB     *sql.DB
	Prefix string
}

// NewReportHandler constructs the handler. prefix is the table prefix (e.g.
// escalated_); an empty prefix falls back to the default.
func NewReportHandler(db *sql.DB, prefix string) *ReportHandler {
	if prefix == "" {
		prefix = "escalated_"
	}
	return &ReportHandler{DB: db, Prefix: prefix}
}

func (h *ReportHandler) t(name string) string { return h.Prefix + name }

// reportTicket is the slim projection the report queries scan into. Only the
// columns the analytics need are selected.
type reportTicket struct {
	ID              int64
	Reference       string
	Subject         string
	Status          int
	Priority        int
	AssignedTo      sql.NullString
	SLAPolicyID     sql.NullInt64
	SLABreached     bool
	FirstResponseAt sql.NullTime
	ResolvedAt      sql.NullTime
	CreatedAt       time.Time
}

// reportRating is the slim projection for CSAT ratings in a window.
type reportRating struct {
	TicketID int64
	Rating   int
}

// Index handles GET /admin/reports — dashboard overview for the period.
func (h *ReportHandler) Index(w http.ResponseWriter, r *http.Request) {
	days := reportDays(r)
	start, end := reportWindow(days)

	tickets, err := h.ticketsIn(r.Context(), start, end, true)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	ratings, err := h.ratingsIn(r.Context(), start, end)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resolved := 0
	statusCounts := map[int]int{}
	priorityCounts := map[int]int{}
	dayCounts := map[string]int{}
	for _, tk := range tickets {
		if tk.ResolvedAt.Valid {
			resolved++
		}
		statusCounts[tk.Status]++
		priorityCounts[tk.Priority]++
		dayCounts[tk.CreatedAt.Format("2006-01-02")]++
	}

	volume := []reportLabelValue{}
	for _, d := range services.DateSeries(start, end) {
		key := d.Format("2006-01-02")
		volume = append(volume, reportLabelValue{Label: key, Value: dayCounts[key]})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"period_days":              days,
		"total_tickets":            len(tickets),
		"resolved_tickets":         resolved,
		"avg_first_response_hours": avgHours(frtHours(tickets)),
		"avg_resolution_hours":     avgHours(resolutionHours(tickets)),
		"sla_compliance_rate":      slaComplianceRate(tickets),
		"csat_average":             ratingAverage(ratings),
		"by_status":                sortedLabelValues(statusCounts, statusLabel),
		"by_priority":              sortedLabelValues(priorityCounts, priorityLabel),
		"volume":                   volume,
	})
}

// FirstResponseTime handles GET /admin/reports/first-response-time — FRT
// distribution + percentiles.
func (h *ReportHandler) FirstResponseTime(w http.ResponseWriter, r *http.Request) {
	days := reportDays(r)
	start, end := reportWindow(days)

	tickets, err := h.ticketsIn(r.Context(), start, end, true)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	hours := frtHours(tickets)

	writeJSON(w, http.StatusOK, map[string]any{
		"period_days":  days,
		"sample_size":  len(hours),
		"distribution": services.BuildDistribution(hours, "hours"),
		"percentiles":  services.CalculatePercentiles(hours),
	})
}

// ResolutionTime handles GET /admin/reports/resolution-time — resolution
// distribution + percentiles.
func (h *ReportHandler) ResolutionTime(w http.ResponseWriter, r *http.Request) {
	days := reportDays(r)
	start, end := reportWindow(days)

	tickets, err := h.ticketsIn(r.Context(), start, end, true)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	hours := resolutionHours(tickets)

	writeJSON(w, http.StatusOK, map[string]any{
		"period_days":  days,
		"sample_size":  len(hours),
		"distribution": services.BuildDistribution(hours, "hours"),
		"percentiles":  services.CalculatePercentiles(hours),
	})
}

// agentRankRow is one row of the composite-scored agent leaderboard.
type agentRankRow struct {
	AgentID            string   `json:"agent_id"`
	TotalTickets       int      `json:"total_tickets"`
	ResolvedTickets    int      `json:"resolved_tickets"`
	ResolutionRate     float64  `json:"resolution_rate"`
	AvgResponseHours   *float64 `json:"avg_response_hours"`
	AvgResolutionHours *float64 `json:"avg_resolution_hours"`
	CsatAverage        *float64 `json:"csat_average"`
	CompositeScore     float64  `json:"composite_score"`
	Rank               int      `json:"rank"`
}

// AgentRanking handles GET /admin/reports/agent-ranking — composite-scored
// agent leaderboard.
func (h *ReportHandler) AgentRanking(w http.ResponseWriter, r *http.Request) {
	days := reportDays(r)
	start, end := reportWindow(days)

	tickets, err := h.ticketsIn(r.Context(), start, end, true)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	ratings, err := h.ratingsIn(r.Context(), start, end)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	ticketAgent := map[int64]string{}
	groups := map[string][]reportTicket{}
	for _, tk := range tickets {
		if !tk.AssignedTo.Valid || tk.AssignedTo.String == "" {
			continue
		}
		agent := tk.AssignedTo.String
		ticketAgent[tk.ID] = agent
		groups[agent] = append(groups[agent], tk)
	}

	agentRatings := map[string][]int{}
	for _, rr := range ratings {
		if agent, ok := ticketAgent[rr.TicketID]; ok {
			agentRatings[agent] = append(agentRatings[agent], rr.Rating)
		}
	}

	rows := make([]agentRankRow, 0, len(groups))
	for agent, tks := range groups {
		total := len(tks)
		resolved := 0
		for _, tk := range tks {
			if tk.ResolvedAt.Valid {
				resolved++
			}
		}
		resolutionRate := 0.0
		if total > 0 {
			resolutionRate = round1(float64(resolved) / float64(total) * 100)
		}
		avgFrt := avgHoursPtr(frtHours(tks))
		avgRes := avgHoursPtr(resolutionHours(tks))
		avgCsat := ratingAveragePtr(agentRatings[agent])

		rows = append(rows, agentRankRow{
			AgentID:            agent,
			TotalTickets:       total,
			ResolvedTickets:    resolved,
			ResolutionRate:     resolutionRate,
			AvgResponseHours:   avgFrt,
			AvgResolutionHours: avgRes,
			CsatAverage:        avgCsat,
			CompositeScore:     services.CompositeScore(resolutionRate, avgFrt, avgRes, avgCsat),
		})
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].CompositeScore != rows[j].CompositeScore {
			return rows[i].CompositeScore > rows[j].CompositeScore
		}
		return rows[i].AgentID < rows[j].AgentID
	})
	for i := range rows {
		rows[i].Rank = i + 1
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"period_days": days,
		"ranking":     rows,
	})
}

// slaBreachRow is one recent SLA-breached ticket in the SLA report.
type slaBreachRow struct {
	ID          int64     `json:"id"`
	Reference   string    `json:"reference"`
	Subject     string    `json:"subject"`
	AssignedTo  *string   `json:"assigned_to"`
	SLABreached bool      `json:"sla_breached"`
	CreatedAt   time.Time `json:"created_at"`
}

// SLA handles GET /admin/reports/sla — SLA compliance rate + recent breaches.
func (h *ReportHandler) SLA(w http.ResponseWriter, r *http.Request) {
	days := reportDays(r)
	start, end := reportWindow(days)

	tickets, err := h.ticketsIn(r.Context(), start, end, true)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	withPolicy := 0
	breached := 0
	breaches := []slaBreachRow{}
	// tickets are ordered created_at DESC, so the first 100 breaches captured
	// are the most recent ones.
	for _, tk := range tickets {
		if !tk.SLAPolicyID.Valid {
			continue
		}
		withPolicy++
		if !tk.SLABreached {
			continue
		}
		breached++
		if len(breaches) < 100 {
			var assigned *string
			if tk.AssignedTo.Valid {
				s := tk.AssignedTo.String
				assigned = &s
			}
			breaches = append(breaches, slaBreachRow{
				ID:          tk.ID,
				Reference:   tk.Reference,
				Subject:     tk.Subject,
				AssignedTo:  assigned,
				SLABreached: true,
				CreatedAt:   tk.CreatedAt,
			})
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"period_days":     days,
		"total":           withPolicy,
		"breached":        breached,
		"compliance_rate": slaComplianceRate(tickets),
		"breaches":        breaches,
	})
}

// CSAT handles GET /admin/reports/csat — satisfaction average, response rate,
// and rating breakdown.
func (h *ReportHandler) CSAT(w http.ResponseWriter, r *http.Request) {
	days := reportDays(r)
	start, end := reportWindow(days)

	totalTickets, err := h.countTickets(r.Context(), start, end)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	ratings, err := h.ratingsIn(r.Context(), start, end)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	counts := map[int]int{}
	for _, rr := range ratings {
		counts[rr.Rating]++
	}
	scores := make([]int, 0, len(counts))
	for k := range counts {
		scores = append(scores, k)
	}
	sort.Ints(scores)
	breakdown := make([]reportLabelValue, 0, len(scores))
	for _, k := range scores {
		breakdown = append(breakdown, reportLabelValue{Label: strconv.Itoa(k), Value: counts[k]})
	}

	responseRate := 0.0
	if totalTickets > 0 {
		responseRate = round1(float64(len(ratings)) / float64(totalTickets) * 100)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"period_days":   days,
		"csat_average":  ratingAverage(ratings),
		"response_rate": responseRate,
		"total_ratings": len(ratings),
		"breakdown":     breakdown,
	})
}

// PeriodComparison handles GET /admin/reports/period-comparison — current vs
// previous period deltas.
func (h *ReportHandler) PeriodComparison(w http.ResponseWriter, r *http.Request) {
	days := reportDays(r)
	now := time.Now()
	currentStart := now.AddDate(0, 0, -days)
	previousStart := now.AddDate(0, 0, -2*days)

	current, err := h.periodStats(r.Context(), currentStart, now)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	previous, err := h.periodStats(r.Context(), previousStart, currentStart)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"period_days": days,
		"current":     current,
		"previous":    previous,
		"changes":     services.CalculateChanges(current, previous),
	})
}

// periodStats computes a single period's aggregate metrics for the
// current-vs-previous comparison. The window is [start, end).
func (h *ReportHandler) periodStats(ctx context.Context, start, end time.Time) (services.PeriodStats, error) {
	tickets, err := h.ticketsIn(ctx, start, end, false)
	if err != nil {
		return services.PeriodStats{}, err
	}

	created := len(tickets)
	resolved := 0
	breaches := 0
	for _, tk := range tickets {
		if tk.ResolvedAt.Valid {
			resolved++
		}
		if tk.SLAPolicyID.Valid && tk.SLABreached {
			breaches++
		}
	}
	rate := 0.0
	if created > 0 {
		rate = round1(float64(resolved) / float64(created) * 100)
	}

	return services.PeriodStats{
		TotalCreated:   created,
		TotalResolved:  resolved,
		ResolutionRate: rate,
		AvgFRTHours:    avgHoursPtr(frtHours(tickets)),
		AvgResHours:    avgHoursPtr(resolutionHours(tickets)),
		SLABreaches:    breaches,
	}, nil
}

// ticketsIn loads the slim ticket projection created within [start, end]
// (endInclusive) or [start, end) (endExclusive), ordered newest-first.
func (h *ReportHandler) ticketsIn(ctx context.Context, start, end time.Time, endInclusive bool) ([]reportTicket, error) {
	op := "<"
	if endInclusive {
		op = "<="
	}
	q := fmt.Sprintf(
		`SELECT id, reference, subject, status, priority, assigned_to, sla_policy_id,
		        sla_breached, first_response_at, resolved_at, created_at
		   FROM %s
		  WHERE created_at >= ? AND created_at %s ?
		  ORDER BY created_at DESC`, h.t("tickets"), op)

	rows, err := h.DB.QueryContext(ctx, q, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []reportTicket{}
	for rows.Next() {
		var tk reportTicket
		if err := rows.Scan(
			&tk.ID, &tk.Reference, &tk.Subject, &tk.Status, &tk.Priority,
			&tk.AssignedTo, &tk.SLAPolicyID, &tk.SLABreached,
			&tk.FirstResponseAt, &tk.ResolvedAt, &tk.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, tk)
	}
	return out, rows.Err()
}

// countTickets returns the number of tickets created within [start, end].
func (h *ReportHandler) countTickets(ctx context.Context, start, end time.Time) (int, error) {
	var n int
	err := h.DB.QueryRowContext(ctx, fmt.Sprintf(
		`SELECT COUNT(1) FROM %s WHERE created_at >= ? AND created_at <= ?`, h.t("tickets")),
		start, end).Scan(&n)
	return n, err
}

// ratingsIn loads CSAT ratings created within [start, end].
func (h *ReportHandler) ratingsIn(ctx context.Context, start, end time.Time) ([]reportRating, error) {
	rows, err := h.DB.QueryContext(ctx, fmt.Sprintf(
		`SELECT ticket_id, rating FROM %s WHERE created_at >= ? AND created_at <= ?`, h.t("satisfaction_ratings")),
		start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []reportRating{}
	for rows.Next() {
		var rr reportRating
		if err := rows.Scan(&rr.TicketID, &rr.Rating); err != nil {
			return nil, err
		}
		out = append(out, rr)
	}
	return out, rows.Err()
}

// reportLabelValue is a {label, value} pair used for grouped counts
// (status/priority breakdowns, daily volume, CSAT rating breakdown).
type reportLabelValue struct {
	Label string `json:"label"`
	Value int    `json:"value"`
}

// reportDays reads the ?days query param, defaulting to 30 and clamped to
// [1, 365]. DateSeries additionally caps the volume axis at 90 buckets.
func reportDays(r *http.Request) int {
	days := 30
	if v := r.URL.Query().Get("days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			days = n
		}
	}
	if days < 1 {
		days = 1
	}
	if days > 365 {
		days = 365
	}
	return days
}

// reportWindow returns the [start, end] time window for the given day count.
func reportWindow(days int) (time.Time, time.Time) {
	end := time.Now()
	return end.AddDate(0, 0, -days), end
}

// reportHoursBetween returns the hours elapsed between from and to, rounded to
// two decimals. Negative spans (clock skew / bad data) are reported as invalid.
func reportHoursBetween(from, to time.Time) (float64, bool) {
	hrs := math.Round(to.Sub(from).Hours()*100) / 100
	if hrs < 0 {
		return 0, false
	}
	return hrs, true
}

// frtHours extracts valid first-response-time spans (in hours) from a ticket set.
func frtHours(tickets []reportTicket) []float64 {
	out := []float64{}
	for _, tk := range tickets {
		if tk.FirstResponseAt.Valid {
			if hrs, ok := reportHoursBetween(tk.CreatedAt, tk.FirstResponseAt.Time); ok {
				out = append(out, hrs)
			}
		}
	}
	return out
}

// resolutionHours extracts valid resolution-time spans (in hours) from a ticket set.
func resolutionHours(tickets []reportTicket) []float64 {
	out := []float64{}
	for _, tk := range tickets {
		if tk.ResolvedAt.Valid {
			if hrs, ok := reportHoursBetween(tk.CreatedAt, tk.ResolvedAt.Time); ok {
				out = append(out, hrs)
			}
		}
	}
	return out
}

// avgHours returns the mean of values rounded to one decimal, or 0 when empty.
func avgHours(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return round1(sum / float64(len(values)))
}

// avgHoursPtr is avgHours but returns nil (JSON null) for an empty set, so the
// CSAT/composite math can distinguish "no data" from a genuine zero.
func avgHoursPtr(values []float64) *float64 {
	if len(values) == 0 {
		return nil
	}
	v := avgHours(values)
	return &v
}

// ratingAverage returns the mean CSAT rating rounded to one decimal, or 0 when empty.
func ratingAverage(ratings []reportRating) float64 {
	if len(ratings) == 0 {
		return 0
	}
	sum := 0
	for _, rr := range ratings {
		sum += rr.Rating
	}
	return round1(float64(sum) / float64(len(ratings)))
}

// ratingAveragePtr returns the mean of raw rating scores, or nil when empty.
func ratingAveragePtr(scores []int) *float64 {
	if len(scores) == 0 {
		return nil
	}
	sum := 0
	for _, s := range scores {
		sum += s
	}
	v := round1(float64(sum) / float64(len(scores)))
	return &v
}

// slaComplianceRate returns the percentage of policy-bound tickets that did not
// breach, rounded to one decimal. With no policy-bound tickets it returns 100.
func slaComplianceRate(tickets []reportTicket) float64 {
	withPolicy := 0
	breached := 0
	for _, tk := range tickets {
		if tk.SLAPolicyID.Valid {
			withPolicy++
			if tk.SLABreached {
				breached++
			}
		}
	}
	if withPolicy == 0 {
		return 100.0
	}
	return round1(float64(withPolicy-breached) / float64(withPolicy) * 100)
}

// sortedLabelValues turns an int-keyed count map into label/value pairs sorted
// by label, so the JSON output is deterministic.
func sortedLabelValues(counts map[int]int, label func(int) string) []reportLabelValue {
	out := make([]reportLabelValue, 0, len(counts))
	for k, v := range counts {
		out = append(out, reportLabelValue{Label: label(k), Value: v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	return out
}

// statusLabel maps a ticket status integer to its canonical name.
func statusLabel(s int) string {
	if name, ok := models.StatusName[s]; ok {
		return name
	}
	return strconv.Itoa(s)
}

// priorityLabel maps a ticket priority integer to its canonical name.
func priorityLabel(p int) string {
	if name, ok := models.PriorityName[p]; ok {
		return name
	}
	return strconv.Itoa(p)
}

// round1 rounds to one decimal place.
func round1(v float64) float64 {
	return math.Round(v*10) / 10
}
