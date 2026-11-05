package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/escalated-dev/escalated-go/models"
)

// The agent panel's optional inbox and queue props (shared frontend
// docs/agent-panel.md), after escalated-laravel's AgentInbox.
//
// Every number is a count over the same filters the ticket list applies, run
// through the same store query (and so the ticket visibility scope), so it is
// what the agent would see by opening that list. Rail counts (counts, views)
// are of unresolved, unsnoozed tickets unless a view names a status; stats are
// for the current queue. unread counts the open tickets unread for the agent
// in each workspace queue. Nothing is cached: a list request runs fifteen
// count queries.

// agentQueueCookie remembers the queue last shown in the list, for the ticket
// page opened from it.
const agentQueueCookie = "escalated_agent_queue"

// inboxPresentationKeys pick a page, never which tickets are in a queue.
var inboxPresentationKeys = map[string]bool{"page": true, "per_page": true, "view": true}

var inboxQueueTitles = map[string]string{"mine": "My inbox", "unassigned": "Unassigned", "all": "All tickets"}

// inboxView is a view in the rail: its id, name and the list filters it links
// to. ID is the id as a string, for matching view=<id>; PropID is the id as the
// prop carries it (a built-in view's name, a saved view's number).
type inboxView struct {
	ID      string
	Name    string
	Filters map[string]any
	PropID  any
}

// maxInboxSavedViews bounds the saved views listed, so the counts stay bounded
// (laravel AgentInbox::MAX_SAVED_VIEWS).
const maxInboxSavedViews = 50

// agentQueue is the queue a list shows: its key, title and own filters (what
// its counts and stats are over).
type agentQueue struct {
	Key     string
	Title   string
	Filters map[string]any
}

// builtInInboxViews are the views every agent has.
func builtInInboxViews(uid models.UserID) []inboxView {
	me := string(uid)
	views := []inboxView{
		{"urgent", "Urgent", map[string]any{"priority": "urgent", "open": 1}, nil},
		{"sla_at_risk", "SLA at risk", map[string]any{"sla_at_risk": 1}, nil},
		{"waiting_on_me", "Waiting on me", map[string]any{"assigned_to": me, "status": "waiting_on_agent"}, nil},
		{"waiting_on_customer", "Waiting on customer", map[string]any{"assigned_to": me, "status": "waiting_on_customer"}, nil},
		{"recently_updated", "Recently updated", map[string]any{"open": 1, "updated_within_hours": 24, "sort_by": "updated_at"}, nil},
	}
	for i := range views {
		views[i].PropID = views[i].ID
	}
	return views
}

// inboxViews are the built-in views, then the agent's own and shared saved
// views by position (laravel AgentInbox::views). A store that cannot list
// saved views lists only the built-in ones.
func (h *AgentHandler) inboxViews(ctx context.Context, uid models.UserID) []inboxView {
	views := builtInInboxViews(uid)
	if uid.Empty() {
		return views
	}
	saved, err := h.store.ListSavedViews(ctx, uid, true)
	if err != nil {
		return views
	}
	for i, sv := range saved {
		if i == maxInboxSavedViews {
			break
		}
		filters := map[string]any{}
		if len(sv.Filters) > 0 {
			if err := json.Unmarshal(sv.Filters, &filters); err != nil || filters == nil {
				filters = map[string]any{}
			}
		}
		views = append(views, inboxView{
			ID:      strconv.FormatInt(sv.ID, 10),
			Name:    sv.Name,
			Filters: normaliseViewFilters(filters, uid),
			PropID:  sv.ID,
		})
	}
	return views
}

// normaliseViewFilters reads a saved view's assigned=me and assigned_to=me as
// the agent's id, as the list does for a query.
func normaliseViewFilters(filters map[string]any, uid models.UserID) map[string]any {
	if filters["assigned"] == "me" || filters["assigned_to"] == "me" {
		filters["assigned_to"] = string(uid)
	}
	delete(filters, "assigned")
	return filters
}

