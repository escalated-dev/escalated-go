package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
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

	// UsersPage renders the users page with an errors prop. A rejected role
	// change answers an Inertia visit with it, as Go keeps no session to
	// flash the errors through a redirect back. Nil redirects back instead.
	UsersPage func(w http.ResponseWriter, r *http.Request, errs map[string]string)
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
	field, err := readRoleIDs(r)
	if err != nil {
		http.Error(w, "invalid input", http.StatusBadRequest)
		return
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
	ids, errs := validateRoleIDs(field, known)
	if len(errs) > 0 {
		validationErrors(w, r, errs, h.usersPage(w, r))
		return
	}
	if current := h.Pages.currentUser(r); current == models.UserID(user) {
		keeps, err := h.Roles.RolesGrant(r.Context(), ids, "role.manage")
		if err != nil {
			http.Error(w, "could not read roles", http.StatusInternalServerError)
			return
		}
		if !keeps {
			validationErrors(w, r, map[string]string{"role_ids": "You cannot remove your own role management."}, h.usersPage(w, r))
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

// usersPage answers a rejected role change on an Inertia visit: the users
// page again with the errors, or back to it when no page is wired.
func (h *RoleHandler) usersPage(w http.ResponseWriter, r *http.Request) func(map[string]string) {
	return func(errs map[string]string) {
		if h.UsersPage != nil {
			h.UsersPage(w, r, errs)
			return
		}
		backTo(w, r, h.Pages.path("/admin/users"))
	}
}

// roleIDsField is the role_ids input as sent: whether it was there, whether
// it was an array, and its items.
type roleIDsField struct {
	present bool
	isArray bool
	items   []any
}

// readRoleIDs reads role_ids keeping what Laravel's rules look at, which the
// flattened readInput loses: null and scalars are not arrays, and each item
// keeps its index. A form post's role_ids / role_ids[] values are an array.
func readRoleIDs(r *http.Request) (roleIDsField, error) {
	ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if ct == "application/x-www-form-urlencoded" || ct == "multipart/form-data" {
		in, err := readInput(r)
		if err != nil {
			return roleIDsField{}, err
		}
		field := roleIDsField{isArray: true}
		for _, key := range []string{"role_ids", "role_ids[]"} {
			values, ok := in[key]
			field.present = field.present || ok
			for _, v := range values {
				field.items = append(field.items, v)
			}
		}
		return field, nil
	}
	if r.Body == nil {
		return roleIDsField{}, nil
	}
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		if errors.Is(err, io.EOF) {
			return roleIDsField{}, nil
		}
		return roleIDsField{}, err
	}
	value, ok := raw["role_ids"]
	if !ok {
		return roleIDsField{}, nil
	}
	dec := json.NewDecoder(strings.NewReader(string(value)))
	dec.UseNumber()
	var decoded any
	if err := dec.Decode(&decoded); err != nil {
		return roleIDsField{}, err
	}
	items, isArray := decoded.([]any)
	return roleIDsField{present: true, isArray: isArray, items: items}, nil
}

// validateRoleIDs applies escalated-laravel's rules for role_ids
// (present|array, each integer|exists) and words and keys the errors as
// Laravel does: role_ids, or role_ids.N for an item. It returns the ids in
// order without repeats.
func validateRoleIDs(field roleIDsField, known map[int64]bool) ([]int64, map[string]string) {
	if !field.present {
		return nil, map[string]string{"role_ids": "The role ids field must be present."}
	}
	if !field.isArray {
		return nil, map[string]string{"role_ids": "The role ids field must be an array."}
	}
	errs := map[string]string{}
	ids := []int64{}
	seen := map[int64]bool{}
	for i, item := range field.items {
		key := "role_ids." + strconv.Itoa(i)
		id, ok := roleIDInteger(item)
		if !ok {
			errs[key] = "The " + key + " field must be an integer."
			continue
		}
		if !known[id] {
			errs[key] = "The selected " + key + " is invalid."
			continue
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids, errs
}

// roleIDInteger is Laravel's integer rule (FILTER_VALIDATE_INT): a whole
// number, or a string of one.
func roleIDInteger(item any) (int64, bool) {
	var s string
	switch v := item.(type) {
	case json.Number:
		s = v.String()
		if f, err := v.Float64(); err == nil && f == float64(int64(f)) && strings.ContainsAny(s, ".eE") {
			return int64(f), true
		}
	case string:
		s = strings.TrimSpace(v)
	default:
		return 0, false
	}
	id, err := strconv.ParseInt(s, 10, 64)
	return id, err == nil
}
