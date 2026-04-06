package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/renderer"
	"github.com/escalated-dev/escalated-go/store"
)

// AdminHandler serves admin configuration endpoints for departments, tags, and SLA policies.
type AdminHandler struct {
	store    store.Store
	renderer renderer.Renderer
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
	_ = h.renderer.Render(w, r, "Admin/Departments/Index", map[string]any{
		"departments": depts,
	})
}

// CreateDepartment handles POST /admin/departments
func (h *AdminHandler) CreateDepartment(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name        string  `json:"name"`
		Description *string `json:"description"`
		Email       *string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Name == "" {
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

	if err := h.store.CreateDepartment(r.Context(), d); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"department": d})
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

	if v, ok := updates["name"].(string); ok {
		d.Name = v
		d.Slug = slugify(v)
	}
	if v, ok := updates["is_active"].(bool); ok {
		d.IsActive = v
	}

	if err := h.store.UpdateDepartment(r.Context(), d); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"department": d})
}

// DeleteDepartment handles DELETE /admin/departments/{id}
func (h *AdminHandler) DeleteDepartment(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := h.store.DeleteDepartment(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// --- Tags ---

// ListTags renders the tags admin page.
func (h *AdminHandler) ListTags(w http.ResponseWriter, r *http.Request) {
	tags, err := h.store.ListTags(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = h.renderer.Render(w, r, "Admin/Tags/Index", map[string]any{
		"tags": tags,
	})
}

// CreateTag handles POST /admin/tags
func (h *AdminHandler) CreateTag(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name  string  `json:"name"`
		Color *string `json:"color"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Name == "" {
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

	writeJSON(w, http.StatusCreated, map[string]any{"tag": t})
}

// DeleteTag handles DELETE /admin/tags/{id}
func (h *AdminHandler) DeleteTag(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := h.store.DeleteTag(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// --- SLA Policies ---

// ListSLAPolicies renders the SLA policies admin page.
func (h *AdminHandler) ListSLAPolicies(w http.ResponseWriter, r *http.Request) {
	policies, err := h.store.ListSLAPolicies(r.Context(), false)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = h.renderer.Render(w, r, "Admin/SLAPolicies/Index", map[string]any{
		"sla_policies": policies,
	})
}

// CreateSLAPolicy handles POST /admin/sla-policies
func (h *AdminHandler) CreateSLAPolicy(w http.ResponseWriter, r *http.Request) {
	var in models.SLAPolicy
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	in.IsActive = true

	if err := h.store.CreateSLAPolicy(r.Context(), &in); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
	if err := h.store.DeleteSLAPolicy(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
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
