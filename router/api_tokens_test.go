package router_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	escalated "github.com/escalated-dev/escalated-go"
	"github.com/escalated-dev/escalated-go/handlers"
	"github.com/escalated-dev/escalated-go/internal/sqldialect"
	"github.com/escalated-dev/escalated-go/models"
)

// API tokens (escalated-laravel Admin\ApiTokenController and
// AuthenticateApiToken): admins issue bearer tokens on
// Escalated/Admin/ApiTokens/Index, the token is shown once, and with
// Config.APITokensEnabled a token authenticates the REST API as its owner,
// narrowed by its abilities.

// tokenOwners is the host's answer to "is this user still staff?".
type tokenOwners struct {
	mu    sync.Mutex
	roles map[models.UserID]*handlers.APITokenOwner
}

func (o *tokenOwners) set(id models.UserID, roles *handlers.APITokenOwner) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.roles[id] = roles
}

func (o *tokenOwners) lookup(_ context.Context, id models.UserID) (*handlers.APITokenOwner, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.roles[id], nil
}

func newTokenFixture(t *testing.T, enabled bool) (*uiFixture, *tokenOwners) {
	t.Helper()
	owners := &tokenOwners{roles: map[models.UserID]*handlers.APITokenOwner{
		uiAgent: {IsAgent: true, IsAdmin: true},
	}}
	f := newUIFixture(t, func(cfg *escalated.Config) {
		cfg.APITokensEnabled = enabled
		cfg.APITokenOwner = owners.lookup
	})
	return f, owners
}

// bearer sends a request authenticated only by a bearer token: no host
// session, so the host's own checks all say no.
func bearer(h http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

// issue creates a token through the admin page and returns its plain text.
func issue(t *testing.T, h http.Handler, body string) string {
	t.Helper()
	rec := pageJSON(h, http.MethodPost, "/escalated/admin/api-tokens", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("store: %d %s", rec.Code, rec.Body.String())
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("the response showing a token must not be cached; Cache-Control %q", cc)
	}
	var page inertiaPage
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("store: not an Inertia page: %s", rec.Body.String())
	}
	if page.Component != "Escalated/Admin/ApiTokens/Index" {
		t.Fatalf("store component %q", page.Component)
	}
	flash := obj(t, page.Props, "flash")
	plain, _ := flash["plain_text_token"].(string)
	if len(plain) != 64 {
		t.Fatalf("flash.plain_text_token %v", flash["plain_text_token"])
	}
	if flash["success"] != "API token created." {
		t.Errorf("flash.success %v", flash["success"])
	}
	return plain
}

