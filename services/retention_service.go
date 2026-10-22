package services

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"time"

	"github.com/escalated-dev/escalated-go/internal/sqldialect"
	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/store"
)

// retentionDaysMap maps a retention setting value to a window in days.
// Mirrors the Laravel/Phoenix retention day map. "never" (and unknown
// values) mean retention is disabled.
var retentionDaysMap = map[string]int{
	"90_days":  90,
	"180_days": 180,
	"365_days": 365,
	"1_year":   365,
	"2_years":  730,
	"3_years":  1095,
	"5_years":  1825,
}

// RetentionDays returns the retention window in days for a setting value and
// whether retention is enabled (false for "never" or any unknown value). Pure.
func RetentionDays(setting string) (int, bool) {
	days, ok := retentionDaysMap[setting]
	if !ok || days <= 0 {
		return 0, false
	}
	return days, true
}

// RetentionCutoff returns the cutoff time for a setting (records older than
// this are expired) and whether retention is enabled. Pure.
func RetentionCutoff(setting string, now time.Time) (time.Time, bool) {
	days, ok := RetentionDays(setting)
	if !ok {
		return time.Time{}, false
	}
	return now.AddDate(0, 0, -days), true
}

// RetentionReport summarizes a purge run. On a dry run the *Deleted counts
// are what a real run would delete.
type RetentionReport struct {
	DryRun                    bool `json:"dry_run"`
	ClosedTicketsDeleted      int  `json:"closed_tickets_deleted"`
	AttachmentsDeleted        int  `json:"attachments_deleted"`
	AuditLogsDeleted          int  `json:"audit_logs_deleted"`
	GuestVerificationsDeleted int  `json:"guest_verifications_deleted"`

	// ClosedTicketCandidates is ClosedTicketsDeleted under its old name, kept
	// for clients of the purge endpoint that read it.
	ClosedTicketCandidates int `json:"closed_ticket_candidates"`
}

// RetentionPreview is what the configured retention periods would purge now:
// the data retention page's purgePreview.
type RetentionPreview struct {
	Tickets     int `json:"tickets"`
	Attachments int `json:"attachments"`
	AuditLogs   int `json:"audit_logs"`
}

// RetentionService applies the data retention settings (retention_closed_tickets,
// retention_attachments, retention_audit_logs; each "never" unless set). Ports
// Laravel's escalated:purge-expired command.
//
// Tickets have no soft delete in this package, so a closed ticket past its
// period is deleted outright, with the rows that point at it; there is no
// 30-day grace period and no --force. Attachment rows are deleted; their
// files are the host's (StoragePath is whatever the host stored) and are left
// alone.
type RetentionService struct {
	db     *sql.DB
	store  store.Store
	prefix string
}

// NewRetentionService creates a RetentionService over tables prefixed
// "escalated_". Use WithPrefix for another Config.TablePrefix.
func NewRetentionService(db *sql.DB, s store.Store) *RetentionService {
	return &RetentionService{db: db, store: s, prefix: "escalated_"}
}

// WithPrefix sets the table prefix (Config.TablePrefix) and returns rs.
func (rs *RetentionService) WithPrefix(prefix string) *RetentionService {
	if prefix != "" {
		rs.prefix = prefix
	}
	return rs
}

func (rs *RetentionService) t(name string) string { return rs.prefix + name }

func (rs *RetentionService) q(query string) string { return sqldialect.Rebind(rs.db, query) }

