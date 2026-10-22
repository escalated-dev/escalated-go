package handlers

import (
	"database/sql"
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/renderer"
	"github.com/escalated-dev/escalated-go/services"
	"github.com/escalated-dev/escalated-go/store"
)

// AdminHandler serves admin configuration endpoints for departments, tags, and SLA policies.
type AdminHandler struct {
	store    store.Store
	renderer renderer.Renderer

	// Audit records department, tag and SLA policy changes in the audit log
	// (Laravel's Auditable models). The routers set it with UserID.
	Audit  *services.AuditLogService
	UserID func(r *http.Request) models.UserID

	// DB and TablePrefix back the pages' counts (an SLA policy's tickets).
	// Optional; the router sets them.
	DB          *sql.DB
	TablePrefix string

	// RoutePrefix builds the pages' redirects; TargetedSLAs is the SLA form's
	// targeted switch (Config.TargetedSLAs). Set by the router.
	RoutePrefix  string
	TargetedSLAs bool

	// CustomFields backs the custom field and custom object admin. Set by
	// the router.
	CustomFields *services.CustomFieldService
}

func (h *AdminHandler) slaIndexURL() string {
	prefix := h.RoutePrefix
	if prefix == "" {
		prefix = "/escalated"
	}
	return prefix + "/admin/sla-policies"
}

// slaFormProps are the SLA form's options (Laravel SlaPolicyController::
// formOptions): priorities, targeted, departments and ticketTypes.
func (h *AdminHandler) slaFormProps(r *http.Request, policy *models.SLAPolicy) map[string]any {
	props := map[string]any{
		"priorities":  uiPriorities(),
		"targeted":    h.TargetedSLAs,
		"departments": uiDepartments(r.Context(), h.store),
		"ticketTypes": models.TicketTypes,
	}
	if policy != nil {
		props["policy"] = slaPolicyProps(policy, 0)
	}
	return props
}

func (h *AdminHandler) renderSLAForm(w http.ResponseWriter, r *http.Request, policy *models.SLAPolicy, errs map[string]string) {
	props := h.slaFormProps(r, policy)
	if errs != nil {
		props["errors"] = pageErrors(errs)
	}
	_ = h.renderer.Render(w, r, "Escalated/Admin/SlaPolicies/Form", props)
}

// CreateSLAPolicyForm renders Escalated/Admin/SlaPolicies/Form for a new
// policy (GET /admin/sla-policies/create).
func (h *AdminHandler) CreateSLAPolicyForm(w http.ResponseWriter, r *http.Request) {
	h.renderSLAForm(w, r, nil, nil)
}

// EditSLAPolicyForm renders Escalated/Admin/SlaPolicies/Form with the policy
// (GET /admin/sla-policies/{id}/edit).
func (h *AdminHandler) EditSLAPolicyForm(w http.ResponseWriter, r *http.Request) {
	policy, ok := h.slaPolicyFromPath(w, r)
	if !ok {
		return
	}
	h.renderSLAForm(w, r, policy, nil)
}

func (h *AdminHandler) slaPolicyFromPath(w http.ResponseWriter, r *http.Request) (*models.SLAPolicy, bool) {
	id, err := idFromPath(r)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return nil, false
	}
	policy, err := h.store.GetSLAPolicy(r.Context(), id)
	if err != nil {
		http.Error(w, "could not load the policy", http.StatusInternalServerError)
		return nil, false
	}
	if policy == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return nil, false
	}
	return policy, true
}

// slaPolicyInput is what the SLA form and API clients send.
type slaPolicyInput struct {
	Name               *string         `json:"name"`
	Description        *string         `json:"description"`
	IsDefault          *bool           `json:"is_default"`
	IsActive           *bool           `json:"is_active"`
	FirstResponseHours models.JSONText `json:"first_response_hours"`
	ResolutionHours    models.JSONText `json:"resolution_hours"`
	DepartmentID       *int64          `json:"department_id"`
	TicketType         *string         `json:"ticket_type"`
	BusinessHoursOnly  *bool           `json:"business_hours_only"`
	BusinessScheduleID json.RawMessage `json:"business_schedule_id"`
}

