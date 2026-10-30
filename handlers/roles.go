package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/escalated-dev/escalated-go/access"
	"github.com/escalated-dev/escalated-go/models"
)

// RoleHandler serves Admin > Roles (escalated-laravel Admin\RoleController)
// and the users page's role editor (UserController::updateRoles). The router
// mounts it only behind the roles & permissions switch, which answers 404
// while it is off.
type RoleHandler struct {
	Roles *access.Store
	Pages PageSupport
}

const (
	rolesIndexPage = "Escalated/Admin/Roles/Index"
	rolesFormPage  = "Escalated/Admin/Roles/Form"
)

// Index handles GET /admin/roles.
func (h *RoleHandler) Index(w http.ResponseWriter, r *http.Request) {
	roles, err := h.Roles.Roles(r.Context())
	if err != nil {
		http.Error(w, "could not list roles", http.StatusInternalServerError)
		return
	}
	h.Pages.render(w, r, rolesIndexPage, map[string]any{"roles": roles}, nil)
}

// Create handles GET /admin/roles/create.
func (h *RoleHandler) Create(w http.ResponseWriter, r *http.Request) {
	h.form(w, r, nil, nil)
}

// Edit handles GET /admin/roles/{id}/edit.
func (h *RoleHandler) Edit(w http.ResponseWriter, r *http.Request) {
	role, ok := h.find(w, r)
	if !ok {
		return
	}
	h.form(w, r, role, nil)
}

func (h *RoleHandler) form(w http.ResponseWriter, r *http.Request, role *access.Role, errs map[string]string) {
	all, err := h.Roles.AllPermissions(r.Context())
	if err != nil {
		http.Error(w, "could not list permissions", http.StatusInternalServerError)
		return
	}
	// Laravel's Permission::all()->groupBy('group').
	grouped := map[string][]access.Permission{}
	for _, p := range all {
		grouped[p.Group] = append(grouped[p.Group], p)
	}
	props := map[string]any{"permissions": grouped}
	if role != nil {
		props["role"] = role
	}
	h.Pages.render(w, r, rolesFormPage, props, errs)
}

