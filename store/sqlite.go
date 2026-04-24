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

// SQLiteStore implements Store using a SQLite database.
// It mirrors PostgresStore but uses SQLite-compatible syntax (? placeholders, no ILIKE).
type SQLiteStore struct {
	db     *sql.DB
	prefix string
}

// NewSQLiteStore creates a new SQLite-backed store.
func NewSQLiteStore(db *sql.DB, tablePrefix string) *SQLiteStore {
	return &SQLiteStore{db: db, prefix: tablePrefix}
}

func (s *SQLiteStore) t(name string) string {
	return s.prefix + name
}

// --- Tickets ---

func (s *SQLiteStore) CreateTicket(ctx context.Context, t *models.Ticket) error {
	if t.Reference == "" {
		t.Reference = models.GenerateReference("")
	}
	now := time.Now()
	t.CreatedAt = now
	t.UpdatedAt = now

	q := fmt.Sprintf(`INSERT INTO %s
		(reference, subject, description, status, priority, ticket_type,
		 requester_type, requester_id, guest_name, guest_email, guest_token, contact_id,
		 assigned_to, department_id, sla_policy_id, merged_into_id,
		 sla_first_response_due_at, sla_resolution_due_at, sla_breached,
		 first_response_at, resolved_at, closed_at, metadata, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, s.t("tickets"))

	res, err := s.db.ExecContext(ctx, q,
		t.Reference, t.Subject, t.Description, t.Status, t.Priority, t.TicketType,
		t.RequesterType, t.RequesterID, t.GuestName, t.GuestEmail, t.GuestToken, t.ContactID,
		t.AssignedTo, t.DepartmentID, t.SLAPolicyID, t.MergedIntoID,
		t.SLAFirstResponseDueAt, t.SLAResolutionDueAt, t.SLABreached,
		t.FirstResponseAt, t.ResolvedAt, t.ClosedAt, t.Metadata, t.CreatedAt, t.UpdatedAt,
	)
	if err != nil {
		return err
	}
	t.ID, err = res.LastInsertId()
	return err
}

func (s *SQLiteStore) GetTicket(ctx context.Context, id int64) (*models.Ticket, error) {
	q := fmt.Sprintf(`SELECT id, reference, subject, description, status, priority, ticket_type,
		requester_type, requester_id, guest_name, guest_email, guest_token, contact_id,
		assigned_to, department_id, sla_policy_id, merged_into_id,
		sla_first_response_due_at, sla_resolution_due_at, sla_breached,
		first_response_at, resolved_at, closed_at, metadata, created_at, updated_at
		FROM %s WHERE id = ?`, s.t("tickets"))

	t := &models.Ticket{}
	err := s.db.QueryRowContext(ctx, q, id).Scan(
		&t.ID, &t.Reference, &t.Subject, &t.Description, &t.Status, &t.Priority, &t.TicketType,
		&t.RequesterType, &t.RequesterID, &t.GuestName, &t.GuestEmail, &t.GuestToken, &t.ContactID,
		&t.AssignedTo, &t.DepartmentID, &t.SLAPolicyID, &t.MergedIntoID,
		&t.SLAFirstResponseDueAt, &t.SLAResolutionDueAt, &t.SLABreached,
		&t.FirstResponseAt, &t.ResolvedAt, &t.ClosedAt, &t.Metadata, &t.CreatedAt, &t.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return t, err
}

func (s *SQLiteStore) GetTicketByReference(ctx context.Context, ref string) (*models.Ticket, error) {
	q := fmt.Sprintf(`SELECT id, reference, subject, description, status, priority, ticket_type,
		requester_type, requester_id, guest_name, guest_email, guest_token, contact_id,
		assigned_to, department_id, sla_policy_id, merged_into_id,
		sla_first_response_due_at, sla_resolution_due_at, sla_breached,
		first_response_at, resolved_at, closed_at, metadata, created_at, updated_at
		FROM %s WHERE reference = ?`, s.t("tickets"))

	t := &models.Ticket{}
	err := s.db.QueryRowContext(ctx, q, ref).Scan(
		&t.ID, &t.Reference, &t.Subject, &t.Description, &t.Status, &t.Priority, &t.TicketType,
		&t.RequesterType, &t.RequesterID, &t.GuestName, &t.GuestEmail, &t.GuestToken, &t.ContactID,
		&t.AssignedTo, &t.DepartmentID, &t.SLAPolicyID, &t.MergedIntoID,
		&t.SLAFirstResponseDueAt, &t.SLAResolutionDueAt, &t.SLABreached,
		&t.FirstResponseAt, &t.ResolvedAt, &t.ClosedAt, &t.Metadata, &t.CreatedAt, &t.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return t, err
}

func (s *SQLiteStore) UpdateTicket(ctx context.Context, t *models.Ticket) error {
	t.UpdatedAt = time.Now()
	q := fmt.Sprintf(`UPDATE %s SET
		subject=?, description=?, status=?, priority=?, ticket_type=?,
		assigned_to=?, department_id=?, sla_policy_id=?, merged_into_id=?,
		sla_first_response_due_at=?, sla_resolution_due_at=?, sla_breached=?,
		first_response_at=?, resolved_at=?, closed_at=?, metadata=?, updated_at=?
		WHERE id=?`, s.t("tickets"))

	_, err := s.db.ExecContext(ctx, q,
		t.Subject, t.Description, t.Status, t.Priority, t.TicketType,
		t.AssignedTo, t.DepartmentID, t.SLAPolicyID, t.MergedIntoID,
		t.SLAFirstResponseDueAt, t.SLAResolutionDueAt, t.SLABreached,
		t.FirstResponseAt, t.ResolvedAt, t.ClosedAt, t.Metadata, t.UpdatedAt,
		t.ID,
	)
	return err
}

func (s *SQLiteStore) ListTickets(ctx context.Context, f models.TicketFilters) ([]*models.Ticket, int, error) {
	var where []string
	var args []any

	if f.Status != nil {
		where = append(where, "status = ?")
		args = append(args, *f.Status)
	}
	if f.Priority != nil {
		where = append(where, "priority = ?")
		args = append(args, *f.Priority)
	}
	if f.TicketType != nil {
		where = append(where, "ticket_type = ?")
		args = append(args, *f.TicketType)
	}
	if f.DepartmentID != nil {
		where = append(where, "department_id = ?")
		args = append(args, *f.DepartmentID)
	}
	if f.AssignedTo != nil {
		where = append(where, "assigned_to = ?")
		args = append(args, *f.AssignedTo)
	}
	if f.RequesterID != nil {
		where = append(where, "requester_id = ?")
		args = append(args, *f.RequesterID)
	}
	if f.Unassigned {
		where = append(where, "assigned_to IS NULL")
	}
	if f.SLABreached != nil && *f.SLABreached {
		where = append(where, "sla_breached = 1")
	}
	if f.Search != "" {
		where = append(where, "(subject LIKE ? OR description LIKE ? OR reference LIKE ?)")
		like := "%" + f.Search + "%"
		args = append(args, like, like, like)
	}

	whereStr := ""
	if len(where) > 0 {
		whereStr = " WHERE " + strings.Join(where, " AND ")
	}

	// Count
	var total int
	countQ := fmt.Sprintf("SELECT COUNT(*) FROM %s%s", s.t("tickets"), whereStr)
	if err := s.db.QueryRowContext(ctx, countQ, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	sortBy := "created_at"
	if f.SortBy != "" {
		sortBy = f.SortBy
	}
	sortOrder := "DESC"
	if f.SortOrder == "asc" {
		sortOrder = "ASC"
	}
	limit := 50
	if f.Limit > 0 && f.Limit <= 200 {
		limit = f.Limit
	}

	q := fmt.Sprintf(`SELECT id, reference, subject, description, status, priority, ticket_type,
		requester_type, requester_id, guest_name, guest_email, guest_token, contact_id,
		assigned_to, department_id, sla_policy_id, merged_into_id,
		sla_first_response_due_at, sla_resolution_due_at, sla_breached,
		first_response_at, resolved_at, closed_at, metadata, created_at, updated_at
		FROM %s%s ORDER BY %s %s LIMIT %d OFFSET %d`,
		s.t("tickets"), whereStr, sortBy, sortOrder, limit, f.Offset)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var tickets []*models.Ticket
	for rows.Next() {
		t := &models.Ticket{}
		if err := rows.Scan(
			&t.ID, &t.Reference, &t.Subject, &t.Description, &t.Status, &t.Priority, &t.TicketType,
			&t.RequesterType, &t.RequesterID, &t.GuestName, &t.GuestEmail, &t.GuestToken, &t.ContactID,
			&t.AssignedTo, &t.DepartmentID, &t.SLAPolicyID, &t.MergedIntoID,
			&t.SLAFirstResponseDueAt, &t.SLAResolutionDueAt, &t.SLABreached,
			&t.FirstResponseAt, &t.ResolvedAt, &t.ClosedAt, &t.Metadata, &t.CreatedAt, &t.UpdatedAt,
		); err != nil {
			return nil, 0, err
		}
		tickets = append(tickets, t)
	}
	return tickets, total, rows.Err()
}

func (s *SQLiteStore) DeleteTicket(ctx context.Context, id int64) error {
	q := fmt.Sprintf("DELETE FROM %s WHERE id = ?", s.t("tickets"))
	_, err := s.db.ExecContext(ctx, q, id)
	return err
}

// --- Replies ---

func (s *SQLiteStore) CreateReply(ctx context.Context, r *models.Reply) error {
	now := time.Now()
	r.CreatedAt = now
	r.UpdatedAt = now

	q := fmt.Sprintf(`INSERT INTO %s
		(ticket_id, body, author_type, author_id, is_internal, is_system, is_pinned, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?)`, s.t("replies"))

	res, err := s.db.ExecContext(ctx, q,
		r.TicketID, r.Body, r.AuthorType, r.AuthorID,
		r.IsInternal, r.IsSystem, r.IsPinned, r.CreatedAt, r.UpdatedAt,
	)
	if err != nil {
		return err
	}
	r.ID, err = res.LastInsertId()
	if err != nil {
		return err
	}

	uq := fmt.Sprintf("UPDATE %s SET updated_at = ? WHERE id = ?", s.t("tickets"))
	_, _ = s.db.ExecContext(ctx, uq, now, r.TicketID)
	return nil
}

func (s *SQLiteStore) GetReply(ctx context.Context, id int64) (*models.Reply, error) {
	q := fmt.Sprintf(`SELECT id, ticket_id, body, author_type, author_id,
		is_internal, is_system, is_pinned, created_at, updated_at
		FROM %s WHERE id = ?`, s.t("replies"))

	r := &models.Reply{}
	err := s.db.QueryRowContext(ctx, q, id).Scan(
		&r.ID, &r.TicketID, &r.Body, &r.AuthorType, &r.AuthorID,
		&r.IsInternal, &r.IsSystem, &r.IsPinned, &r.CreatedAt, &r.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return r, err
}

func (s *SQLiteStore) ListReplies(ctx context.Context, f models.ReplyFilters) ([]*models.Reply, error) {
	var where []string
	var args []any

	where = append(where, "ticket_id = ?")
	args = append(args, f.TicketID)

	if f.Internal != nil {
		where = append(where, "is_internal = ?")
		args = append(args, *f.Internal)
	}
	if f.System != nil {
		where = append(where, "is_system = ?")
		args = append(args, *f.System)
	}
	if f.Pinned != nil {
		where = append(where, "is_pinned = ?")
		args = append(args, *f.Pinned)
	}

	order := "ASC"
	if f.Descending {
		order = "DESC"
	}

	q := fmt.Sprintf(`SELECT id, ticket_id, body, author_type, author_id,
		is_internal, is_system, is_pinned, created_at, updated_at
		FROM %s WHERE %s ORDER BY created_at %s`,
		s.t("replies"), strings.Join(where, " AND "), order)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var replies []*models.Reply
	for rows.Next() {
		r := &models.Reply{}
		if err := rows.Scan(
			&r.ID, &r.TicketID, &r.Body, &r.AuthorType, &r.AuthorID,
			&r.IsInternal, &r.IsSystem, &r.IsPinned, &r.CreatedAt, &r.UpdatedAt,
		); err != nil {
			return nil, err
		}
		replies = append(replies, r)
	}
	return replies, rows.Err()
}

func (s *SQLiteStore) UpdateReply(ctx context.Context, r *models.Reply) error {
	r.UpdatedAt = time.Now()
	q := fmt.Sprintf(`UPDATE %s SET body=?, is_internal=?, is_pinned=?, updated_at=? WHERE id=?`,
		s.t("replies"))
	_, err := s.db.ExecContext(ctx, q, r.Body, r.IsInternal, r.IsPinned, r.UpdatedAt, r.ID)
	return err
}

func (s *SQLiteStore) DeleteReply(ctx context.Context, id int64) error {
	q := fmt.Sprintf("DELETE FROM %s WHERE id = ?", s.t("replies"))
	_, err := s.db.ExecContext(ctx, q, id)
	return err
}

// --- Departments ---

func (s *SQLiteStore) CreateDepartment(ctx context.Context, d *models.Department) error {
	now := time.Now()
	d.CreatedAt = now
	d.UpdatedAt = now

	q := fmt.Sprintf(`INSERT INTO %s (name, slug, description, email, is_active, default_sla_policy_id, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?)`, s.t("departments"))

	res, err := s.db.ExecContext(ctx, q,
		d.Name, d.Slug, d.Description, d.Email, d.IsActive, d.DefaultSLAPolicyID, d.CreatedAt, d.UpdatedAt,
	)
	if err != nil {
		return err
	}
	d.ID, err = res.LastInsertId()
	return err
}

func (s *SQLiteStore) GetDepartment(ctx context.Context, id int64) (*models.Department, error) {
	q := fmt.Sprintf(`SELECT id, name, slug, description, email, is_active, default_sla_policy_id, created_at, updated_at
		FROM %s WHERE id = ?`, s.t("departments"))

	d := &models.Department{}
	err := s.db.QueryRowContext(ctx, q, id).Scan(
		&d.ID, &d.Name, &d.Slug, &d.Description, &d.Email, &d.IsActive, &d.DefaultSLAPolicyID, &d.CreatedAt, &d.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return d, err
}

func (s *SQLiteStore) ListDepartments(ctx context.Context, activeOnly bool) ([]*models.Department, error) {
	q := fmt.Sprintf(`SELECT id, name, slug, description, email, is_active, default_sla_policy_id, created_at, updated_at
		FROM %s`, s.t("departments"))
	if activeOnly {
		q += " WHERE is_active = 1"
	}
	q += " ORDER BY name"

	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var depts []*models.Department
	for rows.Next() {
		d := &models.Department{}
		if err := rows.Scan(
			&d.ID, &d.Name, &d.Slug, &d.Description, &d.Email, &d.IsActive, &d.DefaultSLAPolicyID, &d.CreatedAt, &d.UpdatedAt,
		); err != nil {
			return nil, err
		}
		depts = append(depts, d)
	}
	return depts, rows.Err()
}

func (s *SQLiteStore) UpdateDepartment(ctx context.Context, d *models.Department) error {
	d.UpdatedAt = time.Now()
	q := fmt.Sprintf(`UPDATE %s SET name=?, slug=?, description=?, email=?, is_active=?,
		default_sla_policy_id=?, updated_at=? WHERE id=?`, s.t("departments"))
	_, err := s.db.ExecContext(ctx, q,
		d.Name, d.Slug, d.Description, d.Email, d.IsActive, d.DefaultSLAPolicyID, d.UpdatedAt, d.ID,
	)
	return err
}

func (s *SQLiteStore) DeleteDepartment(ctx context.Context, id int64) error {
	q := fmt.Sprintf("DELETE FROM %s WHERE id = ?", s.t("departments"))
	_, err := s.db.ExecContext(ctx, q, id)
	return err
}

// --- Tags ---

func (s *SQLiteStore) CreateTag(ctx context.Context, t *models.Tag) error {
	now := time.Now()
	t.CreatedAt = now
	t.UpdatedAt = now

	q := fmt.Sprintf(`INSERT INTO %s (name, slug, color, description, created_at, updated_at)
		VALUES (?,?,?,?,?,?)`, s.t("tags"))

	res, err := s.db.ExecContext(ctx, q,
		t.Name, t.Slug, t.Color, t.Description, t.CreatedAt, t.UpdatedAt,
	)
	if err != nil {
		return err
	}
	t.ID, err = res.LastInsertId()
	return err
}

func (s *SQLiteStore) GetTag(ctx context.Context, id int64) (*models.Tag, error) {
	q := fmt.Sprintf(`SELECT id, name, slug, color, description, created_at, updated_at
		FROM %s WHERE id = ?`, s.t("tags"))

	t := &models.Tag{}
	err := s.db.QueryRowContext(ctx, q, id).Scan(
		&t.ID, &t.Name, &t.Slug, &t.Color, &t.Description, &t.CreatedAt, &t.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return t, err
}

func (s *SQLiteStore) ListTags(ctx context.Context) ([]*models.Tag, error) {
	q := fmt.Sprintf(`SELECT id, name, slug, color, description, created_at, updated_at
		FROM %s ORDER BY name`, s.t("tags"))

	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []*models.Tag
	for rows.Next() {
		t := &models.Tag{}
		if err := rows.Scan(
			&t.ID, &t.Name, &t.Slug, &t.Color, &t.Description, &t.CreatedAt, &t.UpdatedAt,
		); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

func (s *SQLiteStore) UpdateTag(ctx context.Context, t *models.Tag) error {
	t.UpdatedAt = time.Now()
	q := fmt.Sprintf(`UPDATE %s SET name=?, slug=?, color=?, description=?, updated_at=? WHERE id=?`,
		s.t("tags"))
	_, err := s.db.ExecContext(ctx, q, t.Name, t.Slug, t.Color, t.Description, t.UpdatedAt, t.ID)
	return err
}

func (s *SQLiteStore) DeleteTag(ctx context.Context, id int64) error {
	jq := fmt.Sprintf("DELETE FROM %s WHERE tag_id = ?", s.t("ticket_tags"))
	_, _ = s.db.ExecContext(ctx, jq, id)

	q := fmt.Sprintf("DELETE FROM %s WHERE id = ?", s.t("tags"))
	_, err := s.db.ExecContext(ctx, q, id)
	return err
}

func (s *SQLiteStore) AddTagToTicket(ctx context.Context, ticketID, tagID int64) error {
	q := fmt.Sprintf(`INSERT OR IGNORE INTO %s (ticket_id, tag_id) VALUES (?, ?)`,
		s.t("ticket_tags"))
	_, err := s.db.ExecContext(ctx, q, ticketID, tagID)
	return err
}

func (s *SQLiteStore) RemoveTagFromTicket(ctx context.Context, ticketID, tagID int64) error {
	q := fmt.Sprintf("DELETE FROM %s WHERE ticket_id = ? AND tag_id = ?", s.t("ticket_tags"))
	_, err := s.db.ExecContext(ctx, q, ticketID, tagID)
	return err
}

func (s *SQLiteStore) GetTicketTags(ctx context.Context, ticketID int64) ([]*models.Tag, error) {
	q := fmt.Sprintf(`SELECT t.id, t.name, t.slug, t.color, t.description, t.created_at, t.updated_at
		FROM %s t INNER JOIN %s tt ON t.id = tt.tag_id
		WHERE tt.ticket_id = ? ORDER BY t.name`, s.t("tags"), s.t("ticket_tags"))

	rows, err := s.db.QueryContext(ctx, q, ticketID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []*models.Tag
	for rows.Next() {
		t := &models.Tag{}
		if err := rows.Scan(
			&t.ID, &t.Name, &t.Slug, &t.Color, &t.Description, &t.CreatedAt, &t.UpdatedAt,
		); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

// --- SLA Policies ---

func (s *SQLiteStore) CreateSLAPolicy(ctx context.Context, p *models.SLAPolicy) error {
	now := time.Now()
	p.CreatedAt = now
	p.UpdatedAt = now

	q := fmt.Sprintf(`INSERT INTO %s
		(name, description, first_response_hours, resolution_hours, is_active, is_default, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?)`, s.t("sla_policies"))

	res, err := s.db.ExecContext(ctx, q,
		p.Name, p.Description, p.FirstResponseHours, p.ResolutionHours,
		p.IsActive, p.IsDefault, p.CreatedAt, p.UpdatedAt,
	)
	if err != nil {
		return err
	}
	p.ID, err = res.LastInsertId()
	return err
}

func (s *SQLiteStore) GetSLAPolicy(ctx context.Context, id int64) (*models.SLAPolicy, error) {
	q := fmt.Sprintf(`SELECT id, name, description, first_response_hours, resolution_hours,
		is_active, is_default, created_at, updated_at
		FROM %s WHERE id = ?`, s.t("sla_policies"))

	p := &models.SLAPolicy{}
	err := s.db.QueryRowContext(ctx, q, id).Scan(
		&p.ID, &p.Name, &p.Description, &p.FirstResponseHours, &p.ResolutionHours,
		&p.IsActive, &p.IsDefault, &p.CreatedAt, &p.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return p, err
}

func (s *SQLiteStore) GetDefaultSLAPolicy(ctx context.Context) (*models.SLAPolicy, error) {
	q := fmt.Sprintf(`SELECT id, name, description, first_response_hours, resolution_hours,
		is_active, is_default, created_at, updated_at
		FROM %s WHERE is_default = 1 AND is_active = 1 LIMIT 1`, s.t("sla_policies"))

	p := &models.SLAPolicy{}
	err := s.db.QueryRowContext(ctx, q).Scan(
		&p.ID, &p.Name, &p.Description, &p.FirstResponseHours, &p.ResolutionHours,
		&p.IsActive, &p.IsDefault, &p.CreatedAt, &p.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return p, err
}

func (s *SQLiteStore) ListSLAPolicies(ctx context.Context, activeOnly bool) ([]*models.SLAPolicy, error) {
	q := fmt.Sprintf(`SELECT id, name, description, first_response_hours, resolution_hours,
		is_active, is_default, created_at, updated_at
		FROM %s`, s.t("sla_policies"))
	if activeOnly {
		q += " WHERE is_active = 1"
	}
	q += " ORDER BY name"

	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var policies []*models.SLAPolicy
	for rows.Next() {
		p := &models.SLAPolicy{}
		if err := rows.Scan(
			&p.ID, &p.Name, &p.Description, &p.FirstResponseHours, &p.ResolutionHours,
			&p.IsActive, &p.IsDefault, &p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, err
		}
		policies = append(policies, p)
	}
	return policies, rows.Err()
}

func (s *SQLiteStore) UpdateSLAPolicy(ctx context.Context, p *models.SLAPolicy) error {
	p.UpdatedAt = time.Now()
	q := fmt.Sprintf(`UPDATE %s SET name=?, description=?, first_response_hours=?,
		resolution_hours=?, is_active=?, is_default=?, updated_at=? WHERE id=?`,
		s.t("sla_policies"))
	_, err := s.db.ExecContext(ctx, q,
		p.Name, p.Description, p.FirstResponseHours, p.ResolutionHours,
		p.IsActive, p.IsDefault, p.UpdatedAt, p.ID,
	)
	return err
}

func (s *SQLiteStore) DeleteSLAPolicy(ctx context.Context, id int64) error {
	q := fmt.Sprintf("DELETE FROM %s WHERE id = ?", s.t("sla_policies"))
	_, err := s.db.ExecContext(ctx, q, id)
	return err
}

// --- Activities ---

func (s *SQLiteStore) CreateActivity(ctx context.Context, a *models.Activity) error {
	a.CreatedAt = time.Now()
	q := fmt.Sprintf(`INSERT INTO %s (ticket_id, action, causer_type, causer_id, details, created_at)
		VALUES (?,?,?,?,?,?)`, s.t("ticket_activities"))

	res, err := s.db.ExecContext(ctx, q,
		a.TicketID, a.Action, a.CauserType, a.CauserID, a.Details, a.CreatedAt,
	)
	if err != nil {
		return err
	}
	a.ID, err = res.LastInsertId()
	return err
}

func (s *SQLiteStore) ListActivities(ctx context.Context, ticketID int64, limit int) ([]*models.Activity, error) {
	if limit <= 0 {
		limit = 20
	}
	q := fmt.Sprintf(`SELECT id, ticket_id, action, causer_type, causer_id, details, created_at
		FROM %s WHERE ticket_id = ? ORDER BY created_at DESC LIMIT ?`,
		s.t("ticket_activities"))

	rows, err := s.db.QueryContext(ctx, q, ticketID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var activities []*models.Activity
	for rows.Next() {
		a := &models.Activity{}
		if err := rows.Scan(
			&a.ID, &a.TicketID, &a.Action, &a.CauserType, &a.CauserID, &a.Details, &a.CreatedAt,
		); err != nil {
			return nil, err
		}
		activities = append(activities, a)
	}
	return activities, rows.Err()
}

// --- Snooze ---

func (s *SQLiteStore) ListSnoozedDueBefore(ctx context.Context, before time.Time) ([]*models.Ticket, error) {
	q := fmt.Sprintf(`SELECT id, reference, subject, description, status, priority, ticket_type,
		requester_type, requester_id, guest_name, guest_email, guest_token, contact_id,
		assigned_to, department_id, sla_policy_id, merged_into_id,
		snoozed_until, snoozed_by, status_before_snooze,
		sla_first_response_due_at, sla_resolution_due_at, sla_breached,
		first_response_at, resolved_at, closed_at, metadata, created_at, updated_at
		FROM %s WHERE status = ? AND snoozed_until IS NOT NULL AND snoozed_until <= ?`,
		s.t("tickets"))

	rows, err := s.db.QueryContext(ctx, q, models.StatusSnoozed, before)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tickets []*models.Ticket
	for rows.Next() {
		t := &models.Ticket{}
		if err := rows.Scan(
			&t.ID, &t.Reference, &t.Subject, &t.Description, &t.Status, &t.Priority, &t.TicketType,
			&t.RequesterType, &t.RequesterID, &t.GuestName, &t.GuestEmail, &t.GuestToken, &t.ContactID,
			&t.AssignedTo, &t.DepartmentID, &t.SLAPolicyID, &t.MergedIntoID,
			&t.SnoozedUntil, &t.SnoozedBy, &t.StatusBeforeSnooze,
			&t.SLAFirstResponseDueAt, &t.SLAResolutionDueAt, &t.SLABreached,
			&t.FirstResponseAt, &t.ResolvedAt, &t.ClosedAt, &t.Metadata, &t.CreatedAt, &t.UpdatedAt,
		); err != nil {
			return nil, err
		}
		tickets = append(tickets, t)
	}
	return tickets, rows.Err()
}

// --- Saved Views ---

func (s *SQLiteStore) CreateSavedView(ctx context.Context, sv *models.SavedView) error {
	q := fmt.Sprintf(`INSERT INTO %s (name, filters, user_id, is_shared, position, icon, color, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?)`, s.t("saved_views"))
	now := time.Now()
	sv.CreatedAt = now
	sv.UpdatedAt = now
	res, err := s.db.ExecContext(ctx, q,
		sv.Name, sv.Filters, sv.UserID, sv.IsShared, sv.Position, sv.Icon, sv.Color, sv.CreatedAt, sv.UpdatedAt,
	)
	if err != nil {
		return err
	}
	sv.ID, _ = res.LastInsertId()
	return nil
}

func (s *SQLiteStore) GetSavedView(ctx context.Context, id int64) (*models.SavedView, error) {
	q := fmt.Sprintf(`SELECT id, name, filters, user_id, is_shared, position, icon, color, created_at, updated_at
		FROM %s WHERE id = ?`, s.t("saved_views"))
	sv := &models.SavedView{}
	err := s.db.QueryRowContext(ctx, q, id).Scan(
		&sv.ID, &sv.Name, &sv.Filters, &sv.UserID, &sv.IsShared, &sv.Position, &sv.Icon, &sv.Color, &sv.CreatedAt, &sv.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return sv, nil
}

func (s *SQLiteStore) ListSavedViews(ctx context.Context, userID int64, includeShared bool) ([]*models.SavedView, error) {
	var q string
	var args []any
	if includeShared {
		q = fmt.Sprintf(`SELECT id, name, filters, user_id, is_shared, position, icon, color, created_at, updated_at
			FROM %s WHERE user_id = ? OR is_shared = 1 ORDER BY position ASC, id ASC`, s.t("saved_views"))
		args = []any{userID}
	} else {
		q = fmt.Sprintf(`SELECT id, name, filters, user_id, is_shared, position, icon, color, created_at, updated_at
			FROM %s WHERE user_id = ? ORDER BY position ASC, id ASC`, s.t("saved_views"))
		args = []any{userID}
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var views []*models.SavedView
	for rows.Next() {
		sv := &models.SavedView{}
		if err := rows.Scan(&sv.ID, &sv.Name, &sv.Filters, &sv.UserID, &sv.IsShared, &sv.Position, &sv.Icon, &sv.Color, &sv.CreatedAt, &sv.UpdatedAt); err != nil {
			return nil, err
		}
		views = append(views, sv)
	}
	return views, rows.Err()
}

func (s *SQLiteStore) UpdateSavedView(ctx context.Context, sv *models.SavedView) error {
	sv.UpdatedAt = time.Now()
	q := fmt.Sprintf(`UPDATE %s SET name=?, filters=?, is_shared=?, position=?, icon=?, color=?, updated_at=? WHERE id=?`, s.t("saved_views"))
	_, err := s.db.ExecContext(ctx, q, sv.Name, sv.Filters, sv.IsShared, sv.Position, sv.Icon, sv.Color, sv.UpdatedAt, sv.ID)
	return err
}

func (s *SQLiteStore) DeleteSavedView(ctx context.Context, id int64) error {
	q := fmt.Sprintf(`DELETE FROM %s WHERE id = ?`, s.t("saved_views"))
	_, err := s.db.ExecContext(ctx, q, id)
	return err
}

func (s *SQLiteStore) ReorderSavedViews(ctx context.Context, userID int64, ids []int64) error {
	for i, id := range ids {
		q := fmt.Sprintf(`UPDATE %s SET position = ? WHERE id = ? AND user_id = ?`, s.t("saved_views"))
		if _, err := s.db.ExecContext(ctx, q, i, id, userID); err != nil {
			return err
		}
	}
	return nil
}

// --- Attachments ---

func (s *SQLiteStore) CreateAttachment(ctx context.Context, a *models.Attachment) error {
	now := time.Now()
	a.CreatedAt = now

	q := fmt.Sprintf(`INSERT INTO %s
		(ticket_id, reply_id, original_filename, mime_type, size, storage_path, created_at)
		VALUES (?,?,?,?,?,?,?)`, s.t("attachments"))

	res, err := s.db.ExecContext(ctx, q,
		a.TicketID, a.ReplyID, a.OriginalFilename, a.MimeType, a.Size, a.StoragePath, a.CreatedAt,
	)
	if err != nil {
		return err
	}
	a.ID, err = res.LastInsertId()
	return err
}

func (s *SQLiteStore) GetAttachmentByID(ctx context.Context, id int64) (*models.Attachment, error) {
	q := fmt.Sprintf(`SELECT id, ticket_id, reply_id, original_filename, mime_type, size, storage_path, created_at
		FROM %s WHERE id = ?`, s.t("attachments"))

	a := &models.Attachment{}
	err := s.db.QueryRowContext(ctx, q, id).Scan(
		&a.ID, &a.TicketID, &a.ReplyID, &a.OriginalFilename, &a.MimeType, &a.Size, &a.StoragePath, &a.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return a, err
}

func (s *SQLiteStore) GetAttachmentsByTicketID(ctx context.Context, ticketID int64) ([]*models.Attachment, error) {
	q := fmt.Sprintf(`SELECT id, ticket_id, reply_id, original_filename, mime_type, size, storage_path, created_at
		FROM %s WHERE ticket_id = ? ORDER BY created_at ASC`, s.t("attachments"))

	rows, err := s.db.QueryContext(ctx, q, ticketID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var attachments []*models.Attachment
	for rows.Next() {
		a := &models.Attachment{}
		if err := rows.Scan(
			&a.ID, &a.TicketID, &a.ReplyID, &a.OriginalFilename, &a.MimeType, &a.Size, &a.StoragePath, &a.CreatedAt,
		); err != nil {
			return nil, err
		}
		attachments = append(attachments, a)
	}
	return attachments, rows.Err()
}

func (s *SQLiteStore) GetAttachmentsByReplyID(ctx context.Context, replyID int64) ([]*models.Attachment, error) {
	q := fmt.Sprintf(`SELECT id, ticket_id, reply_id, original_filename, mime_type, size, storage_path, created_at
		FROM %s WHERE reply_id = ? ORDER BY created_at ASC`, s.t("attachments"))

	rows, err := s.db.QueryContext(ctx, q, replyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var attachments []*models.Attachment
	for rows.Next() {
		a := &models.Attachment{}
		if err := rows.Scan(
			&a.ID, &a.TicketID, &a.ReplyID, &a.OriginalFilename, &a.MimeType, &a.Size, &a.StoragePath, &a.CreatedAt,
		); err != nil {
			return nil, err
		}
		attachments = append(attachments, a)
	}
	return attachments, rows.Err()
}

// --- Contacts (Pattern B public-ticket dedupe) ---

func (s *SQLiteStore) GetContactByEmail(ctx context.Context, normalizedEmail string) (*models.Contact, error) {
	q := fmt.Sprintf(`SELECT id, email, name, user_id, metadata, created_at, updated_at
		FROM %s WHERE email = ?`, s.t("contacts"))

	c := &models.Contact{}
	var metadataRaw sql.NullString
	err := s.db.QueryRowContext(ctx, q, normalizedEmail).Scan(
		&c.ID, &c.Email, &c.Name, &c.UserID, &metadataRaw, &c.CreatedAt, &c.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if metadataRaw.Valid && metadataRaw.String != "" {
		_ = json.Unmarshal([]byte(metadataRaw.String), &c.Metadata)
	}
	return c, nil
}

func (s *SQLiteStore) CreateContact(ctx context.Context, c *models.Contact) error {
	now := time.Now()
	c.CreatedAt = now
	c.UpdatedAt = now
	metadata := "{}"
	if c.Metadata != nil {
		b, err := json.Marshal(c.Metadata)
		if err == nil {
			metadata = string(b)
		}
	}
	q := fmt.Sprintf(`INSERT INTO %s (email, name, user_id, metadata, created_at, updated_at)
		VALUES (?,?,?,?,?,?)`, s.t("contacts"))
	res, err := s.db.ExecContext(ctx, q, c.Email, c.Name, c.UserID, metadata, c.CreatedAt, c.UpdatedAt)
	if err != nil {
		return err
	}
	c.ID, err = res.LastInsertId()
	return err
}

func (s *SQLiteStore) UpdateContactName(ctx context.Context, id int64, name string) error {
	q := fmt.Sprintf(`UPDATE %s SET name = ?, updated_at = ? WHERE id = ?`, s.t("contacts"))
	_, err := s.db.ExecContext(ctx, q, name, time.Now(), id)
	return err
}
