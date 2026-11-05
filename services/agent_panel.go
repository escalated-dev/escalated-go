package services

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/escalated-dev/escalated-go/internal/sqldialect"
	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/ticketaccess"
)

// AgentStatuses are the statuses an agent may set (ADR
// 2026-11-05-agent-panel-data).
var AgentStatuses = []string{"online", "away", "offline"}

// presenceThrottle is how often a customer's presence is written at most.
const presenceThrottle = 30 * time.Second

// defaultOnlineWindow is how long after they were last seen a customer still
// shows as online on the agent pages.
const defaultOnlineWindow = 120 * time.Second

// unavailableTitle is a notification's title once its ticket is out of the
// agent's sight.
const unavailableTitle = "A ticket you can no longer view"

var notificationTitles = map[string]string{
	"ticket_assigned":  "%s was assigned to you",
	"ticket_reply":     "New reply on %s",
	"sla_warning":      "SLA due soon on %s",
	"sla_breached":     "SLA breached on %s",
	"chat_transferred": "A chat was transferred to you (%s)",
	"generic":          "Update on %s",
}

// AgentPanel keeps the agent panel's data (escalated-developer-context ADR
// 2026-11-05-agent-panel-data, after escalated-laravel #292-#296): each
// agent's own status, per-agent unread markers, customer presence (shown on
// agent pages only) and the agents' in-app notifications.
//
// The ticket service hands it every ticket event (TicketService.Panel).
// Nothing here can fail the operation that triggered it: errors are logged.
type AgentPanel struct {
	DB     *sql.DB
	Prefix string
	// Access, when set, redacts notifications about tickets the agent can no
	// longer see before they are broadcast.
	Access *ticketaccess.Access
	// Broadcaster, when set, receives each new notification as
	// notification.created on the agent's private channel (UserChannel).
	Broadcaster *Broadcaster
	// NotificationsDisabled switches the in-app notifications off: nothing is
	// stored and the feed routes answer 404.
	NotificationsDisabled bool
	// OnlineWindow is how long a customer shows as online after they were
	// last seen (default 120 seconds).
	OnlineWindow time.Duration
	// RoutePrefix builds a notification's link to the agent ticket page.
	RoutePrefix string
	Logger      *log.Logger
}

func (p *AgentPanel) t(name string) string {
	if p.Prefix == "" {
		return "escalated_" + name
	}
	return p.Prefix + name
}

// q fills {name} table placeholders and rebinds the placeholders for the
// database.
func (p *AgentPanel) q(query string) string {
	for _, name := range []string{"tickets", "ticket_reads", "customer_presence", "notifications", "agent_statuses", "ticket_followers", "articles", "article_categories"} {
		query = strings.ReplaceAll(query, "{"+name+"}", p.t(name))
	}
	return sqldialect.Rebind(p.DB, query)
}

func (p *AgentPanel) logf(format string, args ...any) {
	if p.Logger != nil {
		p.Logger.Printf(format, args...)
		return
	}
	log.Printf(format, args...)
}

func panelNow() time.Time { return time.Now().UTC().Truncate(time.Second) }

// placeholders is "?, ?, ..." for n values.
func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?, ", n), ", ")
}

// NotificationsEnabled reports whether the in-app notifications feed is on.
func (p *AgentPanel) NotificationsEnabled() bool {
	return p != nil && !p.NotificationsDisabled
}

// ---- Agent status ---------------------------------------------------------------

// ValidAgentStatus reports whether s is one of AgentStatuses.
func ValidAgentStatus(s string) bool {
	for _, status := range AgentStatuses {
		if s == status {
			return true
		}
	}
	return false
}

// Status is the agent's status, "offline" until they set one.
func (p *AgentPanel) Status(ctx context.Context, uid models.UserID) string {
	if p == nil || uid.Empty() {
		return "offline"
	}
	var status string
	err := p.DB.QueryRowContext(ctx, p.q(`SELECT status FROM {agent_statuses} WHERE user_id = ?`), string(uid)).Scan(&status)
	if err != nil || !ValidAgentStatus(status) {
		return "offline"
	}
	return status
}

