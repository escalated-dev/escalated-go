package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/store"
)

// UserLookup resolves host users for display on the shared pages. Escalated
// owns no users table, so names and emails come from the host. Both are
// optional: a nil Name shows staff as "Support" and a signed-in requester as
// "Unknown"; a nil Email shows no address. Guests are shown from the ticket.
type UserLookup struct {
	Name  func(ctx context.Context, id models.UserID) string
	Email func(ctx context.Context, id models.UserID) (string, error)
}

func (u UserLookup) name(ctx context.Context, id models.UserID) string {
	if u.Name == nil || id.Empty() {
		return ""
	}
	return u.Name(ctx, id)
}

func (u UserLookup) email(ctx context.Context, id models.UserID) string {
	if u.Email == nil || id.Empty() {
		return ""
	}
	email, err := u.Email(ctx, id)
	if err != nil {
		return ""
	}
	return email
}

// uiPresenter builds the props the shared Vue pages read, with the key names
// and shapes escalated-laravel's controllers produce (snake_case, status and
// priority as slugs, relations nested). Every map is built field by field, so
// nothing a model holds for internal use (guest tokens, storage paths) reaches
// a page.
type uiPresenter struct {
	store       store.Store
	users       UserLookup
	routePrefix string

	departments map[int64]*models.Department
}

func newUIPresenter(s store.Store, users UserLookup, routePrefix string) *uiPresenter {
	if routePrefix == "" {
		routePrefix = "/escalated"
	}
	return &uiPresenter{store: s, users: users, routePrefix: routePrefix}
}

// uiStatus is the status slug the pages compare against ("open", "resolved").
func uiStatus(status int) string {
	return models.StatusName[status]
}

// uiPriority is the priority slug the pages compare against ("low", "urgent").
func uiPriority(priority int) string {
	return models.PriorityName[priority]
}

// uiPriorities lists the priority slugs in order, as Laravel's
// config('escalated.priorities').
func uiPriorities() []string {
	keys := make([]int, 0, len(models.PriorityName))
	for k := range models.PriorityName {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, models.PriorityName[k])
	}
	return out
}

// parseStatus reads a status filter or form value: a slug, or the integer.
func parseStatus(v string) (int, bool) {
	v = strings.TrimSpace(v)
	for k, name := range models.StatusName {
		if name == v {
			return k, true
		}
	}
	if n, err := strconv.Atoi(v); err == nil {
		if _, ok := models.StatusName[n]; ok {
			return n, true
		}
	}
	return 0, false
}

// parsePriority reads a priority filter or form value: a slug, or the integer.
func parsePriority(v string) (int, bool) {
	v = strings.TrimSpace(v)
	for k, name := range models.PriorityName {
		if name == v {
			return k, true
		}
	}
	if n, err := strconv.Atoi(v); err == nil {
		if _, ok := models.PriorityName[n]; ok {
			return n, true
		}
	}
	return 0, false
}

