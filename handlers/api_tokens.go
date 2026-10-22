package handlers

import (
	"context"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/services"
)

// APITokenOwner is what the host says about a token's owner at request time.
// A token never grants more than its owner has now: an owner who is no longer
// staff loses the API at once, whatever the token says.
type APITokenOwner struct {
	IsAgent bool
	IsAdmin bool
}

func (o *APITokenOwner) staff() bool { return o != nil && (o.IsAgent || o.IsAdmin) }

// APITokenOwnerFunc looks a token's owner up; nil (with a nil error) when the
// user no longer exists.
type APITokenOwnerFunc func(ctx context.Context, id models.UserID) (*APITokenOwner, error)

// APITokenOwnerFromDirectory answers APITokenOwnerFunc from the host's
// UserDirectory, for integer user ids. Nil when dir is nil.
func APITokenOwnerFromDirectory(dir UserDirectory) APITokenOwnerFunc {
	if dir == nil {
		return nil
	}
	return func(ctx context.Context, id models.UserID) (*APITokenOwner, error) {
		n, err := strconv.ParseInt(string(id), 10, 64)
		if err != nil {
			return nil, nil
		}
		u, err := dir.GetUser(ctx, n)
		if err != nil || u == nil {
			return nil, err
		}
		return &APITokenOwner{IsAgent: u.IsAgent, IsAdmin: u.IsAdmin}, nil
	}
}

// APITokenIdentity is the token a request authenticated with, and its owner.
type APITokenIdentity struct {
	Token *models.APIToken
	Owner APITokenOwner
}

// Agent reports whether the token may act as an agent: its owner is staff and
// it carries the agent ability (or *).
func (i *APITokenIdentity) Agent() bool {
	return i.Owner.staff() && i.Token.HasAbility(models.APITokenAbilityAgent)
}

// Admin reports whether the token may act as an admin: its owner is an admin
// and it carries the admin ability (or *).
func (i *APITokenIdentity) Admin() bool {
	return i.Owner.IsAdmin && i.Token.HasAbility(models.APITokenAbilityAdmin)
}

type apiTokenContextKey struct{}

// WithAPIToken returns ctx carrying the token a request authenticated with.
func WithAPIToken(ctx context.Context, id *APITokenIdentity) context.Context {
	return context.WithValue(ctx, apiTokenContextKey{}, id)
}

// APITokenFrom returns the token the request authenticated with, or nil.
func APITokenFrom(ctx context.Context) *APITokenIdentity {
	id, _ := ctx.Value(apiTokenContextKey{}).(*APITokenIdentity)
	return id
}

// APITokenAuth authenticates REST API requests that carry an Escalated API
// token as `Authorization: Bearer <token>` (escalated-laravel
// AuthenticateApiToken with the agent ability).
//
// A request without a bearer header, or outside Applies, passes through
// untouched. A bearer value this package did not issue is left to the host's
// own checks (HostAgent, HostAdmin), so a host that authenticates the API with
// tokens of its own keeps working; if those say no, it is answered 401.
type APITokenAuth struct {
	Tokens    *services.APITokenService
	Owner     APITokenOwnerFunc
	HostAgent func(r *http.Request) bool
	HostAdmin func(r *http.Request) bool
	// Applies reports whether a request path is one tokens authenticate.
	Applies func(path string) bool
}

// Middleware wraps next.
func (a *APITokenAuth) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		plain := bearerToken(r)
		if plain == "" || a.Tokens == nil || (a.Applies != nil && !a.Applies(r.URL.Path)) {
			next.ServeHTTP(w, r)
			return
		}
		token, err := a.Tokens.FindByPlainText(r.Context(), plain)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		if token == nil {
			if (a.HostAgent != nil && a.HostAgent(r)) || (a.HostAdmin != nil && a.HostAdmin(r)) {
				next.ServeHTTP(w, r)
				return
			}
			log.Printf("escalated: API authentication failed: invalid token (path %s)", r.URL.Path)
			writeJSON(w, http.StatusUnauthorized, map[string]any{"message": "Invalid token."})
			return
		}
		if token.IsExpired(time.Now()) {
			log.Printf("escalated: API authentication failed: expired token %d", token.ID)
			writeJSON(w, http.StatusUnauthorized, map[string]any{"message": "Token has expired."})
			return
		}
		if !token.HasAbility(models.APITokenAbilityAgent) {
			writeJSON(w, http.StatusForbidden, map[string]any{"message": "Insufficient permissions."})
			return
		}
		var owner *APITokenOwner
		if a.Owner != nil {
			if owner, err = a.Owner(r.Context(), token.UserID); err != nil {
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
		}
		if owner == nil {
			log.Printf("escalated: API authentication failed: owner of token %d not found", token.ID)
			writeJSON(w, http.StatusUnauthorized, map[string]any{"message": "Token owner not found."})
			return
		}
		if !owner.staff() {
			log.Printf("escalated: API authentication failed: owner of token %d no longer has agent access", token.ID)
			writeJSON(w, http.StatusForbidden, map[string]any{"message": "User no longer has agent access."})
			return
		}
		if err := a.Tokens.Touch(r.Context(), token, clientIP(r)); err != nil {
			log.Printf("escalated: recording use of API token %d: %v", token.ID, err)
		}
		next.ServeHTTP(w, r.WithContext(WithAPIToken(r.Context(), &APITokenIdentity{Token: token, Owner: *owner})))
	})
}

