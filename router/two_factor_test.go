package router_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/pquerna/otp/totp"

	escalated "github.com/escalated-dev/escalated-go"
	"github.com/escalated-dev/escalated-go/handlers"
	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/router"
)

// Two-factor (ADR 2026-10-29-configurable-access-features, port checklist
// "Two-factor"): an install-level switch, off by default. On, staff with a
// confirmed TOTP enrolment are challenged after the host's login until this
// session passes; the pass is bound to the host's session id.

const headerSession = "X-Test-Session"

func twoFactorOn(on bool) func(*escalated.Config) {
	return func(cfg *escalated.Config) {
		cfg.Access.TwoFactor = &on
		cfg.Access.Secret = "test-two-factor-secret"
		cfg.Access.SessionID = func(r *http.Request) string { return r.Header.Get(headerSession) }
	}
}

// browser is one signed-in user's browser: their id, gate role, host session
// id and cookies.
type browser struct {
	h       http.Handler
	user    string
	role    string
	session string
	cookies map[string]*http.Cookie
}

func newBrowser(h http.Handler, user, role, session string) *browser {
	return &browser{h: h, user: user, role: role, session: session, cookies: map[string]*http.Cookie{}}
}

func (b *browser) do(method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	r.Header.Set("X-Inertia", "true")
	r.Header.Set(headerUser, b.user)
	r.Header.Set(headerRole, b.role)
	r.Header.Set(headerSession, b.session)
	for k, v := range headers {
		if v == "" {
			r.Header.Del(k)
		} else {
			r.Header.Set(k, v)
		}
	}
	for _, c := range b.cookies {
		r.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	b.h.ServeHTTP(rec, r)
	for _, c := range rec.Result().Cookies() {
		if c.MaxAge < 0 {
			delete(b.cookies, c.Name)
		} else {
			b.cookies[c.Name] = c
		}
	}
	return rec
}

func (b *browser) get(path string) *httptest.ResponseRecorder {
	return b.do(http.MethodGet, path, "", nil)
}

func (b *browser) post(path, body string) *httptest.ResponseRecorder {
	return b.do(http.MethodPost, path, body, nil)
}

// enrol enrols the user through the service and returns their TOTP secret
// and recovery codes.
func enrol(t *testing.T, esc *escalated.Escalated, user string) (string, []string) {
	t.Helper()
	ctx := context.Background()
	uri, codes, err := esc.TwoFactor().Setup(ctx, models.UserID(user), user+"@example.com")
	if err != nil {
		t.Fatal(err)
	}
	secret := secretOf(t, uri)
	e, err := esc.TwoFactor().Find(ctx, models.UserID(user))
	if err != nil || e == nil {
		t.Fatalf("pending enrolment: %v", err)
	}
	// Confirm with the previous step's code, so the current step stays free
	// for the test.
	code, _ := totp.GenerateCode(secret, time.Now().Add(-30*time.Second))
	if ok, err := esc.TwoFactor().Confirm(ctx, e, code); err != nil || !ok {
		t.Fatalf("confirm: %v %v", ok, err)
	}
	return secret, codes
}

func secretOf(t *testing.T, uri string) string {
	t.Helper()
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "otpauth" {
		t.Fatalf("qr_uri %q", uri)
	}
	return u.Query().Get("secret")
}

func nowCode(secret string) string {
	code, _ := totp.GenerateCode(secret, time.Now())
	return code
}

func wantLocation(t *testing.T, rec *httptest.ResponseRecorder, what, to string) {
	t.Helper()
	if (rec.Code != http.StatusFound && rec.Code != http.StatusSeeOther) || rec.Header().Get("Location") != to {
		t.Fatalf("%s: got %d to %q, want a redirect to %q: %s", what, rec.Code, rec.Header().Get("Location"), to, rec.Body.String())
	}
}

func TestTwoFactorSwitchOffByDefault(t *testing.T) {
	for _, name := range []string{"chi", "stdlib"} {
		t.Run(name, func(t *testing.T) {
			f := newUIFixture(t, func(cfg *escalated.Config) {
				cfg.Access.Secret = "test-two-factor-secret"
			})
			h := f.mounts()[name]
			if f.esc.Features.TwoFactor {
				t.Fatal("two-factor is on by default")
			}
			enrol(t, f.esc, uiAgent)
			b := newBrowser(h, uiAgent, "admin", "s1")

			// Enrolled, but nobody is challenged while the switch is off.
			if rec := b.get("/escalated/agent/tickets"); rec.Code != http.StatusOK {
				t.Errorf("enrolled user with the switch off: %d", rec.Code)
			}
			page := pageOf(t, b.get("/escalated/admin/departments"))
			if obj(t, sharedEscalated(t, page), "features")["two_factor"] != false {
				t.Error("features.two_factor is not false")
			}
			if sharedEscalated(t, page)["two_factor_required"] != false {
				t.Error("two_factor_required is not false")
			}
			for _, req := range [][2]string{
				{http.MethodGet, "/escalated/admin/settings/two-factor"},
				{http.MethodPost, "/escalated/admin/settings/two-factor/setup"},
				{http.MethodPost, "/escalated/admin/settings/two-factor/confirm"},
				{http.MethodPost, "/escalated/admin/settings/two-factor/disable"},
				{http.MethodGet, "/escalated/two-factor/challenge"},
				{http.MethodPost, "/escalated/two-factor/challenge"},
			} {
				if rec := b.do(req[0], req[1], `{"code":"123456"}`, nil); rec.Code != http.StatusNotFound {
					t.Errorf("%s %s with the switch off: %d, want 404", req[0], req[1], rec.Code)
				}
			}
			// The settings page does not offer the requirement, and a posted
			// switch or requirement changes nothing.
			if rec := b.post("/escalated/admin/settings", `{"two_factor":true,"two_factor_required":true}`); rec.Code >= 400 {
				t.Fatalf("settings post: %d", rec.Code)
			}
			if got := f.setting(t, "two_factor_required"); got != "" {
				t.Errorf("two_factor_required was stored with the switch off: %q", got)
			}
			settings := pageOf(t, b.get("/escalated/admin/settings"))
			for _, k := range list(t, settings.Props, "supported_settings") {
				if k == "two_factor_required" {
					t.Error("supported_settings offers two_factor_required with the switch off")
				}
			}
			if rec := b.get("/escalated/two-factor/challenge"); rec.Code != http.StatusNotFound {
				t.Errorf("challenge after posting the setting: %d", rec.Code)
			}
		})
	}
}

func TestTwoFactorNeedsTheSessionAndASecret(t *testing.T) {
	on := true
	for what, configure := range map[string]func(*escalated.Config){
		"no session id": func(cfg *escalated.Config) { cfg.Access.TwoFactor = &on; cfg.Access.Secret = "x" },
		"no secret": func(cfg *escalated.Config) {
			cfg.Access.TwoFactor = &on
			cfg.Access.SessionID = func(*http.Request) string { return "s" }
		},
	} {
		cfg := escalated.DefaultConfig()
		cfg.DB = newUIFixture(t).esc.Config.DB
		configure(&cfg)
		if _, err := escalated.New(cfg); err == nil {
			t.Errorf("%s: New accepted two-factor without it", what)
		}
	}
}

func TestTwoFactorChallengesEnrolledStaff(t *testing.T) {
	for _, name := range []string{"chi", "stdlib"} {
		t.Run(name, func(t *testing.T) {
			f := newUIFixture(t, twoFactorOn(true))
			h := f.mounts()[name]
			secret, _ := enrol(t, f.esc, uiAgent)

			admin := newBrowser(h, uiAgent, "admin", "s1")
			wantLocation(t, admin.get("/escalated/admin/departments"), "admin page", "/escalated/two-factor/challenge")
			agent := newBrowser(h, uiAgent, "agent", "s2")
			wantLocation(t, agent.get("/escalated/agent/tickets?status=open"), "agent page", "/escalated/two-factor/challenge")

			// A JSON client gets 403 with two_factor_required.
			rec := agent.do(http.MethodGet, "/escalated/api/tickets", "", map[string]string{"X-Inertia": "", "Accept": "application/json"})
			if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), `"two_factor_required":true`) {
				t.Errorf("JSON client: %d %s", rec.Code, rec.Body.String())
			}

			// The challenge page.
			page := pageOf(t, agent.get("/escalated/two-factor/challenge"))
			if page.Component != "Escalated/Auth/TwoFactorChallenge" {
				t.Fatalf("challenge component %q", page.Component)
			}
			if page.Props["action"] != "/escalated/two-factor/challenge" {
				t.Errorf("action %v", page.Props["action"])
			}
			hasKeys(t, "challenge", page.Props, "error")

			// A wrong code is an error on code.
			wrong := pageOf(t, agent.post("/escalated/two-factor/challenge", `{"code":"000000"}`))
			hasKeys(t, "errors", obj(t, wrong.Props, "errors"), "code")

			// A valid code returns to the page asked for.
			code := nowCode(secret)
			wantLocation(t, agent.post("/escalated/two-factor/challenge", `{"code":"`+code+`"}`), "verify", "/escalated/agent/tickets?status=open")
			if rec := agent.get("/escalated/agent/tickets"); rec.Code != http.StatusOK {
				t.Errorf("after passing: %d", rec.Code)
			}
			// The pass is this session's and this user's.
			if rec := admin.get("/escalated/admin/departments"); rec.Code != http.StatusFound {
				t.Errorf("another session passed too: %d", rec.Code)
			}
			stolen := newBrowser(h, "903", "agent", "s2")
			stolen.cookies = agent.cookies
			enrol(t, f.esc, "903")
			if rec := stolen.get("/escalated/agent/tickets"); rec.Code != http.StatusFound {
				t.Errorf("another user's pass counted: %d", rec.Code)
			}

			// The same code (same time step) is refused afterwards.
			again := pageOf(t, admin.post("/escalated/two-factor/challenge", `{"code":"`+code+`"}`))
			hasKeys(t, "errors", obj(t, again.Props, "errors"), "code")

			// A new login (a new session id) is challenged again.
			agent.session = "s3"
			wantLocation(t, agent.get("/escalated/agent/tickets"), "new login", "/escalated/two-factor/challenge")
		})
	}
}

