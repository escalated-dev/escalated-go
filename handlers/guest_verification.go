package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/escalated-dev/escalated-go/middleware"
	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/services"
)

// GuestVerificationHandler serves POST verification (email a code) and POST
// lookup (find tickets by reference with a verified email) for the guest API
// and the widget. See docs/guest-access.md.
type GuestVerificationHandler struct {
	Access *services.GuestAccess

	// Allow applies the route's per-IP budget for scope and answers 429 when
	// it is spent. Nil means the router throttles the route itself.
	Allow func(w http.ResponseWriter, r *http.Request, scope middleware.GuestScope) bool

	// Enabled reports whether the routes are switched on (the widget's
	// Enabled flag). Nil means on.
	Enabled func() bool

	// ClientIP keys the per-client code budget. Nil means RemoteAddr's host.
	ClientIP func(r *http.Request) string

	// ChatEnabled reports whether live chat is on; a chat code is refused
	// with 403 while it is off. Nil means on.
	ChatEnabled func(r *http.Request) bool
}

// Request handles POST verification: {email, purpose} -> 202
// {verification_id, expires_in, message}. The code goes only to the mailbox.
func (h *GuestVerificationHandler) Request(w http.ResponseWriter, r *http.Request) {
	setGuestPrivacyHeaders(w)
	if !h.enabled(w) || !h.allow(w, r, middleware.GuestTicketScope) {
		return
	}
	var in struct {
		Email   string `json:"email"`
		Purpose string `json:"purpose"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeGuestValidation(w, map[string][]string{"email": {"Enter a valid email address."}})
		return
	}
	errs := map[string][]string{}
	if !services.ValidGuestEmail(in.Email) {
		errs["email"] = []string{"Enter a valid email address."}
	}
	if !services.ValidGuestPurpose(in.Purpose) {
		errs["purpose"] = []string{"The purpose must be ticket, chat or lookup."}
	}
	if len(errs) > 0 {
		writeGuestValidation(w, errs)
		return
	}
	if in.Purpose == services.GuestPurposeChat && h.ChatEnabled != nil && !h.ChatEnabled(r) {
		writeJSON(w, http.StatusForbidden, map[string]string{"message": "Live chat is not enabled."})
		return
	}

	id, err := h.Access.Challenge(r.Context(), in.Email, in.Purpose, h.clientIP(r))
	if !writeGuestAccessError(w, err) {
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"verification_id": id,
		"expires_in":      services.GuestCodeExpiresIn,
		"message":         "Check your email for a verification code.",
	})
}

// Lookup handles POST lookup: {email, reference, verification_id,
// verification_code} -> {data: [{reference, subject, guest_access_token,
// expires_at}]}. The code is consumed even when nothing matches.
func (h *GuestVerificationHandler) Lookup(w http.ResponseWriter, r *http.Request) {
	setGuestPrivacyHeaders(w)
	if !h.enabled(w) || !h.allow(w, r, middleware.GuestReplyScope) {
		return
	}
	var in struct {
		Email     string `json:"email"`
		Reference string `json:"reference"`
		guestProof
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeGuestValidation(w, map[string][]string{"reference": {"The reference field is required."}})
		return
	}
	errs := in.validate()
	if !services.ValidGuestEmail(in.Email) {
		errs["email"] = []string{"Enter a valid email address."}
	}
	if strings.TrimSpace(in.Reference) == "" || len(in.Reference) > 255 {
		errs["reference"] = []string{"The reference field is required."}
	}
	if len(errs) > 0 {
		writeGuestValidation(w, errs)
		return
	}
	if !writeGuestAccessError(w, h.Access.Consume(r.Context(), in.VerificationID, in.VerificationCode, in.Email, services.GuestPurposeLookup)) {
		return
	}
	matches, err := h.Access.Lookup(r.Context(), in.Email, in.Reference)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "Lookup failed."})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": matches})
}

func (h *GuestVerificationHandler) enabled(w http.ResponseWriter) bool {
	if h.Enabled == nil || h.Enabled() {
		return true
	}
	writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "widget is disabled", "message": "The widget is disabled."})
	return false
}

func (h *GuestVerificationHandler) allow(w http.ResponseWriter, r *http.Request, scope middleware.GuestScope) bool {
	return h.Allow == nil || h.Allow(w, r, scope)
}

func (h *GuestVerificationHandler) clientIP(r *http.Request) string {
	if h.ClientIP != nil {
		return h.ClientIP(r)
	}
	if i := strings.LastIndex(r.RemoteAddr, ":"); i > 0 {
		return r.RemoteAddr[:i]
	}
	return r.RemoteAddr
}

// guestProof is the email code a guest sends with a write.
type guestProof struct {
	VerificationID   string `json:"verification_id"`
	VerificationCode string `json:"verification_code"`
}

func (p guestProof) validate() map[string][]string {
	errs := map[string][]string{}
	if strings.TrimSpace(p.VerificationID) == "" {
		errs["verification_id"] = []string{"Request a verification code first."}
	}
	if strings.TrimSpace(p.VerificationCode) == "" || len(p.VerificationCode) > 16 {
		errs["verification_code"] = []string{"Enter the 8-digit code from your email."}
	}
	return errs
}

// createVerifiedGuestTicket consumes a ticket code for email, creates the
// ticket and issues its grant. A failed create hands the code back.
func createVerifiedGuestTicket(ctx context.Context, ga *services.GuestAccess, ts *services.TicketService, proof guestProof, email string, in services.CreateTicketInput) (*models.Ticket, string, time.Time, error) {
	if err := ga.Consume(ctx, proof.VerificationID, proof.VerificationCode, email, services.GuestPurposeTicket); err != nil {
		return nil, "", time.Time{}, err
	}
	normalized := services.NormalizeGuestEmail(email)
	in.GuestEmail = &normalized
	t, err := ts.Create(ctx, in)
	if err != nil {
		ga.Release(ctx, proof.VerificationID)
		return nil, "", time.Time{}, err
	}
	token, expires, err := ga.Issue(ctx, t.ID, normalized)
	if err != nil {
		return nil, "", time.Time{}, err
	}
	t.GuestToken = nil
	return t, token, expires, nil
}

// writeGuestAccessError answers a GuestAccess error and reports whether there
// was none (true: carry on).
func writeGuestAccessError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return true
	}
	var budget *services.GuestCodeBudgetError
	switch {
	case errors.As(err, &budget):
		secs := int((budget.RetryAfter + time.Second - 1) / time.Second)
		w.Header().Set("Retry-After", strconv.Itoa(secs))
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"message": "Please wait before requesting another code."})
	case errors.Is(err, services.ErrGuestEmailInvalid):
		writeGuestValidation(w, map[string][]string{"email": {"Enter a valid email address."}})
	case errors.Is(err, services.ErrGuestCodeInvalid):
		writeGuestValidation(w, map[string][]string{"verification_code": {"This code is invalid, expired or already used. Request a new code."}})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "Something went wrong. Please try again."})
	}
	return false
}

// writeGuestValidation answers 422 {message, errors} like Laravel validation.
func writeGuestValidation(w http.ResponseWriter, errs map[string][]string) {
	msg := "The given data was invalid."
	for _, field := range []string{"verification_code", "verification_id", "email", "purpose", "reference"} {
		if m, ok := errs[field]; ok && len(m) > 0 {
			msg = m[0]
			break
		}
	}
	writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"message": msg, "error": msg, "errors": errs})
}

// setGuestPrivacyHeaders keeps guest responses (which can carry grants) out
// of caches and referrers (Laravel GuestPrivacyHeaders).
func setGuestPrivacyHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
}

// GuestPrivacyHeaders sets the guest privacy headers before the wrapped
// handler runs, so every answer carries them: errors and 429s from the rate
// limiter included.
func GuestPrivacyHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setGuestPrivacyHeaders(w)
		next.ServeHTTP(w, r)
	})
}

// bearerToken returns the Authorization: Bearer value.
func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if len(h) > 7 && strings.EqualFold(h[:7], "Bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}