// clientIP is the connection's address. Forwarding headers are the host's
// proxy's to vouch for, so they are not read here.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// APITokenHandler is the admin API token screen, Escalated/Admin/ApiTokens/Index,
// and its JSON (escalated-laravel Admin\ApiTokenController).
type APITokenHandler struct {
	Tokens *services.APITokenService
	Owner  APITokenOwnerFunc
	// Agents and Directory list the users a token can be issued to: Agents
	// when set, else the staff in Directory.
	Agents    SkillAgentDirectory
	Directory UserDirectory
	// Enabled is Config.APITokensEnabled: whether tokens authenticate the API.
	Enabled bool
	Pages   PageSupport
}

const apiTokensPage = "Escalated/Admin/ApiTokens/Index"

// Index handles GET /admin/api-tokens.
func (h *APITokenHandler) Index(w http.ResponseWriter, r *http.Request) {
	if h.Pages.wantsPage(r) {
		h.renderIndex(w, r, nil, nil)
		return
	}
	tokens, _, err := h.rows(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tokens": tokens})
}

// Store handles POST /admin/api-tokens. The token's plain text is in the
// response once: the page's flash.plain_text_token, or plain_text_token in
// the JSON.
func (h *APITokenHandler) Store(w http.ResponseWriter, r *http.Request) {
	in, err := readInput(r)
	if err != nil {
		h.invalid(w, r, map[string]string{"name": "The request could not be read."})
		return
	}
	errs := map[string]string{}
	name := strings.TrimSpace(in.Get("name"))
	if name == "" {
		errs["name"] = requiredMessage("name")
	} else if len(name) > 255 {
		errs["name"] = "The name may not be greater than 255 characters."
	}
	userID := models.UserID(strings.TrimSpace(in.Get("user_id")))
	if userID.Empty() {
		errs["user_id"] = requiredMessage("user id")
	} else {
		owner, err := h.owner(r.Context(), userID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if !owner.staff() {
			errs["user_id"] = "Choose an agent for the token."
		}
	}
	abilities, msg := tokenAbilities(in, true)
	if msg != "" {
		errs["abilities"] = msg
	}
	var expiresAt *time.Time
	if v := strings.TrimSpace(in.Get("expires_in_days")); v != "" {
		days, err := strconv.Atoi(v)
		if err != nil || days < 1 || days > 365 {
			errs["expires_in_days"] = "The token must expire in 1 to 365 days."
		} else {
			t := time.Now().Add(time.Duration(days) * 24 * time.Hour)
			expiresAt = &t
		}
	}
	if len(errs) > 0 {
		h.invalid(w, r, errs)
		return
	}

	token, plain, err := h.Tokens.Create(r.Context(), userID, name, abilities, expiresAt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if h.Pages.Renderer != nil && isInertia(r) {
		// No session to flash through, so the page comes back with the token
		// in its flash prop, which is where the page reads it.
		h.renderIndex(w, r, nil, map[string]any{"success": "API token created.", "plain_text_token": plain})
		return
	}
	row, err := h.row(r, token, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"token": row, "plain_text_token": plain})
}

// Update handles PATCH /admin/api-tokens/{id}: name and/or abilities.
func (h *APITokenHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	in, err := readInput(r)
	if err != nil {
		h.invalid(w, r, map[string]string{"name": "The request could not be read."})
		return
	}
	errs := map[string]string{}
	var name *string
	if _, ok := in["name"]; ok {
		v := strings.TrimSpace(in.Get("name"))
		if v == "" || len(v) > 255 {
			errs["name"] = "The name must be 1 to 255 characters."
		}
		name = &v
	}
	var abilities []string
	if _, ok := in["abilities"]; ok {
		var msg string
		if abilities, msg = tokenAbilities(in, true); msg != "" {
			errs["abilities"] = msg
		}
	}
	if len(errs) > 0 {
		h.invalid(w, r, errs)
		return
	}
	found, err := h.Tokens.Update(r.Context(), id, name, abilities)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !found {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	h.Pages.succeed(w, r, h.Pages.path("/admin/api-tokens"), http.StatusOK, map[string]any{"id": id})
}

// Destroy handles DELETE /admin/api-tokens/{id}: the token stops working.
func (h *APITokenHandler) Destroy(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	found, err := h.Tokens.Delete(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !found {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	h.Pages.succeed(w, r, h.Pages.path("/admin/api-tokens"), http.StatusNoContent, nil)
}

func (h *APITokenHandler) owner(ctx context.Context, id models.UserID) (*APITokenOwner, error) {
	if h.Owner == nil {
		return nil, nil
	}
	return h.Owner(ctx, id)
}

func (h *APITokenHandler) invalid(w http.ResponseWriter, r *http.Request, errs map[string]string) {
	if h.Pages.Renderer != nil && isInertia(r) {
		h.renderIndex(w, r, errs, nil)
		return
	}
	validationErrors(w, r, errs, nil)
}

func (h *APITokenHandler) renderIndex(w http.ResponseWriter, r *http.Request, errs map[string]string, flash map[string]any) {
	tokens, users, err := h.rows(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	props := map[string]any{
		"tokens":      tokens,
		"users":       users,
		"api_enabled": h.Enabled,
	}
	if flash != nil {
		props["flash"] = flash
	}
	h.Pages.render(w, r, apiTokensPage, props, errs)
}

// tokenUser is one entry of the page's users prop.
type tokenUser struct {
	ID    any    `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// users are the staff a token can be issued to.
func (h *APITokenHandler) users(ctx context.Context) ([]tokenUser, error) {
	out := []tokenUser{}
	if h.Agents != nil {
		agents, err := h.Agents.ListAgentsForSkillForm(ctx)
		if err != nil {
			return nil, err
		}
		for _, a := range agents {
			out = append(out, tokenUser{ID: a.ID, Name: a.Name, Email: a.Email})
		}
		return out, nil
	}
	if h.Directory == nil {
		return out, nil
	}
	// The first 500 users, as escalated-laravel lists them.
	for page := 1; page <= 25; page++ {
		res, err := h.Directory.ListUsers(ctx, "", page, 20)
		if err != nil {
			return nil, err
		}
		for _, u := range res.Data {
			if u.IsAgent || u.IsAdmin {
				out = append(out, tokenUser{ID: u.ID, Name: u.Name, Email: u.Email})
			}
		}
		if page >= res.LastPage {
			break
		}
	}
	return out, nil
}

func (h *APITokenHandler) rows(r *http.Request) ([]map[string]any, []tokenUser, error) {
	users, err := h.users(r.Context())
	if err != nil {
		return nil, nil, err
	}
	tokens, err := h.Tokens.List(r.Context())
	if err != nil {
		return nil, nil, err
	}
	known := map[string]tokenUser{}
	for _, u := range users {
		known[stringID(u.ID)] = u
	}
	out := make([]map[string]any, 0, len(tokens))
	for _, t := range tokens {
		row, err := h.row(r, t, known)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, row)
	}
	return out, users, nil
}

// row is a token as the page lists it: never the token or its digest.
func (h *APITokenHandler) row(r *http.Request, t *models.APIToken, known map[string]tokenUser) (map[string]any, error) {
	name, email := h.Pages.Users.name(r.Context(), t.UserID), h.Pages.Users.email(r.Context(), t.UserID)
	if u, ok := known[string(t.UserID)]; ok {
		if name == "" {
			name = u.Name
		}
		if email == "" {
			email = u.Email
		}
	}
	return map[string]any{
		"id":           t.ID,
		"name":         t.Name,
		"user_id":      t.UserID,
		"user_name":    nullable(name),
		"user_email":   nullable(email),
		"abilities":    t.Abilities,
		"last_used_at": isoTime(t.LastUsedAt),
		"last_used_ip": t.LastUsedIP,
		"expires_at":   isoTime(t.ExpiresAt),
		"is_expired":   t.IsExpired(time.Now()),
		"created_at":   t.CreatedAt.UTC().Format(time.RFC3339),
	}, nil
}

// tokenAbilities reads abilities[] (or abilities as a JSON list); each must be
// agent, admin or *.
func tokenAbilities(in url.Values, required bool) ([]string, string) {
	raw := in["abilities"]
	if len(raw) == 0 {
		raw = in["abilities[]"]
	}
	out := []string{}
	for _, a := range raw {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		valid := false
		for _, allowed := range models.APITokenAbilities {
			if a == allowed {
				valid = true
			}
		}
		if !valid {
			return nil, "Each ability must be agent, admin or *."
		}
		out = append(out, a)
	}
	if required && len(out) == 0 {
		return nil, "Choose at least one ability."
	}
	return out, ""
}

func stringID(v any) string {
	switch id := v.(type) {
	case int64:
		return strconv.FormatInt(id, 10)
	case string:
		return id
	default:
		return ""
	}
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func isoTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}