// apply copies the fields that were sent onto p.
func (in slaPolicyInput) apply(p *models.SLAPolicy) map[string]string {
	errs := map[string]string{}
	if in.Name != nil {
		p.Name = strings.TrimSpace(*in.Name)
	}
	if p.Name == "" {
		errs["name"] = requiredMessage("name")
	}
	if in.Description != nil {
		desc := strings.TrimSpace(*in.Description)
		p.Description = &desc
		if desc == "" {
			p.Description = nil
		}
	}
	if in.IsDefault != nil {
		p.IsDefault = *in.IsDefault
	}
	if in.IsActive != nil {
		p.IsActive = *in.IsActive
	}
	if in.BusinessHoursOnly != nil {
		p.BusinessHoursOnly = *in.BusinessHoursOnly
	}
	for field, raw := range map[string]models.JSONText{"first_response_hours": in.FirstResponseHours, "resolution_hours": in.ResolutionHours} {
		if len(raw) == 0 || string(raw) == "null" {
			continue
		}
		hours, ok := parseHours(raw)
		if !ok {
			errs[field] = "The " + strings.ReplaceAll(field, "_", " ") + " must be hours for each priority."
			continue
		}
		clean, _ := json.Marshal(hours)
		if field == "first_response_hours" {
			p.FirstResponseHours = clean
		} else {
			p.ResolutionHours = clean
		}
	}
	if in.DepartmentID != nil {
		p.DepartmentID = in.DepartmentID
		if *in.DepartmentID == 0 {
			p.DepartmentID = nil
		}
	}
	if in.TicketType != nil {
		p.TicketType = in.TicketType
		if *in.TicketType == "" {
			p.TicketType = nil
		}
	}
	return errs
}

// parseHours reads {priority: hours}. The form's inputs send numbers as
// strings and leave unset priorities empty; those are dropped.
func parseHours(raw models.JSONText) (map[string]float64, bool) {
	var in map[string]any
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, false
	}
	out := map[string]float64{}
	for priority, v := range in {
		switch val := v.(type) {
		case nil:
		case float64:
			out[priority] = val
		case string:
			if strings.TrimSpace(val) == "" {
				continue
			}
			n, err := strconv.ParseFloat(strings.TrimSpace(val), 64)
			if err != nil || n < 0 {
				return nil, false
			}
			out[priority] = n
		default:
			return nil, false
		}
	}
	return out, true
}

// UpdateSLAPolicy handles PUT/PATCH /admin/sla-policies/{id}
// (escalated.admin.sla-policies.update).
func (h *AdminHandler) UpdateSLAPolicy(w http.ResponseWriter, r *http.Request) {
	policy, ok := h.slaPolicyFromPath(w, r)
	if !ok {
		return
	}
	old := *policy
	var in slaPolicyInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid input", http.StatusBadRequest)
		return
	}
	errs := in.apply(policy)
	h.applyPolicySchedule(r, in, policy, errs)
	if len(errs) > 0 {
		if isInertia(r) {
			h.renderSLAForm(w, r, &old, errs)
			return
		}
		validationErrors(w, r, errs, nil)
		return
	}
	if err := h.store.UpdateSLAPolicy(r.Context(), policy); err != nil {
		http.Error(w, "could not update the policy", http.StatusInternalServerError)
		return
	}
	h.audit(r, "updated", "sla_policy", policy.ID, old, policy)
	if isInertia(r) {
		seeOther(w, r, h.slaIndexURL())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sla_policy": policy})
}

// applyPolicySchedule sets the policy's business_schedule_id when it was sent.
func (h *AdminHandler) applyPolicySchedule(r *http.Request, in slaPolicyInput, p *models.SLAPolicy, errs map[string]string) {
	id, msg, present := h.rawScheduleReference(r.Context(), in.BusinessScheduleID)
	switch {
	case !present:
	case msg != "":
		errs["business_schedule_id"] = msg
	default:
		p.BusinessScheduleID = id
	}
}

