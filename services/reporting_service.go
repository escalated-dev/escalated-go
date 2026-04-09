package services

import (
	"fmt"
	"math"
	"sort"
	"time"
)

// Percentiles holds p50 through p99 values
type Percentiles struct {
	P50 float64 `json:"p50"`
	P75 float64 `json:"p75"`
	P90 float64 `json:"p90"`
	P95 float64 `json:"p95"`
	P99 float64 `json:"p99"`
}

// SLABreachTrendEntry represents a single day of SLA breach data
type SLABreachTrendEntry struct {
	Date               string `json:"date"`
	FRTBreaches        int    `json:"frt_breaches"`
	ResolutionBreaches int    `json:"resolution_breaches"`
	TotalBreaches      int    `json:"total_breaches"`
}

// DistributionResult holds bucket and stats data for a distribution
type DistributionResult struct {
	Buckets     []DistributionBucket `json:"buckets"`
	Stats       DistributionStats    `json:"stats"`
	Percentiles Percentiles          `json:"percentiles"`
}

// DistributionBucket represents a single bucket in a distribution
type DistributionBucket struct {
	Range string `json:"range"`
	Count int    `json:"count"`
}

// DistributionStats holds summary statistics
type DistributionStats struct {
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
	Avg    float64 `json:"avg"`
	Median float64 `json:"median"`
	Count  int     `json:"count"`
	Unit   string  `json:"unit"`
}

// TrendEntry represents a daily trend data point
type TrendEntry struct {
	Date        string      `json:"date"`
	AvgHours    *float64    `json:"avg_hours"`
	Count       int         `json:"count"`
	Percentiles Percentiles `json:"percentiles"`
}

// AgentFRTEntry represents FRT data for a single agent
type AgentFRTEntry struct {
	AgentID     int         `json:"agent_id"`
	AvgHours    float64     `json:"avg_hours"`
	Count       int         `json:"count"`
	Percentiles Percentiles `json:"percentiles"`
}

// AgentRankingEntry represents an agent's performance ranking
type AgentRankingEntry struct {
	AgentID        int      `json:"agent_id"`
	TotalTickets   int      `json:"total_tickets"`
	ResolvedCount  int      `json:"resolved_count"`
	ResolutionRate float64  `json:"resolution_rate"`
	AvgFRTHours    *float64 `json:"avg_frt_hours"`
	AvgResHours    *float64 `json:"avg_resolution_hours"`
	CompositeScore float64  `json:"composite_score"`
}

// CohortEntry represents a cohort analysis result
type CohortEntry struct {
	Name           string   `json:"name"`
	Total          int      `json:"total"`
	Resolved       int      `json:"resolved"`
	ResolutionRate float64  `json:"resolution_rate"`
	AvgResHours    *float64 `json:"avg_resolution_hours"`
	AvgFRTHours    *float64 `json:"avg_frt_hours"`
}

// PeriodStats holds period statistics for comparison
type PeriodStats struct {
	TotalCreated   int      `json:"total_created"`
	TotalResolved  int      `json:"total_resolved"`
	ResolutionRate float64  `json:"resolution_rate"`
	AvgFRTHours    *float64 `json:"avg_frt_hours"`
	AvgResHours    *float64 `json:"avg_resolution_hours"`
	SLABreaches    int      `json:"sla_breaches"`
}

// PeriodComparison holds current vs previous period data
type PeriodComparison struct {
	Current  PeriodStats        `json:"current"`
	Previous PeriodStats        `json:"previous"`
	Changes  map[string]float64 `json:"changes"`
}

// ReportingService provides advanced reporting functionality
type ReportingService struct{}

// NewReportingService creates a new reporting service
func NewReportingService() *ReportingService {
	return &ReportingService{}
}

// CalculatePercentiles computes p50 through p99 for a slice of float64
func CalculatePercentiles(values []float64) Percentiles {
	if len(values) == 0 {
		return Percentiles{}
	}
	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)
	return Percentiles{
		P50: percentileValue(sorted, 50),
		P75: percentileValue(sorted, 75),
		P90: percentileValue(sorted, 90),
		P95: percentileValue(sorted, 95),
		P99: percentileValue(sorted, 99),
	}
}

