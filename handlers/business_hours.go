package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/store"
)

// Business hours admin (Laravel Admin\BusinessHoursController): the
// Escalated/Admin/BusinessHours/{Index,Form} pages and their writes at
// /admin/business-hours (escalated.admin.business-hours.*).

var validTimezones = func() map[string]bool {
	m := make(map[string]bool, len(timezoneIdentifiers))
	for _, z := range timezoneIdentifiers {
		m[z] = true
	}
	return m
}()

var scheduleDays = []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"}

func (h *AdminHandler) businessSchedules(w http.ResponseWriter) store.BusinessSchedules {
	repo := store.BusinessSchedulesFor(h.store)
	if repo == nil {
		http.Error(w, "this store keeps no business schedules", http.StatusNotFound)
	}
	return repo
}

// businessScheduleProps is a schedule as the pages read it: Laravel's model
// with its holidays loaded.
func businessScheduleProps(s *models.BusinessSchedule) map[string]any {
	days := map[string]any{}
	for day, hours := range s.Hours {
		entry := map[string]any{"start": hours.Start, "end": hours.End}
		if hours.Enabled != nil {
			entry["enabled"] = *hours.Enabled
		}
		days[day] = entry
	}
	holidays := make([]map[string]any, 0, len(s.Holidays))
	for _, hol := range s.Holidays {
		holidays = append(holidays, map[string]any{
			"id":          hol.ID,
			"schedule_id": hol.BusinessScheduleID,
			"name":        hol.Name,
			"date":        hol.Date.Format("2006-01-02"),
			"recurring":   hol.IsRecurring,
			"created_at":  uiTimeValue(hol.CreatedAt),
		})
	}
	return map[string]any{
		"id":         s.ID,
		"name":       s.Name,
		"timezone":   s.Timezone,
		"is_default": s.IsDefault,
		"schedule":   days,
		"holidays":   holidays,
		"created_at": uiTimeValue(s.CreatedAt),
		"updated_at": uiTimeValue(s.UpdatedAt),
	}
}

// ListBusinessHours renders Escalated/Admin/BusinessHours/Index
// (GET /admin/business-hours).
func (h *AdminHandler) ListBusinessHours(w http.ResponseWriter, r *http.Request) {
	repo := h.businessSchedules(w)
	if repo == nil {
		return
	}
	schedules, err := repo.ListBusinessSchedules(r.Context())
	if err != nil {
		http.Error(w, "could not load the schedules", http.StatusInternalServerError)
		return
	}
	rows := make([]map[string]any, 0, len(schedules))
	for _, s := range schedules {
		rows = append(rows, businessScheduleProps(s))
	}
	_ = h.renderer.Render(w, r, "Escalated/Admin/BusinessHours/Index", map[string]any{"schedules": rows})
}

func (h *AdminHandler) renderBusinessHoursForm(w http.ResponseWriter, r *http.Request, s *models.BusinessSchedule, errs map[string]string) {
	props := map[string]any{"timezones": timezoneIdentifiers}
	if s != nil {
		props["schedule"] = businessScheduleProps(s)
	}
	if errs != nil {
		props["errors"] = pageErrors(errs)
	}
	_ = h.renderer.Render(w, r, "Escalated/Admin/BusinessHours/Form", props)
}

// CreateBusinessHoursForm renders the form for a new schedule
// (GET /admin/business-hours/create).
func (h *AdminHandler) CreateBusinessHoursForm(w http.ResponseWriter, r *http.Request) {
	h.renderBusinessHoursForm(w, r, nil, nil)
}

func (h *AdminHandler) businessScheduleFromPath(w http.ResponseWriter, r *http.Request) (store.BusinessSchedules, *models.BusinessSchedule, bool) {
	repo := h.businessSchedules(w)
	if repo == nil {
		return nil, nil, false
	}
	id, err := idFromPath(r)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return nil, nil, false
	}
	s, err := repo.GetBusinessSchedule(r.Context(), id)
	if err != nil {
		http.Error(w, "could not load the schedule", http.StatusInternalServerError)
		return nil, nil, false
	}
	if s == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return nil, nil, false
	}
	return repo, s, true
}

// EditBusinessHoursForm renders the form with the schedule
// (GET /admin/business-hours/{id}/edit).
func (h *AdminHandler) EditBusinessHoursForm(w http.ResponseWriter, r *http.Request) {
	_, s, ok := h.businessScheduleFromPath(w, r)
	if !ok {
		return
	}
	h.renderBusinessHoursForm(w, r, s, nil)
}

