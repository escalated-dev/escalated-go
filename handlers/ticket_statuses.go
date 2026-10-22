package handlers

import (
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/services"
)

// Custom ticket statuses admin (Laravel Admin\StatusController): the
// Escalated/Admin/Statuses/{Index,Form} pages and their writes at
// /admin/statuses (escalated.admin.statuses.*).

func (h *AdminHandler) ticketStatuses(w http.ResponseWriter) *services.TicketStatusService {
	if h.Statuses == nil || h.Statuses.DB == nil {
		http.Error(w, "ticket statuses are not configured", http.StatusNotFound)
		return nil
	}
	return h.Statuses
}

// ticketStatusProps is a status with Laravel's column names, plus is_system.
func ticketStatusProps(st *models.TicketStatus) map[string]any {
	return map[string]any{
		"id":          st.ID,
		"label":       st.Label,
		"slug":        st.Slug,
		"category":    st.Category,
		"color":       st.Color,
		"description": st.Description,
		"position":    st.Position,
		"is_default":  st.IsDefault,
		"is_system":   st.IsSystem,
		"created_at":  uiTimeValue(st.CreatedAt),
		"updated_at":  uiTimeValue(st.UpdatedAt),
	}
}

func (h *AdminHandler) statusesIndexURL() string { return h.adminURL("/statuses") }

func (h *AdminHandler) renderStatusesIndex(w http.ResponseWriter, r *http.Request, svc *services.TicketStatusService, errs map[string]string) {
	ctx := r.Context()
	if err := svc.EnsureSystem(ctx); err != nil {
		http.Error(w, "could not load the statuses", http.StatusInternalServerError)
		return
	}
	statuses, err := svc.List(ctx)
	if err != nil {
		http.Error(w, "could not load the statuses", http.StatusInternalServerError)
		return
	}
	rows := make([]map[string]any, 0, len(statuses))
	for _, st := range statuses {
		rows = append(rows, ticketStatusProps(st))
	}
	props := map[string]any{"statuses": rows, "categories": models.StatusCategories}
	if errs != nil {
		props["errors"] = pageErrors(errs)
	}
	_ = h.renderer.Render(w, r, "Escalated/Admin/Statuses/Index", props)
}

// ListStatuses renders Escalated/Admin/Statuses/Index (GET /admin/statuses):
// every status, system rows included, by category then position.
func (h *AdminHandler) ListStatuses(w http.ResponseWriter, r *http.Request) {
	svc := h.ticketStatuses(w)
	if svc == nil {
		return
	}
	h.renderStatusesIndex(w, r, svc, nil)
}

func (h *AdminHandler) renderStatusForm(w http.ResponseWriter, r *http.Request, st *models.TicketStatus, errs map[string]string) {
	props := map[string]any{"categories": models.StatusCategories}
	if st != nil {
		props["status"] = ticketStatusProps(st)
	}
	if errs != nil {
		props["errors"] = pageErrors(errs)
	}
	_ = h.renderer.Render(w, r, "Escalated/Admin/Statuses/Form", props)
}

// CreateStatusForm renders the form for a new status
// (GET /admin/statuses/create).
func (h *AdminHandler) CreateStatusForm(w http.ResponseWriter, r *http.Request) {
	if h.ticketStatuses(w) == nil {
		return
	}
	h.renderStatusForm(w, r, nil, nil)
}

func (h *AdminHandler) statusFromPath(w http.ResponseWriter, r *http.Request) (*services.TicketStatusService, *models.TicketStatus, bool) {
	svc := h.ticketStatuses(w)
	if svc == nil {
		return nil, nil, false
	}
	id, err := idFromPathName(r, "id")
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return nil, nil, false
	}
	st, err := svc.Get(r.Context(), id)
	if err != nil {
		http.Error(w, "could not load the status", http.StatusInternalServerError)
		return nil, nil, false
	}
	if st == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return nil, nil, false
	}
	return svc, st, true
}

// EditStatusForm renders the form with the status
// (GET /admin/statuses/{id}/edit).
func (h *AdminHandler) EditStatusForm(w http.ResponseWriter, r *http.Request) {
	_, st, ok := h.statusFromPath(w, r)
	if !ok {
		return
	}
	h.renderStatusForm(w, r, st, nil)
}

