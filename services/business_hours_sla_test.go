package services

import (
	"context"
	"testing"
	"time"

	"github.com/escalated-dev/escalated-go/internal/testdb"
	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/store"
)

// oneHourOn is a UTC schedule open 09:00-10:00 on a single weekday.
func oneHourOn(name, day string, isDefault bool) *models.BusinessSchedule {
	return &models.BusinessSchedule{Name: name, Timezone: "UTC", IsDefault: isDefault, IsActive: true,
		Hours: map[string]models.DaySchedule{day: {Start: "09:00", End: "10:00"}}}
}

func TestSLAScheduleSelectionPolicyThenDepartmentThenDefault(t *testing.T) {
	ctx := context.Background()
	s := testdb.Store(t, testdb.Open(t))
	repo := store.BusinessSchedulesFor(s)
	onPolicy, onDept, onDefault := oneHourOn("policy", "monday", false), oneHourOn("department", "tuesday", false), oneHourOn("default", "wednesday", true)
	for _, sc := range []*models.BusinessSchedule{onPolicy, onDept, onDefault} {
		if err := repo.CreateBusinessSchedule(ctx, sc); err != nil {
			t.Fatal(err)
		}
	}
	withSchedule := &models.Department{Name: "Billing", Slug: "billing", IsActive: true, BusinessScheduleID: &onDept.ID}
	without := &models.Department{Name: "Sales", Slug: "sales", IsActive: true}
	for _, d := range []*models.Department{withSchedule, without} {
		if err := s.CreateDepartment(ctx, d); err != nil {
			t.Fatal(err)
		}
	}

	// Sunday 2026-04-05 12:00 UTC; half an hour of business time.
	base := time.Date(2026, 4, 5, 12, 0, 0, 0, time.UTC)
	policy := func(businessHours bool, scheduleID *int64) *models.SLAPolicy {
		return &models.SLAPolicy{ID: 1, BusinessHoursOnly: businessHours, BusinessScheduleID: scheduleID,
			FirstResponseHours: models.JSONText(`{"medium":0.5}`), ResolutionHours: models.JSONText(`{"medium":1}`)}
	}
	ticket := func(dept *int64) *models.Ticket {
		return &models.Ticket{Priority: models.PriorityMedium, DepartmentID: dept}
	}
	cases := []struct {
		name        string
		policy      *models.SLAPolicy
		ticket      *models.Ticket
		first, resl time.Time
	}{
		{"policy schedule wins", policy(true, &onPolicy.ID), ticket(&withSchedule.ID),
			time.Date(2026, 4, 6, 9, 30, 0, 0, time.UTC), time.Date(2026, 4, 6, 10, 0, 0, 0, time.UTC)},
		{"department schedule next", policy(true, nil), ticket(&withSchedule.ID),
			time.Date(2026, 4, 7, 9, 30, 0, 0, time.UTC), time.Date(2026, 4, 7, 10, 0, 0, 0, time.UTC)},
		{"default schedule last", policy(true, nil), ticket(&without.ID),
			time.Date(2026, 4, 8, 9, 30, 0, 0, time.UTC), time.Date(2026, 4, 8, 10, 0, 0, 0, time.UTC)},
		{"no department", policy(true, nil), ticket(nil),
			time.Date(2026, 4, 8, 9, 30, 0, 0, time.UTC), time.Date(2026, 4, 8, 10, 0, 0, 0, time.UTC)},
		{"calendar policy ignores schedules", policy(false, &onPolicy.ID), ticket(&withSchedule.ID),
			base.Add(30 * time.Minute), base.Add(time.Hour)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			setSLADeadlines(ctx, s, c.policy, c.ticket, base)
			if c.ticket.SLAFirstResponseDueAt == nil || !c.ticket.SLAFirstResponseDueAt.Equal(c.first) {
				t.Errorf("first response due %v, want %v", c.ticket.SLAFirstResponseDueAt, c.first)
			}
			if c.ticket.SLAResolutionDueAt == nil || !c.ticket.SLAResolutionDueAt.Equal(c.resl) {
				t.Errorf("resolution due %v, want %v", c.ticket.SLAResolutionDueAt, c.resl)
			}
		})
	}

	// With no schedule anywhere a business-hours policy keeps today's
	// calendar-hour deadlines.
	for _, sc := range []*models.BusinessSchedule{onPolicy, onDept, onDefault} {
		if err := repo.DeleteBusinessSchedule(ctx, sc.ID); err != nil {
			t.Fatal(err)
		}
	}
	legacy := ticket(nil)
	setSLADeadlines(ctx, s, policy(true, nil), legacy, base)
	if !legacy.SLAFirstResponseDueAt.Equal(base.Add(30 * time.Minute)) {
		t.Errorf("no schedule: first response due %v, want calendar time", legacy.SLAFirstResponseDueAt)
	}
}

