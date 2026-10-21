package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/escalated-dev/escalated-go/internal/sqldialect"
	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/ticketaccess"
)

// uiQueries holds the few page lookups the Store interface has no method for
// (counts, followers, ratings). They go through ticketaccess, so the ticket
// visibility rules apply to every count. A nil DB answers zero values.
type uiQueries struct {
	DB     *sql.DB
	Prefix string
}

func (q uiQueries) t(name string) string {
	prefix := q.Prefix
	if prefix == "" {
		prefix = "escalated_"
	}
	return prefix + name
}

func (q uiQueries) count(ctx context.Context, query string, args ...any) int {
	if q.DB == nil {
		return 0
	}
	var n int
	if err := ticketaccess.QueryRowContext(ctx, q.DB, sqldialect.Rebind(q.DB, query), args...).Scan(&n); err != nil {
		return 0
	}
	return n
}

// openStatuses is the IN list of the statuses Ticket.IsOpen counts as open.
func openStatuses() string {
	var ids []int
	for status := range models.StatusName {
		if (&models.Ticket{Status: status}).IsOpen() {
			ids = append(ids, status)
		}
	}
	sort.Ints(ids)
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.Itoa(id)
	}
	return "(" + strings.Join(parts, ",") + ")"
}

// countTickets counts tickets matching where (a SQL condition with ?
// placeholders).
func (q uiQueries) countTickets(ctx context.Context, where string, args ...any) int {
	return q.count(ctx, fmt.Sprintf("SELECT COUNT(1) FROM %s WHERE %s", q.t("tickets"), where), args...)
}

func (q uiQueries) followersCount(ctx context.Context, ticketID int64) int {
	return q.count(ctx, fmt.Sprintf("SELECT COUNT(1) FROM %s WHERE ticket_id = ?", q.t("ticket_followers")), ticketID)
}

func (q uiQueries) isFollowing(ctx context.Context, ticketID int64, uid models.UserID) bool {
	if uid.Empty() {
		return false
	}
	return q.count(ctx, fmt.Sprintf("SELECT COUNT(1) FROM %s WHERE ticket_id = ? AND user_id = ?", q.t("ticket_followers")), ticketID, string(uid)) > 0
}

// follow records the user as a follower of the ticket (once).
func (q uiQueries) follow(ctx context.Context, ticketID int64, uid models.UserID) error {
	if q.DB == nil {
		return fmt.Errorf("no database")
	}
	_, err := ticketaccess.ExecContext(ctx, q.DB, sqldialect.Rebind(q.DB, fmt.Sprintf(
		`INSERT INTO %s (ticket_id, user_id) VALUES (?, ?) ON CONFLICT DO NOTHING`, q.t("ticket_followers"))), ticketID, uid)
	return err
}

// unfollow removes the user from the ticket's followers.
func (q uiQueries) unfollow(ctx context.Context, ticketID int64, uid models.UserID) error {
	if q.DB == nil {
		return fmt.Errorf("no database")
	}
	_, err := ticketaccess.ExecContext(ctx, q.DB, sqldialect.Rebind(q.DB, fmt.Sprintf(
		`DELETE FROM %s WHERE ticket_id = ? AND user_id = ?`, q.t("ticket_followers"))), ticketID, uid)
	return err
}

// rating is the CSAT rating the requester gave, or nil when there is none.
func (q uiQueries) rating(ctx context.Context, ticketID int64) *models.SatisfactionRating {
	if q.DB == nil {
		return nil
	}
	var r models.SatisfactionRating
	var comment sql.NullString
	err := ticketaccess.QueryRowContext(ctx, q.DB, sqldialect.Rebind(q.DB, fmt.Sprintf(
		`SELECT id, rating, comment, created_at FROM %s WHERE ticket_id = ? AND rating > 0 ORDER BY id DESC LIMIT 1`,
		q.t("satisfaction_ratings"))), ticketID).Scan(&r.ID, &r.Rating, &comment, &r.CreatedAt)
	if err != nil {
		return nil
	}
	r.TicketID = ticketID
	if comment.Valid {
		r.Comment = &comment.String
	}
	return &r
}

// requesterTicketCount is Laravel's Ticket::requester_ticket_count: the
// requester's tickets, by guest email for a guest.
func (q uiQueries) requesterTicketCount(ctx context.Context, t *models.Ticket) int {
	if t.RequesterID != nil && !t.RequesterID.Empty() {
		n := q.countTickets(ctx, "requester_id = ?", string(*t.RequesterID))
		if n < 1 {
			n = 1
		}
		return n
	}
	if t.GuestEmail != nil && *t.GuestEmail != "" {
		if n := q.countTickets(ctx, "guest_email = ?", *t.GuestEmail); n > 0 {
			return n
		}
	}
	return 1
}

// groupCounts counts the rows of table per column value (withCount), keyed
// by that value.
func (q uiQueries) groupCounts(ctx context.Context, table, column string) map[int64]int {
	out := map[int64]int{}
	if q.DB == nil {
		return out
	}
	rows, err := ticketaccess.QueryContext(ctx, q.DB, fmt.Sprintf(
		`SELECT %[2]s, COUNT(1) FROM %[1]s WHERE %[2]s IS NOT NULL GROUP BY %[2]s`, q.t(table), column))
	if err != nil {
		return out
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id int64
		var n int
		if rows.Scan(&id, &n) == nil {
			out[id] = n
		}
	}
	return out
}

// slaTicketCounts is SlaPolicy::withCount('tickets'), keyed by policy id.
func (q uiQueries) slaTicketCounts(ctx context.Context) map[int64]int {
	out := map[int64]int{}
	if q.DB == nil {
		return out
	}
	rows, err := ticketaccess.QueryContext(ctx, q.DB, fmt.Sprintf(
		`SELECT sla_policy_id, COUNT(1) FROM %s WHERE sla_policy_id IS NOT NULL GROUP BY sla_policy_id`, q.t("tickets")))
	if err != nil {
		return out
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id int64
		var n int
		if rows.Scan(&id, &n) == nil {
			out[id] = n
		}
	}
	return out
}

// linkedTicketIDs are the tickets linked to this one in either direction
// (Laravel's linksAsParent and linksAsChild).
func (q uiQueries) linkedTicketIDs(ctx context.Context, ticketID int64) []int64 {
	if q.DB == nil {
		return nil
	}
	rows, err := ticketaccess.QueryContext(ctx, q.DB, sqldialect.Rebind(q.DB, fmt.Sprintf(
		`SELECT child_ticket_id FROM %[1]s WHERE parent_ticket_id = ?
		 UNION SELECT parent_ticket_id FROM %[1]s WHERE child_ticket_id = ?`, q.t("ticket_links"))), ticketID, ticketID)
	if err != nil {
		return nil
	}
	defer func() { _ = rows.Close() }()
	var ids []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func startOfDay(now time.Time) time.Time {
	y, m, d := now.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, now.Location())
}

// startOfWeek is Monday 00:00, as Carbon's startOfWeek().
func startOfWeek(now time.Time) time.Time {
	day := startOfDay(now)
	offset := (int(day.Weekday()) + 6) % 7
	return day.AddDate(0, 0, -offset)
}