// PurgeExpired applies the retention settings as of now: closed tickets
// (status closed, closed_at before the cutoff) are deleted with their rows,
// then attachments and audit log rows created before their cutoffs, then
// expired guest verification codes. When dryRun is true nothing is deleted;
// the counts are still reported.
func (rs *RetentionService) PurgeExpired(ctx context.Context, now time.Time, dryRun bool) (*RetentionReport, error) {
	report := &RetentionReport{DryRun: dryRun}

	n, err := rs.purgeClosedTickets(ctx, now, dryRun)
	if err != nil {
		return nil, err
	}
	report.ClosedTicketsDeleted = n
	report.ClosedTicketCandidates = n

	n, err = rs.purgeWith(ctx, "retention_attachments",
		"SELECT COUNT(1) FROM "+rs.t("attachments")+" WHERE created_at < ?",
		"DELETE FROM "+rs.t("attachments")+" WHERE created_at < ?", now, dryRun)
	if err != nil {
		return nil, err
	}
	report.AttachmentsDeleted = n

	n, err = rs.purgeWith(ctx, "retention_audit_logs",
		"SELECT COUNT(1) FROM "+rs.t("audit_logs")+" WHERE created_at < ?",
		"DELETE FROM "+rs.t("audit_logs")+" WHERE created_at < ?", now, dryRun)
	if err != nil {
		return nil, err
	}
	report.AuditLogsDeleted = n

	n, err = rs.purgeGuestVerifications(ctx, now, dryRun)
	if err != nil {
		return nil, err
	}
	report.GuestVerificationsDeleted = n

	return report, nil
}

// Preview counts what each retention period would purge now. It never fails:
// a count it cannot make is 0, as Laravel's page does.
func (rs *RetentionService) Preview(ctx context.Context, now time.Time) RetentionPreview {
	var p RetentionPreview
	if cutoff, enabled, err := rs.cutoff(ctx, "retention_closed_tickets", now); err == nil && enabled {
		p.Tickets, _ = rs.count(ctx, "SELECT COUNT(1) FROM "+rs.t("tickets")+" WHERE status = ? AND closed_at IS NOT NULL AND closed_at < ?", models.StatusClosed, cutoff)
	}
	if cutoff, enabled, err := rs.cutoff(ctx, "retention_attachments", now); err == nil && enabled {
		p.Attachments, _ = rs.count(ctx, "SELECT COUNT(1) FROM "+rs.t("attachments")+" WHERE created_at < ?", cutoff)
	}
	if cutoff, enabled, err := rs.cutoff(ctx, "retention_audit_logs", now); err == nil && enabled {
		p.AuditLogs, _ = rs.count(ctx, "SELECT COUNT(1) FROM "+rs.t("audit_logs")+" WHERE created_at < ?", cutoff)
	}
	return p
}