func percentileValue(sorted []float64, p float64) float64 {
	if len(sorted) == 1 {
		return math.Round(sorted[0]*100) / 100
	}
	k := (p / 100) * float64(len(sorted)-1)
	f := int(math.Floor(k))
	c := int(math.Ceil(k))
	if f == c {
		return math.Round(sorted[f]*100) / 100
	}
	return math.Round((sorted[f]+(k-float64(f))*(sorted[c]-sorted[f]))*100) / 100
}

// BuildDistribution creates a distribution result from values
func BuildDistribution(values []float64, unit string) DistributionResult {
	if len(values) == 0 {
		return DistributionResult{Buckets: []DistributionBucket{}, Stats: DistributionStats{}}
	}
	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)

	maxVal := sorted[len(sorted)-1]
	bucketSize := int(math.Max(math.Ceil(maxVal/10), 1))
	var buckets []DistributionBucket
	for start := 0; start <= int(math.Ceil(maxVal)); start += bucketSize {
		end := start + bucketSize
		count := 0
		for _, v := range sorted {
			if v >= float64(start) && v < float64(end) {
				count++
			}
		}
		if count > 0 {
			buckets = append(buckets, DistributionBucket{
				Range: fmtRange(start, end),
				Count: count,
			})
		}
	}

	avg := 0.0
	for _, v := range sorted {
		avg += v
	}
	avg = math.Round((avg/float64(len(sorted)))*100) / 100

	return DistributionResult{
		Buckets: buckets,
		Stats: DistributionStats{
			Min:    sorted[0],
			Max:    sorted[len(sorted)-1],
			Avg:    avg,
			Median: percentileValue(sorted, 50),
			Count:  len(sorted),
			Unit:   unit,
		},
		Percentiles: CalculatePercentiles(sorted),
	}
}

// CompositeScore calculates a weighted composite performance score
func CompositeScore(resRate float64, avgFRT, avgRes, avgCSAT *float64) float64 {
	score := (resRate / 100) * 30
	weights := 30.0
	if avgFRT != nil && *avgFRT > 0 {
		score += math.Max(1-*avgFRT/24, 0) * 25
		weights += 25
	}
	if avgRes != nil && *avgRes > 0 {
		score += math.Max(1-*avgRes/72, 0) * 25
		weights += 25
	}
	if avgCSAT != nil {
		score += (*avgCSAT / 5) * 20
		weights += 20
	}
	if weights == 0 {
		return 0
	}
	return math.Round((score/weights)*1000) / 10
}

// DateSeries generates a series of dates between from and to (max 90 days)
func DateSeries(from, to time.Time) []time.Time {
	days := int(to.Sub(from).Hours()/24) + 1
	if days < 1 {
		days = 1
	}
	if days > 90 {
		days = 90
	}
	dates := make([]time.Time, days)
	for i := 0; i < days; i++ {
		dates[i] = from.AddDate(0, 0, i)
	}
	return dates
}

// CalculateChanges computes percentage changes between two period stats
func CalculateChanges(current, previous PeriodStats) map[string]float64 {
	changes := make(map[string]float64)
	pairs := map[string][2]float64{
		"total_created":   {float64(current.TotalCreated), float64(previous.TotalCreated)},
		"total_resolved":  {float64(current.TotalResolved), float64(previous.TotalResolved)},
		"resolution_rate": {current.ResolutionRate, previous.ResolutionRate},
	}
	for key, vals := range pairs {
		if vals[1] == 0 {
			if vals[0] > 0 {
				changes[key] = 100
			} else {
				changes[key] = 0
			}
		} else {
			changes[key] = math.Round((vals[0]-vals[1])/vals[1]*1000) / 10
		}
	}
	return changes
}

func fmtRange(start, end int) string {
	return fmt.Sprintf("%d-%d", start, end)
}
