package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/escalated-dev/escalated-go/renderer"
)

// AdminUser is the trimmed projection of a host user surfaced to the
// admin users-management page. Mirrors the Laravel UserController which
// exposes id/name/email plus the two role flags the install command
// adds to the host's users table.
type AdminUser struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Email   string `json:"email"`
	IsAdmin bool   `json:"is_admin"`
	IsAgent bool   `json:"is_agent"`
}

// UserDirectory is the contract a host implements so the admin
// users-management surface can list and mutate the host's user table.
// Hosts plug it in via Config.UserDirectory at New() time. Plugins
// don't own the user table, so this stays a hook rather than a
// store.Store method — different hosts pick different role schemes
// (boolean columns, Spatie-style pivots, custom enums, ...).
type UserDirectory interface {
	// ListUsers returns one page of users matching search (matches name
	// or email, case-insensitive) ordered by is_admin DESC, is_agent
	// DESC, id ASC. perPage is fixed at 20 by the caller.
	ListUsers(ctx context.Context, search string, page, perPage int) (UserPage, error)

	// GetUser fetches a single user by id, or returns (nil, nil) when
	// the id doesn't exist (the handler maps that to 404).
	GetUser(ctx context.Context, id int64) (*AdminUser, error)

	// UpdateUserRoles persists the role-flag changes. Implementations
	// should treat unset map keys as "leave alone" — the handler only
	// passes the flags it actually wants to flip.
	UpdateUserRoles(ctx context.Context, id int64, updates UserRoleUpdates) error
}

// UserPage is the paginator shape the Vue Index page expects. Mirrors
// the Laravel paginator JSON: data + meta keys the Vue page reads
// (current_page, last_page, total, per_page).
type UserPage struct {
	Data        []AdminUser `json:"data"`
	CurrentPage int         `json:"current_page"`
	LastPage    int         `json:"last_page"`
	PerPage     int         `json:"per_page"`
	Total       int         `json:"total"`
}

// UserRoleUpdates carries the optional admin/agent flag changes for one
// user. nil pointers mean "leave that column untouched" — the handler
// only fills the field whose role string came in on the request.
type UserRoleUpdates struct {
	IsAdmin *bool
	IsAgent *bool
}

// UserHandler serves the admin users-management page and the
// PATCH endpoint that flips admin/agent role flags. Mirrors the
// Laravel Admin\UserController in escalated-laravel.
type UserHandler struct {
	directory UserDirectory
	renderer  renderer.Renderer
	currentID func(r *http.Request) int64
}

// NewUserHandler constructs a UserHandler. directory may be nil when a
// host hasn't wired one up — in that case the routes degrade gracefully
// (Index renders an empty page; UpdateRole reports 501).
func NewUserHandler(directory UserDirectory, rend renderer.Renderer, currentID func(r *http.Request) int64) *UserHandler {
	if currentID == nil {
		currentID = func(_ *http.Request) int64 { return 0 }
	}
	return &UserHandler{directory: directory, renderer: rend, currentID: currentID}
}

// Index handles GET /admin/users — renders the Inertia page with the
// paginated user list, the search filter, and the current user's id
// so the Vue page can disable the self-demote toggle.
func (h *UserHandler) Index(w http.ResponseWriter, r *http.Request) {
	search := strings.TrimSpace(r.URL.Query().Get("search"))
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}

	var users UserPage
	if h.directory != nil {
		u, err := h.directory.ListUsers(r.Context(), search, page, 20)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		users = u
	} else {
		users = UserPage{Data: []AdminUser{}, CurrentPage: page, LastPage: 1, PerPage: 20, Total: 0}
	}
	// Defensive — never let `null` leak into the data slot of the
	// paginator; the Vue page iterates without a null check.
	if users.Data == nil {
		users.Data = []AdminUser{}
	}
	if users.PerPage == 0 {
		users.PerPage = 20
	}
	if users.CurrentPage == 0 {
		users.CurrentPage = page
	}

	var currentUserID any
	if uid := h.currentID(r); uid > 0 {
		currentUserID = uid
	}

	_ = h.renderer.Render(w, r, "Escalated/Admin/Users/Index", map[string]any{
		"users":         users,
		"filters":       map[string]any{"search": search},
		"currentUserId": currentUserID,
	})
}

// UpdateRole handles PATCH /admin/users/{user}/role. Body: {role, value}.
//
// Role semantics (mirrors escalated-laravel Admin\UserController):
//   - role="admin", value=true  → is_admin=true, is_agent=true (admin implies agent)
//   - role="admin", value=false → is_admin=false                (does NOT also clear agent)
//   - role="agent", value=true  → is_agent=true
//   - role="agent", value=false → is_agent=false; if target was admin, also is_admin=false
//
// Self-demote guard: a logged-in admin demoting their own admin flag is
// blocked with 422 — they would lock themselves out of the panel they
// just used to make the call.
func (h *UserHandler) UpdateRole(w http.ResponseWriter, r *http.Request) {
	if h.directory == nil {
		http.Error(w, "user directory not configured", http.StatusNotImplemented)
		return
	}

	targetID, err := idFromPathName(r, "user")
	if err != nil {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}

	var in struct {
		Role  string `json:"role"`
		Value *bool  `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid input", http.StatusBadRequest)
		return
	}
	if in.Role != "admin" && in.Role != "agent" {
		http.Error(w, "role must be admin or agent", http.StatusUnprocessableEntity)
		return
	}
	if in.Value == nil {
		http.Error(w, "value is required", http.StatusUnprocessableEntity)
		return
	}
	value := *in.Value

	target, err := h.directory.GetUser(r.Context(), targetID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if target == nil {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}

	// Self-demote guard. Compare by id (the only stable handle we
	// have) — the test must be against the request's current user.
	if in.Role == "admin" && !value {
		if currentID := h.currentID(r); currentID > 0 && currentID == target.ID {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"error": "You cannot remove your own admin role.",
			})
			return
		}
	}

	updates := UserRoleUpdates{}
	if in.Role == "admin" {
		updates.IsAdmin = boolPtr(value)
		// Admins are agents — flipping admin on also turns agent on.
		// Flipping admin off does NOT also clear agent: an ex-admin can
		// still answer tickets unless explicitly demoted.
		if value {
			updates.IsAgent = boolPtr(true)
		}
	} else {
		updates.IsAgent = boolPtr(value)
		// Revoking agent from an admin would leave the admin gate on
		// but the agent gate off — confusing. Demote fully.
		if !value && target.IsAdmin {
			updates.IsAdmin = boolPtr(false)
		}
	}

	if err := h.directory.UpdateUserRoles(r.Context(), target.ID, updates); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"status": "updated"})
}

func boolPtr(b bool) *bool { return &b }