func (rs *RetentionService) count(ctx context.Context, query string, args ...any) (int, error) {
	var n int
	if err := rs.db.QueryRowContext(ctx, rs.q(query), args...).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func (rs *RetentionService) purgeWith(ctx context.Context, settingKey, countQuery, deleteQuery string, now time.Time, dryRun bool) (int, error) {
	cutoff, enabled, err := rs.cutoff(ctx, settingKey, now)
	if err != nil || !enabled {
		return 0, err
	}

	count, err := rs.count(ctx, countQuery, cutoff)
	if err != nil {
		return 0, err
	}
	if count == 0 || dryRun {
		return count, nil
	}
	if _, err := rs.db.ExecContext(ctx, rs.q(deleteQuery), cutoff); err != nil {
		return 0, err
	}
	return count, nil
}

// purgeBatch is how many tickets one transaction deletes.
const purgeBatch = 200

// purgeClosedTickets deletes closed tickets past the retention_closed_tickets
// cutoff, a batch per transaction, children first: rows with a foreign key are
// deleted explicitly too, as SQLite only cascades when the host turned
// foreign_keys on.
func (rs *RetentionService) purgeClosedTickets(ctx context.Context, now time.Time, dryRun bool) (int, error) {
	cutoff, enabled, err := rs.cutoff(ctx, "retention_closed_tickets", now)
	if err != nil || !enabled {
		return 0, err
	}
	where := " WHERE status = ? AND closed_at IS NOT NULL AND closed_at < ?"
	count, err := rs.count(ctx, "SELECT COUNT(1) FROM "+rs.t("tickets")+where, models.StatusClosed, cutoff)
	if err != nil || count == 0 || dryRun {
		return count, err
	}

	deleted := 0
	for {
		ids, err := rs.ticketIDs(ctx, "SELECT id FROM "+rs.t("tickets")+where+" ORDER BY id LIMIT "+strconv.Itoa(purgeBatch), models.StatusClosed, cutoff)
		if err != nil {
			return deleted, err
		}
		if len(ids) == 0 {
			return deleted, nil
		}
		if err := rs.deleteTickets(ctx, ids); err != nil {
			return deleted, err
		}
		deleted += len(ids)
	}
}

func (rs *RetentionService) ticketIDs(ctx context.Context, query string, args ...any) ([]any, error) {
	rows, err := rs.db.QueryContext(ctx, rs.q(query), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []any
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// deleteTickets deletes the tickets and every row pointing at them in one
// transaction. Tickets merged into one of them are kept and unlinked.
func (rs *RetentionService) deleteTickets(ctx context.Context, ids []any) error {
	in := "(" + strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",") + ")"
	twice := append(append([]any{}, ids...), ids...)
	statements := []struct {
		query string
		args  []any
	}{
		{"DELETE FROM " + rs.t("chat_messages") + " WHERE chat_session_id IN (SELECT id FROM " + rs.t("chat_sessions") + " WHERE ticket_id IN " + in + ")", ids},
		{"DELETE FROM " + rs.t("chat_sessions") + " WHERE ticket_id IN " + in, ids},
		{"DELETE FROM " + rs.t("side_conversation_replies") + " WHERE side_conversation_id IN (SELECT id FROM " + rs.t("side_conversations") + " WHERE ticket_id IN " + in + ")", ids},
		{"DELETE FROM " + rs.t("side_conversations") + " WHERE ticket_id IN " + in, ids},
		{"DELETE FROM " + rs.t("workflow_logs") + " WHERE ticket_id IN " + in, ids},
		{"DELETE FROM " + rs.t("delayed_actions") + " WHERE ticket_id IN " + in, ids},
		{"DELETE FROM " + rs.t("satisfaction_ratings") + " WHERE ticket_id IN " + in, ids},
		{"DELETE FROM " + rs.t("ticket_links") + " WHERE parent_ticket_id IN " + in + " OR child_ticket_id IN " + in, twice},
		{"DELETE FROM " + rs.t("custom_field_values") + " WHERE entity_type = 'ticket' AND entity_id IN " + in, ids},
		{"DELETE FROM " + rs.t("attachments") + " WHERE ticket_id IN " + in, ids},
		{"DELETE FROM " + rs.t("replies") + " WHERE ticket_id IN " + in, ids},
		{"DELETE FROM " + rs.t("ticket_tags") + " WHERE ticket_id IN " + in, ids},
		{"DELETE FROM " + rs.t("ticket_followers") + " WHERE ticket_id IN " + in, ids},
		{"DELETE FROM " + rs.t("ticket_subjects") + " WHERE ticket_id IN " + in, ids},
		{"DELETE FROM " + rs.t("ticket_activities") + " WHERE ticket_id IN " + in, ids},
		{"UPDATE " + rs.t("tickets") + " SET merged_into_id = NULL WHERE merged_into_id IN " + in, ids},
		{"DELETE FROM " + rs.t("tickets") + " WHERE id IN " + in, ids},
	}

	tx, err := rs.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for _, s := range statements {
		if _, err := tx.ExecContext(ctx, rs.q(s.query), s.args...); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// purgeGuestVerifications deletes guest verification codes that have expired.
// It runs whatever the settings say, as Laravel's command does.
func (rs *RetentionService) purgeGuestVerifications(ctx context.Context, now time.Time, dryRun bool) (int, error) {
	count, err := rs.count(ctx, "SELECT COUNT(1) FROM "+rs.t("guest_verifications")+" WHERE expires_at <= ?", now)
	if err != nil || count == 0 || dryRun {
		return count, err
	}
	if _, err := rs.db.ExecContext(ctx, rs.q("DELETE FROM "+rs.t("guest_verifications")+" WHERE expires_at <= ?"), now); err != nil {
		return 0, err
	}
	return count, nil
}

func (rs *RetentionService) cutoff(ctx context.Context, settingKey string, now time.Time) (time.Time, bool, error) {
	setting, err := rs.store.GetSetting(ctx, settingKey)
	if err != nil {
		return time.Time{}, false, err
	}
	if setting == "" {
		setting = "never"
	}
	cutoff, enabled := RetentionCutoff(setting, now)
	return cutoff, enabled, nil
}
