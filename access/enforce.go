package access

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/renderer"
)

// StatusResolver turns a submitted status (a slug, a number or a custom
// status's slug) into its base status (models.Status*).
type StatusResolver func(ctx context.Context, value any) (status int, ok bool)

// Enforcer requires each admin, agent and staff API route's permission
// (RoutePermissions) from the signed-in user's roles while roles &
// permissions is on, and does nothing while it is off.
//
// It runs under the route prefix after the API token middleware, so an API
// token request is checked as the token's owner. The admin and agent gates
// still decide who reaches those areas at all; this adds the permission on
// top, so both must pass.
type Enforcer struct {
	// Prefix is Config.RoutePrefix.
	Prefix   string
	Features Features
	Store    *Store
	UserID   func(*http.Request) models.UserID
	Status   StatusResolver
	// Logf reports a failure to read permissions. Nil uses the log package.
	Logf func(format string, args ...any)
}

// Middleware is the enforcement as net/http middleware.
func (e *Enforcer) Middleware(next http.Handler) http.Handler {
	if e == nil || !e.Features.RolesPermissions {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, ok := strings.CutPrefix(r.URL.Path, e.Prefix)
		if !ok || !Governed(path) {
			next.ServeHTTP(w, r)
			return
		}
		var uid models.UserID
		if e.UserID != nil {
			uid = e.UserID(r)
		}
		if uid.Empty() {
			forbid(w, r, "Unauthorized.")
			return
		}
		required, ok, err := e.required(r, path)
		if err != nil {
			http.Error(w, "invalid input", http.StatusBadRequest)
			return
		}
		if !ok {
			forbid(w, r, "This route has no permission mapping.")
			return
		}
		if len(required) > 0 {
			granted, err := e.Store.PermissionsOf(r.Context(), uid)
			if err != nil {
				e.logf("escalated: reading permissions of user %s: %v", uid, err)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
			have := map[string]bool{}
			for _, slug := range granted {
				have[slug] = true
			}
			for _, slug := range required {
				if !have[slug] {
					forbid(w, r, "You do not have the required permission: "+slug)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (e *Enforcer) logf(format string, args ...any) {
	if e.Logf != nil {
		e.Logf(format, args...)
		return
	}
	log.Printf(format, args...)
}

// required is Mapped plus what the request body decides: a bulk action's own
// permission, ticket.close for a change to a resolved, closed or reopened
// status, and reply.create_internal for an internal reply.
func (e *Enforcer) required(r *http.Request, path string) ([]string, bool, error) {
	required, ok := Mapped(r.Method, path)
	if !ok {
		return nil, false, nil
	}
	route := normalize(path)
	bulk := compile(staff + "/tickets/bulk").MatchString(route)
	replies := r.Method == http.MethodPost && compile("/{admin,agent,api}/tickets/*/replies").MatchString(route)
	if !bulk && !replies && !contains(required, "ticket.edit") {
		return required, true, nil
	}
	in, err := peekInput(r)
	if err != nil {
		return nil, false, err
	}
	switch {
	case bulk:
		action := first(in, "action")
		required = append(required, BulkActions[action]...)
		if action == "status" {
			required = append(required, e.closing(r.Context(), in, "value")...)
		}
	case replies:
		if truthy(first(in, "is_internal")) || truthy(first(in, "is_internal_note")) {
			required = []string{"reply.create_internal"}
		}
	default:
		required = append(required, e.closing(r.Context(), in, "status")...)
	}
	return dedupe(required), true, nil
}

func (e *Enforcer) closing(ctx context.Context, in map[string][]any, key string) []string {
	values := in[key]
	if len(values) == 0 {
		values = in[key+"[]"]
	}
	if len(values) == 0 || values[0] == nil || values[0] == "" {
		return nil
	}
	value := values[0]
	if s, isString := value.(string); isString {
		value = strings.TrimSpace(s)
		if n, err := strconv.Atoi(s); err == nil {
			value = float64(n)
		}
	}
	if e.Status != nil {
		if status, ok := e.Status(ctx, value); ok && ClosingStatuses[status] {
			return []string{"ticket.close"}
		}
		return nil
	}
	switch v := value.(type) {
	case string:
		if v == "resolved" || v == "closed" || v == "reopened" {
			return []string{"ticket.close"}
		}
	case float64:
		if ClosingStatuses[int(v)] {
			return []string{"ticket.close"}
		}
	}
	return nil
}

// peekInput reads the request's fields (JSON, urlencoded or multipart) and
// puts the body back for the handler.
func peekInput(r *http.Request) (map[string][]any, error) {
	out := map[string][]any{}
	if r.Body == nil || r.Body == http.NoBody {
		return out, nil
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	_ = r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(raw))
	ct, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	switch ct {
	case "application/x-www-form-urlencoded":
		values, err := url.ParseQuery(string(raw))
		if err != nil {
			return nil, err
		}
		return fromValues(values), nil
	case "multipart/form-data":
		clone := r.Clone(r.Context())
		clone.Body = io.NopCloser(bytes.NewReader(raw))
		clone.Header = r.Header.Clone()
		if err := clone.ParseMultipartForm(32 << 20); err != nil {
			return nil, err
		}
		_ = params
		return fromValues(clone.MultipartForm.Value), nil
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return out, nil
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	for k, v := range body {
		if list, isList := v.([]any); isList {
			out[k] = list
		} else {
			out[k] = []any{v}
		}
	}
	return out, nil
}

func fromValues(values map[string][]string) map[string][]any {
	out := map[string][]any{}
	for k, vs := range values {
		for _, v := range vs {
			out[k] = append(out[k], v)
		}
	}
	return out
}

func first(in map[string][]any, key string) string {
	if v := in[key]; len(v) > 0 {
		switch val := v[0].(type) {
		case string:
			return strings.TrimSpace(val)
		case bool:
			return strconv.FormatBool(val)
		case float64:
			return strconv.FormatFloat(val, 'f', -1, 64)
		}
	}
	return ""
}

func truthy(v string) bool {
	switch strings.ToLower(v) {
	case "1", "true", "on", "yes":
		return true
	}
	return false
}

func contains(list []string, slug string) bool {
	for _, s := range list {
		if s == slug {
			return true
		}
	}
	return false
}

func dedupe(list []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(list))
	for _, s := range list {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// forbid answers 403: {message} for a JSON client, plain text otherwise.
func forbid(w http.ResponseWriter, r *http.Request, message string) {
	if r.Header.Get("X-Inertia") == "" && strings.Contains(r.Header.Get("Accept"), "json") {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"message": message})
		return
	}
	http.Error(w, message, http.StatusForbidden)
}

// Sharer shares escalated.features and escalated.permissions with every
// page: with roles & permissions on, the signed-in user's slugs; with it
// off, ["*"] for staff (the gates decide everything) and [] for anyone else.
type Sharer struct {
	Features Features
	Store    *Store
	IsStaff  func(*http.Request) bool
	UserID   func(*http.Request) models.UserID
	// TwoFactorRequired is escalated.two_factor_required: whether the
	// signed-in staff user must still enrol. Nil is false.
	TwoFactorRequired func(*http.Request) bool
}

// Middleware adds the shared props, computed only when a page renders.
func (s *Sharer) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := renderer.WithShared(r.Context(), "escalated", func() any {
			return map[string]any{
				"features":            s.Features.Shared(),
				"permissions":         s.permissions(r),
				"two_factor_required": s.TwoFactorRequired != nil && s.TwoFactorRequired(r),
			}
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Permissions is what the shared `permissions` prop holds for the request.
func (s *Sharer) permissions(r *http.Request) []string {
	if !s.Features.RolesPermissions {
		if s.IsStaff != nil && s.IsStaff(r) {
			return []string{"*"}
		}
		return []string{}
	}
	if s.UserID == nil || s.Store == nil {
		return []string{}
	}
	slugs, err := s.Store.PermissionsOf(r.Context(), s.UserID(r))
	if err != nil {
		return []string{}
	}
	return slugs
}

// Allows reports whether the request's user holds the permission: always
// while roles & permissions is off, else from their roles.
func (s *Sharer) Allows(r *http.Request, slug string) bool {
	if !s.Features.RolesPermissions {
		return true
	}
	return contains(s.permissions(r), slug)
}
