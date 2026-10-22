package services

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/escalated-dev/escalated-go/internal/sqldialect"
	"github.com/escalated-dev/escalated-go/models"
)

// TicketStatusService keeps the custom ticket statuses (Laravel TicketStatus,
// the shared statuses spec): the admin CRUD, the system rows standing for the
// built-in statuses, and resolving a status input to a built-in or a custom
// status.
//
// Every table name is built from Prefix, so a custom Config.TablePrefix works
// without the ticket-access query rewriting.
type TicketStatusService struct {
	DB     *sql.DB
	Prefix string
}

// ErrSystemStatus is returned for a change a system row does not allow.
var ErrSystemStatus = errors.New("system statuses cannot be deleted")

func (s *TicketStatusService) t(name string) string { return s.Prefix + name }

func (s *TicketStatusService) q(query string) string { return sqldialect.Rebind(s.DB, query) }

const ticketStatusColumns = "id, label, slug, category, color, description, position, is_default, is_system, created_at, updated_at"

func scanTicketStatus(row interface{ Scan(...any) error }) (*models.TicketStatus, error) {
	st := &models.TicketStatus{}
	if err := row.Scan(&st.ID, &st.Label, &st.Slug, &st.Category, &st.Color, &st.Description, &st.Position,
		&st.IsDefault, &st.IsSystem, &st.CreatedAt, &st.UpdatedAt); err != nil {
		return nil, err
	}
	return st, nil
}

