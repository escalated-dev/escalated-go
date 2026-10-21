package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/escalated-dev/escalated-go/actions"
	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/renderer"
	"github.com/escalated-dev/escalated-go/services"
	"github.com/escalated-dev/escalated-go/store"
)

// AgentHandler serves the agent dashboard and ticket management views.
type AgentHandler struct {
	store       store.Store
	tickets     *services.TicketService
	assignments *services.AssignmentService
	renderer    renderer.Renderer
	userID      func(r *http.Request) models.UserID

	// Actions, OnCustomAction, and RoutePrefix are wired by the router to
	// support host-defined custom ticket actions. They may be nil/empty.
	Actions        *actions.Registry
	OnCustomAction func(ctx context.Context, e actions.CustomActionEvent) error
	RoutePrefix    string

	// The pages' lookups, wired by the router. All optional: DB and
	// TablePrefix back the counts, followers and ratings; Users names people;
	// Subjects, Macros and CannedResponses fill the ticket screen's panels.
	DB              *sql.DB
	TablePrefix     string
	Users           UserLookup
	Subjects        *services.TicketSubjectService
	Macros          *services.MacroService
	CannedResponses *services.CannedResponseService

	// Agents lists agents for the admin pages' assignee controls; IsAdmin
	// gates the bulk delete. Both optional.
	Agents  SkillAgentDirectory
	IsAdmin func(r *http.Request) bool
}

// NewAgentHandler creates a new AgentHandler.
func NewAgentHandler(s store.Store, ts *services.TicketService, as *services.AssignmentService, rend renderer.Renderer, userIDFunc func(r *http.Request) models.UserID) *AgentHandler {
	return &AgentHandler{
		store:       s,
		tickets:     ts,
		assignments: as,
		renderer:    rend,
		userID:      userIDFunc,
	}
}

// presenter returns a page presenter for one request.
func (h *AgentHandler) presenter() *uiPresenter {
	return newUIPresenter(h.store, h.Users, h.RoutePrefix)
}

func (h *AgentHandler) queries() uiQueries {
	return uiQueries{DB: h.DB, Prefix: h.TablePrefix}
}

// Dashboard renders Escalated/Agent/Dashboard with the props of Laravel's
// Agent\DashboardController: stats, recentTickets, needsAttention and
// myPerformance.
func (h *AgentHandler) Dashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := h.userID(r)
	q := h.queries()
	p := h.presenter()
	now := time.Now()
	open := "status IN " + openStatuses()

	stats := map[string]any{
		"open":           q.countTickets(ctx, open),
		"my_assigned":    0,
		"unassigned":     q.countTickets(ctx, open+" AND assigned_to IS NULL"),
		"sla_breached":   q.countTickets(ctx, open+" AND sla_breached = ?", true),
		"resolved_today": q.countTickets(ctx, "resolved_at >= ?", startOfDay(now)),
	}
	resolvedThisWeek := 0
	if !uid.Empty() {
		stats["my_assigned"] = q.countTickets(ctx, open+" AND assigned_to = ?", string(uid))
		resolvedThisWeek = q.countTickets(ctx, "assigned_to = ? AND resolved_at >= ?", string(uid), startOfWeek(now))
	}

	recent, _, _ := h.store.ListTickets(ctx, models.TicketFilters{Limit: 10, SortBy: "created_at", SortOrder: "desc"})

	breached := true
	breaching, _, _ := h.store.ListTickets(ctx, models.TicketFilters{SLABreached: &breached, Limit: 50, SortBy: "created_at", SortOrder: "desc"})

	var urgent []*models.Ticket
	for _, priority := range []int{models.PriorityUrgent, models.PriorityCritical} {
		pr := priority
		found, _, _ := h.store.ListTickets(ctx, models.TicketFilters{Unassigned: true, Priority: &pr, Limit: 50, SortBy: "created_at", SortOrder: "desc"})
		urgent = append(urgent, found...)
	}
	sort.SliceStable(urgent, func(i, j int) bool { return urgent[i].CreatedAt.After(urgent[j].CreatedAt) })

	_ = h.renderer.Render(w, r, "Escalated/Agent/Dashboard", map[string]any{
		"stats":         stats,
		"recentTickets": p.ticketRows(ctx, recent),
		"needsAttention": map[string]any{
			"sla_breaching":     p.ticketRows(ctx, firstOpen(breaching, 5)),
			"unassigned_urgent": p.ticketRows(ctx, firstOpen(urgent, 5)),
		},
		"myPerformance": map[string]any{"resolved_this_week": resolvedThisWeek},
	})
}

