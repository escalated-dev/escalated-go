package handlers

import (
	"context"
	"database/sql"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/renderer"
	"github.com/escalated-dev/escalated-go/services"
	"github.com/escalated-dev/escalated-go/store"
)

// CustomerHandler serves the customer-facing ticket portal.
type CustomerHandler struct {
	store    store.Store
	tickets  *services.TicketService
	renderer renderer.Renderer
	userID   func(r *http.Request) models.UserID

	// The pages' lookups, wired by the router. All optional: RoutePrefix
	// builds links, Users names people, DB and TablePrefix back the rating.
	RoutePrefix string
	Users       UserLookup
	DB          *sql.DB
	TablePrefix string
}

// NewCustomerHandler creates a new CustomerHandler.
func NewCustomerHandler(s store.Store, ts *services.TicketService, rend renderer.Renderer, userIDFunc func(r *http.Request) models.UserID) *CustomerHandler {
	return &CustomerHandler{
		store:    s,
		tickets:  ts,
		renderer: rend,
		userID:   userIDFunc,
	}
}

// customerFilterKeys are the filters the customer list honours and echoes, as
// Laravel's Customer\TicketController::index $filterKeys.
var customerFilterKeys = []string{
	"status", "priority", "ticket_type", "search",
	"tag", "has_attachments", "created_after", "created_before",
	"sort_by", "sort_dir",
}

func (h *CustomerHandler) presenter() *uiPresenter {
	return newUIPresenter(h.store, h.Users, h.RoutePrefix)
}

func (h *CustomerHandler) routePrefix() string {
	if h.RoutePrefix == "" {
		return "/escalated"
	}
	return h.RoutePrefix
}

// Index renders Escalated/Customer/Index: the signed-in user's tickets as
// Laravel's paginator (15 a page) and the filters it honoured.
func (h *CustomerHandler) Index(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := h.userID(r)
	q := r.URL.Query()
	pg := uiPageFrom(q, 15)

	f := models.TicketFilters{RequesterID: &uid, Limit: pg.PerPage, Offset: pg.offset()}
	if v, ok := parseStatus(q.Get("status")); ok {
		f.Status = &v
	}
	if v, ok := parsePriority(q.Get("priority")); ok {
		f.Priority = &v
	}
	if v := q.Get("ticket_type"); v != "" {
		f.TicketType = &v
	}
	f.Search = q.Get("search")
	f.SortBy = q.Get("sort_by")
	f.SortOrder = strings.ToLower(q.Get("sort_dir"))

	tickets, total, err := h.store.ListTickets(ctx, f)
	if err != nil {
		http.Error(w, "could not list tickets", http.StatusInternalServerError)
		return
	}

	p := h.presenter()
	rows := make([]map[string]any, 0, len(tickets))
	for _, t := range tickets {
		rows = append(rows, h.customerRow(r, p, t))
	}

	filters := map[string]string{}
	for _, key := range customerFilterKeys {
		if q.Has(key) {
			filters[key] = q.Get(key)
		}
	}

	_ = h.renderer.Render(w, r, "Escalated/Customer/Index", map[string]any{
		"tickets": uiPaginator(rows, total, pg, r.URL.Path),
		"filters": filters,
	})
}

// customerRow is a list row with nothing about staff but display names.
func (h *CustomerHandler) customerRow(r *http.Request, p *uiPresenter, t *models.Ticket) map[string]any {
	ctx := r.Context()
	requester := p.requester(ctx, t)
	var department any
	if d := p.department(ctx, t.DepartmentID); d != nil {
		department = map[string]any{"name": d["name"]}
	}
	var assignee any
	if t.AssignedTo != nil && !t.AssignedTo.Empty() {
		assignee = customerStaff(p.users.name(ctx, *t.AssignedTo))
	}
	row := map[string]any{
		"id":                t.ID,
		"reference":         t.Reference,
		"subject":           t.Subject,
		"status":            uiStatus(t.Status),
		"priority":          uiPriority(t.Priority),
		"ticket_type":       t.TicketType,
		"channel":           uiChannel(t),
		"requester":         requester,
		"requester_name":    requester["name"],
		"requester_email":   requester["email"],
		"department":        department,
		"assignee":          assignee,
		"is_live_chat":      t.Status == models.StatusLive,
		"is_snoozed":        false,
		"last_reply_at":     nil,
		"last_reply_author": nil,
		"created_at":        uiTimeValue(t.CreatedAt),
		"updated_at":        uiTimeValue(t.UpdatedAt),
	}
	public := false
	if replies, err := h.store.ListReplies(ctx, models.ReplyFilters{TicketID: t.ID, Internal: &public}); err == nil {
		if latest := latestReply(replies); latest != nil {
			row["last_reply_at"] = uiTimeValue(latest.CreatedAt)
			row["last_reply_author"] = customerReplyAuthor(ctx, p, t, latest)
		}
	}
	return row
}

