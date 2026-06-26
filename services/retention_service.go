package services

import (
	"context"
	"database/sql"
	"time"

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

// RetentionReport summarizes a purge run.
type RetentionReport struct {
	DryRun                 bool `json:"dry_run"`
	AttachmentsDeleted     int  `json:"attachments_deleted"`
	AuditLogsDeleted       int  `json:"audit_logs_deleted"`
	ClosedTicketCandidates int  `json:"closed_ticket_candidates"`
}

// RetentionService purges attachments and audit logs older than the
// configured retention policy. Closed-ticket retention is intentionally only
// *reported*, not enforced: that requires a soft-delete grace period (a
// deleted_at column the schema does not yet have). Mirrors the Phoenix
// escalated.purge_expired task and the Laravel escalated:purge-expired command.
type RetentionService struct {
	db    *sql.DB
	store store.Store
}

// NewRetentionService creates a RetentionService.
func NewRetentionService(db *sql.DB, s store.Store) *RetentionService {
	return &RetentionService{db: db, store: s}
}

// PurgeExpired deletes expired attachments and audit logs (per the configured
// retention settings) and reports the closed-ticket purge candidate count.
// When dryRun is true nothing is deleted; counts are still reported.
func (rs *RetentionService) PurgeExpired(ctx context.Context, now time.Time, dryRun bool) (*RetentionReport, error) {
	report := &RetentionReport{DryRun: dryRun}

	n, err := rs.purgeWith(ctx, "retention_attachments",
		"SELECT COUNT(1) FROM escalated_attachments WHERE created_at < ?",
		"DELETE FROM escalated_attachments WHERE created_at < ?", now, dryRun)
	if err != nil {
		return nil, err
	}
	report.AttachmentsDeleted = n

	n, err = rs.purgeWith(ctx, "retention_audit_logs",
		"SELECT COUNT(1) FROM escalated_audit_logs WHERE created_at < ?",
		"DELETE FROM escalated_audit_logs WHERE created_at < ?", now, dryRun)
	if err != nil {
		return nil, err
	}
	report.AuditLogsDeleted = n

	candidates, err := rs.closedTicketCandidates(ctx, now)
	if err != nil {
		return nil, err
	}
	report.ClosedTicketCandidates = candidates

	return report, nil
}

func (rs *RetentionService) purgeWith(ctx context.Context, settingKey, countQuery, deleteQuery string, now time.Time, dryRun bool) (int, error) {
	cutoff, enabled, err := rs.cutoff(ctx, settingKey, now)
	if err != nil || !enabled {
		return 0, err
	}

	var count int
	if err := rs.db.QueryRowContext(ctx, countQuery, cutoff).Scan(&count); err != nil {
		return 0, err
	}
	if count == 0 || dryRun {
		return count, nil
	}
	if _, err := rs.db.ExecContext(ctx, deleteQuery, cutoff); err != nil {
		return 0, err
	}
	return count, nil
}

func (rs *RetentionService) closedTicketCandidates(ctx context.Context, now time.Time) (int, error) {
	cutoff, enabled, err := rs.cutoff(ctx, "retention_closed_tickets", now)
	if err != nil || !enabled {
		return 0, err
	}

	var count int
	if err := rs.db.QueryRowContext(ctx,
		"SELECT COUNT(1) FROM escalated_tickets WHERE status = ? AND closed_at IS NOT NULL AND closed_at < ?",
		models.StatusClosed, cutoff).Scan(&count); err != nil {
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
