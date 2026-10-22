package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/escalated-dev/escalated-go/models"
)

// BusinessSchedules stores business schedules and their holidays (Laravel
// BusinessSchedule / Holiday). It is optional: a custom Store that does not
// implement it simply has no schedules, and business-hours SLA policies then
// keep calendar-hour deadlines.
type BusinessSchedules interface {
	// ListBusinessSchedules returns every schedule with its holidays, by id.
	ListBusinessSchedules(ctx context.Context) ([]*models.BusinessSchedule, error)
	// GetBusinessSchedule returns the schedule with its holidays, or nil.
	GetBusinessSchedule(ctx context.Context, id int64) (*models.BusinessSchedule, error)
	// GetDefaultBusinessSchedule returns the active default schedule with
	// the lowest id, or nil.
	GetDefaultBusinessSchedule(ctx context.Context) (*models.BusinessSchedule, error)
	// CreateBusinessSchedule inserts the schedule and its holidays. A
	// default schedule unsets every other default.
	CreateBusinessSchedule(ctx context.Context, s *models.BusinessSchedule) error
	// UpdateBusinessSchedule saves the schedule and replaces its holidays.
	UpdateBusinessSchedule(ctx context.Context, s *models.BusinessSchedule) error
	// DeleteBusinessSchedule deletes the schedule and its holidays and
	// clears the policies and departments that pointed at it.
	DeleteBusinessSchedule(ctx context.Context, id int64) error
}

// BusinessSchedulesFor returns s's business schedules, or nil when the store
// does not keep any.
func BusinessSchedulesFor(s Store) BusinessSchedules {
	if bs, ok := s.(BusinessSchedules); ok {
		return bs
	}
	return nil
}

// scheduleSQL is the one implementation behind both built-in stores.
type scheduleSQL struct {
	db       *sql.DB
	prefix   string
	postgres bool
}

func (q scheduleSQL) bind(query string) string {
	if !q.postgres {
		return query
	}
	var out strings.Builder
	n := 0
	for _, c := range query {
		if c == '?' {
			n++
			fmt.Fprintf(&out, "$%d", n)
			continue
		}
		out.WriteRune(c)
	}
	return out.String()
}

const scheduleColumns = "id, name, timezone, hours, is_default, is_active, created_at, updated_at"

func (q scheduleSQL) list(ctx context.Context, where string, args ...any) ([]*models.BusinessSchedule, error) {
	rows, err := q.db.QueryContext(ctx, q.bind("SELECT "+scheduleColumns+" FROM "+q.prefix+"business_schedules "+where), args...)
	if err != nil {
		return nil, err
	}
	var out []*models.BusinessSchedule
	byID := map[int64]*models.BusinessSchedule{}
	for rows.Next() {
		s := &models.BusinessSchedule{Holidays: []models.Holiday{}}
		var hours sql.NullString
		if err := rows.Scan(&s.ID, &s.Name, &s.Timezone, &hours, &s.IsDefault, &s.IsActive, &s.CreatedAt, &s.UpdatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		s.Hours = map[string]models.DaySchedule{}
		if hours.Valid && hours.String != "" {
			_ = json.Unmarshal([]byte(hours.String), &s.Hours)
		}
		out = append(out, s)
		byID[s.ID] = s
	}
	err = rows.Err()
	rows.Close()
	if err != nil || len(out) == 0 {
		return out, err
	}

	ids := make([]string, 0, len(out))
	for _, s := range out {
		ids = append(ids, fmt.Sprint(s.ID))
	}
	hrows, err := q.db.QueryContext(ctx, "SELECT id, business_schedule_id, name, date, is_recurring, created_at FROM "+q.prefix+
		"holidays WHERE business_schedule_id IN ("+strings.Join(ids, ",")+") ORDER BY date, id")
	if err != nil {
		return nil, err
	}
	defer hrows.Close()
	for hrows.Next() {
		var h models.Holiday
		var date any
		if err := hrows.Scan(&h.ID, &h.BusinessScheduleID, &h.Name, &date, &h.IsRecurring, &h.CreatedAt); err != nil {
			return nil, err
		}
		h.Date = holidayDate(date)
		if s := byID[h.BusinessScheduleID]; s != nil {
			s.Holidays = append(s.Holidays, h)
		}
	}
	return out, hrows.Err()
}

// holidayDate reads a DATE column, which drivers return as a time or as text.
func holidayDate(v any) time.Time {
	var text string
	switch d := v.(type) {
	case time.Time:
		return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)
	case []byte:
		text = string(d)
	case string:
		text = d
	}
	if len(text) >= 10 {
		if t, err := time.Parse("2006-01-02", text[:10]); err == nil {
			return t
		}
	}
	return time.Time{}
}