// customerStaff is a staff member as a customer sees them: a display name
// only (Laravel CustomerTicketPayload::assignee).
func customerStaff(name string) map[string]any {
	if name == "" {
		name = "Support"
	}
	return map[string]any{"id": 0, "name": name, "email": ""}
}

func customerReplyAuthor(ctx context.Context, p *uiPresenter, t *models.Ticket, r *models.Reply) string {
	if isRequesterReply(t, r) {
		name, _ := p.requester(ctx, t)["name"].(string)
		return name
	}
	if name := p.users.name(ctx, *r.AuthorID); name != "" {
		return name
	}
	return "Support"
}

// CreateForm renders Escalated/Customer/Create with the active departments
// and the priority slugs (Laravel Customer\TicketController::create).
func (h *CustomerHandler) CreateForm(w http.ResponseWriter, r *http.Request) {
	h.renderCreate(w, r, nil)
}

func (h *CustomerHandler) renderCreate(w http.ResponseWriter, r *http.Request, errs map[string]string) {
	props := map[string]any{
		"departments": uiDepartments(r.Context(), h.store),
		"priorities":  uiPriorities(),
	}
	if errs != nil {
		props["errors"] = pageErrors(errs)
	}
	_ = h.renderer.Render(w, r, "Escalated/Customer/Create", props)
}

// Show renders Escalated/Customer/Show for a ticket the signed-in user
// requested, named by its reference or id, as Laravel's
// CustomerTicketPayload::customerWeb: public replies only, staff by display
// name.
func (h *CustomerHandler) Show(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	t, err := resolveTicket(ctx, h.store, pathParam(r, "id"))
	if err != nil {
		http.Error(w, "could not load ticket", http.StatusInternalServerError)
		return
	}
	if t == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if !requestedBy(t, h.userID(r)) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	_ = h.renderer.Render(w, r, "Escalated/Customer/Show", map[string]any{
		"ticket": h.customerPayload(r, t),
	})
}

// customerPayload is Laravel's CustomerTicketPayload::customerWeb.
func (h *CustomerHandler) customerPayload(r *http.Request, t *models.Ticket) map[string]any {
	return webTicketPayload(r, h.store, h.presenter(), uiQueries{DB: h.DB, Prefix: h.TablePrefix}, t, true)
}

// webTicketPayload is Laravel's CustomerTicketPayload::customerWeb (and, with
// files false, guestWeb without its grant expiry): public replies only, staff
// by display name. Guests get no file list: this package has no guest
// attachment access.
func webTicketPayload(r *http.Request, s store.Store, p *uiPresenter, q uiQueries, t *models.Ticket, files bool) map[string]any {
	ctx := r.Context()
	var department any
	if d := p.department(ctx, t.DepartmentID); d != nil {
		department = map[string]any{"name": d["name"]}
	}

	public := false
	replies, _ := s.ListReplies(ctx, models.ReplyFilters{TicketID: t.ID, Internal: &public})
	sortNewestFirst(replies)
	thread := []map[string]any{}
	for _, rpl := range replies {
		if rpl.IsInternal {
			continue
		}
		attachments := []map[string]any{}
		if files {
			attachments = p.replyAttachments(ctx, rpl.ID)
		}
		thread = append(thread, map[string]any{
			"id":               rpl.ID,
			"body":             rpl.Body,
			"is_internal_note": false,
			"is_pinned":        rpl.IsPinned,
			"author":           map[string]any{"name": customerReplyAuthor(ctx, p, t, rpl)},
			"attachments":      attachments,
			"created_at":       uiTimeValue(rpl.CreatedAt),
		})
	}

	var rating any
	if got := q.rating(ctx, t.ID); got != nil {
		rating = map[string]any{"rating": got.Rating, "comment": got.Comment}
	}
	attachments := []map[string]any{}
	if files {
		attachments = p.ticketAttachments(ctx, t.ID)
	}

	return map[string]any{
		"id":                  t.ID,
		"reference":           t.Reference,
		"subject":             t.Subject,
		"description":         t.Description,
		"status":              uiStatus(t.Status),
		"priority":            uiPriority(t.Priority),
		"channel":             uiChannel(t),
		"department":          department,
		"attachments":         attachments,
		"replies":             thread,
		"satisfaction_rating": rating,
		"resolved_at":         uiTime(t.ResolvedAt),
		"closed_at":           uiTime(t.ClosedAt),
		"created_at":          uiTimeValue(t.CreatedAt),
		"updated_at":          uiTimeValue(t.UpdatedAt),
	}
}