// SetStatus stores the agent's status.
func (p *AgentPanel) SetStatus(ctx context.Context, uid models.UserID, status string) error {
	if !ValidAgentStatus(status) {
		return fmt.Errorf("invalid agent status %q", status)
	}
	_, err := p.DB.ExecContext(ctx, p.q(`INSERT INTO {agent_statuses} (user_id, status, updated_at) VALUES (?, ?, ?)
		ON CONFLICT (user_id) DO UPDATE SET status = excluded.status, updated_at = excluded.updated_at`),
		string(uid), status, panelNow())
	return err
}

// ---- Unread markers -------------------------------------------------------------

// MarkRead marks the ticket read for the agent. The caller has already found
// the ticket through the agent's visibility scope.
func (p *AgentPanel) MarkRead(ctx context.Context, uid models.UserID, ticketID int64) error {
	now := panelNow()
	return p.putRead(ctx, uid, ticketID, &now)
}

// MarkUnread marks the ticket unread for the agent until they open it again.
func (p *AgentPanel) MarkUnread(ctx context.Context, uid models.UserID, ticketID int64) error {
	return p.putRead(ctx, uid, ticketID, nil)
}

func (p *AgentPanel) putRead(ctx context.Context, uid models.UserID, ticketID int64, readAt *time.Time) error {
	if p == nil || uid.Empty() {
		return nil
	}
	now := panelNow()
	var at any
	if readAt != nil {
		at = *readAt
	}
	_, err := p.DB.ExecContext(ctx, p.q(`INSERT INTO {ticket_reads} (ticket_id, user_id, last_read_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (user_id, ticket_id) DO UPDATE SET last_read_at = excluded.last_read_at, updated_at = excluded.updated_at`),
		ticketID, string(uid), at, now, now)
	return err
}

// RowFields are is_unread, requester_online and requester_last_seen_at for
// each ticket, keyed by ticket id: one query for the tickets' activity, one
// for the agent's markers and one for presence, whatever the page size. The
// tickets are read through ctx's visibility scope.
func (p *AgentPanel) RowFields(ctx context.Context, uid models.UserID, ticketIDs []int64) map[int64]map[string]any {
	out := map[int64]map[string]any{}
	if p == nil || len(ticketIDs) == 0 {
		return out
	}
	args := make([]any, len(ticketIDs))
	for i, id := range ticketIDs {
		args[i] = id
	}

	type ticketRow struct {
		activity *time.Time
		subject  [2]string
	}
	tickets := map[int64]ticketRow{}
	rows, err := ticketaccess.QueryContext(ctx, p.DB, p.q(`SELECT id, requester_type, requester_id, contact_id, last_customer_activity_at
		FROM {tickets} WHERE id IN (`+placeholders(len(args))+`)`), args...)
	if err != nil {
		p.logf("escalated: agent panel rows: %v", err)
		return out
	}
	for rows.Next() {
		var id int64
		var requesterType sql.NullString
		var requesterID models.UserID
		var contactID sql.NullInt64
		var activity sql.NullTime
		if err := rows.Scan(&id, &requesterType, &requesterID, &contactID, &activity); err != nil {
			continue
		}
		row := ticketRow{subject: presenceSubject(id, requesterType.String, requesterID, contactID)}
		if activity.Valid {
			at := activity.Time
			row.activity = &at
		}
		tickets[id] = row
	}
	_ = rows.Close()

	reads := map[int64]*time.Time{}
	readRows, err := p.DB.QueryContext(ctx, p.q(`SELECT ticket_id, last_read_at FROM {ticket_reads}
		WHERE user_id = ? AND ticket_id IN (`+placeholders(len(args))+`)`), append([]any{string(uid)}, args...)...)
	if err == nil {
		for readRows.Next() {
			var id int64
			var at sql.NullTime
			if readRows.Scan(&id, &at) == nil {
				if at.Valid {
					t := at.Time
					reads[id] = &t
				} else {
					reads[id] = nil
				}
			}
		}
		_ = readRows.Close()
	}

	subjectIDs := []any{}
	for _, row := range tickets {
		subjectIDs = append(subjectIDs, row.subject[1])
	}
	seen := map[[2]string]time.Time{}
	if len(subjectIDs) > 0 {
		presenceRows, err := p.DB.QueryContext(ctx, p.q(`SELECT subject_type, subject_id, last_seen_at FROM {customer_presence}
			WHERE subject_id IN (`+placeholders(len(subjectIDs))+`)`), subjectIDs...)
		if err == nil {
			for presenceRows.Next() {
				var kind, id string
				var at time.Time
				if presenceRows.Scan(&kind, &id, &at) == nil {
					seen[[2]string{kind, id}] = at
				}
			}
			_ = presenceRows.Close()
		}
	}

	window := p.OnlineWindow
	if window <= 0 {
		window = defaultOnlineWindow
	}
	onlineAfter := time.Now().Add(-window)
	for id, row := range tickets {
		read, hasRead := reads[id]
		fields := map[string]any{
			"is_unread":              unread(hasRead, read, row.activity),
			"requester_online":       false,
			"requester_last_seen_at": nil,
		}
		if at, ok := seen[row.subject]; ok {
			fields["requester_online"] = !at.Before(onlineAfter)
			fields["requester_last_seen_at"] = at.UTC()
		}
		out[id] = fields
	}
	return out
}