func (q scheduleSQL) one(ctx context.Context, where string, args ...any) (*models.BusinessSchedule, error) {
	all, err := q.list(ctx, where, args...)
	if err != nil || len(all) == 0 {
		return nil, err
	}
	return all[0], nil
}

func (q scheduleSQL) ListBusinessSchedules(ctx context.Context) ([]*models.BusinessSchedule, error) {
	return q.list(ctx, "ORDER BY id")
}

func (q scheduleSQL) GetBusinessSchedule(ctx context.Context, id int64) (*models.BusinessSchedule, error) {
	return q.one(ctx, "WHERE id = ?", id)
}

func (q scheduleSQL) GetDefaultBusinessSchedule(ctx context.Context) (*models.BusinessSchedule, error) {
	return q.one(ctx, "WHERE is_default = ? AND is_active = ? ORDER BY id LIMIT 1", true, true)
}

func hoursJSON(s *models.BusinessSchedule) string {
	hours := s.Hours
	if hours == nil {
		hours = map[string]models.DaySchedule{}
	}
	raw, _ := json.Marshal(hours)
	return string(raw)
}

func (q scheduleSQL) write(ctx context.Context, s *models.BusinessSchedule, create bool) error {
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC()
	s.UpdatedAt = now
	if s.Timezone == "" {
		s.Timezone = "UTC"
	}
	table := q.prefix + "business_schedules"
	if create {
		s.CreatedAt = now
		insert := "INSERT INTO " + table + " (name, timezone, hours, is_default, is_active, created_at, updated_at) VALUES (?,?,?,?,?,?,?)"
		args := []any{s.Name, s.Timezone, hoursJSON(s), s.IsDefault, s.IsActive, s.CreatedAt, s.UpdatedAt}
		if q.postgres {
			if err := tx.QueryRowContext(ctx, q.bind(insert+" RETURNING id"), args...).Scan(&s.ID); err != nil {
				return err
			}
		} else {
			res, err := tx.ExecContext(ctx, insert, args...)
			if err != nil {
				return err
			}
			if s.ID, err = res.LastInsertId(); err != nil {
				return err
			}
		}
	} else {
		if _, err := tx.ExecContext(ctx, q.bind("UPDATE "+table+" SET name=?, timezone=?, hours=?, is_default=?, is_active=?, updated_at=? WHERE id=?"),
			s.Name, s.Timezone, hoursJSON(s), s.IsDefault, s.IsActive, s.UpdatedAt, s.ID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, q.bind("DELETE FROM "+q.prefix+"holidays WHERE business_schedule_id = ?"), s.ID); err != nil {
			return err
		}
	}
	if s.IsDefault {
		if _, err := tx.ExecContext(ctx, q.bind("UPDATE "+table+" SET is_default = ? WHERE id <> ?"), false, s.ID); err != nil {
			return err
		}
	}
	for i := range s.Holidays {
		h := &s.Holidays[i]
		h.BusinessScheduleID = s.ID
		h.CreatedAt = now
		if _, err := tx.ExecContext(ctx, q.bind("INSERT INTO "+q.prefix+"holidays (business_schedule_id, name, date, is_recurring, created_at) VALUES (?,?,?,?,?)"),
			s.ID, h.Name, h.Date.Format("2006-01-02"), h.IsRecurring, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (q scheduleSQL) CreateBusinessSchedule(ctx context.Context, s *models.BusinessSchedule) error {
	return q.write(ctx, s, true)
}

func (q scheduleSQL) UpdateBusinessSchedule(ctx context.Context, s *models.BusinessSchedule) error {
	return q.write(ctx, s, false)
}

// DeleteBusinessSchedule does by hand what the foreign keys declare, because
// SQLite only enforces them when the host turned foreign_keys on.
func (q scheduleSQL) DeleteBusinessSchedule(ctx context.Context, id int64) error {
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, stmt := range []string{
		"UPDATE " + q.prefix + "sla_policies SET business_schedule_id = NULL WHERE business_schedule_id = ?",
		"UPDATE " + q.prefix + "departments SET business_schedule_id = NULL WHERE business_schedule_id = ?",
		"DELETE FROM " + q.prefix + "holidays WHERE business_schedule_id = ?",
		"DELETE FROM " + q.prefix + "business_schedules WHERE id = ?",
	} {
		if _, err := tx.ExecContext(ctx, q.bind(stmt), id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *PostgresStore) schedules() scheduleSQL {
	return scheduleSQL{db: s.db, prefix: s.prefix, postgres: true}
}

func (s *SQLiteStore) schedules() scheduleSQL {
	return scheduleSQL{db: s.db, prefix: s.prefix}
}

// ListBusinessSchedules implements BusinessSchedules.
func (s *PostgresStore) ListBusinessSchedules(ctx context.Context) ([]*models.BusinessSchedule, error) {
	return s.schedules().ListBusinessSchedules(ctx)
}

// GetBusinessSchedule implements BusinessSchedules.
func (s *PostgresStore) GetBusinessSchedule(ctx context.Context, id int64) (*models.BusinessSchedule, error) {
	return s.schedules().GetBusinessSchedule(ctx, id)
}

// GetDefaultBusinessSchedule implements BusinessSchedules.
func (s *PostgresStore) GetDefaultBusinessSchedule(ctx context.Context) (*models.BusinessSchedule, error) {
	return s.schedules().GetDefaultBusinessSchedule(ctx)
}

// CreateBusinessSchedule implements BusinessSchedules.
func (s *PostgresStore) CreateBusinessSchedule(ctx context.Context, bs *models.BusinessSchedule) error {
	return s.schedules().CreateBusinessSchedule(ctx, bs)
}

// UpdateBusinessSchedule implements BusinessSchedules.
func (s *PostgresStore) UpdateBusinessSchedule(ctx context.Context, bs *models.BusinessSchedule) error {
	return s.schedules().UpdateBusinessSchedule(ctx, bs)
}

// DeleteBusinessSchedule implements BusinessSchedules.
func (s *PostgresStore) DeleteBusinessSchedule(ctx context.Context, id int64) error {
	return s.schedules().DeleteBusinessSchedule(ctx, id)
}

// ListBusinessSchedules implements BusinessSchedules.
func (s *SQLiteStore) ListBusinessSchedules(ctx context.Context) ([]*models.BusinessSchedule, error) {
	return s.schedules().ListBusinessSchedules(ctx)
}

// GetBusinessSchedule implements BusinessSchedules.
func (s *SQLiteStore) GetBusinessSchedule(ctx context.Context, id int64) (*models.BusinessSchedule, error) {
	return s.schedules().GetBusinessSchedule(ctx, id)
}

// GetDefaultBusinessSchedule implements BusinessSchedules.
func (s *SQLiteStore) GetDefaultBusinessSchedule(ctx context.Context) (*models.BusinessSchedule, error) {
	return s.schedules().GetDefaultBusinessSchedule(ctx)
}

// CreateBusinessSchedule implements BusinessSchedules.
func (s *SQLiteStore) CreateBusinessSchedule(ctx context.Context, bs *models.BusinessSchedule) error {
	return s.schedules().CreateBusinessSchedule(ctx, bs)
}

// UpdateBusinessSchedule implements BusinessSchedules.
func (s *SQLiteStore) UpdateBusinessSchedule(ctx context.Context, bs *models.BusinessSchedule) error {
	return s.schedules().UpdateBusinessSchedule(ctx, bs)
}

// DeleteBusinessSchedule implements BusinessSchedules.
func (s *SQLiteStore) DeleteBusinessSchedule(ctx context.Context, id int64) error {
	return s.schedules().DeleteBusinessSchedule(ctx, id)
}
