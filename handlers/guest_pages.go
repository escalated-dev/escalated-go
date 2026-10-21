package handlers

import (
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/escalated-dev/escalated-go/internal/sqldialect"
	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/services"
	"github.com/escalated-dev/escalated-go/store"
	"github.com/escalated-dev/escalated-go/ticketaccess"
)

// GuestPages serves the guest ticket pages (Laravel Guest\TicketController)
// on the verified guest flow: Escalated/Guest/Create, which asks for an email
// code through /api/guest/verification before it posts, and
// Escalated/Guest/Show, opened by the expiring grant the ticket is created
// with. Mount it without the signed-in-user guard, behind the guest rate
// limits.
type GuestPages struct {
	Store   store.Store
	Tickets *services.TicketService
	Access  *services.GuestAccess
	DB      *sql.DB
	Prefix  string // table prefix
	Pages   PageSupport
}

func (h *GuestPages) presenter() *uiPresenter {
	return newUIPresenter(h.Store, h.Pages.Users, h.Pages.RoutePrefix)
}

func (h *GuestPages) showURL(token string) string {
	return h.Pages.path("/guest/" + url.PathEscape(token))
}

func (h *GuestPages) renderCreate(w http.ResponseWriter, r *http.Request, errs map[string]string) {
	setGuestPrivacyHeaders(w)
	h.Pages.render(w, r, "Escalated/Guest/Create", map[string]any{
		"departments":      uiDepartments(r.Context(), h.Store),
		"priorities":       uiPriorities(),
		"verification_url": h.Pages.path("/api/guest/verification"),
		"lookup_url":       h.Pages.path("/api/guest/lookup"),
	}, errs)
}

// Create handles GET /guest/create (escalated.guest.tickets.create).
func (h *GuestPages) Create(w http.ResponseWriter, r *http.Request) {
	h.renderCreate(w, r, nil)
}

// Submit handles POST /guest (escalated.guest.tickets.store): the form's
// fields plus the verification_id and verification_code the page got by
// email. A page visit goes on to the ticket, opened by its new grant.
func (h *GuestPages) Submit(w http.ResponseWriter, r *http.Request) {
	setGuestPrivacyHeaders(w)
	in, err := readInput(r)
	if err != nil {
		http.Error(w, "invalid input", http.StatusBadRequest)
		return
	}
	rerender := func(errs map[string]string) { h.renderCreate(w, r, errs) }

	errs := map[string]string{}
	name := strings.TrimSpace(in.Get("guest_name"))
	email := strings.TrimSpace(in.Get("guest_email"))
	subject := strings.TrimSpace(in.Get("subject"))
	description := strings.TrimSpace(in.Get("description"))
	proof := guestProof{VerificationID: strings.TrimSpace(in.Get("verification_id")), VerificationCode: strings.TrimSpace(in.Get("verification_code"))}
	if name == "" || len(name) > 255 {
		errs["guest_name"] = requiredMessage("name")
	}
	if !services.ValidGuestEmail(email) {
		errs["guest_email"] = "Enter a valid email address."
	}
	if subject == "" || len(subject) > 255 {
		errs["subject"] = requiredMessage("subject")
	}
	if description == "" || len(description) > 65535 {
		errs["description"] = requiredMessage("description")
	}
	for field, msgs := range proof.validate() {
		errs[field] = msgs[0]
	}
	priority := models.PriorityMedium
	if v := in.Get("priority"); v != "" {
		if p, ok := parsePriority(v); ok {
			priority = p
		} else {
			errs["priority"] = "The selected priority is invalid."
		}
	}
	var departmentID *int64
	if v := strings.TrimSpace(in.Get("department_id")); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		var dept *models.Department
		if err == nil {
			dept, _ = h.Store.GetDepartment(r.Context(), id)
		}
		if dept == nil {
			errs["department_id"] = "The selected department is invalid."
		} else {
			departmentID = &id
		}
	}
	for k, v := range rejectUploads(r) {
		errs[k] = v
	}
	if len(errs) > 0 {
		validationErrors(w, r, errs, rerender)
		return
	}
	if h.Access == nil {
		http.Error(w, "Guest tickets are not available.", http.StatusServiceUnavailable)
		return
	}

	_, token, _, err := createVerifiedGuestTicket(r.Context(), h.Access, h.Tickets, proof, email, services.CreateTicketInput{
		Subject:      subject,
		Description:  description,
		Priority:     priority,
		GuestName:    &name,
		DepartmentID: departmentID,
	})
	if err != nil {
		var budget *services.GuestCodeBudgetError
		switch {
		case errors.Is(err, services.ErrGuestCodeInvalid):
			validationErrors(w, r, map[string]string{"verification_code": "This code is invalid, expired or already used. Request a new code."}, rerender)
		case errors.Is(err, services.ErrGuestEmailInvalid):
			validationErrors(w, r, map[string]string{"guest_email": "Enter a valid email address."}, rerender)
		case errors.As(err, &budget):
			validationErrors(w, r, map[string]string{"verification_code": "Please wait before requesting another code."}, rerender)
		default:
			http.Error(w, "Something went wrong. Please try again.", http.StatusInternalServerError)
		}
		return
	}

	if isInertia(r) {
		seeOther(w, r, h.showURL(token))
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"guest_access_token": token, "url": h.showURL(token)})
}