// unread: a read row with no time is a ticket marked unread; otherwise it is
// unread when the customer was active after the agent last read it.
func unread(hasRead bool, readAt, activity *time.Time) bool {
	if hasRead && readAt == nil {
		return true
	}
	if activity == nil {
		return false
	}
	return !hasRead || readAt.Before(*activity)
}

// presenceSubject is whose presence a ticket shows: the requesting user, else
// the ticket's contact, else the ticket itself (a guest chat).
func presenceSubject(ticketID int64, requesterType string, requesterID models.UserID, contactID sql.NullInt64) [2]string {
	switch {
	case !requesterID.Empty() && !strings.EqualFold(requesterType, "guest") && !strings.EqualFold(requesterType, "contact"):
		return [2]string{"user", string(requesterID)}
	case contactID.Valid:
		return [2]string{"contact", fmt.Sprint(contactID.Int64)}
	default:
		return [2]string{"ticket", fmt.Sprint(ticketID)}
	}
}

func ticketSubject(t *models.Ticket) [2]string {
	requesterType := ""
	if t.RequesterType != nil {
		requesterType = *t.RequesterType
	}
	var requesterID models.UserID
	if t.RequesterID != nil {
		requesterID = *t.RequesterID
	}
	var contact sql.NullInt64
	if t.ContactID != nil {
		contact = sql.NullInt64{Int64: *t.ContactID, Valid: true}
	}
	return presenceSubject(t.ID, requesterType, requesterID, contact)
}

// CustomerSide reports whether a reply came from the customer side: a public
// reply with no author (a guest, an inbound email) or by the requesting user.
// Agents' replies, internal notes and system replies are not.
func CustomerSide(t *models.Ticket, r *models.Reply) bool {
	if r == nil || r.IsInternal || r.IsSystem {
		return false
	}
	if r.AuthorID == nil || r.AuthorID.Empty() {
		return true
	}
	subject := ticketSubject(t)
	return subject[0] == "user" && subject[1] == string(*r.AuthorID)
}

func (p *AgentPanel) customerActivity(ctx context.Context, ticketID int64, at time.Time) {
	if _, err := p.DB.ExecContext(ctx, p.q(`UPDATE {tickets} SET last_customer_activity_at = ? WHERE id = ?`), at.UTC().Truncate(time.Second), ticketID); err != nil {
		p.logf("escalated: agent panel activity: %v", err)
	}
}

// ---- Customer presence ------------------------------------------------------------

// SeeUser records that the signed-in customer is on the site, at most every
// 30 seconds.
func (p *AgentPanel) SeeUser(ctx context.Context, uid models.UserID) {
	if p == nil || uid.Empty() {
		return
	}
	p.see(ctx, [2]string{"user", string(uid)})
}

// SeeTicketCustomer records that a ticket's customer (requester, contact or
// guest) is on the site.
func (p *AgentPanel) SeeTicketCustomer(ctx context.Context, t *models.Ticket) {
	if p == nil || t == nil {
		return
	}
	p.see(ctx, ticketSubject(t))
}

// see is a conditional UPDATE, so a customer seen within the throttle costs
// one statement; an INSERT that does nothing on a conflict adds a new one.
func (p *AgentPanel) see(ctx context.Context, subject [2]string) {
	now := panelNow()
	res, err := p.DB.ExecContext(ctx, p.q(`UPDATE {customer_presence} SET last_seen_at = ?, updated_at = ?
		WHERE subject_type = ? AND subject_id = ? AND last_seen_at < ?`), now, now, subject[0], subject[1], now.Add(-presenceThrottle))
	if err != nil {
		p.logf("escalated: agent panel presence: %v", err)
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return
	}
	if _, err := p.DB.ExecContext(ctx, p.q(`INSERT INTO {customer_presence} (subject_type, subject_id, last_seen_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?) ON CONFLICT (subject_type, subject_id) DO NOTHING`), subject[0], subject[1], now, now, now); err != nil {
		p.logf("escalated: agent panel presence: %v", err)
	}
}