func TestAdminAPITokenPage(t *testing.T) {
	for _, name := range []string{"chi", "stdlib"} {
		t.Run(name, func(t *testing.T) {
			f, _ := newTokenFixture(t, true)
			h := f.mounts()[name]
			index := "/escalated/admin/api-tokens"

			page := visit(t, h, index, uiAgent, "admin")
			if page.Component != "Escalated/Admin/ApiTokens/Index" {
				t.Fatalf("component %q", page.Component)
			}
			hasKeys(t, "props", page.Props, "tokens", "users", "api_enabled")
			if page.Props["api_enabled"] != true {
				t.Errorf("api_enabled %v", page.Props["api_enabled"])
			}
			if len(list(t, page.Props, "tokens")) != 0 {
				t.Errorf("tokens %v", page.Props["tokens"])
			}
			user := first(t, list(t, page.Props, "users"), "users")
			if user["name"] != "Ada Agent" || user["email"] != "ada@example.com" || user["id"].(float64) != 900 {
				t.Errorf("users %v", page.Props["users"])
			}

			plain := issue(t, h, `{"name":"CI","user_id":900,"abilities":["agent"],"expires_in_days":30}`)

			page = visit(t, h, index, uiAgent, "admin")
			row := first(t, list(t, page.Props, "tokens"), "tokens")
			hasKeys(t, "token", row, "id", "name", "user_name", "user_email", "abilities", "last_used_at",
				"last_used_ip", "expires_at", "is_expired", "created_at")
			if _, leaked := row["token"]; leaked {
				t.Error("the token list includes the token")
			}
			if strings.Contains(mustJSON(t, page.Props), plain) {
				t.Error("the page shows the token again")
			}
			if row["name"] != "CI" || row["user_name"] != "Ada Agent" || row["user_email"] != "ada@example.com" || row["is_expired"] != false {
				t.Errorf("token row %v", row)
			}
			expires, err := time.Parse(time.RFC3339, row["expires_at"].(string))
			if err != nil || expires.Sub(time.Now().Add(30*24*time.Hour)).Abs() > time.Minute {
				t.Errorf("expires_at %v", row["expires_at"])
			}
			id := int64(row["id"].(float64))

			// Validation answers on the page.
			wantPageErrors(t, pageJSON(h, http.MethodPost, index, `{"name":"","user_id":900,"abilities":["agent"]}`), "no name", "Escalated/Admin/ApiTokens/Index", "name")
			wantPageErrors(t, pageJSON(h, http.MethodPost, index, `{"name":"X","user_id":424242,"abilities":["agent"]}`), "not staff", "Escalated/Admin/ApiTokens/Index", "user_id")
			wantPageErrors(t, pageJSON(h, http.MethodPost, index, `{"name":"X","user_id":900,"abilities":["root"]}`), "bad ability", "Escalated/Admin/ApiTokens/Index", "abilities")
			wantPageErrors(t, pageJSON(h, http.MethodPost, index, `{"name":"X","user_id":900,"abilities":["agent"],"expires_in_days":366}`), "too long", "Escalated/Admin/ApiTokens/Index", "expires_in_days")

			wantRedirect(t, pageJSON(h, http.MethodPatch, index+"/"+itoa(id), `{"name":"Deploys","abilities":["*"]}`), "update", index)
			page = visit(t, h, index, uiAgent, "admin")
			row = first(t, list(t, page.Props, "tokens"), "tokens")
			if row["name"] != "Deploys" || mustJSON(t, row["abilities"]) != `["*"]` {
				t.Errorf("after update %v", row)
			}

			if rec := bearer(h, http.MethodGet, "/escalated/api/tickets", plain, ""); rec.Code != http.StatusOK {
				t.Fatalf("token before revoke: %d %s", rec.Code, rec.Body.String())
			}
			wantRedirect(t, pageJSON(h, http.MethodDelete, index+"/"+itoa(id), ""), "destroy", index)
			if rec := bearer(h, http.MethodGet, "/escalated/api/tickets", plain, ""); rec.Code != http.StatusUnauthorized {
				t.Errorf("revoked token: %d", rec.Code)
			}

			// API clients get JSON: 201 with the token once.
			rec := sendAPI(h, http.MethodPost, index, uiAgent, "admin", `{"name":"Script","user_id":900,"abilities":["agent"]}`)
			var out map[string]any
			if rec.Code != http.StatusCreated || json.Unmarshal(rec.Body.Bytes(), &out) != nil {
				t.Fatalf("API store: %d %s", rec.Code, rec.Body.String())
			}
			if s, _ := out["plain_text_token"].(string); len(s) != 64 {
				t.Errorf("API store body %v", out)
			}
			rec = sendAPI(h, http.MethodGet, index, uiAgent, "admin", "")
			if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), out["plain_text_token"].(string)) {
				t.Errorf("API list: %d %s", rec.Code, rec.Body.String())
			}

			// Admins only.
			if rec := send(h, http.MethodGet, index, uiAgent, "agent", ""); rec.Code != http.StatusForbidden {
				t.Errorf("agent on the token page: %d", rec.Code)
			}
		})
	}
}