// Create handles the ticket form (Laravel Customer\TicketController::store).
// It reads JSON or a form, takes priority as a slug ("medium") or the integer,
// and validates like CreateTicketRequest. A page visit (X-Inertia) is
// redirected to the new ticket, or shown the form again with errors; an API
// client gets 201 with the ticket, or 422.
func (h *CustomerHandler) Create(w http.ResponseWriter, r *http.Request) {
	in, err := readInput(r)
	if err != nil {
		http.Error(w, "invalid input", http.StatusBadRequest)
		return
	}

	errs := map[string]string{}
	subject := strings.TrimSpace(in.Get("subject"))
	description := strings.TrimSpace(in.Get("description"))
	if subject == "" {
		errs["subject"] = requiredMessage("subject")
	} else if len(subject) > 255 {
		errs["subject"] = "The subject may not be greater than 255 characters."
	}
	if description == "" {
		errs["description"] = requiredMessage("description")
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
			dept, _ = h.store.GetDepartment(r.Context(), id)
		}
		if dept == nil {
			errs["department_id"] = "The selected department is invalid."
		} else {
			departmentID = &id
		}
	}
	ticketType := strings.TrimSpace(in.Get("ticket_type"))
	if ticketType != "" && !slices.Contains(models.TicketTypes, ticketType) {
		errs["ticket_type"] = "The selected ticket type is invalid."
	}
	if len(errs) > 0 {
		validationErrors(w, r, errs, func(errs map[string]string) { h.renderCreate(w, r, errs) })
		return
	}

	uid := h.userID(r)
	reqType := "User"

	t, err := h.tickets.Create(r.Context(), services.CreateTicketInput{
		Subject:       subject,
		Description:   description,
		Priority:      priority,
		TicketType:    ticketType,
		RequesterType: &reqType,
		RequesterID:   &uid,
		DepartmentID:  departmentID,
	})
	if err != nil {
		http.Error(w, "could not create the ticket", http.StatusInternalServerError)
		return
	}

	if isInertia(r) {
		seeOther(w, r, h.routePrefix()+"/tickets/"+url.PathEscape(t.Reference))
		return
	}
	t.PopulateComputed(nil)
	writeJSON(w, http.StatusCreated, map[string]any{"ticket": t})
}

// ownTicket loads the ticket the path names (reference or id) when the
// signed-in user requested it: 404 when there is none, 403 when it is
// someone else's.
func (h *CustomerHandler) ownTicket(w http.ResponseWriter, r *http.Request) (*models.Ticket, bool) {
	t, err := resolveTicket(r.Context(), h.store, pathParam(r, "id"))
	if err != nil {
		http.Error(w, "could not load ticket", http.StatusInternalServerError)
		return nil, false
	}
	if t == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return nil, false
	}
	if !requestedBy(t, h.userID(r)) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return nil, false
	}
	return t, true
}

func (h *CustomerHandler) showURL(t *models.Ticket) string {
	return h.routePrefix() + "/tickets/" + url.PathEscape(t.Reference)
}

// done ends a successful write: back to the page for a page visit, JSON
// otherwise.
func (h *CustomerHandler) done(w http.ResponseWriter, r *http.Request, t *models.Ticket, code int, body any) {
	if isInertia(r) {
		backTo(w, r, h.showURL(t))
		return
	}
	writeJSON(w, code, body)
}