// ---- Events -------------------------------------------------------------------------

// TicketCreated is customer activity: a new ticket is unread for every agent.
func (p *AgentPanel) TicketCreated(ctx context.Context, t *models.Ticket) {
	if p == nil || t == nil {
		return
	}
	at := t.CreatedAt
	if at.IsZero() {
		at = time.Now()
	}
	p.customerActivity(ctx, t.ID, at)
}

// ReplyCreated handles a public reply: from the customer side it is
// customer activity, and the assignee and followers are notified (never the
// author).
func (p *AgentPanel) ReplyCreated(ctx context.Context, t *models.Ticket, r *models.Reply) {
	if p == nil || t == nil || !CustomerSide(t, r) {
		return
	}
	at := r.CreatedAt
	if at.IsZero() {
		at = time.Now()
	}
	p.customerActivity(ctx, t.ID, at)

	recipients := []models.UserID{}
	if t.AssignedTo != nil {
		recipients = append(recipients, *t.AssignedTo)
	}
	rows, err := p.DB.QueryContext(ctx, p.q(`SELECT user_id FROM {ticket_followers} WHERE ticket_id = ?`), t.ID)
	if err == nil {
		for rows.Next() {
			var id models.UserID
			if rows.Scan(&id) == nil {
				recipients = append(recipients, id)
			}
		}
		_ = rows.Close()
	}
	notified := map[models.UserID]bool{}
	for _, id := range recipients {
		if id.Empty() || notified[id] {
			continue
		}
		notified[id] = true
		p.Notify(ctx, id, "ticket_reply", t, map[string]any{"reply_id": r.ID}, r.AuthorID)
	}
}

// TicketEvent handles the other ticket events: assignment, SLA warnings and
// breaches and chat transfers notify; a chat starting or a visitor's chat
// message is customer activity.
func (p *AgentPanel) TicketEvent(ctx context.Context, event string, t *models.Ticket, payload map[string]any, causer *models.UserID) {
	if p == nil || t == nil {
		return
	}
	switch event {
	case "ticket.assigned":
		if agent := userIDOf(payload["agent_id"]); !agent.Empty() {
			p.Notify(ctx, agent, "ticket_assigned", t, nil, causer)
		}
	case "sla.warning", "sla.breached":
		if t.AssignedTo != nil {
			kind := strings.Replace(event, ".", "_", 1)
			p.Notify(ctx, *t.AssignedTo, kind, t, map[string]any{"sla_type": payload["type"]}, nil)
		}
	case "chat.transferred":
		chat, _ := payload["chat"].(map[string]any)
		to := userIDOf(chat["to_agent_id"])
		from := userIDOf(chat["from_agent_id"])
		if !to.Empty() {
			p.Notify(ctx, to, "chat_transferred", t, map[string]any{"session_id": chat["session_id"]}, &from)
		}
	case "chat.started":
		p.customerActivity(ctx, t.ID, time.Now())
	case "chat.message":
		if chat, _ := payload["chat"].(map[string]any); chat != nil && chat["author_type"] == "visitor" {
			p.customerActivity(ctx, t.ID, time.Now())
		}
	}
}

func userIDOf(v any) models.UserID {
	switch id := v.(type) {
	case nil:
		return ""
	case models.UserID:
		return id
	case *models.UserID:
		if id == nil {
			return ""
		}
		return *id
	case string:
		return models.UserID(id)
	default:
		return models.UserID(fmt.Sprint(id))
	}
}

// ---- Notifications ----------------------------------------------------------------------