// validateInboxQuery checks the filters added for the agent panel: open,
// sla_at_risk and resolved_today are booleans, updated_within_hours an
// integer from 1 to 8760.
func validateInboxQuery(q url.Values) map[string]string {
	errs := map[string]string{}
	for _, key := range []string{"open", "sla_at_risk", "resolved_today"} {
		switch strings.ToLower(strings.TrimSpace(q.Get(key))) {
		case "", "0", "1", "true", "false":
		default:
			errs[key] = fmt.Sprintf("The %s field must be true or false.", key)
		}
	}
	if v := strings.TrimSpace(q.Get("updated_within_hours")); v != "" {
		if n, err := strconv.Atoi(v); err != nil || n < 1 || n > 8760 {
			errs["updated_within_hours"] = "The updated_within_hours field must be an integer between 1 and 8760."
		}
	}
	return errs
}

// normaliseInboxQuery reads assigned=me and assigned_to=me as the agent's id.
func normaliseInboxQuery(q url.Values, uid models.UserID) url.Values {
	out := url.Values{}
	for k, v := range q {
		out[k] = append([]string(nil), v...)
	}
	if (out.Get("assigned") == "me" || out.Get("assigned_to") == "me") && !uid.Empty() {
		out.Set("assigned_to", string(uid))
	}
	out.Del("assigned")
	return out
}

// queueFor is the queue a list request shows.
func queueFor(q url.Values, uid models.UserID, views []inboxView) agentQueue {
	if id := q.Get("view"); id != "" {
		for _, v := range views {
			if v.ID == id {
				return viewQueue(v)
			}
		}
	}
	if sel := comparableQuery(q); len(sel) > 0 {
		for _, v := range views {
			if maps.Equal(comparableQuery(filterValues(v.Filters)), sel) {
				return viewQueue(v)
			}
		}
	}
	if !uid.Empty() && q.Get("assigned_to") == string(uid) {
		return workspaceQueue("mine", uid)
	}
	if truthy(q.Get("unassigned")) {
		return workspaceQueue("unassigned", uid)
	}
	return workspaceQueue("all", uid)
}

// inboxListQuery is what the list is filtered by: the request's filters, over
// a view picked by id.
func inboxListQuery(q url.Values, queue agentQueue) url.Values {
	if q.Get("view") == "" || !strings.HasPrefix(queue.Key, "view:") {
		return q
	}
	out := filterValues(queue.Filters)
	for k, v := range q {
		if k != "view" {
			out[k] = v
		}
	}
	return out
}

func viewQueue(v inboxView) agentQueue {
	return agentQueue{Key: "view:" + v.ID, Title: v.Name, Filters: v.Filters}
}

func workspaceQueue(key string, uid models.UserID) agentQueue {
	switch key {
	case "mine":
		return agentQueue{Key: "mine", Title: inboxQueueTitles["mine"], Filters: map[string]any{"assigned_to": string(uid)}}
	case "unassigned":
		return agentQueue{Key: "unassigned", Title: inboxQueueTitles["unassigned"], Filters: map[string]any{"unassigned": 1}}
	}
	return agentQueue{Key: "all", Title: inboxQueueTitles["all"], Filters: map[string]any{}}
}