func TestAPITokenAuthenticatesTheRESTAPI(t *testing.T) {
	for _, name := range []string{"chi", "stdlib"} {
		t.Run(name, func(t *testing.T) {
			f, owners := newTokenFixture(t, true)
			h := f.mounts()[name]

			agentToken := issue(t, h, `{"name":"agent","user_id":900,"abilities":["agent"]}`)
			allToken := issue(t, h, `{"name":"all","user_id":900,"abilities":["*"]}`)
			adminToken := issue(t, h, `{"name":"admin","user_id":900,"abilities":["admin"]}`)

			for _, path := range []string{"/escalated/api/tickets", "/escalated/api/departments", "/escalated/api/tags"} {
				for label, token := range map[string]string{"agent": agentToken, "*": allToken} {
					if rec := bearer(h, http.MethodGet, path, token, ""); rec.Code != http.StatusOK {
						t.Errorf("%s token on %s: %d %s", label, path, rec.Code, rec.Body.String())
					}
				}
			}

			// The token's owner is the actor: a reply is theirs.
			rec := bearer(h, http.MethodPost, "/escalated/api/tickets/"+itoa(f.ticket.ID)+"/replies", agentToken, `{"body":"From the CLI"}`)
			if rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
				t.Fatalf("reply with a token: %d %s", rec.Code, rec.Body.String())
			}
			db := f.esc.Config.DB
			var author models.UserID
			if err := db.QueryRow(sqldialect.Rebind(db, `SELECT author_id FROM escalated_replies WHERE body = ?`), "From the CLI").Scan(&author); err != nil || author != uiAgent {
				t.Errorf("reply author %q %v, want the token's owner", author, err)
			}

			// Use is recorded.
			var lastIP *string
			_ = db.QueryRow(sqldialect.Rebind(db, `SELECT last_used_ip FROM escalated_api_tokens WHERE name = ?`), "agent").Scan(&lastIP)
			if lastIP == nil || *lastIP == "" {
				t.Error("last_used_ip was not recorded")
			}

			// An admin-only token is not an agent token (Laravel's :agent).
			if rec := bearer(h, http.MethodGet, "/escalated/api/tickets", adminToken, ""); rec.Code != http.StatusForbidden {
				t.Errorf("admin-only token on the agent API: %d", rec.Code)
			}
			// Unknown and missing tokens.
			if rec := bearer(h, http.MethodGet, "/escalated/api/tickets", strings.Repeat("a", 64), ""); rec.Code != http.StatusUnauthorized {
				t.Errorf("unknown token: %d", rec.Code)
			}
			if rec := bearer(h, http.MethodGet, "/escalated/api/tickets", "", ""); rec.Code != http.StatusForbidden {
				t.Errorf("no token: %d", rec.Code)
			}
			// A token never reaches beyond the REST API.
			if rec := bearer(h, http.MethodGet, "/escalated/admin/api-tokens", allToken, ""); rec.Code != http.StatusForbidden {
				t.Errorf("token on an admin page: %d", rec.Code)
			}

			// Expired.
			if _, err := db.Exec(sqldialect.Rebind(db, `UPDATE escalated_api_tokens SET expires_at = ? WHERE name = ?`), time.Now().Add(-time.Minute).UTC(), "all"); err != nil {
				t.Fatal(err)
			}
			rec = bearer(h, http.MethodGet, "/escalated/api/tickets", allToken, "")
			if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "expired") {
				t.Errorf("expired token: %d %s", rec.Code, rec.Body.String())
			}

			// An owner who is no longer staff loses the API at once.
			owners.set(uiAgent, &handlers.APITokenOwner{})
			if rec := bearer(h, http.MethodGet, "/escalated/api/tickets", agentToken, ""); rec.Code != http.StatusForbidden {
				t.Errorf("demoted owner: %d", rec.Code)
			}
			owners.set(uiAgent, nil)
			if rec := bearer(h, http.MethodGet, "/escalated/api/tickets", agentToken, ""); rec.Code != http.StatusUnauthorized {
				t.Errorf("deleted owner: %d", rec.Code)
			}

			// The host's own authentication keeps working beside tokens.
			if rec := send(h, http.MethodGet, "/escalated/api/tickets", uiAgent, "agent", ""); rec.Code != http.StatusOK {
				t.Errorf("host-authenticated agent: %d", rec.Code)
			}
		})
	}
}

// Tokens are opt-in: without Config.APITokensEnabled a bearer header changes
// nothing, so a host that authenticates the API itself is unaffected.
func TestAPITokensAreOptIn(t *testing.T) {
	for _, name := range []string{"chi", "stdlib"} {
		t.Run(name, func(t *testing.T) {
			f, _ := newTokenFixture(t, false)
			h := f.mounts()[name]
			page := visit(t, h, "/escalated/admin/api-tokens", uiAgent, "admin")
			if page.Props["api_enabled"] != false {
				t.Errorf("api_enabled %v", page.Props["api_enabled"])
			}
			plain := issue(t, h, `{"name":"CI","user_id":900,"abilities":["*"]}`)
			if rec := bearer(h, http.MethodGet, "/escalated/api/tickets", plain, ""); rec.Code != http.StatusForbidden {
				t.Errorf("token while disabled: %d", rec.Code)
			}
			// A host-issued bearer token on the host's own auth routes is
			// still the host's business.
			r := httptest.NewRequest(http.MethodGet, "/escalated/api/tickets", nil)
			r.Header.Set("Authorization", "Bearer host-session-token")
			r.Header.Set(headerUser, uiAgent)
			r.Header.Set(headerRole, "agent")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, r)
			if rec.Code != http.StatusOK {
				t.Errorf("host-authenticated request with its own bearer header: %d", rec.Code)
			}
		})
	}
}

// With tokens enabled, a bearer header the package did not issue is left to
// the host's checks rather than refused outright.
func TestHostBearerTokensStillWorkWithAPITokensEnabled(t *testing.T) {
	for _, name := range []string{"chi", "stdlib"} {
		t.Run(name, func(t *testing.T) {
			f, _ := newTokenFixture(t, true)
			h := f.mounts()[name]
			r := httptest.NewRequest(http.MethodGet, "/escalated/api/tickets", nil)
			r.Header.Set("Authorization", "Bearer host-session-token")
			r.Header.Set(headerUser, uiAgent)
			r.Header.Set(headerRole, "agent")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, r)
			if rec.Code != http.StatusOK {
				t.Errorf("host-authenticated request: %d %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