func TestTwoFactorRecoveryCodesWorkOnce(t *testing.T) {
	for _, name := range []string{"chi", "stdlib"} {
		t.Run(name, func(t *testing.T) {
			f := newUIFixture(t, twoFactorOn(true))
			h := f.mounts()[name]
			_, codes := enrol(t, f.esc, uiAgent)

			b := newBrowser(h, uiAgent, "agent", "s1")
			wantLocation(t, b.post("/escalated/two-factor/challenge", `{"recovery_code":"`+strings.ToLower(codes[0])+`"}`), "recovery", "/escalated/agent")
			if rec := b.get("/escalated/agent/tickets"); rec.Code != http.StatusOK {
				t.Errorf("after a recovery code: %d", rec.Code)
			}
			other := newBrowser(h, uiAgent, "agent", "s2")
			page := pageOf(t, other.post("/escalated/two-factor/challenge", `{"recovery_code":"`+codes[0]+`"}`))
			hasKeys(t, "errors", obj(t, page.Props, "errors"), "recovery_code")
			e, _ := f.esc.TwoFactor().Find(context.Background(), uiAgent)
			if len(e.RecoveryCodes) != 7 {
				t.Errorf("recovery codes left: %d", len(e.RecoveryCodes))
			}
		})
	}
}

func TestTwoFactorVerifyIsThrottled(t *testing.T) {
	f := newUIFixture(t, twoFactorOn(true))
	enrol(t, f.esc, uiAgent)
	// Both routers share the limit: it is the Escalated's.
	mounts := f.mounts()
	for i := 0; i < 5; i++ {
		h := mounts[map[bool]string{true: "chi", false: "stdlib"}[i%2 == 0]]
		if rec := newBrowser(h, uiAgent, "agent", "s1").post("/escalated/two-factor/challenge", `{"code":"000000"}`); rec.Code == http.StatusTooManyRequests {
			t.Fatalf("attempt %d throttled", i+1)
		}
	}
	rec := newBrowser(mounts["chi"], uiAgent, "agent", "s1").post("/escalated/two-factor/challenge", `{"code":"000000"}`)
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Errorf("6th attempt: %d (Retry-After %q)", rec.Code, rec.Header().Get("Retry-After"))
	}
	// Per user: someone else is not held up.
	enrol(t, f.esc, "903")
	if rec := newBrowser(mounts["chi"], "903", "agent", "s9").post("/escalated/two-factor/challenge", `{"code":"000000"}`); rec.Code == http.StatusTooManyRequests {
		t.Error("another user was throttled")
	}
}

