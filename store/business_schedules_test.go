package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/escalated-dev/escalated-go/internal/testdb"
	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/store"
)

func TestBusinessSchedulesRoundTripWithHolidays(t *testing.T) {
	ctx := context.Background()
	s := testdb.Store(t, testdb.Open(t))
	repo := store.BusinessSchedulesFor(s)
	if repo == nil {
		t.Fatal("the built-in store does not expose business schedules")
	}

	first := &models.BusinessSchedule{Name: "Office", Timezone: "America/New_York", IsDefault: true, IsActive: true,
		Hours: models.DefaultBusinessHours(),
		Holidays: []models.Holiday{
			{Name: "New Year", Date: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), IsRecurring: true},
			{Name: "Offsite", Date: time.Date(2026, 6, 12, 0, 0, 0, 0, time.UTC)},
		}}
	if err := repo.CreateBusinessSchedule(ctx, first); err != nil {
		t.Fatalf("create: %v", err)
	}
	second := &models.BusinessSchedule{Name: "Night", Timezone: "UTC", IsDefault: true, IsActive: true,
		Hours: map[string]models.DaySchedule{"friday": {Start: "22:00", End: "06:00"}}}
	if err := repo.CreateBusinessSchedule(ctx, second); err != nil {
		t.Fatalf("create second: %v", err)
	}

	got, err := repo.GetBusinessSchedule(ctx, first.ID)
	if err != nil || got == nil {
		t.Fatalf("get: %v %v", got, err)
	}
	if got.IsDefault {
		t.Error("a new default schedule should unset the previous default")
	}
	if got.Timezone != "America/New_York" || got.Hours["monday"].Start != "09:00" || got.Hours["saturday"].Open() {
		t.Errorf("schedule did not round-trip: %+v", got)
	}
	if len(got.Holidays) != 2 || got.Holidays[0].Name != "New Year" || !got.Holidays[0].IsRecurring ||
		got.Holidays[1].Date.Format("2006-01-02") != "2026-06-12" || got.Holidays[1].BusinessScheduleID != first.ID {
		t.Errorf("holidays did not round-trip: %+v", got.Holidays)
	}

	def, err := repo.GetDefaultBusinessSchedule(ctx)
	if err != nil || def == nil || def.ID != second.ID {
		t.Fatalf("default: %+v %v", def, err)
	}

	got.Name = "Head office"
	got.IsDefault = true
	got.Holidays = []models.Holiday{{Name: "Boxing Day", Date: time.Date(2026, 12, 26, 0, 0, 0, 0, time.UTC)}}
	if err := repo.UpdateBusinessSchedule(ctx, got); err != nil {
		t.Fatalf("update: %v", err)
	}
	all, err := repo.ListBusinessSchedules(ctx)
	if err != nil || len(all) != 2 {
		t.Fatalf("list: %d %v", len(all), err)
	}
	for _, sc := range all {
		switch sc.ID {
		case first.ID:
			if sc.Name != "Head office" || !sc.IsDefault || len(sc.Holidays) != 1 || sc.Holidays[0].Name != "Boxing Day" {
				t.Errorf("updated schedule: %+v", sc)
			}
		case second.ID:
			if sc.IsDefault {
				t.Error("updating another schedule to default should unset this one")
			}
		}
	}

	// Policies and departments can point at a schedule; deleting it clears
	// the reference.
	policy := &models.SLAPolicy{Name: "Gold", FirstResponseHours: models.JSONText(`{}`), ResolutionHours: models.JSONText(`{}`),
		IsActive: true, BusinessHoursOnly: true, BusinessScheduleID: &first.ID}
	if err := s.CreateSLAPolicy(ctx, policy); err != nil {
		t.Fatalf("policy: %v", err)
	}
	dept := &models.Department{Name: "Support", Slug: "support", IsActive: true, BusinessScheduleID: &first.ID}
	if err := s.CreateDepartment(ctx, dept); err != nil {
		t.Fatalf("department: %v", err)
	}
	p, _ := s.GetSLAPolicy(ctx, policy.ID)
	if !p.BusinessHoursOnly || p.BusinessScheduleID == nil || *p.BusinessScheduleID != first.ID {
		t.Errorf("policy schedule fields did not round-trip: %+v", p)
	}
	d, _ := s.GetDepartment(ctx, dept.ID)
	if d.BusinessScheduleID == nil || *d.BusinessScheduleID != first.ID {
		t.Errorf("department schedule did not round-trip: %+v", d)
	}

	if err := repo.DeleteBusinessSchedule(ctx, first.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if gone, _ := repo.GetBusinessSchedule(ctx, first.ID); gone != nil {
		t.Error("the schedule is still there")
	}
	p, _ = s.GetSLAPolicy(ctx, policy.ID)
	d, _ = s.GetDepartment(ctx, dept.ID)
	if p.BusinessScheduleID != nil || d.BusinessScheduleID != nil {
		t.Errorf("references survived the delete: policy %v department %v", p.BusinessScheduleID, d.BusinessScheduleID)
	}
	if !p.BusinessHoursOnly {
		t.Error("deleting a schedule should not change the policy's mode")
	}
}