// slaPolicyProps is an SLA policy as the admin pages read it, with Laravel's
// tickets_count.
func slaPolicyProps(p *models.SLAPolicy, ticketsCount int) map[string]any {
	firstResponse, _ := uiJSON(p.FirstResponseHours).(map[string]any)
	resolution, _ := uiJSON(p.ResolutionHours).(map[string]any)
	if firstResponse == nil {
		firstResponse = map[string]any{}
	}
	if resolution == nil {
		resolution = map[string]any{}
	}
	return map[string]any{
		"id":                   p.ID,
		"name":                 p.Name,
		"description":          p.Description,
		"is_default":           p.IsDefault,
		"is_active":            p.IsActive,
		"business_hours_only":  p.BusinessHoursOnly,
		"business_schedule_id": p.BusinessScheduleID,
		"first_response_hours": firstResponse,
		"resolution_hours":     resolution,
		"department_id":        p.DepartmentID,
		"ticket_type":          p.TicketType,
		"tickets_count":        ticketsCount,
		"created_at":           uiTimeValue(p.CreatedAt),
		"updated_at":           uiTimeValue(p.UpdatedAt),
	}
}

// audit records one admin change; failures never fail the request.
func (h *AdminHandler) audit(r *http.Request, action, entityType string, id int64, oldValues, newValues any) {
	if h.Audit == nil {
		return
	}
	var uid models.UserID
	if h.UserID != nil {
		uid = h.UserID(r)
	}
	ip := r.RemoteAddr
	if host, _, err := net.SplitHostPort(ip); err == nil {
		ip = host
	}
	_ = h.Audit.Record(action, entityType, id, uid, ip, r.UserAgent(), oldValues, newValues)
}

// NewAdminHandler creates a new AdminHandler.
func NewAdminHandler(s store.Store, rend renderer.Renderer) *AdminHandler {
	return &AdminHandler{store: s, renderer: rend}
}

// --- Departments ---

// ListDepartments renders the departments admin page.
func (h *AdminHandler) ListDepartments(w http.ResponseWriter, r *http.Request) {
	depts, err := h.store.ListDepartments(r.Context(), false)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	counts := uiQueries{DB: h.DB, Prefix: h.TablePrefix}.groupCounts(r.Context(), "tickets", "department_id")
	rows := make([]map[string]any, 0, len(depts))
	for _, d := range depts {
		rows = append(rows, departmentProps(d, counts[d.ID]))
	}
	_ = h.renderer.Render(w, r, "Escalated/Admin/Departments/Index", map[string]any{
		"departments": rows,
	})
}

// departmentProps is a department as the admin pages read it, with Laravel's
// tickets_count. Departments here have no agent membership, so agents_count
// is null.
func departmentProps(d *models.Department, tickets int) map[string]any {
	return map[string]any{
		"id":                   d.ID,
		"name":                 d.Name,
		"slug":                 d.Slug,
		"description":          d.Description,
		"email":                d.Email,
		"is_active":            d.IsActive,
		"business_schedule_id": d.BusinessScheduleID,
		"tickets_count":        tickets,
		"agents_count":         nil,
		"created_at":           uiTimeValue(d.CreatedAt),
		"updated_at":           uiTimeValue(d.UpdatedAt),
	}
}

func (h *AdminHandler) adminURL(suffix string) string {
	prefix := h.RoutePrefix
	if prefix == "" {
		prefix = "/escalated"
	}
	return prefix + "/admin" + suffix
}

// pageDone ends a successful admin write: a 303 to location for a page
// visit, the JSON body otherwise.
func (h *AdminHandler) pageDone(w http.ResponseWriter, r *http.Request, location string, code int, body any) {
	if isInertia(r) {
		seeOther(w, r, location)
		return
	}
	writeJSON(w, code, body)
}

func (h *AdminHandler) renderDepartmentForm(w http.ResponseWriter, r *http.Request, d *models.Department, errs map[string]string) {
	props := map[string]any{"department": nil}
	if d != nil {
		props["department"] = departmentProps(d, 0)
	}
	if errs != nil {
		props["errors"] = pageErrors(errs)
	}
	_ = h.renderer.Render(w, r, "Escalated/Admin/Departments/Form", props)
}

// CreateDepartmentForm renders Escalated/Admin/Departments/Form for a new
// department (GET /admin/departments/create).
func (h *AdminHandler) CreateDepartmentForm(w http.ResponseWriter, r *http.Request) {
	h.renderDepartmentForm(w, r, nil, nil)
}

// EditDepartmentForm renders the form with the department
// (GET /admin/departments/{id}/edit).
func (h *AdminHandler) EditDepartmentForm(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	d, err := h.store.GetDepartment(r.Context(), id)
	if err != nil || d == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	h.renderDepartmentForm(w, r, d, nil)
}

