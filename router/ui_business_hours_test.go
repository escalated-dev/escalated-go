package router_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/store"
)

// Escalated/Admin/BusinessHours/{Index,Form} with Laravel
// BusinessHoursController's props and writes.
func TestAdminBusinessHoursPages(t *testing.T) {
	f := newUIFixture(t)
	repo := store.BusinessSchedulesFor(f.esc.Store)
	ctx := context.Background()
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			index := "/escalated/admin/business-hours"

			create := visit(t, h, index+"/create", uiAgent, "admin")
			if create.Component != "Escalated/Admin/BusinessHours/Form" {
				t.Fatalf("create component %q", create.Component)
			}
			hasKeys(t, "create props", create.Props, "timezones")
			zones := list(t, create.Props, "timezones")
			hasZone := false
			for _, z := range zones {
				hasZone = hasZone || z == "America/New_York"
			}
			if len(zones) < 300 || !hasZone {
				t.Errorf("timezones: %d entries", len(zones))
			}

			wantRedirect(t, pageJSON(h, http.MethodPost, index, `{"name":"Office `+name+`","timezone":"America/New_York","is_default":true,
				"schedule":{"monday":{"start":"09:00","end":"17:00"},"saturday":{"start":"","end":""}},
				"holidays":[{"name":"New Year","date":"2026-01-01","recurring":true},{"name":"Offsite","date":"2026-06-12","recurring":false}]}`),
				"store", index)

			var office *models.BusinessSchedule
			all, _ := repo.ListBusinessSchedules(ctx)
			for _, s := range all {
				if s.Name == "Office "+name {
					office = s
				}
			}
			if office == nil {
				t.Fatal("the schedule was not stored")
			}
			if !office.IsDefault || office.Timezone != "America/New_York" || office.Hours["monday"].End != "17:00" || len(office.Holidays) != 2 {
				t.Errorf("stored %+v", office)
			}

			page := visit(t, h, index, uiAgent, "admin")
			if page.Component != "Escalated/Admin/BusinessHours/Index" {
				t.Fatalf("index component %q", page.Component)
			}
			var row map[string]any
			for _, item := range list(t, page.Props, "schedules") {
				if m := item.(map[string]any); m["name"] == "Office "+name {
					row = m
				}
			}
			if row == nil {
				t.Fatalf("schedules %v", page.Props["schedules"])
			}
			hasKeys(t, "schedule", row, "id", "name", "timezone", "is_default", "schedule", "holidays", "created_at", "updated_at")
			if row["is_default"] != true || obj(t, obj(t, row, "schedule"), "monday")["start"] != "09:00" {
				t.Errorf("schedule row %v", row)
			}
			holiday := first(t, list(t, row, "holidays"), "holidays")
			hasKeys(t, "holiday", holiday, "id", "schedule_id", "name", "date", "recurring")
			if holiday["date"] != "2026-01-01" || holiday["recurring"] != true {
				t.Errorf("holiday %v", holiday)
			}

			edit := visit(t, h, index+"/"+itoa(office.ID)+"/edit", uiAgent, "admin")
			if edit.Component != "Escalated/Admin/BusinessHours/Form" {
				t.Fatalf("edit component %q", edit.Component)
			}
			hasKeys(t, "edit props", edit.Props, "schedule", "timezones")
			sched := obj(t, edit.Props, "schedule")
			hasKeys(t, "edit schedule", sched, "id", "name", "timezone", "is_default", "schedule", "holidays")
			hasKeys(t, "edit holiday", first(t, list(t, sched, "holidays"), "holidays"), "name", "date", "recurring")

			// Update replaces the holidays.
			wantRedirect(t, pageJSON(h, http.MethodPut, index+"/"+itoa(office.ID), `{"name":"HQ `+name+`","timezone":"Europe/London","is_default":false,
				"schedule":{"tuesday":{"start":"08:00","end":"12:00"}},"holidays":[{"name":"Boxing Day","date":"2026-12-26","recurring":true}]}`),
				"update", index)
			updated, _ := repo.GetBusinessSchedule(ctx, office.ID)
			if updated.Name != "HQ "+name || updated.Timezone != "Europe/London" || updated.IsDefault ||
				updated.Hours["tuesday"].Start != "08:00" || len(updated.Holidays) != 1 || updated.Holidays[0].Name != "Boxing Day" {
				t.Errorf("updated %+v", updated)
			}

			for what, body := range map[string]string{
				"name":            `{"name":"","timezone":"UTC","schedule":{}}`,
				"timezone":        `{"name":"X","timezone":"Mars/Base","schedule":{}}`,
				"schedule":        `{"name":"X","timezone":"UTC"}`,
				"holidays.0.date": `{"name":"X","timezone":"UTC","schedule":{},"holidays":[{"name":"Bad","date":"not a date"}]}`,
				"holidays.0.name": `{"name":"X","timezone":"UTC","schedule":{},"holidays":[{"name":"","date":"2026-01-01"}]}`,
			} {
				wantPageErrors(t, pageJSON(h, http.MethodPost, index, body), "store with a bad "+what, "Escalated/Admin/BusinessHours/Form", what)
			}
			wantPageErrors(t, pageJSON(h, http.MethodPut, index+"/"+itoa(office.ID), `{"name":" ","timezone":"UTC","schedule":{}}`),
				"update without a name", "Escalated/Admin/BusinessHours/Form", "name")

			if visit := sendAPI(h, http.MethodGet, index, uiAgent, "agent", ""); visit.Code != http.StatusForbidden && visit.Code != http.StatusNotFound && visit.Code != http.StatusUnauthorized {
				t.Errorf("an agent reached the admin page: %d", visit.Code)
			}

			wantRedirect(t, pageJSON(h, http.MethodDelete, index+"/"+itoa(office.ID), ""), "destroy", index)
			if got, _ := repo.GetBusinessSchedule(ctx, office.ID); got != nil {
				t.Error("the schedule was not deleted")
			}
		})
	}
}