// rememberQueue keeps the queue for the ticket page, which is opened from the list.
func (h *AgentHandler) rememberQueue(w http.ResponseWriter, queue agentQueue) {
	path := h.RoutePrefix
	if path == "" {
		path = "/"
	}
	http.SetCookie(w, &http.Cookie{
		Name:     agentQueueCookie,
		Value:    queue.Key,
		Path:     path,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// recalledQueue is the queue last shown in the list, or All tickets. The
// cookie only names a queue; an unknown name selects nothing.
func recalledQueue(r *http.Request, uid models.UserID, views []inboxView) agentQueue {
	c, err := r.Cookie(agentQueueCookie)
	if err != nil {
		return workspaceQueue("all", uid)
	}
	if id, ok := strings.CutPrefix(c.Value, "view:"); ok {
		for _, v := range views {
			if v.ID == id {
				return viewQueue(v)
			}
		}
		return workspaceQueue("all", uid)
	}
	if c.Value == "mine" && uid.Empty() {
		return workspaceQueue("all", uid)
	}
	return workspaceQueue(c.Value, uid)
}

func queueProp(queue agentQueue) map[string]any {
	return map[string]any{"key": queue.Key, "title": queue.Title}
}

// inboxProp is the inbox prop for the agent and queue.
func (h *AgentHandler) inboxProp(ctx context.Context, uid models.UserID, queue agentQueue, inboxViews []inboxView) map[string]any {
	base := queue.Filters
	with := func(extra map[string]any) map[string]any {
		out := maps.Clone(base)
		maps.Copy(out, extra)
		return out
	}

	views := make([]map[string]any, 0, len(inboxViews))
	for _, v := range inboxViews {
		views = append(views, map[string]any{
			"id": v.PropID, "name": v.Name, "count": h.countOpen(ctx, v.Filters), "filters": v.Filters,
		})
	}
	mine, mineUnread := 0, 0
	if !uid.Empty() {
		mine = h.countOpen(ctx, map[string]any{"assigned_to": string(uid)})
		mineUnread = h.countUnread(ctx, uid, map[string]any{"assigned_to": string(uid)})
	}

	return map[string]any{
		"counts": map[string]any{
			"mine":       mine,
			"unassigned": h.countOpen(ctx, map[string]any{"unassigned": 1}),
			"all":        h.countOpen(ctx, map[string]any{}),
		},
		// Of the same open tickets, those unread for this agent (ADR
		// 2026-11-05-agent-panel-data).
		"unread": map[string]any{
			"mine":       mineUnread,
			"unassigned": h.countUnread(ctx, uid, map[string]any{"unassigned": 1}),
			"all":        h.countUnread(ctx, uid, map[string]any{}),
		},
		"views": views,
		// Each stat is the list the tile opens: the queue plus the tile's filters.
		"stats": map[string]any{
			"open":           h.countTickets(ctx, with(map[string]any{"status": "open"})),
			"urgent":         h.countTickets(ctx, with(map[string]any{"priority": "urgent", "open": 1})),
			"sla_at_risk":    h.countTickets(ctx, with(map[string]any{"sla_at_risk": 1})),
			"resolved_today": h.countTickets(ctx, with(map[string]any{"status": "resolved", "resolved_today": 1})),
		},
	}
}

// countOpen counts unresolved, unsnoozed tickets, unless the filters name a
// status themselves.
func (h *AgentHandler) countOpen(ctx context.Context, filters map[string]any) int {
	if s, _ := filters["status"].(string); s == "" {
		filters = maps.Clone(filters)
		filters["open"] = 1
	}
	return h.countTickets(ctx, filters)
}

// countUnread counts the open tickets (as countOpen) unread for the agent.
func (h *AgentHandler) countUnread(ctx context.Context, uid models.UserID, filters map[string]any) int {
	if uid.Empty() {
		return 0
	}
	if s, _ := filters["status"].(string); s == "" {
		filters = maps.Clone(filters)
		filters["open"] = 1
	}
	f, err := h.ticketFilters(ctx, filterValues(filters))
	if err != nil {
		return 0
	}
	f.UnreadFor = uid
	f.CountOnly = true
	f.Limit = 1
	_, total, err := h.store.ListTickets(ctx, f)
	if err != nil {
		return 0
	}
	return total
}

// countTickets is the total the list shows for these filters.
func (h *AgentHandler) countTickets(ctx context.Context, filters map[string]any) int {
	f, err := h.ticketFilters(ctx, filterValues(filters))
	if err != nil {
		return 0
	}
	f.CountOnly = true
	f.Limit = 1
	_, total, err := h.store.ListTickets(ctx, f)
	if err != nil {
		return 0
	}
	return total
}

// filterValues is a view's filters as a query string. A list value (a saved
// view's tags, say) becomes the key repeated.
func filterValues(filters map[string]any) url.Values {
	out := url.Values{}
	for k, v := range filters {
		switch val := v.(type) {
		case nil:
		case []any:
			for _, item := range val {
				out.Add(k, fmt.Sprint(item))
			}
		case bool:
			if val {
				out.Set(k, "1")
			} else {
				out.Set(k, "0")
			}
		default:
			out.Set(k, fmt.Sprint(val))
		}
	}
	return out
}

// comparableQuery is a query without empty values or presentation keys, for
// comparing a URL with a view.
func comparableQuery(q url.Values) map[string]string {
	out := map[string]string{}
	for k, v := range q {
		if inboxPresentationKeys[k] {
			continue
		}
		vals := []string{}
		for _, s := range v {
			if s != "" {
				vals = append(vals, s)
			}
		}
		if len(vals) == 0 {
			continue
		}
		sort.Strings(vals)
		out[k] = strings.Join(vals, ",")
	}
	return out
}
