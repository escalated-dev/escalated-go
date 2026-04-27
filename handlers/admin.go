package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
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
	writeJSON(w, http.StatusOK, payload)
}

// UpdatePublicTicketsSettings handles PUT /admin/settings/public-tickets.
// Validates the mode against the known enum (falls back to unassigned
// for unknown values), clears mode-specific fields on switch to
// prevent stale guest_user_id leaking back into prompt_signup
// behavior, and truncates signup URL templates at 500 chars.
func (h *AdminHandler) UpdatePublicTicketsSettings(w http.ResponseWriter, r *http.Request) {
	var in struct {
		GuestPolicyMode              string  `json:"guest_policy_mode"`
		GuestPolicyUserID            *int64  `json:"guest_policy_user_id"`
		GuestPolicySignupURLTemplate *string `json:"guest_policy_signup_url_template"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	mode := in.GuestPolicyMode
	if _, ok := validGuestPolicyModes[mode]; !ok {
		mode = "unassigned"
	}

	ctx := r.Context()
	if err := h.store.SetSetting(ctx, "guest_policy_mode", mode); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var userIDValue string
	if mode == "guest_user" && in.GuestPolicyUserID != nil && *in.GuestPolicyUserID > 0 {
		userIDValue = strconv.FormatInt(*in.GuestPolicyUserID, 10)
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

	h.GetPublicTicketsSettings(w, r)
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