// businessHoursInput is what the form posts (Laravel's validation rules:
// name required|max:255, timezone required|timezone, is_default boolean,
// schedule required|array, holidays.*.name required, holidays.*.date date,
// holidays.*.recurring boolean).
type businessHoursInput struct {
	Name      *string         `json:"name"`
	Timezone  *string         `json:"timezone"`
	IsDefault any             `json:"is_default"`
	Schedule  json.RawMessage `json:"schedule"`
	Holidays  []struct {
		Name      *string `json:"name"`
		Date      *string `json:"date"`
		Recurring any     `json:"recurring"`
	} `json:"holidays"`
}

// boolish reads Laravel's boolean rule: true, false, 1, 0, "1", "0".
func boolish(v any) (bool, bool) {
	switch b := v.(type) {
	case nil:
		return false, true
	case bool:
		return b, true
	case float64:
		return b == 1, b == 0 || b == 1
	case string:
		switch b {
		case "1", "true":
			return true, true
		case "0", "false", "":
			return false, true
		}
	}
	return false, false
}

func validClock(s string) bool {
	hh, mm, found := strings.Cut(s, ":")
	if !found || len(hh) != 2 || len(mm) != 2 {
		return false
	}
	hr, err1 := strconv.Atoi(hh)
	mi, err2 := strconv.Atoi(mm)
	return err1 == nil && err2 == nil && hr >= 0 && mi >= 0 && mi < 60 && (hr < 24 || (hr == 24 && mi == 0))
}

func parseHolidayDate(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if len(s) > 10 && s[10] == 'T' {
		s = s[:10]
	}
	t, err := time.Parse("2006-01-02", s)
	return t, err == nil
}

// apply validates the input into s.
func (in businessHoursInput) apply(s *models.BusinessSchedule) map[string]string {
	errs := map[string]string{}
	name := ""
	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
	}
	switch {
	case name == "":
		errs["name"] = requiredMessage("name")
	case len([]rune(name)) > 255:
		errs["name"] = "The name may not be greater than 255 characters."
	}
	s.Name = name

	tz := ""
	if in.Timezone != nil {
		tz = strings.TrimSpace(*in.Timezone)
	}
	switch {
	case tz == "":
		errs["timezone"] = requiredMessage("timezone")
	case !validTimezones[tz]:
		errs["timezone"] = "The timezone must be a valid timezone."
	}
	s.Timezone = tz

	isDefault, ok := boolish(in.IsDefault)
	if !ok {
		errs["is_default"] = "The is default field must be true or false."
	}
	s.IsDefault = isDefault

	var days map[string]models.DaySchedule
	if len(in.Schedule) == 0 || string(in.Schedule) == "null" {
		errs["schedule"] = requiredMessage("schedule")
	} else if err := json.Unmarshal(in.Schedule, &days); err != nil {
		errs["schedule"] = "The schedule must be opening hours for each day."
	} else {
		s.Hours = map[string]models.DaySchedule{}
		for _, day := range scheduleDays {
			hours, present := days[day]
			if !present {
				continue
			}
			hours.Start, hours.End = strings.TrimSpace(hours.Start), strings.TrimSpace(hours.End)
			if (hours.Start != "" && !validClock(hours.Start)) || (hours.End != "" && !validClock(hours.End)) {
				errs["schedule"] = "The " + day + " hours must be times like 09:00."
			}
			s.Hours[day] = hours
		}
	}

	s.Holidays = []models.Holiday{}
	for i, raw := range in.Holidays {
		key := fmt.Sprintf("holidays.%d.", i)
		hol := models.Holiday{}
		if raw.Name != nil {
			hol.Name = strings.TrimSpace(*raw.Name)
		}
		if hol.Name == "" {
			errs[key+"name"] = requiredMessage("holiday name")
		} else if len([]rune(hol.Name)) > 255 {
			errs[key+"name"] = "The holiday name may not be greater than 255 characters."
		}
		date := ""
		if raw.Date != nil {
			date = *raw.Date
		}
		if d, ok := parseHolidayDate(date); ok {
			hol.Date = d
		} else if date == "" {
			errs[key+"date"] = requiredMessage("holiday date")
		} else {
			errs[key+"date"] = "The holiday date is not a valid date."
		}
		recurring, ok := boolish(raw.Recurring)
		if !ok {
			errs[key+"recurring"] = "The recurring field must be true or false."
		}
		hol.IsRecurring = recurring
		s.Holidays = append(s.Holidays, hol)
	}
	return errs
}

func (h *AdminHandler) businessHoursIndexURL() string {
	return h.adminURL("/business-hours")
}

