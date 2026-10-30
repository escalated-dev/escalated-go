// Package access holds the install-level access switches (roles &
// permissions, two-factor authentication, single sign-on) and the roles &
// permissions model they enforce, as escalated-developer-context ADR
// 2026-10-29-configurable-access-features describes and escalated-laravel
// implements.
//
// Each switch is read from configuration (Config, else its environment
// variable) and never from the settings table: each one changes who may sign
// in or what they may do, so no stored setting may flip it. All default off.
// Off, the feature's routes answer 404 and the shared `features` prop says it
// is off; on, it is enforced.
package access

import (
	"context"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/escalated-dev/escalated-go/models"
)

// The environment variables a nil Config field reads.
const (
	EnvRolesPermissions  = "ESCALATED_ROLES_PERMISSIONS"
	EnvTwoFactor         = "ESCALATED_TWO_FACTOR"
	EnvTwoFactorRequired = "ESCALATED_TWO_FACTOR_REQUIRED"
	EnvSSO               = "ESCALATED_SSO"
)

// Config holds the install-level switches. A nil switch reads its
// environment variable (EnvRolesPermissions, EnvTwoFactor, EnvSSO); unset or
// unparsable there, it is off.
type Config struct {
	// RolesPermissions enforces Escalated roles & permissions: every admin,
	// agent and staff API route also needs its permission from the user's
	// roles (RoutePermissions). Off, AdminCheck and AgentCheck decide
	// everything, as before.
	RolesPermissions *bool

	// TwoFactor enforces two-factor authentication: staff with a confirmed
	// TOTP enrolment are challenged after the host's login before they reach
	// an admin or agent page. It needs SessionID and a secret
	// (Secret, else GuestAccess.Secret); New fails without them.
	TwoFactor *bool

	// TwoFactorRequired makes every staff user enrol before using anything
	// else. Nil reads EnvTwoFactorRequired. The two_factor_required setting
	// (Admin > Settings) wins when an admin has saved it.
	TwoFactorRequired *bool

	// Secret seals TOTP secrets and recovery codes and signs the access
	// cookies (the two-factor pass). Keep it stable: changing it voids every
	// enrolment. Empty uses Config.GuestAccess.Secret.
	Secret string

	// TwoFactorIssuer names the account in authenticator apps. Empty is
	// "Escalated".
	TwoFactorIssuer string

	// SessionID returns the host's session id for the request. It must
	// change when someone signs in (most session libraries renew it on
	// login), so every new login is challenged again: the challenge's pass
	// mark is bound to it. Required with TwoFactor.
	SessionID func(*http.Request) string

	// RegenerateSession, optional, gives the host session a new id after a
	// passed challenge (as Laravel's session()->regenerate()) and returns the
	// new id.
	RegenerateSession func(http.ResponseWriter, *http.Request) (string, error)

	// SSO enforces single sign-on through the provider an admin picks in
	// Admin > Settings > SSO (SAML, OAuth 2 / OIDC or JWT), at
	// {RoutePrefix}/sso/login. It needs SSOOptions.SignIn,
	// Config.UserIDByEmail and an absolute base URL; New fails without them.
	SSO *bool

	// SSOOptions are the install-level rules around SSO.
	SSOOptions SSOOptions
}

// SSOOptions configure single sign-on. A nil or empty field reads its
// environment variable (ESCALATED_SSO_*), as escalated-laravel's
// escalated.sso config.
type SSOOptions struct {
	// BaseURL is the public origin the IdP reaches this app at
	// (https://support.example.com). Empty uses Notifications.BaseURL, else
	// ESCALATED_SSO_BASE_URL.
	BaseURL string
	// SPEntityID is the SAML SP entity ID. Empty is the SP metadata URL
	// (ESCALATED_SSO_SP_ENTITY_ID).
	SPEntityID string
	// AllowIdPInitiated accepts SAML responses that answer no AuthnRequest
	// (ESCALATED_SSO_ALLOW_IDP_INITIATED). Default false.
	AllowIdPInitiated *bool
	// JWTAudience is the aud a JWT must carry. Empty is BaseURL
	// (ESCALATED_SSO_JWT_AUDIENCE).
	JWTAudience string
	// ClockSkewSeconds is the drift allowed on SAML and JWT validity
	// windows: default 120, at most 300 (ESCALATED_SSO_CLOCK_SKEW).
	ClockSkewSeconds *int
	// CreateUsers creates a host user for an unknown email through
	// CreateUser (ESCALATED_SSO_CREATE_USERS). Off, unknown emails are
	// refused.
	CreateUsers *bool
	// SyncRoles, with roles & permissions on, replaces the user's Escalated
	// roles with the role the IdP asserts, by slug (ESCALATED_SSO_SYNC_ROLES).
	SyncRoles *bool
	// RedirectTo is where people land after signing in. Empty is the agent
	// dashboard, or the admin reports for admin-only users
	// (ESCALATED_SSO_REDIRECT_TO).
	RedirectTo string

	// SignIn signs the user in on the host's session, renewing the session
	// id. Required: Escalated owns no sessions.
	SignIn func(w http.ResponseWriter, r *http.Request, userID models.UserID) error
	// CreateUser creates a host user with that email and name and an
	// unusable password, and returns its id. Required with CreateUsers.
	CreateUser func(ctx context.Context, email, name string) (models.UserID, error)
	// HTTPClient makes the OAuth token and userinfo requests. Nil is
	// http.DefaultClient.
	HTTPClient *http.Client
}

