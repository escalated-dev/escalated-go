package handlers

import (
	"context"
	"log"
	"net/http"
	"strings"

	"github.com/escalated-dev/escalated-go/access"
	"github.com/escalated-dev/escalated-go/models"
)

// TwoFactorHandler serves the two-factor screens (escalated-laravel's
// Admin\TwoFactorController and TwoFactorChallengeController): enrolment at
// Admin/Settings/TwoFactor, open to every staff user, and the challenge at
// Auth/TwoFactorChallenge. The router mounts it only behind the two-factor
// switch, which answers 404 while it is off.
//
// There is no session to flash through, so setup and confirm answer with the
// page itself, carrying flash.two_factor_setup / two_factor_confirmed, and
// are never cached. The secret and the codes are not stored anywhere a later
// page could read them.
type TwoFactorHandler struct {
	Service *access.TwoFactor
	Gate    *access.TwoFactorGate
	Pages   PageSupport
	// Required is whether staff must enrol (setting, else config).
	Required func(context.Context) bool
	// IsAgent picks where a passed challenge goes when nothing was asked
	// for: the agent dashboard for agents, else the admin reports.
	IsAgent func(*http.Request) bool
	// Email labels the account in authenticator apps.
	Email func(context.Context, models.UserID) (string, error)
}

const (
	twoFactorPage = "Escalated/Admin/Settings/TwoFactor"
	challengePage = "Escalated/Auth/TwoFactorChallenge"
)

// Page handles GET /admin/settings/two-factor
// (escalated.admin.two-factor.index).
func (h *TwoFactorHandler) Page(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, nil, nil)
}

func (h *TwoFactorHandler) render(w http.ResponseWriter, r *http.Request, flash map[string]any, errs map[string]string) {
	uid := h.Pages.currentUser(r)
	enrolment, err := h.Service.Find(r.Context(), uid)
	if err != nil {
		h.fail(w, "read the enrolment", err)
		return
	}
	props := map[string]any{
		"enabled":  enrolment != nil && enrolment.Confirmed,
		"pending":  enrolment != nil && !enrolment.Confirmed,
		"required": h.Required != nil && h.Required(r.Context()),
	}
	if flash != nil {
		props["flash"] = flash
		w.Header().Set("Cache-Control", "no-store")
	}
	h.Pages.render(w, r, twoFactorPage, props, errs)
}

// Setup handles POST /admin/settings/two-factor/setup: a new pending secret
// and recovery codes, flashed as two_factor_setup {qr_uri, recovery_codes}.
func (h *TwoFactorHandler) Setup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := h.Pages.currentUser(r)
	if enrolment, err := h.Service.Confirmed(ctx, uid); err != nil {
		h.fail(w, "read the enrolment", err)
		return
	} else if enrolment != nil {
		validationErrors(w, r, map[string]string{"code": "Two-factor authentication is already enabled. Disable it first to set it up again."}, func(errs map[string]string) {
			h.render(w, r, nil, errs)
		})
		return
	}
	email := ""
	if h.Email != nil {
		email, _ = h.Email(ctx, uid)
	}
	uri, codes, err := h.Service.Setup(ctx, uid, email)
	if err != nil {
		h.fail(w, "set up two-factor", err)
		return
	}
	setup := map[string]any{"qr_uri": uri, "recovery_codes": codes}
	if !isInertia(r) {
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, map[string]any{"two_factor_setup": setup})
		return
	}
	h.render(w, r, map[string]any{"two_factor_setup": setup}, nil)
}

