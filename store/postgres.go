package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/escalated-dev/escalated-go/models"
)

// PostgresStore implements Store using a PostgreSQL database.
type PostgresStore struct {
	db     *sql.DB
	prefix string
}

// NewPostgresStore creates a new PostgreSQL-backed store.
func NewPostgresStore(db *sql.DB, tablePrefix string) *PostgresStore {
	return &PostgresStore{db: db, prefix: tablePrefix}
}

func (s *PostgresStore) t(name string) string {
	return s.prefix + name
}

// --- Tickets ---

func (s *PostgresStore) CreateTicket(ctx context.Context, t *models.Ticket) error {
	if t.Reference == "" {
		t.Reference = models.GenerateReference("")
	}
	now := time.Now()
	t.CreatedAt = now
	t.UpdatedAt = now

	q := fmt.Sprintf(`INSERT INTO %s
		(reference, subject, description, status, priority, ticket_type,
		 requester_type, requester_id, guest_name, guest_email, guest_token,
		 assigned_to, department_id, sla_policy_id, merged_into_id,
		 sla_first_response_due_at, sla_resolution_due_at, sla_breached,
		 first_response_at, resolved_at, closed_at, metadata, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24)
		RETURNING id`, s.t("tickets"))

	return s.db.QueryRowContext(ctx, q,
		t.Reference, t.Subject, t.Description, t.Status, t.Priority, t.TicketType,
		t.RequesterType, t.RequesterID, t.GuestName, t.GuestEmail, t.GuestToken,
		t.AssignedTo, t.DepartmentID, t.SLAPolicyID, t.MergedIntoID,
		t.SLAFirstResponseDueAt, t.SLAResolutionDueAt, t.SLABreached,
		t.FirstResponseAt, t.ResolvedAt, t.ClosedAt, t.Metadata, t.CreatedAt, t.UpdatedAt,
	).Scan(&t.ID)
}

