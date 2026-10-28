package handlers

import (
	"context"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/services"
	"github.com/escalated-dev/escalated-go/store"
)

// MobileGuestHandler serves the mobile app's guest ticket routes under
// {RoutePrefix}/api/mobile/guest: create, show and reply. (Verification and
// lookup there are GuestVerificationHandler's, as on every guest surface.)
// It is the contract in escalated-developer-context
// domain-model/mobile-guest-api.md, on the same codes, grants and budgets as
// the guest API, the guest pages and the widget.
type MobileGuestHandler struct {
	Store   store.Store
	Tickets *services.TicketService
	Access  *services.GuestAccess
	// Users names the agents shown on a ticket (nil Name shows "Support").
	Users UserLookup
}

// mobileTextMax is the longest description or reply body accepted.
const mobileTextMax = 65535

// Create handles POST /mobile/guest/tickets: JSON or multipart form fields
// {name, email, subject, description, priority?, department_id?,
// verification_id, verification_code}. Any other field is ignored.
func (h *MobileGuestHandler) Create(w http.ResponseWriter, r *http.Request) {
	setGuestPrivacyHeaders(w)
	in := mobileInput(r)
	field := func(k string) string { return strings.TrimSpace(in.Get(k)) }
	name, email, subject := field("name"), field("email"), field("subject")
	description := in.Get("description")
	proof := guestProof{VerificationID: field("verification_id"), VerificationCode: field("verification_code")}

	errs := proof.validate()
	if proof.VerificationID != "" && !uuidPattern(proof.VerificationID) {
		errs["verification_id"] = []string{"Request a verification code first."}
	}
	if name == "" || len(name) > 255 {
		errs["name"] = []string{requiredMessage("name")}
	}
	if !services.ValidGuestEmail(email) {
		errs["email"] = []string{"Enter a valid email address."}
	}
	if subject == "" || len(subject) > 255 {
		errs["subject"] = []string{requiredMessage("subject")}
	}
	if strings.TrimSpace(description) == "" || len(description) > mobileTextMax {
		errs["description"] = []string{requiredMessage("description")}
	}
	priority := models.PriorityMedium
	if v := field("priority"); v != "" {
		if p, ok := mobilePriority(v); ok {
			priority = p
		} else {
			errs["priority"] = []string{"The selected priority is invalid."}
		}
	}
	var departmentID *int64
	if v := field("department_id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		var dept *models.Department
		if err == nil {
			dept, _ = h.Store.GetDepartment(r.Context(), id)
		}
		if dept == nil {
			errs["department_id"] = []string{"The selected department is invalid."}
		} else {
			departmentID = &id
		}
	}
	mobileRejectUploads(r, errs)
	if len(errs) > 0 {
		writeGuestValidation(w, errs)
		return
	}
	if h.Access == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"message": "Guest tickets are not available."})
		return
	}

	t, token, expires, err := createVerifiedGuestTicket(r.Context(), h.Access, h.Tickets, proof, email, services.CreateTicketInput{
		Subject:      subject,
		Description:  description,
		Priority:     priority,
		GuestName:    &name,
		DepartmentID: departmentID,
	})
	if !writeGuestAccessError(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"data":    h.ticket(r.Context(), t, token, expires, false),
		"message": "Ticket created.",
	})
}