// CreateDepartment handles POST /admin/departments
func (h *AdminHandler) CreateDepartment(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name        string  `json:"name"`
		Slug        string  `json:"slug"`
		Description *string `json:"description"`
		Email       *string `json:"email"`
		IsActive    *bool   `json:"is_active"`

		BusinessScheduleID json.RawMessage `json:"business_schedule_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || strings.TrimSpace(in.Name) == "" {
		if isInertia(r) {
			h.renderDepartmentForm(w, r, nil, map[string]string{"name": requiredMessage("name")})
			return
		}
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	d := &models.Department{
		Name:        in.Name,
		Slug:        slugify(in.Name),
		Description: in.Description,
		Email:       in.Email,
		IsActive:    true,
	}
	if s := slugify(in.Slug); s != "" {
		d.Slug = s
	}
	if in.IsActive != nil {
		d.IsActive = *in.IsActive
	}
	if id, msg, present := h.rawScheduleReference(r.Context(), in.BusinessScheduleID); present {
		if msg != "" {
			validationErrors(w, r, map[string]string{"business_schedule_id": msg}, func(errs map[string]string) { h.renderDepartmentForm(w, r, nil, errs) })
			return
		}
		d.BusinessScheduleID = id
	}

	if err := h.store.CreateDepartment(r.Context(), d); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.audit(r, "created", "department", d.ID, nil, d)

	h.pageDone(w, r, h.adminURL("/departments"), http.StatusCreated, map[string]any{"department": d})
}

// UpdateDepartment handles PATCH /admin/departments/{id}
func (h *AdminHandler) UpdateDepartment(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	d, err := h.store.GetDepartment(r.Context(), id)
	if err != nil || d == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	var updates map[string]any
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		http.Error(w, "invalid input", http.StatusBadRequest)
		return
	}

	oldValues, newValues := map[string]any{}, map[string]any{}
	if v, ok := updates["name"].(string); ok && v != d.Name {
		oldValues["name"], newValues["name"] = d.Name, v
		d.Name = v
		d.Slug = slugify(v)
	}
	if v, ok := updates["name"].(string); ok && strings.TrimSpace(v) == "" {
		if isInertia(r) {
			h.renderDepartmentForm(w, r, d, map[string]string{"name": requiredMessage("name")})
			return
		}
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	if v, ok := updates["slug"].(string); ok && slugify(v) != "" && slugify(v) != d.Slug {
		oldValues["slug"], newValues["slug"] = d.Slug, slugify(v)
		d.Slug = slugify(v)
	}
	if v, ok := updates["description"].(string); ok {
		current := ""
		if d.Description != nil {
			current = *d.Description
		}
		if v != current {
			oldValues["description"], newValues["description"] = current, v
			desc := v
			d.Description = &desc
		}
	}
	if v, ok := updates["is_active"].(bool); ok && v != d.IsActive {
		oldValues["is_active"], newValues["is_active"] = d.IsActive, v
		d.IsActive = v
	}
	if raw, present := updates["business_schedule_id"]; present {
		id, msg := h.scheduleReference(r.Context(), raw, true)
		if msg != "" {
			validationErrors(w, r, map[string]string{"business_schedule_id": msg}, func(errs map[string]string) { h.renderDepartmentForm(w, r, d, errs) })
			return
		}
		oldValues["business_schedule_id"], newValues["business_schedule_id"] = d.BusinessScheduleID, id
		d.BusinessScheduleID = id
	}

	if err := h.store.UpdateDepartment(r.Context(), d); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if len(newValues) > 0 {
		h.audit(r, "updated", "department", d.ID, oldValues, newValues)
	}

	h.pageDone(w, r, h.adminURL("/departments"), http.StatusOK, map[string]any{"department": d})
}

// DeleteDepartment handles DELETE /admin/departments/{id}
func (h *AdminHandler) DeleteDepartment(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	old, _ := h.store.GetDepartment(r.Context(), id)
	if err := h.store.DeleteDepartment(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if old != nil {
		h.audit(r, "deleted", "department", id, old, nil)
	}
	h.pageDone(w, r, h.adminURL("/departments"), http.StatusOK, map[string]string{"status": "deleted"})
}

// --- Tags ---

// ListTags renders the tags admin page.
func (h *AdminHandler) ListTags(w http.ResponseWriter, r *http.Request) {
	tags, err := h.store.ListTags(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	counts := uiQueries{DB: h.DB, Prefix: h.TablePrefix}.groupCounts(r.Context(), "ticket_tags", "tag_id")
	rows := make([]map[string]any, 0, len(tags))
	for _, t := range tags {
		rows = append(rows, map[string]any{
			"id": t.ID, "name": t.Name, "slug": t.Slug, "color": t.Color, "description": t.Description,
			"tickets_count": counts[t.ID], "created_at": uiTimeValue(t.CreatedAt), "updated_at": uiTimeValue(t.UpdatedAt),
		})
	}
	_ = h.renderer.Render(w, r, "Escalated/Admin/Tags/Index", map[string]any{
		"tags": rows,
	})
}

// renderTagsWithErrors shows the tags page again with errors (the page's
// form lives on the list).
func (h *AdminHandler) renderTagsWithErrors(w http.ResponseWriter, r *http.Request, errs map[string]string) {
	tags, _ := h.store.ListTags(r.Context())
	rows := []map[string]any{}
	for _, t := range tags {
		rows = append(rows, map[string]any{"id": t.ID, "name": t.Name, "slug": t.Slug, "color": t.Color})
	}
	_ = h.renderer.Render(w, r, "Escalated/Admin/Tags/Index", map[string]any{"tags": rows, "errors": pageErrors(errs)})
}

// UpdateTag handles PUT/PATCH /admin/tags/{id} (escalated.admin.tags.update):
// name and color.
func (h *AdminHandler) UpdateTag(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	t, err := h.store.GetTag(r.Context(), id)
	if err != nil || t == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	var in struct {
		Name  *string `json:"name"`
		Color *string `json:"color"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid input", http.StatusBadRequest)
		return
	}
	old := *t
	if in.Name != nil {
		if strings.TrimSpace(*in.Name) == "" {
			if isInertia(r) {
				h.renderTagsWithErrors(w, r, map[string]string{"name": requiredMessage("name")})
				return
			}
			http.Error(w, "name is required", http.StatusBadRequest)
			return
		}
		t.Name = *in.Name
		t.Slug = slugify(*in.Name)
	}
	if in.Color != nil {
		t.Color = in.Color
	}
	if err := h.store.UpdateTag(r.Context(), t); err != nil {
		http.Error(w, "could not update the tag", http.StatusInternalServerError)
		return
	}
	h.audit(r, "updated", "tag", t.ID, old, t)
	h.pageDone(w, r, h.adminURL("/tags"), http.StatusOK, map[string]any{"tag": t})
}