func (h *RoleHandler) find(w http.ResponseWriter, r *http.Request) (*access.Role, bool) {
	id, err := strconv.ParseInt(pathParam(r, "id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return nil, false
	}
	role, err := h.Roles.Role(r.Context(), id)
	if err != nil {
		http.Error(w, "could not load the role", http.StatusInternalServerError)
		return nil, false
	}
	if role == nil {
		http.NotFound(w, r)
		return nil, false
	}
	return role, true
}

// Store handles POST /admin/roles.
func (h *RoleHandler) Store(w http.ResponseWriter, r *http.Request) {
	h.save(w, r, nil)
}

// Update handles PUT/PATCH /admin/roles/{id}.
func (h *RoleHandler) Update(w http.ResponseWriter, r *http.Request) {
	role, ok := h.find(w, r)
	if !ok {
		return
	}
	h.save(w, r, role)
}

func (h *RoleHandler) save(w http.ResponseWriter, r *http.Request, role *access.Role) {
	in, err := readInput(r)
	if err != nil {
		http.Error(w, "invalid input", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(in.Get("name"))
	var description *string
	if d := strings.TrimSpace(in.Get("description")); d != "" {
		description = &d
	}
	errs := map[string]string{}
	if name == "" {
		errs["name"] = requiredMessage("name")
	} else if len(name) > 255 {
		errs["name"] = "The name may not be greater than 255 characters."
	}
	var ids []int64
	for _, key := range []string{"permissions", "permissions[]"} {
		for _, v := range in[key] {
			if strings.TrimSpace(v) == "" {
				continue
			}
			id, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			if err != nil {
				errs["permissions"] = "The selected permissions are invalid."
				continue
			}
			ids = append(ids, id)
		}
	}
	if len(ids) > 0 && errs["permissions"] == "" {
		known, err := h.Roles.KnownPermissionIDs(r.Context(), ids)
		if err != nil {
			http.Error(w, "could not read permissions", http.StatusInternalServerError)
			return
		}
		for _, id := range ids {
			if !known[id] {
				errs["permissions"] = "The selected permissions are invalid."
			}
		}
	}
	if len(errs) > 0 {
		validationErrors(w, r, errs, func(errs map[string]string) { h.form(w, r, role, errs) })
		return
	}
	var id int64
	if role != nil {
		id = role.ID
	}
	id, err = h.Roles.SaveRole(r.Context(), id, name, description, ids)
	if err != nil {
		http.Error(w, "could not save the role", http.StatusInternalServerError)
		return
	}
	code := http.StatusOK
	if role == nil {
		code = http.StatusCreated
	}
	h.Pages.succeed(w, r, h.Pages.path("/admin/roles"), code, map[string]any{"id": id})
}

// Destroy handles DELETE /admin/roles/{id}. System roles cannot be deleted.
func (h *RoleHandler) Destroy(w http.ResponseWriter, r *http.Request) {
	role, ok := h.find(w, r)
	if !ok {
		return
	}
	if role.IsSystem {
		validationErrors(w, r, map[string]string{"role": "System roles cannot be deleted."}, func(map[string]string) {
			backTo(w, r, h.Pages.path("/admin/roles"))
		})
		return
	}
	if err := h.Roles.DeleteRole(r.Context(), role.ID); err != nil {
		http.Error(w, "could not delete the role", http.StatusInternalServerError)
		return
	}
	h.Pages.succeed(w, r, h.Pages.path("/admin/roles"), http.StatusNoContent, nil)
}

// UpdateUserRoles handles PUT /admin/users/{user}/roles: role_ids replaces
// the user's Escalated roles. You cannot take away your own role management,
// which would leave nobody able to undo it.
func (h *RoleHandler) UpdateUserRoles(w http.ResponseWriter, r *http.Request) {
	user := strings.TrimSpace(pathParam(r, "user"))
	if user == "" {
		http.NotFound(w, r)
		return
	}
	in, err := readInput(r)
	if err != nil {
		http.Error(w, "invalid input", http.StatusBadRequest)
		return
	}
	_, present := in["role_ids"]
	_, presentForm := in["role_ids[]"]
	errs := map[string]string{}
	if !present && !presentForm {
		errs["role_ids"] = "The role ids field must be present."
	}
	roles, err := h.Roles.Roles(r.Context())
	if err != nil {
		http.Error(w, "could not list roles", http.StatusInternalServerError)
		return
	}
	known := map[int64]bool{}
	for _, role := range roles {
		known[role.ID] = true
	}
	var ids []int64
	seen := map[int64]bool{}
	for _, key := range []string{"role_ids", "role_ids[]"} {
		for _, v := range in[key] {
			if strings.TrimSpace(v) == "" {
				continue
			}
			id, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			if err != nil || !known[id] {
				errs["role_ids"] = "The selected roles are invalid."
				continue
			}
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	if len(errs) > 0 {
		validationErrors(w, r, errs, func(map[string]string) { backTo(w, r, h.Pages.path("/admin/users")) })
		return
	}
	if current := h.Pages.currentUser(r); current == models.UserID(user) {
		keeps, err := h.Roles.RolesGrant(r.Context(), ids, "role.manage")
		if err != nil {
			http.Error(w, "could not read roles", http.StatusInternalServerError)
			return
		}
		if !keeps {
			validationErrors(w, r, map[string]string{"role_ids": "You cannot remove your own role management."}, func(map[string]string) {
				backTo(w, r, h.Pages.path("/admin/users"))
			})
			return
		}
	}
	if err := h.Roles.ReplaceUserRoles(r.Context(), models.UserID(user), ids); err != nil {
		http.Error(w, "could not save the roles", http.StatusInternalServerError)
		return
	}
	if isInertia(r) {
		backTo(w, r, h.Pages.path("/admin/users"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"role_ids": ids})
}