func TestTwoFactorAgentsEnrolThroughTheScreen(t *testing.T) {
	for _, name := range []string{"chi", "stdlib"} {
		t.Run(name, func(t *testing.T) {
			f := newUIFixture(t, twoFactorOn(true))
			h := f.mounts()[name]
			b := newBrowser(h, uiAgent, "agent", "s1") // an agent, not an admin

			page := pageOf(t, b.get("/escalated/admin/settings/two-factor"))
			if page.Component != "Escalated/Admin/Settings/TwoFactor" {
				t.Fatalf("component %q", page.Component)
			}
			if page.Props["enabled"] != false || page.Props["pending"] != false || page.Props["required"] != false {
				t.Errorf("props %v", page.Props)
			}
			if obj(t, sharedEscalated(t, page), "features")["two_factor"] != true {
				t.Error("features.two_factor is not true")
			}

			rec := b.post("/escalated/admin/settings/two-factor/setup", "")
			if !strings.Contains(rec.Header().Get("Cache-Control"), "no-store") {
				t.Error("the setup response may be cached")
			}
			setup := obj(t, obj(t, pageOf(t, rec).Props, "flash"), "two_factor_setup")
			secret := secretOf(t, setup["qr_uri"].(string))
			if codes := list(t, setup, "recovery_codes"); len(codes) != 8 {
				t.Errorf("recovery codes %v", codes)
			}
			if !strings.Contains(setup["qr_uri"].(string), "Escalated:ada@example.com") {
				t.Errorf("qr_uri does not name the account: %v", setup["qr_uri"])
			}
			pending := pageOf(t, b.get("/escalated/admin/settings/two-factor"))
			if pending.Props["pending"] != true || strings.Contains(mustJSON(t, pending), secret) {
				t.Errorf("pending page %v (or it carries the secret)", pending.Props)
			}
			// Sealed at rest.
			var stored string
			if err := f.esc.Config.DB.QueryRow(`SELECT secret FROM escalated_two_factors WHERE user_id = ` + uiAgent).Scan(&stored); err != nil {
				t.Fatal(err)
			}
			if stored == secret || strings.Contains(stored, secret) {
				t.Error("the TOTP secret is stored in the clear")
			}

			bad := pageOf(t, b.post("/escalated/admin/settings/two-factor/confirm", `{"code":"000000"}`))
			hasKeys(t, "errors", obj(t, bad.Props, "errors"), "code")
			confirmed := pageOf(t, b.post("/escalated/admin/settings/two-factor/confirm", `{"code":"`+nowCode(secret)+`"}`))
			flash := obj(t, confirmed.Props, "flash")
			if len(list(t, obj(t, flash, "two_factor_confirmed"), "recovery_codes")) != 8 {
				t.Errorf("confirmed flash %v", flash)
			}
			if confirmed.Props["enabled"] != true {
				t.Errorf("enabled after confirm: %v", confirmed.Props["enabled"])
			}
			// Confirming passes the challenge for this session.
			if rec := b.get("/escalated/agent/tickets"); rec.Code != http.StatusOK {
				t.Errorf("after confirming: %d", rec.Code)
			}

			// Disable removes it.
			if rec := b.post("/escalated/admin/settings/two-factor/disable", ""); rec.Code != http.StatusSeeOther {
				t.Errorf("disable: %d", rec.Code)
			}
			if e, _ := f.esc.TwoFactor().Find(context.Background(), uiAgent); e != nil {
				t.Error("the enrolment is still there")
			}
			// Customers have no enrolment screen.
			if rec := newBrowser(h, customerB, "", "c1").get("/escalated/admin/settings/two-factor"); rec.Code != http.StatusForbidden {
				t.Errorf("customer on the enrolment screen: %d", rec.Code)
			}
		})
	}
}