// A store without schedules (a host's own Store) keeps calendar hours.
func TestSLAScheduleSelectionWithoutScheduleStore(t *testing.T) {
	base := time.Date(2026, 4, 5, 12, 0, 0, 0, time.UTC)
	tk := &models.Ticket{Priority: models.PriorityMedium}
	setSLADeadlines(context.Background(), newMockStore(), &models.SLAPolicy{BusinessHoursOnly: true,
		FirstResponseHours: models.JSONText(`{"medium":2}`), ResolutionHours: models.JSONText(`{}`)}, tk, base)
	if tk.SLAFirstResponseDueAt == nil || !tk.SLAFirstResponseDueAt.Equal(base.Add(2*time.Hour)) || tk.SLAResolutionDueAt != nil {
		t.Errorf("got %v / %v", tk.SLAFirstResponseDueAt, tk.SLAResolutionDueAt)
	}
}

// Ticket creation and ApplyPolicy both count in business hours.
func TestTicketCreationUsesTheBusinessSchedule(t *testing.T) {
	ctx := context.Background()
	s := testdb.Store(t, testdb.Open(t))
	// Open only Mondays 00:00-00:30 UTC, so any deadline lands on a Monday
	// just after midnight.
	sched := &models.BusinessSchedule{Name: "Monday", Timezone: "UTC", IsDefault: true, IsActive: true,
		Hours: map[string]models.DaySchedule{"monday": {Start: "00:00", End: "00:30"}}}
	if err := store.BusinessSchedulesFor(s).CreateBusinessSchedule(ctx, sched); err != nil {
		t.Fatal(err)
	}
	policy := &models.SLAPolicy{Name: "Business", IsActive: true, IsDefault: true, BusinessHoursOnly: true,
		FirstResponseHours: models.JSONText(`{"medium":0.25}`), ResolutionHours: models.JSONText(`{"medium":0.5}`)}
	if err := s.CreateSLAPolicy(ctx, policy); err != nil {
		t.Fatal(err)
	}
	onMonday := func(what string, due *time.Time) {
		t.Helper()
		if due == nil {
			t.Fatalf("%s: no deadline", what)
		}
		u := due.UTC()
		if u.Weekday() != time.Monday || u.Hour() != 0 || u.Minute() > 30 || !due.After(time.Now().Add(-time.Minute)) {
			t.Errorf("%s: %s is not inside a Monday window", what, u.Format(time.RFC3339))
		}
	}
	tk, err := NewTicketService(s).Create(ctx, CreateTicketInput{Subject: "Hi", Description: "Body", Priority: models.PriorityMedium})
	if err != nil {
		t.Fatal(err)
	}
	onMonday("created first response", tk.SLAFirstResponseDueAt)
	onMonday("created resolution", tk.SLAResolutionDueAt)

	tk.SLAFirstResponseDueAt, tk.SLAResolutionDueAt = nil, nil
	if err := s.UpdateTicket(ctx, tk); err != nil {
		t.Fatal(err)
	}
	if err := NewSLAService(s).ApplyPolicy(ctx, tk.ID, policy.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetTicket(ctx, tk.ID)
	onMonday("applied first response", got.SLAFirstResponseDueAt)
}