// CreateTag handles POST /admin/tags
func (h *AdminHandler) CreateTag(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name  string  `json:"name"`
		Color *string `json:"color"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || strings.TrimSpace(in.Name) == "" {
		if isInertia(r) {
			h.renderTagsWithErrors(w, r, map[string]string{"name": requiredMessage("name")})
			return
		}
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	t := &models.Tag{
		Name:  in.Name,
		Slug:  slugify(in.Name),
		Color: in.Color,
	}

	if err := h.store.CreateTag(r.Context(), t); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.audit(r, "created", "tag", t.ID, nil, t)

	h.pageDone(w, r, h.adminURL("/tags"), http.StatusCreated, map[string]any{"tag": t})
}

// DeleteTag handles DELETE /admin/tags/{id}
func (h *AdminHandler) DeleteTag(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	old, _ := h.store.GetTag(r.Context(), id)
	if err := h.store.DeleteTag(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if old != nil {
		h.audit(r, "deleted", "tag", id, old, nil)
	}
	h.pageDone(w, r, h.adminURL("/tags"), http.StatusOK, map[string]string{"status": "deleted"})
}

// --- SLA Policies ---

// ListSLAPolicies renders the SLA policies admin page.
func (h *AdminHandler) ListSLAPolicies(w http.ResponseWriter, r *http.Request) {
	policies, err := h.store.ListSLAPolicies(r.Context(), false)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	counts := uiQueries{DB: h.DB, Prefix: h.TablePrefix}.slaTicketCounts(r.Context())
	rows := make([]map[string]any, 0, len(policies))
	for _, p := range policies {
		rows = append(rows, slaPolicyProps(p, counts[p.ID]))
	}
	_ = h.renderer.Render(w, r, "Escalated/Admin/SlaPolicies/Index", map[string]any{
		"policies": rows,
	})
}

// CreateSLAPolicy handles POST /admin/sla-policies
// (escalated.admin.sla-policies.store). A new policy is active unless
// is_active is false.
func (h *AdminHandler) CreateSLAPolicy(w http.ResponseWriter, r *http.Request) {
	var raw slaPolicyInput
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	in := models.SLAPolicy{IsActive: true, FirstResponseHours: models.JSONText(`{}`), ResolutionHours: models.JSONText(`{}`)}
	errs := raw.apply(&in)
	h.applyPolicySchedule(r, raw, &in, errs)
	if len(errs) > 0 {
		if isInertia(r) {
			h.renderSLAForm(w, r, nil, errs)
			return
		}
		if _, missing := errs["name"]; missing {
			http.Error(w, "name is required", http.StatusBadRequest)
			return
		}
		validationErrors(w, r, errs, nil)
		return
	}

	if err := h.store.CreateSLAPolicy(r.Context(), &in); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.audit(r, "created", "sla_policy", in.ID, nil, in)

	if isInertia(r) {
		seeOther(w, r, h.slaIndexURL())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"sla_policy": in})
}

// DeleteSLAPolicy handles DELETE /admin/sla-policies/{id}
func (h *AdminHandler) DeleteSLAPolicy(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	old, _ := h.store.GetSLAPolicy(r.Context(), id)
	if err := h.store.DeleteSLAPolicy(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if old != nil {
		h.audit(r, "deleted", "sla_policy", id, old, nil)
	}
	if isInertia(r) {
		seeOther(w, r, h.slaIndexURL())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// --- Public-ticket guest policy settings ---
//
// Three keys back the policy that decides the identity a public
// submission is attributed to:
//   - guest_policy_mode              ∈ { unassigned, guest_user, prompt_signup }
//   - guest_policy_user_id           required when mode = guest_user (as decimal string)
//   - guest_policy_signup_url_template  optional when mode = prompt_signup
// Consumers read via store.Store.GetSetting so admins can switch modes
// at runtime without a redeploy. Mirrors the .NET
// AdminSettingsController.GetPublicTicketsSettings + UpdatePublicTicketsSettings.

var validGuestPolicyModes = map[string]struct{}{
	"unassigned":    {},
	"guest_user":    {},
	"prompt_signup": {},
}

// GetPublicTicketsSettings handles GET /admin/settings/public-tickets
// and returns the three guest-policy fields as JSON. Missing keys fall
// back to the shipped defaults (unassigned / no user / empty template).
func (h *AdminHandler) GetPublicTicketsSettings(w http.ResponseWriter, r *http.Request) {
	h.publicTicketsSettings(w, r, PageSupport{Renderer: h.renderer}.wantsPage(r), nil)
}

// publicTicketsSettings answers the settings as JSON, or renders
// Escalated/Admin/Settings/PublicTickets with them (and errs).
func (h *AdminHandler) publicTicketsSettings(w http.ResponseWriter, r *http.Request, page bool, errs map[string]string) {
	ctx := r.Context()
	mode, err := h.store.GetSetting(ctx, "guest_policy_mode")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if mode == "" {
		mode = "unassigned"
	}
	userIDRaw, err := h.store.GetSetting(ctx, "guest_policy_user_id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	template, err := h.store.GetSetting(ctx, "guest_policy_signup_url_template")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	payload := map[string]any{
		"guest_policy_mode":                mode,
		"guest_policy_user_id":             parseOptionalPositiveInt(userIDRaw),
		"guest_policy_signup_url_template": template,
	}
	if page {
		props := map[string]any{"settings": payload}
		if errs != nil {
			props["errors"] = pageErrors(errs)
		}
		_ = h.renderer.Render(w, r, "Escalated/Admin/Settings/PublicTickets", props)
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

// UpdatePublicTicketsSettings handles PUT /admin/settings/public-tickets.
// Validates the mode against the known enum (falls back to unassigned
// for unknown values), clears mode-specific fields on switch to
// prevent stale guest_user_id leaking back into prompt_signup
// behavior, and truncates signup URL templates at 500 chars.
func (h *AdminHandler) UpdatePublicTicketsSettings(w http.ResponseWriter, r *http.Request) {
	var in struct {
		GuestPolicyMode              string         `json:"guest_policy_mode"`
		GuestPolicyUserID            *models.UserID `json:"guest_policy_user_id"`
		GuestPolicySignupURLTemplate *string        `json:"guest_policy_signup_url_template"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	mode := in.GuestPolicyMode
	if isInertia(r) {
		// The page gets Laravel's validation instead of the API's fallbacks.
		errs := map[string]string{}
		if _, ok := validGuestPolicyModes[mode]; !ok {
			errs["guest_policy_mode"] = "The selected guest policy mode is invalid."
		}
		if mode == "guest_user" && (in.GuestPolicyUserID == nil || in.GuestPolicyUserID.Empty()) {
			errs["guest_policy_user_id"] = "Choose the user guest tickets are filed under."
		}
		if len(errs) > 0 {
			h.publicTicketsSettings(w, r, true, errs)
			return
		}
	}
	if _, ok := validGuestPolicyModes[mode]; !ok {
		mode = "unassigned"
	}

	ctx := r.Context()
	if err := h.store.SetSetting(ctx, "guest_policy_mode", mode); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var userIDValue string
	if mode == "guest_user" && in.GuestPolicyUserID != nil && !in.GuestPolicyUserID.Empty() {
		userIDValue = string(*in.GuestPolicyUserID)
	}
	if err := h.store.SetSetting(ctx, "guest_policy_user_id", userIDValue); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var template string
	if mode == "prompt_signup" && in.GuestPolicySignupURLTemplate != nil {
		template = strings.TrimSpace(*in.GuestPolicySignupURLTemplate)
		if len(template) > 500 {
			template = template[:500]
		}
	}
	if err := h.store.SetSetting(ctx, "guest_policy_signup_url_template", template); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if isInertia(r) {
		backTo(w, r, h.adminURL("/settings/public-tickets"))
		return
	}
	h.publicTicketsSettings(w, r, false, nil)
}

