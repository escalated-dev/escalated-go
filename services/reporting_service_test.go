package services

import (
	"testing"
	"time"
)

func TestCalculatePercentiles(t *testing.T) {
	values := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	p := CalculatePercentiles(values)
	if p.P50 != 5.5 {
		t.Errorf("expected p50=5.5, got %v", p.P50)
	}
	if p.P90 < 9 {
		t.Errorf("expected p90>=9, got %v", p.P90)
	}
}

func TestCalculatePercentilesEmpty(t *testing.T) {
	p := CalculatePercentiles([]float64{})
	if p.P50 != 0 {
		t.Errorf("expected p50=0 for empty, got %v", p.P50)
	}
}

func TestCalculatePercentilesSingleValue(t *testing.T) {
	p := CalculatePercentiles([]float64{42})
	if p.P50 != 42 {
		t.Errorf("expected p50=42, got %v", p.P50)
	}
}

func TestBuildDistribution(t *testing.T) {
	values := []float64{1, 2, 3, 4, 5}
	result := BuildDistribution(values, "hours")
	if len(result.Buckets) == 0 {
		t.Error("expected non-empty buckets")
	}
	if result.Stats.Count != 5 {
		t.Errorf("expected count=5, got %d", result.Stats.Count)
	}
	if result.Stats.Unit != "hours" {
		t.Errorf("expected unit=hours, got %s", result.Stats.Unit)
	}
}

func TestBuildDistributionEmpty(t *testing.T) {
	result := BuildDistribution([]float64{}, "hours")
	if len(result.Buckets) != 0 {
		t.Error("expected empty buckets for empty input")
	}
}

func TestCompositeScore(t *testing.T) {
	frt := 2.0
	res := 24.0
	csat := 4.5
	score := CompositeScore(80, &frt, &res, &csat)
	if score <= 0 {
		t.Errorf("expected positive score, got %v", score)
	}
}

func TestDateSeries(t *testing.T) {
	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)
	dates := DateSeries(from, to)
	if len(dates) != 10 {
		t.Errorf("expected 10 dates, got %d", len(dates))
	}
}

func TestDateSeriesMaxDays(t *testing.T) {
	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC)
	dates := DateSeries(from, to)
	if len(dates) > 90 {
		t.Errorf("expected max 90 dates, got %d", len(dates))
	}
}

func TestCalculateChanges(t *testing.T) {
	current := PeriodStats{TotalCreated: 100, TotalResolved: 80, ResolutionRate: 80}
	previous := PeriodStats{TotalCreated: 50, TotalResolved: 40, ResolutionRate: 80}
	changes := CalculateChanges(current, previous)
	if changes["total_created"] != 100 {
		t.Errorf("expected 100%% change, got %v", changes["total_created"])
	}
}