func uiTime(t *time.Time) any {
	if t == nil || t.IsZero() {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}

func uiTimeValue(t time.Time) any {
	return uiTime(&t)
}

func uiJSON(raw models.JSONText) any {
	if len(raw) == 0 {
		return nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil
	}
	return v
}

func (p *uiPresenter) department(ctx context.Context, id *int64) map[string]any {
	if id == nil {
		return nil
	}
	if p.departments == nil {
		p.departments = map[int64]*models.Department{}
		if all, err := p.store.ListDepartments(ctx, false); err == nil {
			for _, d := range all {
				p.departments[d.ID] = d
			}
		}
	}
	d, ok := p.departments[*id]
	if !ok {
		return nil
	}
	return map[string]any{"id": d.ID, "name": d.Name}
}

// uiDepartments is Department::active()->get(['id', 'name']).
func uiDepartments(ctx context.Context, s store.Store) []map[string]any {
	out := []map[string]any{}
	depts, err := s.ListDepartments(ctx, true)
	if err != nil {
		return out
	}
	for _, d := range depts {
		out = append(out, map[string]any{"id": d.ID, "name": d.Name})
	}
	return out
}

// uiTags is Tag::all(['id', 'name', 'color']).
func uiTags(ctx context.Context, s store.Store) []map[string]any {
	out := []map[string]any{}
	tags, err := s.ListTags(ctx)
	if err != nil {
		return out
	}
	for _, t := range tags {
		out = append(out, uiTag(t))
	}
	return out
}

func uiTag(t *models.Tag) map[string]any {
	return map[string]any{"id": t.ID, "name": t.Name, "color": t.Color}
}

func (p *uiPresenter) ticketTags(ctx context.Context, ticketID int64) []map[string]any {
	out := []map[string]any{}
	tags, err := p.store.GetTicketTags(ctx, ticketID)
	if err != nil {
		return out
	}
	for _, t := range tags {
		out = append(out, uiTag(t))
	}
	return out
}

// staff is a host user as another user sees them: id, name, email.
func (p *uiPresenter) staff(ctx context.Context, id *models.UserID) map[string]any {
	if id == nil || id.Empty() {
		return nil
	}
	name := p.users.name(ctx, *id)
	if name == "" {
		name = "Support"
	}
	return map[string]any{"id": *id, "name": name, "email": p.users.email(ctx, *id)}
}

// requester is {name, email}: the guest's details for a guest ticket, the
// host user's otherwise (Laravel Ticket::requester_name / requester_email).
func (p *uiPresenter) requester(ctx context.Context, t *models.Ticket) map[string]any {
	if t.RequesterID == nil || t.RequesterID.Empty() {
		name := "Guest"
		if t.GuestName != nil && *t.GuestName != "" {
			name = *t.GuestName
		}
		email := ""
		if t.GuestEmail != nil {
			email = *t.GuestEmail
		}
		return map[string]any{"name": name, "email": email}
	}
	name := p.users.name(ctx, *t.RequesterID)
	if name == "" {
		name = "Unknown"
	}
	return map[string]any{"name": name, "email": p.users.email(ctx, *t.RequesterID)}
}

// sortNewestFirst orders replies newest first; replies written in the same
// instant keep their insertion order, newest id first.
func sortNewestFirst(replies []*models.Reply) {
	sort.SliceStable(replies, func(i, j int) bool {
		if !replies[i].CreatedAt.Equal(replies[j].CreatedAt) {
			return replies[i].CreatedAt.After(replies[j].CreatedAt)
		}
		return replies[i].ID > replies[j].ID
	})
}

// latestReply is the newest reply, or nil.
func latestReply(replies []*models.Reply) *models.Reply {
	if len(replies) == 0 {
		return nil
	}
	sorted := append([]*models.Reply(nil), replies...)
	sortNewestFirst(sorted)
	return sorted[0]
}

// isRequesterReply reports whether the reply was written by the ticket's
// requester: a guest reply carries no author, a signed-in requester's carries
// their id.
func isRequesterReply(t *models.Ticket, r *models.Reply) bool {
	if r.AuthorID == nil || r.AuthorID.Empty() {
		return true
	}
	return t.RequesterID != nil && *t.RequesterID == *r.AuthorID
}

func firstResponseBreached(t *models.Ticket) bool {
	if t.SLAFirstResponseDueAt == nil {
		return false
	}
	if t.FirstResponseAt != nil {
		return t.FirstResponseAt.After(*t.SLAFirstResponseDueAt)
	}
	return t.IsOpen() && time.Now().After(*t.SLAFirstResponseDueAt)
}

func resolutionBreached(t *models.Ticket) bool {
	if t.SLAResolutionDueAt == nil {
		return false
	}
	if t.ResolvedAt != nil {
		return t.ResolvedAt.After(*t.SLAResolutionDueAt)
	}
	return t.IsOpen() && time.Now().After(*t.SLAResolutionDueAt)
}

// channel is the ticket's channel. The tickets table keeps no channel column,
// so a live-chat ticket is recognised by its status.
func uiChannel(t *models.Ticket) any {
	if t.Channel != nil {
		return *t.Channel
	}
	if t.Status == models.StatusLive {
		return models.ChannelChat
	}
	return nil
}

// ticketRow is a ticket as a list row: what TicketList and the dashboard read.
func (p *uiPresenter) ticketRow(ctx context.Context, t *models.Ticket) map[string]any {
	requester := p.requester(ctx, t)
	row := map[string]any{
		"id":                          t.ID,
		"reference":                   t.Reference,
		"subject":                     t.Subject,
		"status":                      uiStatus(t.Status),
		"priority":                    uiPriority(t.Priority),
		"ticket_type":                 t.TicketType,
		"channel":                     uiChannel(t),
		"requester":                   requester,
		"requester_name":              requester["name"],
		"requester_email":             requester["email"],
		"assigned_to":                 t.AssignedTo,
		"assignee":                    p.staff(ctx, t.AssignedTo),
		"department_id":               t.DepartmentID,
		"department":                  p.department(ctx, t.DepartmentID),
		"tags":                        p.ticketTags(ctx, t.ID),
		"first_response_at":           uiTime(t.FirstResponseAt),
		"first_response_due_at":       uiTime(t.SLAFirstResponseDueAt),
		"resolution_due_at":           uiTime(t.SLAResolutionDueAt),
		"sla_first_response_breached": firstResponseBreached(t),
		"sla_resolution_breached":     resolutionBreached(t),
		"snoozed_until":               uiTime(t.SnoozedUntil),
		"is_snoozed":                  t.SnoozedUntil != nil && t.SnoozedUntil.After(time.Now()),
		"is_live_chat":                t.Status == models.StatusLive,
		"resolved_at":                 uiTime(t.ResolvedAt),
		"closed_at":                   uiTime(t.ClosedAt),
		"created_at":                  uiTimeValue(t.CreatedAt),
		"updated_at":                  uiTimeValue(t.UpdatedAt),
		"latest_reply":                nil,
		"last_reply_at":               nil,
		"last_reply_author":           nil,
	}

	replies, err := p.store.ListReplies(ctx, models.ReplyFilters{TicketID: t.ID})
	if err == nil {
		if latest := latestReply(replies); latest != nil {
			author := p.replyAuthorName(ctx, t, latest)
			row["latest_reply"] = map[string]any{
				"id":               latest.ID,
				"is_internal_note": latest.IsInternal,
				"author":           map[string]any{"name": author},
				"created_at":       uiTimeValue(latest.CreatedAt),
			}
			row["last_reply_at"] = uiTimeValue(latest.CreatedAt)
			row["last_reply_author"] = author
		}
	}
	return row
}

func (p *uiPresenter) ticketRows(ctx context.Context, tickets []*models.Ticket) []map[string]any {
	rows := make([]map[string]any, 0, len(tickets))
	for _, t := range tickets {
		rows = append(rows, p.ticketRow(ctx, t))
	}
	return rows
}

func (p *uiPresenter) replyAuthorName(ctx context.Context, t *models.Ticket, r *models.Reply) string {
	if isRequesterReply(t, r) {
		name, _ := p.requester(ctx, t)["name"].(string)
		return name
	}
	if r.AuthorName != nil && *r.AuthorName != "" {
		return *r.AuthorName
	}
	if name := p.users.name(ctx, *r.AuthorID); name != "" {
		return name
	}
	return "Support"
}

// replyAuthor is the reply's author as staff see it: id, name, email.
func (p *uiPresenter) replyAuthor(ctx context.Context, t *models.Ticket, r *models.Reply) map[string]any {
	if isRequesterReply(t, r) {
		req := p.requester(ctx, t)
		var id any
		if r.AuthorID != nil && !r.AuthorID.Empty() {
			id = *r.AuthorID
		}
		return map[string]any{"id": id, "name": req["name"], "email": req["email"]}
	}
	return map[string]any{
		"id":    *r.AuthorID,
		"name":  p.replyAuthorName(ctx, t, r),
		"email": p.users.email(ctx, *r.AuthorID),
	}
}

func (p *uiPresenter) attachment(a *models.Attachment) map[string]any {
	return map[string]any{
		"id":                a.ID,
		"original_filename": a.OriginalFilename,
		"mime_type":         a.MimeType,
		"size":              a.Size,
		"url":               AttachmentURL(p.routePrefix, a.ID),
	}
}

func (p *uiPresenter) replyAttachments(ctx context.Context, replyID int64) []map[string]any {
	out := []map[string]any{}
	atts, err := p.store.GetAttachmentsByReplyID(ctx, replyID)
	if err != nil {
		return out
	}
	for _, a := range atts {
		out = append(out, p.attachment(a))
	}
	return out
}

// ticketAttachments are the files on the ticket itself, not on a reply.
func (p *uiPresenter) ticketAttachments(ctx context.Context, ticketID int64) []map[string]any {
	out := []map[string]any{}
	atts, err := p.store.GetAttachmentsByTicketID(ctx, ticketID)
	if err != nil {
		return out
	}
	for _, a := range atts {
		if a.ReplyID != nil {
			continue
		}
		out = append(out, p.attachment(a))
	}
	return out
}

// staffReply is a reply as the agent screen reads it.
func (p *uiPresenter) staffReply(ctx context.Context, t *models.Ticket, r *models.Reply) map[string]any {
	kind := "reply"
	if r.IsInternal {
		kind = "note"
	}
	return map[string]any{
		"id":               r.ID,
		"body":             r.Body,
		"is_internal_note": r.IsInternal,
		"is_pinned":        r.IsPinned,
		"type":             kind,
		"author_type":      r.AuthorType,
		"author_id":        r.AuthorID,
		"author":           p.replyAuthor(ctx, t, r),
		"attachments":      p.replyAttachments(ctx, r.ID),
		"created_at":       uiTimeValue(r.CreatedAt),
		"updated_at":       uiTimeValue(r.UpdatedAt),
	}
}

// activityType maps this package's activity names onto Laravel's
// ActivityType values, which the activity timeline labels.
var activityType = map[string]string{
	models.ActionTicketAssigned:   "assigned",
	models.ActionTicketUnassigned: "unassigned",
	models.ActionReplyAdded:       "replied",
	models.ActionInternalNote:     "note_added",
	models.ActionTagsAdded:        "tag_added",
	models.ActionTagsRemoved:      "tag_removed",
	models.ActionTicketEscalated:  "escalated",
	models.ActionTicketSnoozed:    "snoozed",
	models.ActionTicketUnsnoozed:  "unsnoozed",
}

func (p *uiPresenter) activity(ctx context.Context, a *models.Activity) map[string]any {
	kind := a.Action
	if mapped, ok := activityType[kind]; ok {
		kind = mapped
	}
	props, _ := uiJSON(a.Details).(map[string]any)
	if props == nil {
		props = map[string]any{}
	}
	// The timeline reads "to" for priority changes, as Laravel records them.
	if v, ok := props["new_priority"]; ok {
		if _, has := props["to"]; !has {
			props["to"] = v
		}
	}
	var causer any
	if a.CauserID != nil && !a.CauserID.Empty() {
		name := p.users.name(ctx, *a.CauserID)
		if name == "" {
			name = "Support"
		}
		causer = map[string]any{"id": *a.CauserID, "name": name}
	}
	return map[string]any{
		"id":               a.ID,
		"ticket_id":        a.TicketID,
		"type":             kind,
		"description":      strings.ReplaceAll(kind, "_", " "),
		"properties":       props,
		"causer_type":      a.CauserType,
		"causer_id":        a.CauserID,
		"causer":           causer,
		"created_at":       uiTimeValue(a.CreatedAt),
		"created_at_human": models.HumanTime(a.CreatedAt),
	}
}

// uiPage is the page and page size a list was asked for (Laravel paginate()).
type uiPage struct {
	Page, PerPage int
}

func uiPageFrom(q url.Values, defaultPerPage int) uiPage {
	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}
	perPage, _ := strconv.Atoi(q.Get("per_page"))
	if perPage < 1 {
		perPage = defaultPerPage
	}
	if perPage > 100 {
		perPage = 100
	}
	return uiPage{Page: page, PerPage: perPage}
}