// --- Widget embedding settings ---

// GetWidgetSettings handles GET /admin/settings/widget and returns the sites
// admins allowed to embed the widget, as stored (one origin per line or
// comma separated). They are added to WidgetConfig.AllowedOrigins.
func (h *AdminHandler) GetWidgetSettings(w http.ResponseWriter, r *http.Request) {
	origins, err := h.store.GetSetting(r.Context(), WidgetAllowedOriginsSetting)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{WidgetAllowedOriginsSetting: origins})
}

// UpdateWidgetSettings handles PUT /admin/settings/widget. Every entry of
// widget_allowed_origins must be an http(s) origin ("https://shop.example.com")
// or subdomain pattern ("https://*.example.com"); otherwise nothing is saved
// and it answers 422 naming the invalid entries. An empty value clears the list.
func (h *AdminHandler) UpdateWidgetSettings(w http.ResponseWriter, r *http.Request) {
	var in struct {
		AllowedOrigins *string `json:"widget_allowed_origins"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if in.AllowedOrigins != nil {
		value := strings.TrimSpace(*in.AllowedOrigins)
		if _, invalid := ParseWidgetOrigins(value); len(invalid) > 0 {
			message := "Each allowed origin must look like https://shop.example.com or https://*.example.com (no path, no bare *). Invalid: " + strings.Join(invalid, ", ")
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"error":  message,
				"errors": map[string][]string{WidgetAllowedOriginsSetting: {message}},
			})
			return
		}
		if err := h.store.SetSetting(r.Context(), WidgetAllowedOriginsSetting, value); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	h.GetWidgetSettings(w, r)
}

// parseOptionalPositiveInt returns *int64 when s is a positive integer,
// nil otherwise. Used to surface empty-string / zero / invalid stored
// values as JSON null so the Vue page can bind against a nullable.
func parseOptionalPositiveInt(s string) *int64 {
	if s == "" {
		return nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		return nil
	}
	return &n
}

// --- helpers ---

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, " ", "-")
	// Remove non-alphanumeric except hyphens
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