// Resolved fills SSOOptions from the environment where unset.
func (o SSOOptions) Resolved() SSOOptions {
	str := func(v *string, env string) {
		if *v == "" {
			*v = strings.TrimSpace(os.Getenv(env))
		}
	}
	str(&o.BaseURL, "ESCALATED_SSO_BASE_URL")
	o.BaseURL = strings.TrimRight(o.BaseURL, "/")
	str(&o.SPEntityID, "ESCALATED_SSO_SP_ENTITY_ID")
	str(&o.JWTAudience, "ESCALATED_SSO_JWT_AUDIENCE")
	if o.JWTAudience == "" {
		o.JWTAudience = o.BaseURL
	}
	str(&o.RedirectTo, "ESCALATED_SSO_REDIRECT_TO")
	for _, b := range []struct {
		v   **bool
		env string
	}{{&o.AllowIdPInitiated, "ESCALATED_SSO_ALLOW_IDP_INITIATED"}, {&o.CreateUsers, "ESCALATED_SSO_CREATE_USERS"}, {&o.SyncRoles, "ESCALATED_SSO_SYNC_ROLES"}} {
		v := flag(*b.v, b.env)
		*b.v = &v
	}
	skew := 120
	if o.ClockSkewSeconds != nil {
		skew = *o.ClockSkewSeconds
	} else if n, err := strconv.Atoi(strings.TrimSpace(os.Getenv("ESCALATED_SSO_CLOCK_SKEW"))); err == nil {
		skew = n
	}
	skew = max(0, min(300, skew))
	o.ClockSkewSeconds = &skew
	return o
}

// ClockSkew is ClockSkewSeconds as a duration (after Resolved).
func (o SSOOptions) ClockSkew() time.Duration {
	if o.ClockSkewSeconds == nil {
		return 120 * time.Second
	}
	return time.Duration(*o.ClockSkewSeconds) * time.Second
}

// Features are the resolved switches.
type Features struct {
	RolesPermissions bool
	TwoFactor        bool
	SSO              bool
}

// Resolve reads the switches: each Config field, else its environment
// variable.
func (c Config) Resolve() Features {
	return Features{
		RolesPermissions: flag(c.RolesPermissions, EnvRolesPermissions),
		TwoFactor:        flag(c.TwoFactor, EnvTwoFactor),
		SSO:              flag(c.SSO, EnvSSO),
	}
}

// Shared is the `features` prop every page carries.
func (f Features) Shared() map[string]any {
	return map[string]any{
		"roles_permissions": f.RolesPermissions,
		"two_factor":        f.TwoFactor,
		"sso":               f.SSO,
	}
}

func flag(v *bool, env string) bool {
	if v != nil {
		return *v
	}
	raw := strings.ToLower(strings.TrimSpace(os.Getenv(env)))
	switch raw {
	case "on", "yes":
		return true
	}
	b, err := strconv.ParseBool(raw)
	return err == nil && b
}

// RequireFeature answers 404 while a feature is off, the way the disabled
// guest, widget and chat routes do, so its routes stay mounted and a switch
// is all that changes.
func RequireFeature(enabled bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !enabled {
				http.NotFound(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// TwoFactorRequiredDefault is TwoFactorRequired, else EnvTwoFactorRequired.
func (c Config) TwoFactorRequiredDefault() bool {
	return flag(c.TwoFactorRequired, EnvTwoFactorRequired)
}
