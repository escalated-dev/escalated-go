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
	"net/http"
	"os"
	"strconv"
	"strings"
)

// The environment variables a nil Config field reads.
const (
	EnvRolesPermissions  = "ESCALATED_ROLES_PERMISSIONS"
	EnvTwoFactor         = "ESCALATED_TWO_FACTOR"
	EnvTwoFactorRequired = "ESCALATED_TWO_FACTOR_REQUIRED"
)

// Config holds the install-level switches. A nil switch reads its
// environment variable (EnvRolesPermissions, EnvTwoFactor); unset or
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
	// (TwoFactorSecret, else GuestAccess.Secret); New fails without them.
	TwoFactor *bool

	// TwoFactorRequired makes every staff user enrol before using anything
	// else. Nil reads EnvTwoFactorRequired. The two_factor_required setting
	// (Admin > Settings) wins when an admin has saved it.
	TwoFactorRequired *bool

	// TwoFactorSecret seals TOTP secrets and recovery codes and signs the
	// challenge cookie. Keep it stable: changing it voids every enrolment.
	// Empty uses Config.GuestAccess.Secret.
	TwoFactorSecret string

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
}

// Features are the resolved switches.
type Features struct {
	RolesPermissions bool
	TwoFactor        bool
	// SSO stays false until this package enforces it: a port must not show
	// a screen for a feature it does not enforce.
	SSO bool
}

// Resolve reads the switches: each Config field, else its environment
// variable.
func (c Config) Resolve() Features {
	return Features{
		RolesPermissions: flag(c.RolesPermissions, EnvRolesPermissions),
		TwoFactor:        flag(c.TwoFactor, EnvTwoFactor),
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