func TestTwoFactorRequiredSendsStaffToEnrol(t *testing.T) {
	for _, name := range []string{"chi", "stdlib"} {
		t.Run(name, func(t *testing.T) {
			f := newUIFixture(t, twoFactorOn(true))
			h := f.mounts()[name]
			agent := newBrowser(h, "904", "agent", "s1")

			// Not required: unenrolled staff pass.
			if rec := agent.get("/escalated/agent/tickets"); rec.Code != http.StatusOK {
				t.Fatalf("not required: %d", rec.Code)
			}

			// An admin requires it from the settings page, posting only that key
			// (as Admin/Settings/TwoFactor does).
			admin := newBrowser(h, uiAgent, "admin", "a1")
			settings := pageOf(t, admin.get("/escalated/admin/settings"))
			found := false
			for _, k := range list(t, settings.Props, "supported_settings") {
				found = found || k == "two_factor_required"
			}
			if !found || obj(t, settings.Props, "settings")["two_factor_required"] != false {
				t.Errorf("settings page does not offer two_factor_required: %v", settings.Props)
			}
			rec := admin.do(http.MethodPost, "/escalated/admin/settings", `{"two_factor_required":true}`, map[string]string{"Referer": "/escalated/admin/settings/two-factor"})
			wantLocation(t, rec, "partial settings post", "/escalated/admin/settings/two-factor")
			if f.setting(t, "two_factor_required") != "1" {
				t.Fatalf("two_factor_required stored as %q", f.setting(t, "two_factor_required"))
			}

			wantLocation(t, agent.get("/escalated/agent/tickets"), "required, unenrolled", "/escalated/admin/settings/two-factor")
			wantLocation(t, agent.post("/escalated/agent/tickets/bulk", `{}`), "required, a write", "/escalated/admin/settings/two-factor")
			page := pageOf(t, agent.get("/escalated/admin/settings/two-factor"))
			if page.Props["required"] != true || sharedEscalated(t, page)["two_factor_required"] != true {
				t.Errorf("enrolment page while required: %v", page.Props)
			}
			// Customers are not sent to enrol.
			if rec := newBrowser(h, customerB, "", "c1").get("/escalated/tickets"); rec.Code != http.StatusOK {
				t.Errorf("customer while required: %d", rec.Code)
			}

			// The admin is unenrolled staff too: now sent to enrol.
			wantLocation(t, admin.post("/escalated/admin/settings", `{"two_factor_required":false}`), "unenrolled admin while required", "/escalated/admin/settings/two-factor")

			// The setting wins over config, and config alone also requires it.
			if err := f.esc.Store.SetSetting(context.Background(), "two_factor_required", "0"); err != nil {
				t.Fatal(err)
			}
			if rec := agent.get("/escalated/agent/tickets"); rec.Code != http.StatusOK {
				t.Errorf("setting off: %d", rec.Code)
			}
			on := true
			g := newUIFixture(t, twoFactorOn(true), func(cfg *escalated.Config) { cfg.Access.TwoFactorRequired = &on })
			gb := newBrowser(g.mounts()[name], "904", "agent", "s1")
			wantLocation(t, gb.get("/escalated/agent/tickets"), "config requires", "/escalated/admin/settings/two-factor")
			if err := g.esc.Store.SetSetting(context.Background(), "two_factor_required", "0"); err != nil {
				t.Fatal(err)
			}
			if rec := gb.get("/escalated/agent/tickets"); rec.Code != http.StatusOK {
				t.Errorf("the setting (off) does not win over config (on): %d", rec.Code)
			}
		})
	}
}