// Notify stores an in-app notification for uid about the ticket and
// broadcasts it as notification.created. Nobody is notified of their own
// action (causer), and nothing is stored while notifications are off.
func (p *AgentPanel) Notify(ctx context.Context, uid models.UserID, kind string, t *models.Ticket, extra map[string]any, causer *models.UserID) {
	if !p.NotificationsEnabled() || uid.Empty() || t == nil {
		return
	}
	if causer != nil && *causer == uid {
		return
	}
	data := map[string]any{"ticket_id": t.ID, "reference": t.Reference}
	for k, v := range extra {
		data[k] = v
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return
	}
	id := newNotificationID()
	now := panelNow()
	if _, err := p.DB.ExecContext(ctx, p.q(`INSERT INTO {notifications} (id, user_id, type, data, created_at) VALUES (?, ?, ?, ?, ?)`),
		id, string(uid), kind, string(encoded), now); err != nil {
		p.logf("escalated: agent notification: %v", err)
		return
	}
	if p.Broadcaster == nil {
		return
	}
	n := notificationRow{ID: id, Type: kind, Data: data, CreatedAt: now}
	visible := p.Access == nil || !p.Access.Restricted() || p.Access.CanViewUser(ctx, uid, t.ID, true)
	var ticket *ticketSummary
	if visible {
		ticket = &ticketSummary{ID: t.ID, Reference: t.Reference, Subject: t.Subject}
	}
	_, _ = p.Broadcaster.Publish(UserChannel(uid), "notification.created", p.item(n, ticket, true))
}

func newNotificationID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

type notificationRow struct {
	ID        string
	Type      string
	Data      map[string]any
	ReadAt    *time.Time
	CreatedAt time.Time
}

type ticketSummary struct {
	ID        int64
	Reference string
	Subject   string
}

// UnreadCount is the agent's unread notification count.
func (p *AgentPanel) UnreadCount(ctx context.Context, uid models.UserID) int {
	var n int
	_ = p.DB.QueryRowContext(ctx, p.q(`SELECT COUNT(*) FROM {notifications} WHERE user_id = ? AND read_at IS NULL`), string(uid)).Scan(&n)
	return n
}

// NotificationPage is a page of an agent's notifications, newest first:
// the feed items and the paginator meta.
func (p *AgentPanel) NotificationPage(ctx context.Context, uid models.UserID, page, perPage int, unreadOnly bool) ([]map[string]any, map[string]any, error) {
	where := `WHERE user_id = ?`
	if unreadOnly {
		where += ` AND read_at IS NULL`
	}
	var total int
	if err := p.DB.QueryRowContext(ctx, p.q(`SELECT COUNT(*) FROM {notifications} `+where), string(uid)).Scan(&total); err != nil {
		return nil, nil, err
	}
	rows, err := p.DB.QueryContext(ctx, p.q(fmt.Sprintf(`SELECT id, type, data, read_at, created_at FROM {notifications} %s
		ORDER BY created_at DESC, id DESC LIMIT %d OFFSET %d`, where, perPage, (page-1)*perPage)), string(uid))
	if err != nil {
		return nil, nil, err
	}
	var list []notificationRow
	for rows.Next() {
		var n notificationRow
		var data string
		var readAt sql.NullTime
		if err := rows.Scan(&n.ID, &n.Type, &data, &readAt, &n.CreatedAt); err != nil {
			_ = rows.Close()
			return nil, nil, err
		}
		_ = json.Unmarshal([]byte(data), &n.Data)
		if readAt.Valid {
			at := readAt.Time
			n.ReadAt = &at
		}
		list = append(list, n)
	}
	_ = rows.Close()

	items := p.present(ctx, list)
	lastPage := (total + perPage - 1) / perPage
	if lastPage < 1 {
		lastPage = 1
	}
	return items, map[string]any{"current_page": page, "last_page": lastPage, "per_page": perPage, "total": total}, nil
}

// present builds the feed items, reading their tickets through ctx's
// visibility scope in one query: a ticket the agent can no longer see is
// redacted.
func (p *AgentPanel) present(ctx context.Context, list []notificationRow) []map[string]any {
	ids := []any{}
	for _, n := range list {
		if id, ok := notificationTicketID(n.Data); ok {
			ids = append(ids, id)
		}
	}
	tickets := map[int64]*ticketSummary{}
	if len(ids) > 0 {
		rows, err := ticketaccess.QueryContext(ctx, p.DB, p.q(`SELECT id, reference, subject FROM {tickets} WHERE id IN (`+placeholders(len(ids))+`)`), ids...)
		if err == nil {
			for rows.Next() {
				var s ticketSummary
				if rows.Scan(&s.ID, &s.Reference, &s.Subject) == nil {
					tickets[s.ID] = &s
				}
			}
			_ = rows.Close()
		}
	}
	items := make([]map[string]any, 0, len(list))
	for _, n := range list {
		id, hasTicket := notificationTicketID(n.Data)
		items = append(items, p.item(n, tickets[id], hasTicket))
	}
	return items
}

