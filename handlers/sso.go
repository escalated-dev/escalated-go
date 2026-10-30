package handlers

import (
	"context"
	"crypto/subtle"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/escalated-dev/escalated-go/access"
	"github.com/escalated-dev/escalated-go/models"
)

// SSOHandler serves single sign-on (escalated-laravel's SsoController and
// Admin\SsoSettingsController): the sign-in routes under /sso and Admin >
// Settings > SSO. The router mounts it only behind the SSO switch, which
// answers 404 while it is off; each sign-in route also answers 404 unless its
// provider is the one chosen.
type SSOHandler struct {
	SSO   *access.SSO
	Users *access.SSOUsers
	// SignIn signs the user in on the host's session (SSOOptions.SignIn).
	SignIn func(http.ResponseWriter, *http.Request, models.UserID) error
	// Key seals the OAuth state cookie.
	Key []byte
	// RedirectTo is where people land after signing in; empty picks the
	// agent dashboard or admin reports from Owner.
	RedirectTo string
	Owner      APITokenOwnerFunc
	Settings   interface {
		GetSetting(ctx context.Context, key string) (string, error)
		SetSetting(ctx context.Context, key, value string) error
	}
	Pages PageSupport
}

const (
	ssoSettingsPage = "Escalated/Admin/Settings/SsoSettings"
	oauthCookie     = "escalated_sso_oauth"
	secretMask      = "********"
)

func (h *SSOHandler) requireProvider(w http.ResponseWriter, r *http.Request, provider string) bool {
	if h.SSO.Provider(r.Context()) != provider {
		http.NotFound(w, r)
		return false
	}
	return true
}

// Login handles GET /sso/login (escalated.sso.login): start sign-in with the
// chosen provider; 404 with none.
func (h *SSOHandler) Login(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var (
		to  string
		err error
	)
	switch h.SSO.Provider(ctx) {
	case access.ProviderSAML:
		to, err = h.SSO.SAMLLoginURL(ctx)
	case access.ProviderOAuth:
		var state, verifier, sealed string
		to, state, verifier, err = h.SSO.OAuthStart(ctx)
		if err == nil {
			sealed, err = access.SealState(h.Key, state, verifier, time.Now().Add(access.SAMLRequestTTL))
		}
		if err == nil {
			http.SetCookie(w, h.cookie(r, sealed, int(access.SAMLRequestTTL.Seconds())))
		}
	case access.ProviderJWT:
		to, err = h.SSO.JWTLoginURL(ctx)
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		h.refuse(w, err)
		return
	}
	http.Redirect(w, r, to, http.StatusFound)
}