func (p uiPage) offset() int { return (p.Page - 1) * p.PerPage }

// uiPaginator is Laravel's LengthAwarePaginator as Inertia receives it.
func uiPaginator(rows []map[string]any, total int, pg uiPage, path string) map[string]any {
	if rows == nil {
		rows = []map[string]any{}
	}
	last := (total + pg.PerPage - 1) / pg.PerPage
	if last < 1 {
		last = 1
	}
	pageURL := func(n int) string { return fmt.Sprintf("%s?page=%d", path, n) }
	var prev, next any
	if pg.Page > 1 {
		prev = pageURL(pg.Page - 1)
	}
	if pg.Page < last {
		next = pageURL(pg.Page + 1)
	}
	links := []map[string]any{{"url": prev, "label": "&laquo; Previous", "active": false}}
	for n := 1; n <= last; n++ {
		links = append(links, map[string]any{"url": pageURL(n), "label": strconv.Itoa(n), "active": n == pg.Page})
	}
	links = append(links, map[string]any{"url": next, "label": "Next &raquo;", "active": false})

	var from, to any
	if len(rows) > 0 {
		from = pg.offset() + 1
		to = pg.offset() + len(rows)
	}
	return map[string]any{
		"current_page":   pg.Page,
		"data":           rows,
		"first_page_url": pageURL(1),
		"from":           from,
		"last_page":      last,
		"last_page_url":  pageURL(last),
		"links":          links,
		"next_page_url":  next,
		"path":           path,
		"per_page":       pg.PerPage,
		"prev_page_url":  prev,
		"to":             to,
		"total":          total,
	}
}

// resolveTicket finds the ticket a page path names: its reference, which is
// what the pages put in their links, or its id.
func resolveTicket(ctx context.Context, s store.Store, value string) (*models.Ticket, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	if t, err := s.GetTicketByReference(ctx, value); err != nil || t != nil {
		return t, err
	}
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return nil, nil
	}
	return s.GetTicket(ctx, id)
}