// applyStatusInput validates Laravel's rules into st: label
// required|string|max:255, category required|in the five, color
// required|string|max:20, description nullable|string, position integer,
// is_default boolean. A system row's category cannot change.
func applyStatusInput(in map[string]any, st *models.TicketStatus) map[string]string {
	errs := map[string]string{}
	str := func(key string) (string, bool) {
		v, ok := in[key]
		if !ok || v == nil {
			return "", ok
		}
		s, isString := v.(string)
		if !isString {
			errs[key] = "The " + key + " must be a string."
		}
		return strings.TrimSpace(s), true
	}

	label, _ := str("label")
	switch {
	case errs["label"] != "":
	case label == "":
		errs["label"] = requiredMessage("label")
	case utf8.RuneCountInString(label) > 255:
		errs["label"] = "The label may not be greater than 255 characters."
	}
	st.Label = label

	category, _ := str("category")
	switch {
	case errs["category"] != "":
	case category == "":
		errs["category"] = requiredMessage("category")
	case !models.IsStatusCategory(category):
		errs["category"] = "The selected category is invalid."
	case st.IsSystem && category != st.Category:
		errs["category"] = "The category of a system status cannot be changed."
	default:
		st.Category = category
	}

	color, _ := str("color")
	switch {
	case errs["color"] != "":
	case color == "":
		errs["color"] = requiredMessage("color")
	case utf8.RuneCountInString(color) > 20:
		errs["color"] = "The color may not be greater than 20 characters."
	}
	st.Color = color

	if v, ok := str("description"); ok && errs["description"] == "" {
		if v == "" {
			st.Description = nil
		} else {
			st.Description = &v
		}
	}

	if raw, ok := in["position"]; ok && raw != nil {
		n, isNum := raw.(float64)
		if !isNum || n != float64(int(n)) {
			errs["position"] = "The position must be an integer."
		} else {
			st.Position = int(n)
		}
	}

	if raw, ok := in["is_default"]; ok {
		b, valid := boolish(raw)
		if !valid {
			errs["is_default"] = "The is default field must be true or false."
		}
		st.IsDefault = b
	}
	return errs
}

// StoreStatus handles POST /admin/statuses (escalated.admin.statuses.store).
func (h *AdminHandler) StoreStatus(w http.ResponseWriter, r *http.Request) {
	svc := h.ticketStatuses(w)
	if svc == nil {
		return
	}
	in, ok := decodeObject(r)
	if !ok {
		http.Error(w, "invalid input", http.StatusBadRequest)
		return
	}
	st := &models.TicketStatus{}
	if errs := applyStatusInput(in, st); len(errs) > 0 {
		validationErrors(w, r, errs, func(errs map[string]string) { h.renderStatusForm(w, r, nil, errs) })
		return
	}
	if err := svc.Create(r.Context(), st); err != nil {
		http.Error(w, "could not save the status", http.StatusInternalServerError)
		return
	}
	h.audit(r, "created", "ticket_status", st.ID, nil, ticketStatusProps(st))
	h.pageDone(w, r, h.statusesIndexURL(), http.StatusCreated, map[string]any{"message": "Status created.", "status": ticketStatusProps(st)})
}

// UpdateStatus handles PUT/PATCH /admin/statuses/{id}
// (escalated.admin.statuses.update). The slug never changes; a system row
// keeps its category. Changing a custom status's category leaves the tickets
// already in it where they are.
func (h *AdminHandler) UpdateStatus(w http.ResponseWriter, r *http.Request) {
	svc, st, ok := h.statusFromPath(w, r)
	if !ok {
		return
	}
	old := *st
	in, ok := decodeObject(r)
	if !ok {
		http.Error(w, "invalid input", http.StatusBadRequest)
		return
	}
	if errs := applyStatusInput(in, st); len(errs) > 0 {
		validationErrors(w, r, errs, func(errs map[string]string) { h.renderStatusForm(w, r, &old, errs) })
		return
	}
	if err := svc.Update(r.Context(), st); err != nil {
		http.Error(w, "could not save the status", http.StatusInternalServerError)
		return
	}
	h.audit(r, "updated", "ticket_status", st.ID, ticketStatusProps(&old), ticketStatusProps(st))
	h.pageDone(w, r, h.statusesIndexURL(), http.StatusOK, map[string]any{"message": "Status updated.", "status": ticketStatusProps(st)})
}

// DeleteStatus handles DELETE /admin/statuses/{id}
// (escalated.admin.statuses.destroy). Tickets in the status keep their
// built-in status. A system row cannot be deleted: the index again with an
// error for a page visit, 422 otherwise.
func (h *AdminHandler) DeleteStatus(w http.ResponseWriter, r *http.Request) {
	svc, st, ok := h.statusFromPath(w, r)
	if !ok {
		return
	}
	if err := svc.Delete(r.Context(), st); err != nil {
		if errors.Is(err, services.ErrSystemStatus) {
			validationErrors(w, r, map[string]string{"status": "System statuses cannot be deleted."},
				func(errs map[string]string) { h.renderStatusesIndex(w, r, svc, errs) })
			return
		}
		http.Error(w, "could not delete the status", http.StatusInternalServerError)
		return
	}
	h.audit(r, "deleted", "ticket_status", st.ID, ticketStatusProps(st), nil)
	h.pageDone(w, r, h.statusesIndexURL(), http.StatusOK, map[string]any{"message": "Status deleted."})
}