// firstOpen returns up to n of the open tickets, in order.
func firstOpen(tickets []*models.Ticket, n int) []*models.Ticket {
	out := []*models.Ticket{}
	for _, t := range tickets {
		if len(out) == n {
			break
		}
		if t.IsOpen() {
			out = append(out, t)
		}
	}
	return out
}

// agentTicketFilters reads the queue's query string (Laravel's
// TicketController::index filter keys) into store filters.
func agentTicketFilters(q url.Values) models.TicketFilters {
	var f models.TicketFilters
	if v, ok := parseStatus(q.Get("status")); ok {
		f.Status = &v
	}
	if v, ok := parsePriority(q.Get("priority")); ok {
		f.Priority = &v
	}
	if v := q.Get("ticket_type"); v != "" {
		f.TicketType = &v
	}
	if v, err := strconv.ParseInt(q.Get("department_id"), 10, 64); err == nil {
		f.DepartmentID = &v
	}
	if v := q.Get("assigned_to"); v != "" {
		id := models.UserID(v)
		f.AssignedTo = &id
	}
	f.Unassigned = truthy(q.Get("unassigned"))
	if truthy(q.Get("sla_breached")) {
		yes := true
		f.SLABreached = &yes
	}
	if v := q.Get("account_type"); v != "" {
		f.AccountType = &v
	}
	if v := q.Get("account_id"); v != "" {
		f.AccountID = &v
	}
	f.Search = q.Get("search")
	f.SortBy = q.Get("sort_by")
	f.SortOrder = strings.ToLower(q.Get("sort_dir"))
	if f.SortOrder == "" {
		f.SortOrder = strings.ToLower(q.Get("sort_order"))
	}
	return f
}

func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "on", "yes":
		return true
	}
	return false
}

// ListTickets renders Escalated/Agent/TicketIndex: the queue as Laravel's
// paginator (tickets.data, 15 a page), the query string echoed as filters,
// and the active departments and tags for the filter controls.
func (h *AgentHandler) ListTickets(w http.ResponseWriter, r *http.Request) {
	props, ok := h.queueProps(w, r)
	if !ok {
		return
	}
	_ = h.renderer.Render(w, r, "Escalated/Agent/TicketIndex", props)
}

func (h *AgentHandler) queueProps(w http.ResponseWriter, r *http.Request) (map[string]any, bool) {
	ctx := r.Context()
	q := r.URL.Query()
	pg := uiPageFrom(q, 15)
	f := agentTicketFilters(q)
	f.Limit = pg.PerPage
	f.Offset = pg.offset()

	tickets, total, err := h.store.ListTickets(ctx, f)
	if err != nil {
		http.Error(w, "could not list tickets", http.StatusInternalServerError)
		return nil, false
	}

	return map[string]any{
		"tickets":     uiPaginator(h.presenter().ticketRows(ctx, tickets), total, pg, r.URL.Path),
		"filters":     queryFilters(r),
		"departments": uiDepartments(ctx, h.store),
		"tags":        uiTags(ctx, h.store),
	}, true
}

// ShowTicket renders Escalated/Agent/TicketShow for the ticket the path names
// (its reference, or its id), with the props of Laravel's
// Agent\TicketController::show.
func (h *AgentHandler) ShowTicket(w http.ResponseWriter, r *http.Request) {
	t, ok := h.pageTicket(w, r)
	if !ok {
		return
	}
	h.renderShow(w, r, t, nil)
}

// pageTicket loads the ticket the path names (reference or id), answering 404
// when there is none.
func (h *AgentHandler) pageTicket(w http.ResponseWriter, r *http.Request) (*models.Ticket, bool) {
	t, err := resolveTicket(r.Context(), h.store, pathParam(r, "id"))
	if err != nil {
		http.Error(w, "could not load ticket", http.StatusInternalServerError)
		return nil, false
	}
	if t == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return nil, false
	}
	return t, true
}