// StoreBusinessHours handles POST /admin/business-hours
// (escalated.admin.business-hours.store).
func (h *AdminHandler) StoreBusinessHours(w http.ResponseWriter, r *http.Request) {
	repo := h.businessSchedules(w)
	if repo == nil {
		return
	}
	var in businessHoursInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid input", http.StatusBadRequest)
		return
	}
	s := &models.BusinessSchedule{IsActive: true}
	if errs := in.apply(s); len(errs) > 0 {
		validationErrors(w, r, errs, func(errs map[string]string) { h.renderBusinessHoursForm(w, r, nil, errs) })
		return
	}
	if err := repo.CreateBusinessSchedule(r.Context(), s); err != nil {
		http.Error(w, "could not save the schedule", http.StatusInternalServerError)
		return
	}
	h.audit(r, "created", "business_schedule", s.ID, nil, s)
	h.pageDone(w, r, h.businessHoursIndexURL(), http.StatusCreated, map[string]any{"schedule": businessScheduleProps(s)})
}

// UpdateBusinessHours handles PUT/PATCH /admin/business-hours/{id}
// (escalated.admin.business-hours.update). The holidays sent replace the
// schedule's holidays.
func (h *AdminHandler) UpdateBusinessHours(w http.ResponseWriter, r *http.Request) {
	repo, s, ok := h.businessScheduleFromPath(w, r)
	if !ok {
		return
	}
	old := *s
	var in businessHoursInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid input", http.StatusBadRequest)
		return
	}
	if errs := in.apply(s); len(errs) > 0 {
		validationErrors(w, r, errs, func(errs map[string]string) { h.renderBusinessHoursForm(w, r, &old, errs) })
		return
	}
	if err := repo.UpdateBusinessSchedule(r.Context(), s); err != nil {
		http.Error(w, "could not save the schedule", http.StatusInternalServerError)
		return
	}
	h.audit(r, "updated", "business_schedule", s.ID, old, s)
	h.pageDone(w, r, h.businessHoursIndexURL(), http.StatusOK, map[string]any{"schedule": businessScheduleProps(s)})
}

// DeleteBusinessHours handles DELETE /admin/business-hours/{id}
// (escalated.admin.business-hours.destroy). Policies and departments that
// named the schedule fall back to the default one.
func (h *AdminHandler) DeleteBusinessHours(w http.ResponseWriter, r *http.Request) {
	repo, s, ok := h.businessScheduleFromPath(w, r)
	if !ok {
		return
	}
	if err := repo.DeleteBusinessSchedule(r.Context(), s.ID); err != nil {
		http.Error(w, "could not delete the schedule", http.StatusInternalServerError)
		return
	}
	h.audit(r, "deleted", "business_schedule", s.ID, s, nil)
	h.pageDone(w, r, h.businessHoursIndexURL(), http.StatusOK, map[string]string{"status": "deleted"})
}

// scheduleReference reads a business_schedule_id sent with an SLA policy or
// department: nil for null, "" or 0, else an existing schedule's id. present
// is false when the key was not sent, which leaves the stored value alone.
func (h *AdminHandler) scheduleReference(ctx context.Context, raw any, present bool) (*int64, string) {
	if !present || raw == nil {
		return nil, ""
	}
	var id int64
	switch v := raw.(type) {
	case float64:
		id = int64(v)
		if float64(id) != v {
			return nil, "The selected business schedule is invalid."
		}
	case string:
		if strings.TrimSpace(v) == "" {
			return nil, ""
		}
		n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err != nil {
			return nil, "The selected business schedule is invalid."
		}
		id = n
	default:
		return nil, "The selected business schedule is invalid."
	}
	if id == 0 {
		return nil, ""
	}
	repo := store.BusinessSchedulesFor(h.store)
	if repo == nil {
		return nil, "The selected business schedule is invalid."
	}
	if s, err := repo.GetBusinessSchedule(ctx, id); err != nil || s == nil {
		return nil, "The selected business schedule is invalid."
	}
	return &id, ""
}

// rawScheduleReference decodes a json.RawMessage business_schedule_id.
func (h *AdminHandler) rawScheduleReference(ctx context.Context, raw json.RawMessage) (*int64, string, bool) {
	if len(raw) == 0 {
		return nil, "", false
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, "The selected business schedule is invalid.", true
	}
	id, msg := h.scheduleReference(ctx, v, true)
	return id, msg, true
}

// addScheduleOptions adds the SLA policy and department forms' `schedules`
// prop (Laravel passes BusinessSchedule::orderBy('name')->get(['id','name'])):
// every schedule as {id, name}, by name, for the shared Business hours
// schedule picker. A store without business schedules sends no prop, so the
// form shows no picker.
func (h *AdminHandler) addScheduleOptions(ctx context.Context, props map[string]any) {
	repo := store.BusinessSchedulesFor(h.store)
	if repo == nil {
		return
	}
	all, err := repo.ListBusinessSchedules(ctx)
	if err != nil {
		return
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].Name < all[j].Name })
	options := make([]map[string]any, 0, len(all))
	for _, s := range all {
		options = append(options, map[string]any{"id": s.ID, "name": s.Name})
	}
	props["schedules"] = options
}