// Show handles GET /mobile/guest/tickets/{token}: the ticket with its public
// replies, newest first.
func (h *MobileGuestHandler) Show(w http.ResponseWriter, r *http.Request) {
	setGuestPrivacyHeaders(w)
	t, token, expires, ok := h.open(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": h.ticket(r.Context(), t, token, expires, true)})
}

// Reply handles POST /mobile/guest/tickets/{token}/replies: {email, body}.
// The email must be the ticket's guest email, on top of the grant.
func (h *MobileGuestHandler) Reply(w http.ResponseWriter, r *http.Request) {
	setGuestPrivacyHeaders(w)
	t, _, _, ok := h.open(w, r)
	if !ok {
		return
	}
	if t.Status == models.StatusClosed {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": "This ticket is closed."})
		return
	}
	in := mobileInput(r)
	body, email := in.Get("body"), strings.TrimSpace(in.Get("email"))
	errs := map[string][]string{}
	if strings.TrimSpace(body) == "" || len(body) > mobileTextMax {
		errs["body"] = []string{requiredMessage("body")}
	}
	if !services.ValidGuestEmail(email) {
		errs["email"] = []string{"Enter a valid email address."}
	}
	mobileRejectUploads(r, errs)
	if len(errs) > 0 {
		writeGuestValidation(w, errs)
		return
	}
	if t.GuestEmail == nil || services.NormalizeGuestEmail(*t.GuestEmail) != services.NormalizeGuestEmail(email) {
		writeJSON(w, http.StatusForbidden, map[string]string{"message": "The provided email does not match this ticket."})
		return
	}
	if _, err := h.Tickets.AddReply(r.Context(), t.ID, body, nil, nil, false); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "Could not send the reply."})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"message": "Reply sent."})
}

// open loads the ticket a grant opens. Anything else (expired, rotated,
// malformed, over-long, a chat token or a legacy guest token) is 404.
func (h *MobileGuestHandler) open(w http.ResponseWriter, r *http.Request) (*models.Ticket, string, time.Time, bool) {
	notFound := func() (*models.Ticket, string, time.Time, bool) {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Ticket not found."})
		return nil, "", time.Time{}, false
	}
	token := pathParam(r, "token")
	if token == "" || h.Access == nil {
		return notFound()
	}
	id, expires, err := h.Access.ResolveGrant(r.Context(), token)
	if err != nil {
		return notFound()
	}
	t, err := h.Store.GetTicket(r.Context(), id)
	if err != nil || t == nil {
		return notFound()
	}
	return t, token, expires, true
}

// ticket is the allow-listed mobile ticket object. Nothing is serialized
// from the model as a whole.
func (h *MobileGuestHandler) ticket(ctx context.Context, t *models.Ticket, token string, expires time.Time, withReplies bool) map[string]any {
	var grant, grantExpires any
	if token != "" {
		grant, grantExpires = token, uiTimeValue(expires)
	}
	requester := map[string]any{"name": "Guest", "email": ""}
	if t.GuestName != nil && strings.TrimSpace(*t.GuestName) != "" {
		requester["name"] = *t.GuestName
	}
	if t.GuestEmail != nil {
		requester["email"] = *t.GuestEmail
	}
	var assignee any
	if t.AssignedTo != nil && !t.AssignedTo.Empty() {
		assignee = map[string]any{"id": 0, "name": h.staffName(ctx, *t.AssignedTo), "email": ""}
	}
	var department any
	if t.DepartmentID != nil {
		if d, err := h.Store.GetDepartment(ctx, *t.DepartmentID); err == nil && d != nil {
			department = map[string]any{"id": d.ID, "name": d.Name}
		}
	}
	channel := "web"
	if t.Channel != nil && *t.Channel != "" {
		channel = *t.Channel
	}
	replies := []map[string]any{}
	if withReplies {
		replies = h.replies(ctx, t, requester)
	}
	return map[string]any{
		"id":                      t.ID,
		"reference":               t.Reference,
		"guest_access_token":      grant,
		"guest_access_expires_at": grantExpires,
		"subject":                 t.Subject,
		"description":             t.Description,
		"status":                  map[string]any{"value": t.StatusString(), "label": mobileStatusLabel(t.Status)},
		"priority":                map[string]any{"value": t.PriorityString(), "label": widgetStatusLabel(t.PriorityString())},
		"channel":                 channel,
		"metadata":                map[string]any{},
		"requester":               requester,
		"assignee":                assignee,
		"department":              department,
		"tags":                    []any{},
		"replies":                 replies,
		"activities":              []any{},
		"sla": map[string]any{
			"first_response_due_at":   uiTime(t.SLAFirstResponseDueAt),
			"first_response_at":       uiTime(t.FirstResponseAt),
			"first_response_breached": firstResponseBreached(t),
			"resolution_due_at":       uiTime(t.SLAResolutionDueAt),
			"resolution_breached":     t.SLAResolutionBreached(),
		},
		"is_following":    false,
		"followers_count": 0,
		"resolved_at":     uiTime(t.ResolvedAt),
		"closed_at":       uiTime(t.ClosedAt),
		"created_at":      uiTimeValue(t.CreatedAt),
		"updated_at":      uiTimeValue(t.UpdatedAt),
	}
}