func notificationTicketID(data map[string]any) (int64, bool) {
	switch id := data["ticket_id"].(type) {
	case float64:
		return int64(id), true
	case int64:
		return id, true
	case int:
		return int64(id), true
	}
	return 0, false
}

// item is one feed item: {id, type, title, body, ticket, url, created_at,
// read_at}. A notification about a ticket that is gone, or out of sight, is
// redacted.
func (p *AgentPanel) item(n notificationRow, ticket *ticketSummary, hasTicket bool) map[string]any {
	kind := n.Type
	if _, ok := notificationTitles[kind]; !ok {
		kind = "generic"
	}
	out := map[string]any{
		"id": n.ID, "type": kind, "title": unavailableTitle, "body": nil, "ticket": nil, "url": nil,
		"created_at": n.CreatedAt.UTC(), "read_at": nil,
	}
	if n.ReadAt != nil {
		out["read_at"] = n.ReadAt.UTC()
	}
	if hasTicket && ticket == nil {
		return out
	}
	reference, _ := n.Data["reference"].(string)
	if ticket != nil {
		reference = ticket.Reference
		out["body"] = ticket.Subject
		out["ticket"] = map[string]any{"id": ticket.ID, "reference": ticket.Reference, "subject": ticket.Subject}
		prefix := p.RoutePrefix
		if prefix == "" {
			prefix = "/escalated"
		}
		out["url"] = prefix + "/agent/tickets/" + ticket.Reference
	}
	out["title"] = fmt.Sprintf(notificationTitles[kind], reference)
	return out
}

// ReadNotification marks one of the agent's notifications read. It reports
// false when the agent has no notification with that id.
func (p *AgentPanel) ReadNotification(ctx context.Context, uid models.UserID, id string) (bool, error) {
	res, err := p.DB.ExecContext(ctx, p.q(`UPDATE {notifications} SET read_at = COALESCE(read_at, ?) WHERE id = ? AND user_id = ?`), panelNow(), id, string(uid))
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ReadAllNotifications marks every notification of the agent's read.
func (p *AgentPanel) ReadAllNotifications(ctx context.Context, uid models.UserID) error {
	_, err := p.DB.ExecContext(ctx, p.q(`UPDATE {notifications} SET read_at = ? WHERE user_id = ? AND read_at IS NULL`), panelNow(), string(uid))
	return err
}

// ---- Knowledge base search ----------------------------------------------------------------

// KBSearch is the agents' knowledge-base search: published articles whose
// title or body contains term (case-insensitive, % and _ taken literally),
// title matches first, then the most recently updated.
func (p *AgentPanel) KBSearch(ctx context.Context, term string, limit int) ([]map[string]any, error) {
	like := "%" + strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(strings.ToLower(term)) + "%"
	rows, err := p.DB.QueryContext(ctx, p.q(fmt.Sprintf(`SELECT a.id, a.title, a.slug, a.body, a.category_id, c.name, a.updated_at
		FROM {articles} a LEFT JOIN {article_categories} c ON c.id = a.category_id
		WHERE a.status = ? AND (LOWER(a.title) LIKE ? ESCAPE '!' OR LOWER(a.body) LIKE ? ESCAPE '!')
		ORDER BY CASE WHEN LOWER(a.title) LIKE ? ESCAPE '!' THEN 0 ELSE 1 END, a.updated_at DESC, a.id DESC
		LIMIT %d`, limit)), models.ArticleStatusPublished, like, like, like)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id int64
		var title, slug string
		var body, categoryName sql.NullString
		var categoryID sql.NullInt64
		var updated time.Time
		if err := rows.Scan(&id, &title, &slug, &body, &categoryID, &categoryName, &updated); err != nil {
			return nil, err
		}
		var category any
		if categoryID.Valid && categoryName.Valid {
			category = map[string]any{"id": categoryID.Int64, "name": categoryName.String}
		}
		out = append(out, map[string]any{
			"id": id, "title": title, "slug": slug, "body": body.String, "category": category, "updated_at": updated.UTC(),
		})
	}
	return out, rows.Err()
}