func TestTwoFactorEnvSwitchAndTokens(t *testing.T) {
	t.Setenv("ESCALATED_TWO_FACTOR", "true")
	owners := &tokenOwners{roles: map[models.UserID]*handlers.APITokenOwner{uiAgent: {IsAgent: true, IsAdmin: true}}}
	f := newUIFixture(t, func(cfg *escalated.Config) {
		cfg.Access.Secret = "x"
		cfg.Access.SessionID = func(r *http.Request) string { return r.Header.Get(headerSession) }
		cfg.APITokensEnabled = true
		cfg.APITokenOwner = owners.lookup
	})
	if !f.esc.Features.TwoFactor {
		t.Fatal("ESCALATED_TWO_FACTOR=true did not switch it on")
	}
	r := chi.NewRouter()
	router.MountChi(r, f.esc)
	// Issue a token before enrolling, then enrol its owner: the token API is
	// not session-based and is not challenged.
	token := issue(t, r, `{"name":"CLI","user_id":900,"abilities":["*"]}`)
	enrol(t, f.esc, uiAgent)
	if rec := bearer(r, http.MethodGet, "/escalated/api/tickets", token, ""); rec.Code != http.StatusOK {
		t.Errorf("token request of an enrolled owner: %d", rec.Code)
	}
}

// Enrolment is a personal screen: with roles & permissions on it needs no
// permission, so staff without a role can still enrol.
func TestTwoFactorEnrolmentNeedsNoPermission(t *testing.T) {
	f := newUIFixture(t, twoFactorOn(true), rolesOn(true))
	if err := f.esc.Roles().Seed(context.Background()); err != nil {
		t.Fatal(err)
	}
	b := newBrowser(f.mounts()["chi"], staffNoRole, "agent", "s1")
	if rec := b.get("/escalated/admin/settings/two-factor"); rec.Code != http.StatusOK {
		t.Errorf("enrolment without a role: %d", rec.Code)
	}
	if rec := b.post("/escalated/admin/settings/two-factor/setup", ""); rec.Code != http.StatusOK {
		t.Errorf("setup without a role: %d", rec.Code)
	}
	if rec := b.get("/escalated/agent/tickets"); rec.Code != http.StatusForbidden {
		t.Errorf("tickets without a role: %d", rec.Code)
	}
}
