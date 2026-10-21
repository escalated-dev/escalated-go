package services

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"time"

	"github.com/escalated-dev/escalated-go/internal/sqldialect"
	"github.com/escalated-dev/escalated-go/models"
)

// SQLAuditLogStore keeps audit log entries in the escalated_audit_logs table.
// It implements AuditLogStore.
type SQLAuditLogStore struct {
	DB *sql.DB
	// Prefix is the table prefix ("escalated_" when empty).
	Prefix string
}

func (s *SQLAuditLogStore) table() string {
	if s.Prefix == "" {
		return "escalated_audit_logs"
	}
	return s.Prefix + "audit_logs"
}

func (s *SQLAuditLogStore) q(query string) string {
	return sqldialect.Rebind(s.DB, strings.ReplaceAll(query, "{t}", s.table()))
}

// CreateAuditLog inserts an entry.
func (s *SQLAuditLogStore) CreateAuditLog(l *models.AuditLog) error {
	if l.CreatedAt.IsZero() {
		l.CreatedAt = time.Now()
	}
	res, err := sqldialect.ExecInsert(s.DB, strings.ReplaceAll(`INSERT INTO {t}
		(action, entity_type, entity_id, performer_type, performer_id, old_values, new_values, ip_address, user_agent, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, "{t}", s.table()),
		l.Action, l.EntityType, l.EntityID, l.PerformerType, l.PerformerID,
		nullableJSON(l.OldValues), nullableJSON(l.NewValues), l.IPAddress, l.UserAgent, l.CreatedAt)
	if err != nil {
		return err
	}
	l.ID, _ = res.LastInsertId()
	return nil
}

func nullableJSON(v models.JSONText) any {
	if len(v) == 0 {
		return nil
	}
	return string(v)
}

const auditColumns = `id, action, entity_type, entity_id, performer_type, performer_id, old_values, new_values, ip_address, user_agent, created_at`

func (s *SQLAuditLogStore) scan(rows *sql.Rows) ([]*models.AuditLog, error) {
	defer rows.Close()
	out := []*models.AuditLog{}
	for rows.Next() {
		var l models.AuditLog
		var oldV, newV sql.NullString
		if err := rows.Scan(&l.ID, &l.Action, &l.EntityType, &l.EntityID, &l.PerformerType, &l.PerformerID,
			&oldV, &newV, &l.IPAddress, &l.UserAgent, &l.CreatedAt); err != nil {
			return nil, err
		}
		if oldV.Valid {
			l.OldValues = models.JSONText(oldV.String)
		}
		if newV.Valid {
			l.NewValues = models.JSONText(newV.String)
		}
		out = append(out, &l)
	}
	return out, rows.Err()
}

// ListAuditLogsByEntity returns the newest entries for one entity.
func (s *SQLAuditLogStore) ListAuditLogsByEntity(entityType string, entityID int64, limit int) ([]*models.AuditLog, error) {
	rows, err := s.DB.Query(s.q(`SELECT `+auditColumns+` FROM {t} WHERE entity_type = ? AND entity_id = ? ORDER BY created_at DESC, id DESC LIMIT `+strconv.Itoa(limit)), entityType, entityID)
	if err != nil {
		return nil, err
	}
	return s.scan(rows)
}

// ListAuditLogsByPerformer returns the newest entries by one performer.
func (s *SQLAuditLogStore) ListAuditLogsByPerformer(performerType string, performerID models.UserID, limit int) ([]*models.AuditLog, error) {
	rows, err := s.DB.Query(s.q(`SELECT `+auditColumns+` FROM {t} WHERE performer_type = ? AND performer_id = ? ORDER BY created_at DESC, id DESC LIMIT `+strconv.Itoa(limit)), performerType, performerID)
	if err != nil {
		return nil, err
	}
	return s.scan(rows)
}

// AuditLogFilter narrows List (Laravel AuditLogController filters).
type AuditLogFilter struct {
	PerformerID string
	Action      string
	EntityType  string
	From, To    *time.Time
	Page        int
	PerPage     int
}

// List returns one page of entries, newest first, and the total count.
func (s *SQLAuditLogStore) List(ctx context.Context, f AuditLogFilter) ([]*models.AuditLog, int, error) {
	where := []string{"1 = 1"}
	var args []any
	if f.PerformerID != "" {
		where = append(where, "performer_id = ?")
		args = append(args, f.PerformerID)
	}
	if f.Action != "" {
		where = append(where, "action = ?")
		args = append(args, f.Action)
	}
	if f.EntityType != "" {
		where = append(where, "entity_type = ?")
		args = append(args, f.EntityType)
	}
	if f.From != nil {
		where = append(where, "created_at >= ?")
		args = append(args, *f.From)
	}
	if f.To != nil {
		where = append(where, "created_at < ?")
		args = append(args, *f.To)
	}
	cond := strings.Join(where, " AND ")
	var total int
	if err := s.DB.QueryRowContext(ctx, s.q(`SELECT COUNT(*) FROM {t} WHERE `+cond), args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if f.PerPage <= 0 {
		f.PerPage = 50
	}
	if f.Page <= 0 {
		f.Page = 1
	}
	rows, err := s.DB.QueryContext(ctx, s.q(`SELECT `+auditColumns+` FROM {t} WHERE `+cond+
		` ORDER BY created_at DESC, id DESC LIMIT `+strconv.Itoa(f.PerPage)+` OFFSET `+strconv.Itoa((f.Page-1)*f.PerPage)), args...)
	if err != nil {
		return nil, 0, err
	}
	logs, err := s.scan(rows)
	return logs, total, err
}

// EntityTypes returns the distinct entity types that have entries.
func (s *SQLAuditLogStore) EntityTypes(ctx context.Context) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, s.q(`SELECT DISTINCT entity_type FROM {t} ORDER BY entity_type`))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