// List returns every status ordered by category (in the categories' order),
// then position, then id.
func (s *TicketStatusService) List(ctx context.Context) ([]*models.TicketStatus, error) {
	rows, err := s.DB.QueryContext(ctx, s.q("SELECT "+ticketStatusColumns+" FROM "+s.t("ticket_statuses")+" ORDER BY position, id"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byCategory := map[string][]*models.TicketStatus{}
	var other []*models.TicketStatus
	for rows.Next() {
		st, err := scanTicketStatus(rows)
		if err != nil {
			return nil, err
		}
		if models.IsStatusCategory(st.Category) {
			byCategory[st.Category] = append(byCategory[st.Category], st)
		} else {
			other = append(other, st)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := []*models.TicketStatus{}
	for _, c := range models.StatusCategories {
		out = append(out, byCategory[c]...)
	}
	return append(out, other...), nil
}

// Get returns the status with id, or nil.
func (s *TicketStatusService) Get(ctx context.Context, id int64) (*models.TicketStatus, error) {
	return s.one(ctx, "id = ?", id)
}

// BySlug returns the status with slug, or nil.
func (s *TicketStatusService) BySlug(ctx context.Context, slug string) (*models.TicketStatus, error) {
	return s.one(ctx, "slug = ?", slug)
}

func (s *TicketStatusService) one(ctx context.Context, where string, arg any) (*models.TicketStatus, error) {
	st, err := scanTicketStatus(s.DB.QueryRowContext(ctx, s.q("SELECT "+ticketStatusColumns+" FROM "+s.t("ticket_statuses")+" WHERE "+where), arg))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return st, err
}

// EnsureSystem inserts the system row of every built-in status that has none
// (matched by slug). The migration seeds them; this keeps the admin list whole
// on a schema that was not migrated here. Idempotent.
func (s *TicketStatusService) EnsureSystem(ctx context.Context) error {
	positions := map[string]int{}
	for _, b := range models.BuiltInStatuses() {
		category := models.BuiltInStatusCategory(b.Status)
		position := positions[category]
		positions[category]++
		existing, err := s.BySlug(ctx, b.Slug)
		if err != nil {
			return err
		}
		if existing != nil {
			continue
		}
		st := &models.TicketStatus{Label: b.Label, Slug: b.Slug, Category: category, Color: b.Color, Position: position, IsSystem: true}
		if err := s.insert(ctx, st); err != nil {
			return err
		}
	}
	return nil
}

// uniqueSlug de-duplicates base against the statuses' slugs: base, base_2,
// base_3...
func (s *TicketStatusService) uniqueSlug(ctx context.Context, base string) (string, error) {
	if base == "" {
		base = "status"
	}
	for n := 1; ; n++ {
		candidate := base
		if n > 1 {
			candidate = fmt.Sprintf("%s_%d", base, n)
		}
		var count int
		if err := s.DB.QueryRowContext(ctx, s.q("SELECT COUNT(1) FROM "+s.t("ticket_statuses")+" WHERE slug = ?"), candidate).Scan(&count); err != nil {
			return "", err
		}
		// A built-in's name is never a custom slug, even before its system row
		// exists: inputs resolve built-in names first.
		if count == 0 && !isBuiltInName(candidate) {
			return candidate, nil
		}
	}
}

func isBuiltInName(slug string) bool {
	for _, name := range models.StatusName {
		if name == slug {
			return true
		}
	}
	return false
}

// Create inserts a custom status. A blank slug is generated from the label
// (Laravel's Str::slug with "_") and de-duplicated. A default status clears
// is_default on the category's other rows.
func (s *TicketStatusService) Create(ctx context.Context, st *models.TicketStatus) error {
	base := FieldSlug(st.Slug)
	if base == "" {
		base = FieldSlug(st.Label)
	}
	slug, err := s.uniqueSlug(ctx, base)
	if err != nil {
		return err
	}
	st.Slug = slug
	st.IsSystem = false
	if st.Color == "" {
		st.Color = models.DefaultStatusColor
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if st.IsDefault {
		if err := s.clearDefault(ctx, tx, st.Category, 0); err != nil {
			return err
		}
	}
	if err := s.insertTx(ctx, tx, st); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *TicketStatusService) insert(ctx context.Context, st *models.TicketStatus) error {
	return s.insertTx(ctx, s.DB, st)
}

type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func (s *TicketStatusService) insertTx(ctx context.Context, db queryer, st *models.TicketStatus) error {
	now := time.Now().UTC()
	st.CreatedAt, st.UpdatedAt = now, now
	return db.QueryRowContext(ctx, s.q("INSERT INTO "+s.t("ticket_statuses")+
		" (label, slug, category, color, description, position, is_default, is_system, created_at, updated_at)"+
		" VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id"),
		st.Label, st.Slug, st.Category, st.Color, st.Description, st.Position, st.IsDefault, st.IsSystem, now, now).Scan(&st.ID)
}

func (s *TicketStatusService) clearDefault(ctx context.Context, db queryer, category string, exceptID int64) error {
	_, err := db.ExecContext(ctx, s.q("UPDATE "+s.t("ticket_statuses")+" SET is_default = ? WHERE category = ? AND id <> ?"), false, category, exceptID)
	return err
}

// Update saves st's editable columns: label, category, colour, description,
// position and is_default. The slug never changes. A system row keeps its
// category (callers validate that). Tickets already in the status keep their
// built-in status when its category changes; tickets that take it later get
// the new category's anchor.
func (s *TicketStatusService) Update(ctx context.Context, st *models.TicketStatus) error {
	st.UpdatedAt = time.Now().UTC()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if st.IsDefault {
		if err := s.clearDefault(ctx, tx, st.Category, st.ID); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, s.q("UPDATE "+s.t("ticket_statuses")+
		" SET label = ?, category = ?, color = ?, description = ?, position = ?, is_default = ?, updated_at = ? WHERE id = ?"),
		st.Label, st.Category, st.Color, st.Description, st.Position, st.IsDefault, st.UpdatedAt, st.ID); err != nil {
		return err
	}
	return tx.Commit()
}

// Delete removes a custom status. Its tickets keep their built-in status and
// lose the custom one. System rows cannot be deleted (ErrSystemStatus).
func (s *TicketStatusService) Delete(ctx context.Context, st *models.TicketStatus) error {
	if st.IsSystem {
		return ErrSystemStatus
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// The foreign key sets NULL too, but SQLite only enforces it when the host
	// turned foreign_keys on.
	if _, err := tx.ExecContext(ctx, s.q("UPDATE "+s.t("tickets")+" SET ticket_status_id = NULL WHERE ticket_status_id = ?"), st.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, s.q("DELETE FROM "+s.t("ticket_statuses")+" WHERE id = ?"), st.ID); err != nil {
		return err
	}
	return tx.Commit()
}

// Resolve reads a status input: a built-in status (its slug or integer), or
// the slug of a custom status. ok is false for anything else. custom is nil
// for a built-in.
func (s *TicketStatusService) Resolve(ctx context.Context, value any) (status int, custom *models.TicketStatus, ok bool, err error) {
	if status, ok := StatusFromValue(value); ok {
		return status, nil, true, nil
	}
	if s == nil || s.DB == nil {
		return 0, nil, false, nil
	}
	slug, isString := value.(string)
	slug = strings.TrimSpace(slug)
	if !isString || slug == "" {
		return 0, nil, false, nil
	}
	st, err := s.BySlug(ctx, slug)
	if err != nil || st == nil || st.IsSystem {
		return 0, nil, false, err
	}
	return models.CategoryAnchor(st.Category), st, true, nil
}

// ByIDs loads the statuses with the given ids, keyed by id.
func (s *TicketStatusService) ByIDs(ctx context.Context, ids []int64) (map[int64]*models.TicketStatus, error) {
	out := map[int64]*models.TicketStatus{}
	if s == nil || s.DB == nil || len(ids) == 0 {
		return out, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := s.DB.QueryContext(ctx, s.q("SELECT "+ticketStatusColumns+" FROM "+s.t("ticket_statuses")+" WHERE id IN ("+placeholders+")"), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		st, err := scanTicketStatus(rows)
		if err != nil {
			return nil, err
		}
		out[st.ID] = st
	}
	return out, rows.Err()
}

// CustomStatusView is the custom_status a staff view carries.
func CustomStatusView(st *models.TicketStatus) map[string]any {
	if st == nil {
		return nil
	}
	return map[string]any{"id": st.ID, "label": st.Label, "slug": st.Slug, "color": st.Color, "category": st.Category}
}

// StatusCategoryOf is the category of a ticket's effective status: its custom
// status's, else its built-in's.
func StatusCategoryOf(t *models.Ticket, custom *models.TicketStatus) string {
	if custom != nil {
		return custom.Category
	}
	return models.BuiltInStatusCategory(t.Status)
}

// resolveStatusDirect resolves a status input for the runners that write the
// ticket row themselves (workflow and automation actions): the built-in
// status, and the custom status's id or nil. query adapts SQL written against
// escalated_* to the runner's table prefix and placeholders. ErrInvalidStatus
// for an unknown value.
func resolveStatusDirect(ctx context.Context, db *sql.DB, query func(string) string, value any) (int, *int64, error) {
	if status, ok := StatusFromValue(value); ok {
		return status, nil, nil
	}
	slug, isString := value.(string)
	slug = strings.TrimSpace(slug)
	if !isString || slug == "" || db == nil {
		return 0, nil, ErrInvalidStatus
	}
	var id int64
	var category string
	var system bool
	err := db.QueryRowContext(ctx, query(`SELECT id, category, is_system FROM escalated_ticket_statuses WHERE slug = ?`), slug).Scan(&id, &category, &system)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && system) {
		return 0, nil, ErrInvalidStatus
	}
	if err != nil {
		return 0, nil, err
	}
	return models.CategoryAnchor(category), &id, nil
}