func (h *CustomerHandler) invalid(w http.ResponseWriter, r *http.Request, t *models.Ticket, errs map[string]string) {
	validationErrors(w, r, errs, func(errs map[string]string) {
		_ = h.renderer.Render(w, r, "Escalated/Customer/Show", map[string]any{
			"ticket": h.customerPayload(r, t),
			"errors": pageErrors(errs),
		})
	})
}

// Reply handles a customer replying to a ticket they requested, at
// POST /tickets/{id}/replies and /tickets/{id}/reply
// (escalated.customer.tickets.reply).
func (h *CustomerHandler) Reply(w http.ResponseWriter, r *http.Request) {
	t, ok := h.ownTicket(w, r)
	if !ok {
		return
	}
	in, err := readInput(r)
	if err != nil {
		http.Error(w, "invalid input", http.StatusBadRequest)
		return
	}
	if errs := rejectUploads(r); errs != nil {
		h.invalid(w, r, t, errs)
		return
	}
	body := strings.TrimSpace(in.Get("body"))
	if body == "" {
		h.invalid(w, r, t, map[string]string{"body": requiredMessage("body")})
		return
	}

	uid := h.userID(r)
	authorType := "User"
	reply, err := h.tickets.AddReply(r.Context(), t.ID, body, &authorType, &uid, false)
	if err != nil {
		http.Error(w, "could not add the reply", http.StatusInternalServerError)
		return
	}
	h.done(w, r, t, http.StatusCreated, map[string]any{"reply": reply})
}

// Close handles POST /tickets/{id}/close: the requester closes their ticket
// (Laravel allows it by default, tickets.allow_customer_close).
func (h *CustomerHandler) Close(w http.ResponseWriter, r *http.Request) {
	h.changeStatus(w, r, models.StatusClosed)
}

// Reopen handles POST /tickets/{id}/reopen: the requester reopens their
// ticket.
func (h *CustomerHandler) Reopen(w http.ResponseWriter, r *http.Request) {
	h.changeStatus(w, r, models.StatusReopened)
}

func (h *CustomerHandler) changeStatus(w http.ResponseWriter, r *http.Request, status int) {
	t, ok := h.ownTicket(w, r)
	if !ok {
		return
	}
	uid := h.userID(r)
	if err := h.tickets.ChangeStatus(r.Context(), t.ID, status, &uid); err != nil {
		http.Error(w, "could not change the status", http.StatusInternalServerError)
		return
	}
	h.done(w, r, t, http.StatusOK, map[string]any{"status": uiStatus(status)})
}

// Rate handles POST /tickets/{id}/rate (escalated.customer.tickets.rate): the
// requester rates their resolved or closed ticket, once, 1 to 5.
func (h *CustomerHandler) Rate(w http.ResponseWriter, r *http.Request) {
	t, ok := h.ownTicket(w, r)
	if !ok {
		return
	}
	in, err := readInput(r)
	if err != nil {
		http.Error(w, "invalid input", http.StatusBadRequest)
		return
	}
	rating, err := strconv.Atoi(strings.TrimSpace(in.Get("rating")))
	if err != nil || !ratingValid(rating) {
		h.invalid(w, r, t, map[string]string{"rating": "The rating must be between 1 and 5."})
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
	uid := h.userID(r)
	code, msg := recordRating(r.Context(), h.DB, t.ID, rating, comment, t.RequesterType, &uid)
	switch code {
	case http.StatusCreated:
		h.done(w, r, t, http.StatusCreated, map[string]any{"ok": true})
	case http.StatusUnprocessableEntity:
		h.invalid(w, r, t, map[string]string{"rating": msg})
	default:
		http.Error(w, msg, code)
	}
}

// requestedBy reports whether uid is the ticket's requester. A ticket with no
// requester, such as a guest ticket, belongs to no signed-in user, and an empty
// uid is nobody.
func requestedBy(t *models.Ticket, uid models.UserID) bool {
	return !uid.Empty() && t.RequesterID != nil && *t.RequesterID == uid
}

// queryFilters echoes the list query string back as the "filters" prop the
// ticket list pages read to pre-fill their filter controls.
func queryFilters(r *http.Request) map[string]string {
	out := map[string]string{}
	for k, v := range r.URL.Query() {
		if len(v) > 0 {
			out[k] = v[0]
		}
	}
	return out
}