// The SLA policy form's business_hours_only now sticks, and a policy or
// department can name its schedule.
func TestSLAPolicyAndDepartmentKeepTheirBusinessHoursSettings(t *testing.T) {
	f := newUIFixture(t)
	ctx := context.Background()
	repo := store.BusinessSchedulesFor(f.esc.Store)
	sched := &models.BusinessSchedule{Name: "Office", Timezone: "UTC", IsActive: true, Hours: models.DefaultBusinessHours()}
	if err := repo.CreateBusinessSchedule(ctx, sched); err != nil {
		t.Fatal(err)
	}
	h := f.mounts()["chi"]
	index := "/escalated/admin/sla-policies"
	wantRedirect(t, pageJSON(h, http.MethodPost, index,
		`{"name":"Office hours","first_response_hours":{"high":2},"resolution_hours":{},"business_hours_only":true,"business_schedule_id":`+itoa(sched.ID)+`}`),
		"store", index)
	policies, _ := f.esc.Store.ListSLAPolicies(ctx, false)
	var policy *models.SLAPolicy
	for _, p := range policies {
		if p.Name == "Office hours" {
			policy = p
		}
	}
	if policy == nil || !policy.BusinessHoursOnly || policy.BusinessScheduleID == nil || *policy.BusinessScheduleID != sched.ID {
		t.Fatalf("policy %+v", policy)
	}
	edit := visit(t, h, index+"/"+itoa(policy.ID)+"/edit", uiAgent, "admin")
	p := obj(t, edit.Props, "policy")
	if p["business_hours_only"] != true || p["business_schedule_id"] != float64(sched.ID) {
		t.Errorf("policy props %v", p)
	}

	// An update that leaves the keys out leaves them alone.
	wantRedirect(t, pageJSON(h, http.MethodPut, index+"/"+itoa(policy.ID), `{"name":"Renamed"}`), "update", index)
	policy, _ = f.esc.Store.GetSLAPolicy(ctx, policy.ID)
	if !policy.BusinessHoursOnly || policy.BusinessScheduleID == nil {
		t.Errorf("update without the keys cleared them: %+v", policy)
	}
	wantRedirect(t, pageJSON(h, http.MethodPut, index+"/"+itoa(policy.ID), `{"business_hours_only":false,"business_schedule_id":null}`), "update", index)
	policy, _ = f.esc.Store.GetSLAPolicy(ctx, policy.ID)
	if policy.BusinessHoursOnly || policy.BusinessScheduleID != nil {
		t.Errorf("explicit clear: %+v", policy)
	}

	depts, _ := f.esc.Store.ListDepartments(ctx, false)
	dept := depts[0]
	wantRedirect(t, pageJSON(h, http.MethodPut, "/escalated/admin/departments/"+itoa(dept.ID), `{"business_schedule_id":`+itoa(sched.ID)+`}`), "department update", "/escalated/admin/departments")
	got, _ := f.esc.Store.GetDepartment(ctx, dept.ID)
	if got.BusinessScheduleID == nil || *got.BusinessScheduleID != sched.ID {
		t.Errorf("department %+v", got)
	}
	wantRedirect(t, pageJSON(h, http.MethodPut, "/escalated/admin/departments/"+itoa(dept.ID), `{"name":"`+dept.Name+`"}`), "department rename", "/escalated/admin/departments")
	got, _ = f.esc.Store.GetDepartment(ctx, dept.ID)
	if got.BusinessScheduleID == nil {
		t.Error("an update without the key cleared the department's schedule")
	}
	wantPageErrors(t, pageJSON(h, http.MethodPut, "/escalated/admin/departments/"+itoa(dept.ID), `{"business_schedule_id":999999}`),
		"unknown schedule", "Escalated/Admin/Departments/Form", "business_schedule_id")
}