// Metadata handles GET /sso/saml/metadata (escalated.sso.saml.metadata).
func (h *SSOHandler) Metadata(w http.ResponseWriter, r *http.Request) {
	if !h.requireProvider(w, r, access.ProviderSAML) {
		return
	}
	body, err := h.SSO.SAMLMetadata(r.Context())
	if err != nil {
		log.Printf("escalated SSO: %v", err)
		http.Error(w, "SAML metadata is not available.", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/samlmetadata+xml")
	_, _ = w.Write(body)
}

// ACS handles POST /sso/saml/acs (escalated.sso.saml.acs). The IdP posts
// here cross-site; the signed, single-use assertion authenticates it.
func (h *SSOHandler) ACS(w http.ResponseWriter, r *http.Request) {
	if !h.requireProvider(w, r, access.ProviderSAML) {
		return
	}
	if err := r.ParseForm(); err != nil {
		h.refuse(w, err)
		return
	}
	h.signIn(w, r, func(ctx context.Context) (access.Identity, error) {
		return h.SSO.ValidateSAML(ctx, r.PostForm.Get("SAMLResponse"))
	})
}

// OAuthCallback handles GET /sso/oauth/callback
// (escalated.sso.oauth.callback): the state must match the one this browser
// was given (single use), and the code is exchanged with its PKCE verifier.
func (h *SSOHandler) OAuthCallback(w http.ResponseWriter, r *http.Request) {
	if !h.requireProvider(w, r, access.ProviderOAuth) {
		return
	}
	var state, verifier string
	if c, err := r.Cookie(oauthCookie); err == nil {
		state, verifier, _ = access.OpenState(h.Key, c.Value, time.Now())
	}
	// The state is single use: the cookie goes whatever happens next.
	http.SetCookie(w, h.cookie(r, "", -1))
	q := r.URL.Query()
	h.signIn(w, r, func(ctx context.Context) (access.Identity, error) {
		if e := q.Get("error"); e != "" {
			return access.Identity{}, &access.Refusal{Reason: "OAuth refused: the provider returned " + e}
		}
		if state == "" || subtle.ConstantTimeCompare([]byte(state), []byte(q.Get("state"))) != 1 {
			return access.Identity{}, &access.Refusal{Reason: "OAuth refused: missing or mismatched state"}
		}
		if q.Get("code") == "" || verifier == "" {
			return access.Identity{}, &access.Refusal{Reason: "OAuth refused: no authorization code or PKCE verifier"}
		}
		return h.SSO.OAuthIdentity(ctx, q.Get("code"), verifier)
	})
}

// JWTCallback handles GET|POST /sso/jwt/callback (escalated.sso.jwt.callback)
// with the token in `jwt`.
func (h *SSOHandler) JWTCallback(w http.ResponseWriter, r *http.Request) {
	if !h.requireProvider(w, r, access.ProviderJWT) {
		return
	}
	token := r.URL.Query().Get("jwt")
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err == nil && r.PostForm.Get("jwt") != "" {
			token = r.PostForm.Get("jwt")
		}
	}
	h.signIn(w, r, func(ctx context.Context) (access.Identity, error) {
		return h.SSO.ValidateJWT(ctx, token)
	})
}

func (h *SSOHandler) signIn(w http.ResponseWriter, r *http.Request, identify func(context.Context) (access.Identity, error)) {
	ctx := r.Context()
	id, err := identify(ctx)
	if err != nil {
		h.refuse(w, err)
		return
	}
	uid, err := h.Users.Resolve(ctx, id)
	if err != nil {
		h.refuse(w, err)
		return
	}
	if err := h.SignIn(w, r, uid); err != nil {
		h.refuse(w, err)
		return
	}
	log.Printf("escalated SSO: signed in user %s through %s", uid, h.SSO.Provider(ctx))
	http.Redirect(w, r, h.home(ctx, uid), http.StatusSeeOther)
}

func (h *SSOHandler) home(ctx context.Context, uid models.UserID) string {
	if h.RedirectTo != "" {
		return h.RedirectTo
	}
	if h.Owner != nil {
		if owner, err := h.Owner(ctx, uid); err == nil && owner != nil {
			switch {
			case owner.IsAgent:
				return h.Pages.path("/agent")
			case owner.IsAdmin:
				return h.Pages.path("/admin/reports")
			}
			return h.Pages.path("")
		}
	}
	return h.Pages.path("/agent")
}

// refuse answers every refusal the same generic way and logs the reason.
func (h *SSOHandler) refuse(w http.ResponseWriter, err error) {
	log.Printf("escalated SSO: %v", err)
	http.Error(w, "Single sign-on failed. Contact your administrator.", http.StatusForbidden)
}

func (h *SSOHandler) cookie(r *http.Request, value string, maxAge int) *http.Cookie {
	path := h.Pages.path("/sso")
	return &http.Cookie{
		Name:     oauthCookie,
		Value:    value,
		Path:     path,
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https"),
		SameSite: http.SameSiteLaxMode,
	}
}

// SettingsPage handles GET /admin/settings/sso (escalated.admin.settings.sso):
// the sso_* settings, secrets masked, and the endpoints to give the IdP.
func (h *SSOHandler) SettingsPage(w http.ResponseWriter, r *http.Request) {
	h.settingsPage(w, r, nil)
}

func (h *SSOHandler) settingsPage(w http.ResponseWriter, r *http.Request, errs map[string]string) {
	settings, err := h.SSO.Config(r.Context())
	if err != nil {
		http.Error(w, "could not read the SSO settings", http.StatusInternalServerError)
		return
	}
	props := map[string]any{}
	for k, v := range settings {
		props[k] = v
	}
	for _, key := range access.SSOSecretSettings {
		if settings[key] != "" {
			props[key] = secretMask
		}
	}
	h.Pages.render(w, r, ssoSettingsPage, map[string]any{"settings": props, "endpoints": h.SSO.EndpointList()}, errs)
}

// UpdateSettings handles POST /admin/settings/sso
// (escalated.admin.settings.sso.update), as Laravel validates it. A masked
// secret posted back keeps the stored one.
func (h *SSOHandler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	in, err := readInput(r)
	if err != nil {
		http.Error(w, "invalid input", http.StatusBadRequest)
		return
	}
	errs := map[string]string{}
	get := func(k string) string { return strings.TrimSpace(in.Get(k)) }
	provider := get("sso_provider")
	switch provider {
	case access.ProviderNone, access.ProviderSAML, access.ProviderOAuth, access.ProviderJWT:
	default:
		errs["sso_provider"] = "The selected sso provider is invalid."
	}
	urls := []string{"sso_url", "sso_login_url", "sso_logout_url", "sso_metadata_url", "sso_oauth_authorize_url", "sso_oauth_token_url", "sso_oauth_userinfo_url"}
	limits := map[string]int{
		"sso_entity_id": 500, "sso_url": 500, "sso_login_url": 500, "sso_logout_url": 500, "sso_metadata_url": 500,
		"sso_certificate": 10000, "sso_attr_email": 100, "sso_attr_name": 100, "sso_attr_role": 100,
		"sso_jwt_secret": 500, "sso_oauth_authorize_url": 500, "sso_oauth_token_url": 500, "sso_oauth_userinfo_url": 500,
		"sso_oauth_client_id": 255, "sso_oauth_client_secret": 500, "sso_oauth_scopes": 255,
	}
	for key, limit := range limits {
		if len(get(key)) > limit {
			errs[key] = "The " + strings.ReplaceAll(key, "_", " ") + " field is too long."
		}
	}
	for _, key := range urls {
		if v := get(key); v != "" {
			if u, err := url.Parse(v); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
				errs[key] = "The " + strings.ReplaceAll(key, "_", " ") + " field must be a valid URL."
			}
		}
	}
	if alg := get("sso_jwt_algorithm"); alg != "" {
		switch alg {
		case "HS256", "HS384", "HS512", "RS256", "RS384", "RS512":
		default:
			errs["sso_jwt_algorithm"] = "The selected sso jwt algorithm is invalid."
		}
	}
	required := map[string][]string{
		access.ProviderSAML:  {"sso_entity_id", "sso_login_url"},
		access.ProviderJWT:   {"sso_url", "sso_jwt_secret"},
		access.ProviderOAuth: {"sso_oauth_authorize_url", "sso_oauth_token_url", "sso_oauth_userinfo_url", "sso_oauth_client_id"},
	}
	for _, key := range required[provider] {
		if get(key) == "" {
			errs[key] = requiredMessage(strings.ReplaceAll(key, "_", " "))
		}
	}
	if len(errs) > 0 {
		validationErrors(w, r, errs, func(errs map[string]string) { h.settingsPage(w, r, errs) })
		return
	}
	masked := map[string]bool{}
	for _, key := range access.SSOSecretSettings {
		masked[key] = true
	}
	for key := range access.SSOSettingDefaults {
		if _, sent := in[key]; !sent {
			continue
		}
		v := get(key)
		if masked[key] && v == secretMask {
			continue
		}
		if err := h.Settings.SetSetting(r.Context(), key, v); err != nil {
			http.Error(w, "could not save the SSO settings", http.StatusInternalServerError)
			return
		}
	}
	if isInertia(r) {
		backTo(w, r, h.Pages.path("/admin/settings/sso"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