// Confirm handles POST /admin/settings/two-factor/confirm: a six-digit code
// confirms the pending enrolment, flashes two_factor_confirmed
// {recovery_codes}, and counts as passing the challenge for this session.
func (h *TwoFactorHandler) Confirm(w http.ResponseWriter, r *http.Request) {
	in, err := readInput(r)
	if err != nil {
		http.Error(w, "invalid input", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	uid := h.Pages.currentUser(r)
	code := strings.TrimSpace(in.Get("code"))
	rerender := func(errs map[string]string) { h.render(w, r, nil, errs) }
	if len(code) != 6 {
		validationErrors(w, r, map[string]string{"code": "The code field must be 6 characters."}, rerender)
		return
	}
	enrolment, err := h.Service.Find(ctx, uid)
	if err != nil {
		h.fail(w, "read the enrolment", err)
		return
	}
	if enrolment == nil || enrolment.Confirmed {
		validationErrors(w, r, map[string]string{"code": "No pending two-factor setup found."}, rerender)
		return
	}
	ok, err := h.Service.Confirm(ctx, enrolment, code)
	if err != nil {
		h.fail(w, "confirm two-factor", err)
		return
	}
	if !ok {
		validationErrors(w, r, map[string]string{"code": "Invalid verification code."}, rerender)
		return
	}
	// The code just entered proves the second factor for this session.
	if err := h.Gate.MarkPassed(w, r, uid); err != nil {
		h.fail(w, "renew the session", err)
		return
	}
	confirmed := map[string]any{"recovery_codes": enrolment.RecoveryCodes}
	if !isInertia(r) {
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, map[string]any{"two_factor_confirmed": confirmed})
		return
	}
	h.render(w, r, map[string]any{"success": "Two-factor authentication enabled.", "two_factor_confirmed": confirmed}, nil)
}

// Disable handles POST /admin/settings/two-factor/disable.
func (h *TwoFactorHandler) Disable(w http.ResponseWriter, r *http.Request) {
	if err := h.Service.Disable(r.Context(), h.Pages.currentUser(r)); err != nil {
		h.fail(w, "disable two-factor", err)
		return
	}
	if isInertia(r) {
		backTo(w, r, h.Pages.path(access.EnrolmentPath))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Challenge handles GET /two-factor/challenge
// (escalated.two-factor.challenge): Auth/TwoFactorChallenge with the URL to
// post to. Someone with nothing to prove goes where they were headed.
func (h *TwoFactorHandler) Challenge(w http.ResponseWriter, r *http.Request) {
	uid := h.Pages.currentUser(r)
	enrolment, err := h.Service.Confirmed(r.Context(), uid)
	if err != nil {
		h.fail(w, "read the enrolment", err)
		return
	}
	if enrolment == nil || h.Gate.Passed(r, uid) {
		seeOther(w, r, h.intended(w, r))
		return
	}
	h.challenge(w, r, nil)
}

func (h *TwoFactorHandler) challenge(w http.ResponseWriter, r *http.Request, errs map[string]string) {
	h.Pages.render(w, r, challengePage, map[string]any{
		"action": h.Pages.path(access.ChallengePath),
		"error":  "",
	}, errs)
}

// Verify handles POST /two-factor/challenge (escalated.two-factor.verify)
// with code or recovery_code: success marks this session for the user and
// goes to the page they asked for; failure is an error on the field sent.
// Five attempts a minute per user.
func (h *TwoFactorHandler) Verify(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := h.Pages.currentUser(r)
	if allowed, wait := h.Gate.AllowVerify(uid); !allowed {
		w.Header().Set("Retry-After", access.RetryAfter(wait))
		http.Error(w, "Too Many Attempts.", http.StatusTooManyRequests)
		return
	}
	enrolment, err := h.Service.Confirmed(ctx, uid)
	if err != nil {
		h.fail(w, "read the enrolment", err)
		return
	}
	if enrolment == nil {
		seeOther(w, r, h.intended(w, r))
		return
	}
	in, err := readInput(r)
	if err != nil {
		http.Error(w, "invalid input", http.StatusBadRequest)
		return
	}
	code := strings.TrimSpace(in.Get("code"))
	recovery := strings.TrimSpace(in.Get("recovery_code"))
	rerender := func(errs map[string]string) { h.challenge(w, r, errs) }
	switch {
	case code == "" && recovery == "":
		validationErrors(w, r, map[string]string{"code": "The code field is required when recovery code is not present."}, rerender)
		return
	case len(code) > 16 || len(recovery) > 64:
		validationErrors(w, r, map[string]string{"code": "That code is not valid."}, rerender)
		return
	}
	var passed bool
	field := "code"
	if recovery != "" {
		field = "recovery_code"
		passed, err = h.Service.UseRecoveryCode(ctx, enrolment, recovery)
	} else {
		passed, err = h.Service.VerifyAndConsume(ctx, enrolment, code)
	}
	if err != nil {
		h.fail(w, "check the code", err)
		return
	}
	if !passed {
		log.Printf("escalated: two-factor challenge failed for user %s from %s", uid, r.RemoteAddr)
		msg := "That code is not valid."
		if field == "recovery_code" {
			msg = "That recovery code is not valid."
		}
		validationErrors(w, r, map[string]string{field: msg}, rerender)
		return
	}
	if err := h.Gate.MarkPassed(w, r, uid); err != nil {
		h.fail(w, "renew the session", err)
		return
	}
	seeOther(w, r, h.intended(w, r))
}

// intended is the page asked for before the challenge, else the panel home.
func (h *TwoFactorHandler) intended(w http.ResponseWriter, r *http.Request) string {
	if to := h.Gate.TakeIntended(w, r); to != "" {
		return to
	}
	if h.IsAgent != nil && h.IsAgent(r) {
		return h.Pages.path("/agent")
	}
	return h.Pages.path("/admin/reports")
}

func (h *TwoFactorHandler) fail(w http.ResponseWriter, what string, err error) {
	log.Printf("escalated: could not %s: %v", what, err)
	http.Error(w, "Internal Server Error", http.StatusInternalServerError)
}