// renderShow renders the ticket screen, with errs as the errors prop when a
// write from it failed validation.
func (h *AgentHandler) renderShow(w http.ResponseWriter, r *http.Request, t *models.Ticket, errs map[string]string) {
	ctx := r.Context()
	uid := h.userID(r)
	q := h.queries()

	canned := []models.CannedResponse{}
	if h.CannedResponses != nil {
		if list, err := h.CannedResponses.ListForAgent(uid); err == nil && list != nil {
			canned = list
		}
	}
	macros := []map[string]any{}
	if h.Macros != nil {
		if list, err := h.Macros.ListForAgent(uid); err == nil {
			for _, m := range list {
				macros = append(macros, map[string]any{
					"id": m.ID, "name": m.Name, "description": m.Description, "is_shared": m.IsShared,
				})
			}
		}
	}

	detail := h.ticketDetail(r, t)
	props := map[string]any{
		"ticket":          detail,
		"departments":     uiDepartments(ctx, h.store),
		"tags":            uiTags(ctx, h.store),
		"cannedResponses": canned,
		"macros":          macros,
		"is_following":    q.isFollowing(ctx, t.ID, uid),
		"followers_count": q.followersCount(ctx, t.ID),
	}
	if errs != nil {
		props["errors"] = pageErrors(errs)
	}
	if isAdminTicketPath(r) {
		// Laravel Admin\TicketController::show: agents and is_snoozed, no
		// custom actions.
		props["agents"] = h.agents(r)
		props["is_snoozed"] = detail["is_snoozed"]
		_ = h.renderer.Render(w, r, "Escalated/Admin/Tickets/Show", props)
		return
	}
	props["customActions"] = h.customActionsFor(t, uid, "agent")
	_ = h.renderer.Render(w, r, "Escalated/Agent/TicketShow", props)
}

// isAdminTicketPath reports whether the request came through the admin ticket
// routes, which render the admin ticket pages.
func isAdminTicketPath(r *http.Request) bool {
	return strings.Contains(r.URL.Path, "/admin/tickets")
}

// agents lists the agents for the assignee controls, from the host's
// SkillAgentDirectory (Laravel's getAgents()).
func (h *AgentHandler) agents(r *http.Request) []map[string]any {
	out := []map[string]any{}
	if h.Agents == nil {
		return out
	}
	list, err := h.Agents.ListAgentsForSkillForm(r.Context())
	if err != nil {
		return out
	}
	for _, a := range list {
		out = append(out, map[string]any{"id": a.ID, "name": a.Name, "email": a.Email})
	}
	return out
}

// AdminListTickets renders Escalated/Admin/Tickets/Index: the queue's props
// plus agents and savedViews (Laravel Admin\TicketController::index).
// Saved views have no table in this package yet, so savedViews is empty.
func (h *AgentHandler) AdminListTickets(w http.ResponseWriter, r *http.Request) {
	props, ok := h.queueProps(w, r)
	if !ok {
		return
	}
	props["agents"] = h.agents(r)
	props["savedViews"] = []any{}
	_ = h.renderer.Render(w, r, "Escalated/Admin/Tickets/Index", props)
}

