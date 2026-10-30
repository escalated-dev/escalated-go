package access

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/escalated-dev/escalated-go/models"
)

// The challenge cookies. The pass mark is bound to the user and the host's
// session id; the intended URL is a same-origin path.
const (
	passCookie     = "escalated_two_factor"
	intendedCookie = "escalated_two_factor_intended"
)

// TwoFactorGate challenges staff with a confirmed enrolment on the admin,
// agent and staff API routes until this session has passed the challenge,
// and, when two-factor is required, sends staff without an enrolment to
// enrol. It does nothing while two-factor is off, and never challenges an
// API token request (the token API is not session-based).
type TwoFactorGate struct {
	Prefix   string
	Enabled  bool
	Service  *TwoFactor
	Key      []byte
	UserID   func(*http.Request) models.UserID
	IsStaff  func(*http.Request) bool
	IsToken  func(*http.Request) bool
	Required func(*http.Request) bool
	// SessionID is the host's session id; it must change when someone signs
	// in or out. The pass mark is bound to it.
	SessionID func(*http.Request) string
	// RegenerateSession, when set, gives the session a new id after a passed
	// challenge (as Laravel's session()->regenerate()) and returns it.
	RegenerateSession func(http.ResponseWriter, *http.Request) (string, error)
	// Logf reports refusals and errors. Nil uses the log package.
	Logf func(format string, args ...any)

	limiter *attempts
}

// VerifyAttemptsPerMinute is the challenge's limit per user.
const VerifyAttemptsPerMinute = 5

// ChallengePath and EnrolmentPath are under the route prefix.
const (
	ChallengePath = "/two-factor/challenge"
	EnrolmentPath = "/admin/settings/two-factor"
)

// Middleware is the gate as net/http middleware.
func (g *TwoFactorGate) Middleware(next http.Handler) http.Handler {
	if g == nil || !g.Enabled {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, ok := strings.CutPrefix(r.URL.Path, g.Prefix)
		if !ok || !Governed(path) || (g.IsToken != nil && g.IsToken(r)) {
			next.ServeHTTP(w, r)
			return
		}
		uid := g.UserID(r)
		if uid.Empty() {
			next.ServeHTTP(w, r)
			return
		}
		enrolment, err := g.Service.Confirmed(r.Context(), uid)
		if err != nil {
			g.logf("escalated: reading the two-factor enrolment of user %s: %v", uid, err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		if enrolment != nil {
			if g.Passed(r, uid) {
				next.ServeHTTP(w, r)
				return
			}
			g.deny(w, r, ChallengePath, "Two-factor authentication is required.")
			return
		}
		enrolling := normalize(path) == EnrolmentPath || strings.HasPrefix(normalize(path), EnrolmentPath+"/")
		if !enrolling && g.Required != nil && g.Required(r) && g.IsStaff != nil && g.IsStaff(r) {
			g.deny(w, r, EnrolmentPath, "Set up two-factor authentication to continue.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (g *TwoFactorGate) deny(w http.ResponseWriter, r *http.Request, to, message string) {
	if r.Header.Get("X-Inertia") == "" && strings.Contains(r.Header.Get("Accept"), "json") {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]any{"message": message, "two_factor_required": true})
		return
	}
	code := http.StatusSeeOther
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		g.RememberIntended(w, r)
		code = http.StatusFound
	}
	http.Redirect(w, r, g.Prefix+to, code)
}

// Passed reports whether this session has passed the challenge for uid.
func (g *TwoFactorGate) Passed(r *http.Request, uid models.UserID) bool {
	if g.SessionID == nil {
		return false
	}
	c, err := r.Cookie(passCookie)
	if err != nil {
		return false
	}
	return validMark(g.Key, c.Value, uid, g.SessionID(r))
}

// MarkPassed records that this session passed the challenge for uid,
// regenerating the host session first when RegenerateSession is set.
func (g *TwoFactorGate) MarkPassed(w http.ResponseWriter, r *http.Request, uid models.UserID) error {
	session := ""
	if g.SessionID != nil {
		session = g.SessionID(r)
	}
	if g.RegenerateSession != nil {
		renewed, err := g.RegenerateSession(w, r)
		if err != nil {
			return err
		}
		session = renewed
	}
	http.SetCookie(w, g.cookie(r, passCookie, passMark(g.Key, uid, session), 0))
	return nil
}

// Forget clears the pass mark (for a host's logout handler; a new session
// id already invalidates it).
func (g *TwoFactorGate) Forget(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, g.cookie(r, passCookie, "", -1))
}

// RememberIntended keeps the URL a GET asked for, to return to it after the
// challenge.
func (g *TwoFactorGate) RememberIntended(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, g.cookie(r, intendedCookie, r.URL.RequestURI(), 600))
}

// TakeIntended returns the remembered URL once (a local path only), or "".
func (g *TwoFactorGate) TakeIntended(w http.ResponseWriter, r *http.Request) string {
	c, err := r.Cookie(intendedCookie)
	if err != nil {
		return ""
	}
	http.SetCookie(w, g.cookie(r, intendedCookie, "", -1))
	if !strings.HasPrefix(c.Value, "/") || strings.HasPrefix(c.Value, "//") || strings.HasPrefix(c.Value, "/\\") {
		return ""
	}
	return c.Value
}

// AllowVerify counts a challenge attempt for uid: false (with how long to
// wait) past VerifyAttemptsPerMinute.
func (g *TwoFactorGate) AllowVerify(uid models.UserID) (bool, time.Duration) {
	if g.limiter == nil {
		g.limiter = &attempts{limit: VerifyAttemptsPerMinute, now: time.Now}
	}
	return g.limiter.allow(string(uid))
}

// RetryAfter is the Retry-After header value for a wait.
func RetryAfter(wait time.Duration) string {
	seconds := int(wait.Seconds()) + 1
	return strconv.Itoa(seconds)
}

func (g *TwoFactorGate) cookie(r *http.Request, name, value string, maxAge int) *http.Cookie {
	path := g.Prefix
	if path == "" {
		path = "/"
	}
	return &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     path,
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https"),
		SameSite: http.SameSiteLaxMode,
	}
}

func (g *TwoFactorGate) logf(format string, args ...any) {
	if g.Logf != nil {
		g.Logf(format, args...)
		return
	}
	log.Printf(format, args...)
}