// open loads the ticket a guest grant (or, while accepted, a legacy token)
// opens, answering 404 otherwise.
func (h *GuestPages) open(w http.ResponseWriter, r *http.Request) (*models.Ticket, string, bool) {
	setGuestPrivacyHeaders(w)
	token := pathParam(r, "token")
	if token == "" || h.Access == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return nil, "", false
	}
	id, err := h.Access.ResolveTicketID(r.Context(), token)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return nil, "", false
	}
	t, err := h.Store.GetTicket(r.Context(), id)
	if err != nil || t == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return nil, "", false
	}
	return t, token, true
}

func (h *GuestPages) renderShow(w http.ResponseWriter, r *http.Request, t *models.Ticket, token string, errs map[string]string) {
	payload := webTicketPayload(r, h.Store, h.presenter(), uiQueries{DB: h.DB, Prefix: h.Prefix}, t, false)
	delete(payload, "id")
	payload["guest_access_expires_at"] = h.grantExpiry(r, t.ID)
	h.Pages.render(w, r, "Escalated/Guest/Show", map[string]any{"ticket": payload, "token": token}, errs)
}

// grantExpiry is when the ticket's current grant expires (null for a legacy
// token).
func (h *GuestPages) grantExpiry(r *http.Request, ticketID int64) any {
	if h.DB == nil {
		return nil
	}
	prefix := h.Prefix
	if prefix == "" {
		prefix = "escalated_"
	}
	var expires sql.NullTime
	if err := ticketaccess.QueryRowContext(r.Context(), h.DB, sqldialect.Rebind(h.DB,
		`SELECT guest_access_expires_at FROM `+prefix+`tickets WHERE id = ?`), ticketID).Scan(&expires); err != nil || !expires.Valid {
		return nil
	}
	return uiTimeValue(expires.Time)
}

// Show handles GET /guest/{token} (escalated.guest.tickets.show): the
// ticket as Laravel's CustomerTicketPayload::guestWeb, and the token.
func (h *GuestPages) Show(w http.ResponseWriter, r *http.Request) {
	t, token, ok := h.open(w, r)
	if !ok {
		return
	}
	h.renderShow(w, r, t, token, nil)
}

// Reply handles POST /guest/{token}/reply (escalated.guest.tickets.reply).
// The grant is the guest's proof; a closed ticket takes no replies.
func (h *GuestPages) Reply(w http.ResponseWriter, r *http.Request) {
	t, token, ok := h.open(w, r)
	if !ok {
		return
	}
	in, err := readInput(r)
	if err != nil {
		http.Error(w, "invalid input", http.StatusBadRequest)
		return
	}
	rerender := func(errs map[string]string) { h.renderShow(w, r, t, token, errs) }
	if errs := rejectUploads(r); errs != nil {
		validationErrors(w, r, errs, rerender)
		return
	}
	body := strings.TrimSpace(in.Get("body"))
	if body == "" || len(body) > 65535 {
		validationErrors(w, r, map[string]string{"body": requiredMessage("body")}, rerender)
		return
	}
	if t.Status == models.StatusClosed {
		validationErrors(w, r, map[string]string{"body": "This ticket is closed."}, rerender)
		return
	}
	if _, err := h.Tickets.AddReply(r.Context(), t.ID, body, nil, nil, false); err != nil {
		http.Error(w, "Could not send the reply.", http.StatusInternalServerError)
		return
	}
	if isInertia(r) {
		backTo(w, r, h.showURL(token))
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"message": "Reply sent."})
}

// Rate handles POST /guest/{token}/rate (escalated.guest.tickets.rate): a
// 1-5 rating on a resolved or closed ticket, once.
func (h *GuestPages) Rate(w http.ResponseWriter, r *http.Request) {
	t, token, ok := h.open(w, r)
	if !ok {
		return
	}
	in, err := readInput(r)
	if err != nil {
		http.Error(w, "invalid input", http.StatusBadRequest)
		return
	}
	rerender := func(errs map[string]string) { h.renderShow(w, r, t, token, errs) }
	rating, err := strconv.Atoi(strings.TrimSpace(in.Get("rating")))
	if err != nil || !ratingValid(rating) {
		validationErrors(w, r, map[string]string{"rating": "The rating must be between 1 and 5."}, rerender)
		return
	}
	var comment *string
	if v := strings.TrimSpace(in.Get("comment")); v != "" {
		comment = &v
	}
	if h.DB == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	code, msg := recordRating(r.Context(), h.DB, t.ID, rating, comment, nil, nil)
	switch code {
	case http.StatusCreated:
		if isInertia(r) {
			backTo(w, r, h.showURL(token))
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"ok": true})
	case http.StatusUnprocessableEntity:
		validationErrors(w, r, map[string]string{"rating": msg}, rerender)
	default:
		http.Error(w, msg, code)
	}
}