// ticketDetail is the ticket as the agent screen reads it: the list row plus
// its conversation, files, history, SLA, rating, pinned notes, chat and
// related tickets.
func (h *AgentHandler) ticketDetail(r *http.Request, t *models.Ticket) map[string]any {
	ctx := r.Context()
	p := h.presenter()
	q := h.queries()
	detail := p.ticketRow(ctx, t)
	detail["description"] = t.Description
	detail["requester_type"] = t.RequesterType
	detail["requester_id"] = t.RequesterID

	replies, _ := h.store.ListReplies(ctx, models.ReplyFilters{TicketID: t.ID})
	newestFirst := append([]*models.Reply(nil), replies...)
	sortNewestFirst(newestFirst)
	thread := []map[string]any{}
	pinned := []map[string]any{}
	for _, rpl := range newestFirst {
		thread = append(thread, p.staffReply(ctx, t, rpl))
		if rpl.IsPinned && rpl.IsInternal {
			pinned = append(pinned, map[string]any{
				"id":         rpl.ID,
				"body":       rpl.Body,
				"author":     map[string]any{"name": p.replyAuthorName(ctx, t, rpl)},
				"created_at": uiTimeValue(rpl.CreatedAt),
			})
		}
	}
	detail["replies"] = thread
	detail["pinned_notes"] = pinned
	detail["attachments"] = p.ticketAttachments(ctx, t.ID)

	history := []map[string]any{}
	if activities, err := h.store.ListActivities(ctx, t.ID, 20); err == nil {
		for _, a := range activities {
			history = append(history, p.activity(ctx, a))
		}
	}
	detail["activities"] = history

	detail["sla_policy"] = nil
	if t.SLAPolicyID != nil {
		if policy, err := h.store.GetSLAPolicy(ctx, *t.SLAPolicyID); err == nil && policy != nil {
			detail["sla_policy"] = map[string]any{"id": policy.ID, "name": policy.Name}
		}
	}

	detail["satisfaction_rating"] = nil
	if rating := q.rating(ctx, t.ID); rating != nil {
		detail["satisfaction_rating"] = map[string]any{
			"id": rating.ID, "rating": rating.Rating, "comment": rating.Comment, "created_at": uiTimeValue(rating.CreatedAt),
		}
	}

	detail["chat_session_id"] = nil
	detail["chat_started_at"] = nil
	detail["chat_messages"] = []map[string]any{}
	detail["chat_metadata"] = uiJSON(t.ChatMetadata)
	if session, err := h.store.GetChatSessionByTicket(ctx, t.ID); err == nil && session != nil {
		detail["channel"] = models.ChannelChat
		detail["chat_session_id"] = session.ID
		detail["chat_started_at"] = uiTimeValue(session.CreatedAt)
		if meta := uiJSON(session.Metadata()); meta != nil {
			detail["chat_metadata"] = meta
		}
		messages := []map[string]any{}
		oldestFirst := append([]*models.Reply(nil), replies...)
		sortNewestFirst(oldestFirst)
		slices.Reverse(oldestFirst)
		for _, rpl := range oldestFirst {
			if rpl.IsInternal {
				continue
			}
			author := p.replyAuthor(ctx, t, rpl)
			messages = append(messages, map[string]any{
				"id":               rpl.ID,
				"body":             rpl.Body,
				"is_internal_note": false,
				"is_agent":         !isRequesterReply(t, rpl),
				"author":           map[string]any{"id": author["id"], "name": author["name"]},
				"created_at":       uiTimeValue(rpl.CreatedAt),
			})
		}
		detail["chat_messages"] = messages
	}

	detail["requester_ticket_count"] = q.requesterTicketCount(ctx, t)

	// Linked tickets (Laravel's related_tickets), then merge and split
	// relatives.
	related := []map[string]any{}
	seen := map[int64]bool{t.ID: true}
	add := func(id int64, reference, subject string, status int) {
		if seen[id] {
			return
		}
		seen[id] = true
		related = append(related, map[string]any{
			"id": id, "reference": reference, "subject": subject, "status": uiStatus(status),
		})
	}
	for _, id := range q.linkedTicketIDs(ctx, t.ID) {
		if other, err := h.store.GetTicket(ctx, id); err == nil && other != nil {
			add(other.ID, other.Reference, other.Subject, other.Status)
		}
	}
	if relatives, err := h.store.ListRelatedTickets(ctx, t.ID); err == nil {
		for _, rt := range relatives {
			add(rt.ID, rt.Reference, rt.Subject, rt.Status)
		}
	}
	detail["related_tickets"] = related

	subjects := []models.TicketSubjectView{}
	if h.Subjects != nil {
		if views, err := h.Subjects.ListViews(ctx, t.ID); err == nil && views != nil {
			subjects = views
		}
	}
	detail["subjects"] = subjects
	return detail
}

// customActionsFor serializes the visible custom actions for a ticket, adding
// url + method. surface is "agent" or "api" (selects the URL prefix segment).
func (h *AgentHandler) customActionsFor(t *models.Ticket, userID models.UserID, surface string) []map[string]any {
	if h.Actions == nil {
		return []map[string]any{}
	}
	prefix := h.RoutePrefix
	if prefix == "" {
		prefix = "/escalated"
	}
	out := h.Actions.ForTicket(t, userID)
	for _, a := range out {
		a["url"] = fmt.Sprintf("%s/%s/tickets/%d/actions/%v", prefix, surface, t.ID, a["key"])
		a["method"] = "post"
	}
	return out
}