// replies are the ticket's public replies, newest first. Staff appear by
// display name only; the guest's own replies carry the guest's details.
func (h *MobileGuestHandler) replies(ctx context.Context, t *models.Ticket, requester map[string]any) []map[string]any {
	internal := false
	list, _ := h.Store.ListReplies(ctx, models.ReplyFilters{TicketID: t.ID, Internal: &internal})
	public := make([]*models.Reply, 0, len(list))
	for _, rp := range list {
		if rp != nil && !rp.IsInternal {
			public = append(public, rp)
		}
	}
	sort.SliceStable(public, func(i, j int) bool {
		if !public[i].CreatedAt.Equal(public[j].CreatedAt) {
			return public[i].CreatedAt.After(public[j].CreatedAt)
		}
		return public[i].ID > public[j].ID
	})
	out := make([]map[string]any, 0, len(public))
	for _, rp := range public {
		author := map[string]any{"id": 0, "name": requester["name"], "email": requester["email"]}
		if !isRequesterReply(t, rp) {
			name := ""
			if rp.AuthorName != nil {
				name = *rp.AuthorName
			}
			if name == "" {
				name = h.staffName(ctx, *rp.AuthorID)
			}
			author = map[string]any{"id": 0, "name": name, "email": ""}
		}
		out = append(out, map[string]any{
			"id":               rp.ID,
			"body":             rp.Body,
			"is_internal_note": false,
			"is_pinned":        rp.IsPinned,
			"author":           author,
			"attachments":      h.attachments(ctx, rp.ID),
			"created_at":       uiTimeValue(rp.CreatedAt),
		})
	}
	return out
}

// attachments lists a reply's files. There is no guest-authorized download
// route in this package yet, so url is null.
func (h *MobileGuestHandler) attachments(ctx context.Context, replyID int64) []map[string]any {
	out := []map[string]any{}
	atts, err := h.Store.GetAttachmentsByReplyID(ctx, replyID)
	if err != nil {
		return out
	}
	for _, a := range atts {
		out = append(out, map[string]any{"id": a.ID, "filename": a.OriginalFilename, "mime_type": a.MimeType, "size": a.Size, "url": nil})
	}
	return out
}

func (h *MobileGuestHandler) staffName(ctx context.Context, id models.UserID) string {
	if name := h.Users.name(ctx, id); name != "" {
		return name
	}
	return "Support"
}

// mobileStatusLabel is the built-in status's label ("Waiting on Customer").
func mobileStatusLabel(status int) string {
	for _, s := range models.BuiltInStatuses() {
		if s.Status == status {
			return s.Label
		}
	}
	return widgetStatusLabel(models.StatusName[status])
}

// mobilePriority reads a priority slug (low, medium, high, urgent, critical).
func mobilePriority(v string) (int, bool) {
	for k, name := range models.PriorityName {
		if name == v {
			return k, true
		}
	}
	return 0, false
}

// mobileInput reads a JSON body or a form. A body that does not parse counts
// as empty, so the answer is the usual 422.
func mobileInput(r *http.Request) url.Values {
	in, err := readInput(r)
	if err != nil || in == nil {
		return url.Values{}
	}
	return in
}

// mobileRejectUploads refuses files: this package stores no uploads yet, and
// dropping them silently would lose them.
func mobileRejectUploads(r *http.Request, errs map[string][]string) {
	for k, v := range rejectUploads(r) {
		errs[k] = []string{v}
	}
}

func uuidPattern(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
				return false
			}
		}
	}
	return true
}