func (s *PostgresStore) GetTicket(ctx context.Context, id int64) (*models.Ticket, error) {
	q := fmt.Sprintf(`SELECT id, reference, subject, description, status, priority, ticket_type,
		requester_type, requester_id, guest_name, guest_email, guest_token,
		assigned_to, department_id, sla_policy_id, merged_into_id,
		sla_first_response_due_at, sla_resolution_due_at, sla_breached,
		first_response_at, resolved_at, closed_at, metadata, created_at, updated_at
		FROM %s WHERE id = $1`, s.t("tickets"))

	t := &models.Ticket{}
	err := s.db.QueryRowContext(ctx, q, id).Scan(
		&t.ID, &t.Reference, &t.Subject, &t.Description, &t.Status, &t.Priority, &t.TicketType,
		&t.RequesterType, &t.RequesterID, &t.GuestName, &t.GuestEmail, &t.GuestToken,
		&t.AssignedTo, &t.DepartmentID, &t.SLAPolicyID, &t.MergedIntoID,
		&t.SLAFirstResponseDueAt, &t.SLAResolutionDueAt, &t.SLABreached,
		&t.FirstResponseAt, &t.ResolvedAt, &t.ClosedAt, &t.Metadata, &t.CreatedAt, &t.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return t, err
}

func (s *PostgresStore) GetTicketByReference(ctx context.Context, ref string) (*models.Ticket, error) {
	q := fmt.Sprintf(`SELECT id, reference, subject, description, status, priority, ticket_type,
		requester_type, requester_id, guest_name, guest_email, guest_token,
		assigned_to, department_id, sla_policy_id, merged_into_id,
		sla_first_response_due_at, sla_resolution_due_at, sla_breached,
		first_response_at, resolved_at, closed_at, metadata, created_at, updated_at
		FROM %s WHERE reference = $1`, s.t("tickets"))

	t := &models.Ticket{}
	err := s.db.QueryRowContext(ctx, q, ref).Scan(
		&t.ID, &t.Reference, &t.Subject, &t.Description, &t.Status, &t.Priority, &t.TicketType,
		&t.RequesterType, &t.RequesterID, &t.GuestName, &t.GuestEmail, &t.GuestToken,
		&t.AssignedTo, &t.DepartmentID, &t.SLAPolicyID, &t.MergedIntoID,
		&t.SLAFirstResponseDueAt, &t.SLAResolutionDueAt, &t.SLABreached,
		&t.FirstResponseAt, &t.ResolvedAt, &t.ClosedAt, &t.Metadata, &t.CreatedAt, &t.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return t, err
}

func (s *PostgresStore) UpdateTicket(ctx context.Context, t *models.Ticket) error {
	t.UpdatedAt = time.Now()
	q := fmt.Sprintf(`UPDATE %s SET
		subject=$1, description=$2, status=$3, priority=$4, ticket_type=$5,
		assigned_to=$6, department_id=$7, sla_policy_id=$8, merged_into_id=$9,
		sla_first_response_due_at=$10, sla_resolution_due_at=$11, sla_breached=$12,
		first_response_at=$13, resolved_at=$14, closed_at=$15, metadata=$16, updated_at=$17
		WHERE id=$18`, s.t("tickets"))

	_, err := s.db.ExecContext(ctx, q,
		t.Subject, t.Description, t.Status, t.Priority, t.TicketType,
		t.AssignedTo, t.DepartmentID, t.SLAPolicyID, t.MergedIntoID,
		t.SLAFirstResponseDueAt, t.SLAResolutionDueAt, t.SLABreached,
		t.FirstResponseAt, t.ResolvedAt, t.ClosedAt, t.Metadata, t.UpdatedAt,
		t.ID,
	)
	return err
}

func (s *PostgresStore) ListTickets(ctx context.Context, f models.TicketFilters) ([]*models.Ticket, int, error) {
	var where []string
	var args []any
	argN := 1

	addFilter := func(clause string, val any) {
		where = append(where, fmt.Sprintf(clause, argN))
		args = append(args, val)
		argN++
	}

	if f.Status != nil {
		addFilter("status = $%d", *f.Status)
	}
	if f.Priority != nil {
		addFilter("priority = $%d", *f.Priority)
	}
	if f.TicketType != nil {
		addFilter("ticket_type = $%d", *f.TicketType)
	}
	if f.DepartmentID != nil {
		addFilter("department_id = $%d", *f.DepartmentID)
	}
	if f.AssignedTo != nil {
		addFilter("assigned_to = $%d", *f.AssignedTo)
	}
	if f.RequesterID != nil {
		addFilter("requester_id = $%d", *f.RequesterID)
	}
	if f.Unassigned {
		where = append(where, "assigned_to IS NULL")
	}
	if f.SLABreached != nil && *f.SLABreached {
		where = append(where, "sla_breached = TRUE")
	}
	if f.Search != "" {
		clause := fmt.Sprintf("(subject ILIKE $%d OR description ILIKE $%d OR reference ILIKE $%d)", argN, argN+1, argN+2)
		where = append(where, clause)
		like := "%" + f.Search + "%"
		args = append(args, like, like, like)
		argN += 3
	}

	whereStr := ""
	if len(where) > 0 {
		whereStr = " WHERE " + strings.Join(where, " AND ")
	}

	// Count
	countQ := fmt.Sprintf("SELECT COUNT(*) FROM %s%s", s.t("tickets"), whereStr)
	var total int
	if err := s.db.QueryRowContext(ctx, countQ, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// Sort
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
		requester_type, requester_id, guest_name, guest_email, guest_token,
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
			&t.RequesterType, &t.RequesterID, &t.GuestName, &t.GuestEmail, &t.GuestToken,
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

func (s *PostgresStore) DeleteTicket(ctx context.Context, id int64) error {
	q := fmt.Sprintf("DELETE FROM %s WHERE id = $1", s.t("tickets"))
	_, err := s.db.ExecContext(ctx, q, id)
	return err
}

// --- Replies ---

func (s *PostgresStore) CreateReply(ctx context.Context, r *models.Reply) error {
	now := time.Now()
	r.CreatedAt = now
	r.UpdatedAt = now

	q := fmt.Sprintf(`INSERT INTO %s
		(ticket_id, body, author_type, author_id, is_internal, is_system, is_pinned, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`, s.t("replies"))

	err := s.db.QueryRowContext(ctx, q,
		r.TicketID, r.Body, r.AuthorType, r.AuthorID,
		r.IsInternal, r.IsSystem, r.IsPinned, r.CreatedAt, r.UpdatedAt,
	).Scan(&r.ID)
	if err != nil {
		return err
	}

	// Touch the parent ticket's updated_at
	uq := fmt.Sprintf("UPDATE %s SET updated_at = $1 WHERE id = $2", s.t("tickets"))
	_, _ = s.db.ExecContext(ctx, uq, now, r.TicketID)
	return nil
}

func (s *PostgresStore) GetReply(ctx context.Context, id int64) (*models.Reply, error) {
	q := fmt.Sprintf(`SELECT id, ticket_id, body, author_type, author_id,
		is_internal, is_system, is_pinned, created_at, updated_at
		FROM %s WHERE id = $1`, s.t("replies"))

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

func (s *PostgresStore) ListReplies(ctx context.Context, f models.ReplyFilters) ([]*models.Reply, error) {
	var where []string
	var args []any
	argN := 1

	where = append(where, fmt.Sprintf("ticket_id = $%d", argN))
	args = append(args, f.TicketID)
	argN++

	if f.Internal != nil {
		where = append(where, fmt.Sprintf("is_internal = $%d", argN))
		args = append(args, *f.Internal)
		argN++
	}
	if f.System != nil {
		where = append(where, fmt.Sprintf("is_system = $%d", argN))
		args = append(args, *f.System)
		argN++
	}
	if f.Pinned != nil {
		where = append(where, fmt.Sprintf("is_pinned = $%d", argN))
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

func (s *PostgresStore) UpdateReply(ctx context.Context, r *models.Reply) error {
	r.UpdatedAt = time.Now()
	q := fmt.Sprintf(`UPDATE %s SET body=$1, is_internal=$2, is_pinned=$3, updated_at=$4 WHERE id=$5`,
		s.t("replies"))
	_, err := s.db.ExecContext(ctx, q, r.Body, r.IsInternal, r.IsPinned, r.UpdatedAt, r.ID)
	return err
}

func (s *PostgresStore) DeleteReply(ctx context.Context, id int64) error {
	q := fmt.Sprintf("DELETE FROM %s WHERE id = $1", s.t("replies"))
	_, err := s.db.ExecContext(ctx, q, id)
	return err
}

// --- Departments ---

func (s *PostgresStore) CreateDepartment(ctx context.Context, d *models.Department) error {
	now := time.Now()
	d.CreatedAt = now
	d.UpdatedAt = now

	q := fmt.Sprintf(`INSERT INTO %s (name, slug, description, email, is_active, default_sla_policy_id, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`, s.t("departments"))

	return s.db.QueryRowContext(ctx, q,
		d.Name, d.Slug, d.Description, d.Email, d.IsActive, d.DefaultSLAPolicyID, d.CreatedAt, d.UpdatedAt,
	).Scan(&d.ID)
}

func (s *PostgresStore) GetDepartment(ctx context.Context, id int64) (*models.Department, error) {
	q := fmt.Sprintf(`SELECT id, name, slug, description, email, is_active, default_sla_policy_id, created_at, updated_at
		FROM %s WHERE id = $1`, s.t("departments"))

	d := &models.Department{}
	err := s.db.QueryRowContext(ctx, q, id).Scan(
		&d.ID, &d.Name, &d.Slug, &d.Description, &d.Email, &d.IsActive, &d.DefaultSLAPolicyID, &d.CreatedAt, &d.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return d, err
}

func (s *PostgresStore) ListDepartments(ctx context.Context, activeOnly bool) ([]*models.Department, error) {
	q := fmt.Sprintf(`SELECT id, name, slug, description, email, is_active, default_sla_policy_id, created_at, updated_at
		FROM %s`, s.t("departments"))
	if activeOnly {
		q += " WHERE is_active = TRUE"
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

func (s *PostgresStore) UpdateDepartment(ctx context.Context, d *models.Department) error {
	d.UpdatedAt = time.Now()
	q := fmt.Sprintf(`UPDATE %s SET name=$1, slug=$2, description=$3, email=$4, is_active=$5,
		default_sla_policy_id=$6, updated_at=$7 WHERE id=$8`, s.t("departments"))
	_, err := s.db.ExecContext(ctx, q,
		d.Name, d.Slug, d.Description, d.Email, d.IsActive, d.DefaultSLAPolicyID, d.UpdatedAt, d.ID,
	)
	return err
}

func (s *PostgresStore) DeleteDepartment(ctx context.Context, id int64) error {
	q := fmt.Sprintf("DELETE FROM %s WHERE id = $1", s.t("departments"))
	_, err := s.db.ExecContext(ctx, q, id)
	return err
}

// --- Tags ---

func (s *PostgresStore) CreateTag(ctx context.Context, t *models.Tag) error {
	now := time.Now()
	t.CreatedAt = now
	t.UpdatedAt = now

	q := fmt.Sprintf(`INSERT INTO %s (name, slug, color, description, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`, s.t("tags"))

	return s.db.QueryRowContext(ctx, q,
		t.Name, t.Slug, t.Color, t.Description, t.CreatedAt, t.UpdatedAt,
	).Scan(&t.ID)
}

func (s *PostgresStore) GetTag(ctx context.Context, id int64) (*models.Tag, error) {
	q := fmt.Sprintf(`SELECT id, name, slug, color, description, created_at, updated_at
		FROM %s WHERE id = $1`, s.t("tags"))

	t := &models.Tag{}
	err := s.db.QueryRowContext(ctx, q, id).Scan(
		&t.ID, &t.Name, &t.Slug, &t.Color, &t.Description, &t.CreatedAt, &t.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return t, err
}

func (s *PostgresStore) ListTags(ctx context.Context) ([]*models.Tag, error) {
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

func (s *PostgresStore) UpdateTag(ctx context.Context, t *models.Tag) error {
	t.UpdatedAt = time.Now()
	q := fmt.Sprintf(`UPDATE %s SET name=$1, slug=$2, color=$3, description=$4, updated_at=$5 WHERE id=$6`,
		s.t("tags"))
	_, err := s.db.ExecContext(ctx, q, t.Name, t.Slug, t.Color, t.Description, t.UpdatedAt, t.ID)
	return err
}

func (s *PostgresStore) DeleteTag(ctx context.Context, id int64) error {
	// Remove join table entries first
	jq := fmt.Sprintf("DELETE FROM %s WHERE tag_id = $1", s.t("ticket_tags"))
	_, _ = s.db.ExecContext(ctx, jq, id)

	q := fmt.Sprintf("DELETE FROM %s WHERE id = $1", s.t("tags"))
	_, err := s.db.ExecContext(ctx, q, id)
	return err
}

func (s *PostgresStore) AddTagToTicket(ctx context.Context, ticketID, tagID int64) error {
	q := fmt.Sprintf(`INSERT INTO %s (ticket_id, tag_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		s.t("ticket_tags"))
	_, err := s.db.ExecContext(ctx, q, ticketID, tagID)
	return err
}

func (s *PostgresStore) RemoveTagFromTicket(ctx context.Context, ticketID, tagID int64) error {
	q := fmt.Sprintf("DELETE FROM %s WHERE ticket_id = $1 AND tag_id = $2", s.t("ticket_tags"))
	_, err := s.db.ExecContext(ctx, q, ticketID, tagID)
	return err
}

func (s *PostgresStore) GetTicketTags(ctx context.Context, ticketID int64) ([]*models.Tag, error) {
	q := fmt.Sprintf(`SELECT t.id, t.name, t.slug, t.color, t.description, t.created_at, t.updated_at
		FROM %s t INNER JOIN %s tt ON t.id = tt.tag_id
		WHERE tt.ticket_id = $1 ORDER BY t.name`, s.t("tags"), s.t("ticket_tags"))

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

func (s *PostgresStore) CreateSLAPolicy(ctx context.Context, p *models.SLAPolicy) error {
	now := time.Now()
	p.CreatedAt = now
	p.UpdatedAt = now

	q := fmt.Sprintf(`INSERT INTO %s
		(name, description, first_response_hours, resolution_hours, is_active, is_default, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`, s.t("sla_policies"))

	return s.db.QueryRowContext(ctx, q,
		p.Name, p.Description, p.FirstResponseHours, p.ResolutionHours,
		p.IsActive, p.IsDefault, p.CreatedAt, p.UpdatedAt,
	).Scan(&p.ID)
}

func (s *PostgresStore) GetSLAPolicy(ctx context.Context, id int64) (*models.SLAPolicy, error) {
	q := fmt.Sprintf(`SELECT id, name, description, first_response_hours, resolution_hours,
		is_active, is_default, created_at, updated_at
		FROM %s WHERE id = $1`, s.t("sla_policies"))

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

func (s *PostgresStore) GetDefaultSLAPolicy(ctx context.Context) (*models.SLAPolicy, error) {
	q := fmt.Sprintf(`SELECT id, name, description, first_response_hours, resolution_hours,
		is_active, is_default, created_at, updated_at
		FROM %s WHERE is_default = TRUE AND is_active = TRUE LIMIT 1`, s.t("sla_policies"))

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

func (s *PostgresStore) ListSLAPolicies(ctx context.Context, activeOnly bool) ([]*models.SLAPolicy, error) {
	q := fmt.Sprintf(`SELECT id, name, description, first_response_hours, resolution_hours,
		is_active, is_default, created_at, updated_at
		FROM %s`, s.t("sla_policies"))
	if activeOnly {
		q += " WHERE is_active = TRUE"
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

func (s *PostgresStore) UpdateSLAPolicy(ctx context.Context, p *models.SLAPolicy) error {
	p.UpdatedAt = time.Now()
	q := fmt.Sprintf(`UPDATE %s SET name=$1, description=$2, first_response_hours=$3,
		resolution_hours=$4, is_active=$5, is_default=$6, updated_at=$7 WHERE id=$8`,
		s.t("sla_policies"))
	_, err := s.db.ExecContext(ctx, q,
		p.Name, p.Description, p.FirstResponseHours, p.ResolutionHours,
		p.IsActive, p.IsDefault, p.UpdatedAt, p.ID,
	)
	return err
}

func (s *PostgresStore) DeleteSLAPolicy(ctx context.Context, id int64) error {
	q := fmt.Sprintf("DELETE FROM %s WHERE id = $1", s.t("sla_policies"))
	_, err := s.db.ExecContext(ctx, q, id)
	return err
}

// --- Activities ---

func (s *PostgresStore) CreateActivity(ctx context.Context, a *models.Activity) error {
	a.CreatedAt = time.Now()
	q := fmt.Sprintf(`INSERT INTO %s (ticket_id, action, causer_type, causer_id, details, created_at)
		VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`, s.t("ticket_activities"))

	return s.db.QueryRowContext(ctx, q,
		a.TicketID, a.Action, a.CauserType, a.CauserID, a.Details, a.CreatedAt,
	).Scan(&a.ID)
}

func (s *PostgresStore) ListActivities(ctx context.Context, ticketID int64, limit int) ([]*models.Activity, error) {
	if limit <= 0 {
		limit = 20
	}
	q := fmt.Sprintf(`SELECT id, ticket_id, action, causer_type, causer_id, details, created_at
		FROM %s WHERE ticket_id = $1 ORDER BY created_at DESC LIMIT %d`,
		s.t("ticket_activities"), limit)

	rows, err := s.db.QueryContext(ctx, q, ticketID)
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

func (s *PostgresStore) ListSnoozedDueBefore(ctx context.Context, before time.Time) ([]*models.Ticket, error) {
	q := fmt.Sprintf(`SELECT id, reference, subject, description, status, priority, ticket_type,
		requester_type, requester_id, guest_name, guest_email, guest_token,
		assigned_to, department_id, sla_policy_id, merged_into_id,
		snoozed_until, snoozed_by, status_before_snooze,
		sla_first_response_due_at, sla_resolution_due_at, sla_breached,
		first_response_at, resolved_at, closed_at, metadata, created_at, updated_at
		FROM %s WHERE status = $1 AND snoozed_until IS NOT NULL AND snoozed_until <= $2`,
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
			&t.RequesterType, &t.RequesterID, &t.GuestName, &t.GuestEmail, &t.GuestToken,
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

func (s *PostgresStore) CreateSavedView(ctx context.Context, sv *models.SavedView) error {
	q := fmt.Sprintf(`INSERT INTO %s (name, filters, user_id, is_shared, position, icon, color, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`, s.t("saved_views"))
	now := time.Now()
	sv.CreatedAt = now
	sv.UpdatedAt = now
	return s.db.QueryRowContext(ctx, q,
		sv.Name, sv.Filters, sv.UserID, sv.IsShared, sv.Position, sv.Icon, sv.Color, sv.CreatedAt, sv.UpdatedAt,
	).Scan(&sv.ID)
}

func (s *PostgresStore) GetSavedView(ctx context.Context, id int64) (*models.SavedView, error) {
	q := fmt.Sprintf(`SELECT id, name, filters, user_id, is_shared, position, icon, color, created_at, updated_at
		FROM %s WHERE id = $1`, s.t("saved_views"))
	sv := &models.SavedView{}
	err := s.db.QueryRowContext(ctx, q, id).Scan(
		&sv.ID, &sv.Name, &sv.Filters, &sv.UserID, &sv.IsShared, &sv.Position, &sv.Icon, &sv.Color, &sv.CreatedAt, &sv.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return sv, nil
}

func (s *PostgresStore) ListSavedViews(ctx context.Context, userID int64, includeShared bool) ([]*models.SavedView, error) {
	var q string
	var args []any
	if includeShared {
		q = fmt.Sprintf(`SELECT id, name, filters, user_id, is_shared, position, icon, color, created_at, updated_at
			FROM %s WHERE user_id = $1 OR is_shared = TRUE ORDER BY position ASC, id ASC`, s.t("saved_views"))
		args = []any{userID}
	} else {
		q = fmt.Sprintf(`SELECT id, name, filters, user_id, is_shared, position, icon, color, created_at, updated_at
			FROM %s WHERE user_id = $1 ORDER BY position ASC, id ASC`, s.t("saved_views"))
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

func (s *PostgresStore) UpdateSavedView(ctx context.Context, sv *models.SavedView) error {
	sv.UpdatedAt = time.Now()
	q := fmt.Sprintf(`UPDATE %s SET name=$1, filters=$2, is_shared=$3, position=$4, icon=$5, color=$6, updated_at=$7 WHERE id=$8`, s.t("saved_views"))
	_, err := s.db.ExecContext(ctx, q, sv.Name, sv.Filters, sv.IsShared, sv.Position, sv.Icon, sv.Color, sv.UpdatedAt, sv.ID)
	return err
}

func (s *PostgresStore) DeleteSavedView(ctx context.Context, id int64) error {
	q := fmt.Sprintf(`DELETE FROM %s WHERE id = $1`, s.t("saved_views"))
	_, err := s.db.ExecContext(ctx, q, id)
	return err
}

func (s *PostgresStore) ReorderSavedViews(ctx context.Context, userID int64, ids []int64) error {
	for i, id := range ids {
		q := fmt.Sprintf(`UPDATE %s SET position = $1 WHERE id = $2 AND user_id = $3`, s.t("saved_views"))
		if _, err := s.db.ExecContext(ctx, q, i, id, userID); err != nil {
			return err
		}
	}
	return nil
}

// --- Attachments ---

func (s *PostgresStore) CreateAttachment(ctx context.Context, a *models.Attachment) error {
	now := time.Now()
	a.CreatedAt = now

	q := fmt.Sprintf(`INSERT INTO %s
		(ticket_id, reply_id, original_filename, mime_type, size, storage_path, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`, s.t("attachments"))

	return s.db.QueryRowContext(ctx, q,
		a.TicketID, a.ReplyID, a.OriginalFilename, a.MimeType, a.Size, a.StoragePath, a.CreatedAt,
	).Scan(&a.ID)
}

func (s *PostgresStore) GetAttachmentByID(ctx context.Context, id int64) (*models.Attachment, error) {
	q := fmt.Sprintf(`SELECT id, ticket_id, reply_id, original_filename, mime_type, size, storage_path, created_at
		FROM %s WHERE id = $1`, s.t("attachments"))

	a := &models.Attachment{}
	err := s.db.QueryRowContext(ctx, q, id).Scan(
		&a.ID, &a.TicketID, &a.ReplyID, &a.OriginalFilename, &a.MimeType, &a.Size, &a.StoragePath, &a.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return a, err
}

func (s *PostgresStore) GetAttachmentsByTicketID(ctx context.Context, ticketID int64) ([]*models.Attachment, error) {
	q := fmt.Sprintf(`SELECT id, ticket_id, reply_id, original_filename, mime_type, size, storage_path, created_at
		FROM %s WHERE ticket_id = $1 ORDER BY created_at ASC`, s.t("attachments"))

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

func (s *PostgresStore) GetAttachmentsByReplyID(ctx context.Context, replyID int64) ([]*models.Attachment, error) {
	q := fmt.Sprintf(`SELECT id, ticket_id, reply_id, original_filename, mime_type, size, storage_path, created_at
		FROM %s WHERE reply_id = $1 ORDER BY created_at ASC`, s.t("attachments"))

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
