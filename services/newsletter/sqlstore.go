package newsletter

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/escalated-dev/escalated-go/models"
)

type SQLStore struct {
	db      *sql.DB
	prefix  string
	dialect string
}

func NewSQLStore(db *sql.DB, prefix, dialect string) *SQLStore {
	if prefix == "" {
		prefix = "escalated_"
	}
	if dialect == "" {
		dialect = "postgres"
	}
	return &SQLStore{db: db, prefix: prefix, dialect: strings.ToLower(dialect)}
}

func (s *SQLStore) t(name string) string { return s.prefix + name }

func (s *SQLStore) p(n int) string {
	if s.dialect == "postgres" || s.dialect == "postgresql" {
		return fmt.Sprintf("$%d", n)
	}
	return "?"
}

func (s *SQLStore) placeholders(start, count int) string {
	parts := make([]string, count)
	for i := range parts {
		parts[i] = s.p(start + i)
	}
	return strings.Join(parts, ",")
}

func randomToken() (string, error) {
	var b [20]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func nullableUserID(ns sql.NullString) *models.UserID {
	if !ns.Valid || ns.String == "" {
		return nil
	}
	u := models.UserID(ns.String)
	return &u
}

func jsonMap(raw sql.NullString) map[string]any {
	if !raw.Valid || raw.String == "" {
		return nil
	}
	var out map[string]any
	_ = json.Unmarshal([]byte(raw.String), &out)
	return out
}

func jsonString(v map[string]any) string {
	if v == nil {
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func scanNewsletter(row interface{ Scan(...any) error }) (*models.Newsletter, error) {
	n := &models.Newsletter{}
	var createdBy, sentBy sql.NullString
	err := row.Scan(
		&n.ID, &n.Subject, &n.FromEmail, &n.FromName, &n.ReplyTo, &n.TargetListID, &n.TemplateID,
		&n.Theme, &n.BodyMarkdown, &n.Status, &n.ScheduledAt, &n.SentAt, &createdBy, &sentBy,
		&n.SummaryTotal, &n.SummarySent, &n.SummaryOpened, &n.SummaryClicked, &n.SummaryBounced,
		&n.SummaryComplained, &n.CreatedAt, &n.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	n.CreatedBy = nullableUserID(createdBy)
	n.SentBy = nullableUserID(sentBy)
	return n, nil
}

const newsletterCols = `id, subject, from_email, from_name, reply_to, target_list_id, template_id, theme,
body_markdown, status, scheduled_at, sent_at, created_by, sent_by, summary_total, summary_sent,
summary_opened, summary_clicked, summary_bounced, summary_complained, created_at, updated_at`

func scanList(row interface{ Scan(...any) error }) (*models.NewsletterList, error) {
	l := &models.NewsletterList{}
	var raw sql.NullString
	var createdBy sql.NullString
	err := row.Scan(&l.ID, &l.Name, &l.Description, &l.Kind, &raw, &createdBy, &l.CreatedAt, &l.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	l.FilterJSON = jsonMap(raw)
	l.CreatedBy = nullableUserID(createdBy)
	return l, nil
}

const listCols = `id, name, description, kind, filter_json, created_by, created_at, updated_at`

func scanTemplate(row interface{ Scan(...any) error }) (*models.NewsletterTemplate, error) {
	t := &models.NewsletterTemplate{}
	var raw sql.NullString
	var createdBy sql.NullString
	err := row.Scan(&t.ID, &t.Name, &t.Theme, &t.SubjectTemplate, &t.BodyMarkdown, &raw, &createdBy, &t.CreatedAt, &t.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	t.MergeFieldsSchema = jsonMap(raw)
	t.CreatedBy = nullableUserID(createdBy)
	return t, nil
}

const templateCols = `id, name, theme, subject_template, body_markdown, merge_fields_schema, created_by, created_at, updated_at`

func scanContact(row interface{ Scan(...any) error }) (*models.Contact, error) {
	c := &models.Contact{}
	var raw sql.NullString
	err := row.Scan(&c.ID, &c.Email, &c.Name, &c.UserID, &raw, &c.MarketingOptOutAt, &c.CreatedAt, &c.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	c.Metadata = jsonMap(raw)
	return c, nil
}

const contactCols = `id, email, name, user_id, metadata, marketing_opt_out_at, created_at, updated_at`

func scanDelivery(row interface{ Scan(...any) error }) (*models.NewsletterDelivery, error) {
	d := &models.NewsletterDelivery{}
	err := row.Scan(
		&d.ID, &d.NewsletterID, &d.ContactID, &d.EmailAtSend, &d.Status, &d.TrackingToken,
		&d.SentAt, &d.OpenedAt, &d.LastClickedAt, &d.ClicksCount, &d.BounceReason,
		&d.FailureReason, &d.AttemptCount, &d.ClaimedAt, &d.NextAttemptAt, &d.IsTest, &d.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return d, err
}

const deliveryCols = `id, newsletter_id, contact_id, email_at_send, status, tracking_token, sent_at,
opened_at, last_clicked_at, clicks_count, bounce_reason, failure_reason, attempt_count, claimed_at,
next_attempt_at, is_test, created_at`

func (s *SQLStore) GetSetting(ctx context.Context, key string) (string, error) {
	var value sql.NullString
	err := s.db.QueryRowContext(ctx, fmt.Sprintf(`SELECT value FROM %s WHERE key = %s`, s.t("settings"), s.p(1)), key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil || !value.Valid {
		return "", err
	}
	return value.String, nil
}

func (s *SQLStore) SetSetting(ctx context.Context, key, value string) error {
	q := fmt.Sprintf(`INSERT INTO %s (key, value, created_at, updated_at) VALUES (%s,%s,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value, updated_at = CURRENT_TIMESTAMP`, s.t("settings"), s.p(1), s.p(2))
	if s.dialect == "postgres" || s.dialect == "postgresql" {
		q = strings.ReplaceAll(q, "excluded.", "EXCLUDED.")
	}
	_, err := s.db.ExecContext(ctx, q, key, value)
	return err
}

func (s *SQLStore) ListNewsletters(ctx context.Context, statuses []string, limit int) ([]*models.Newsletter, error) {
	args := make([]any, len(statuses))
	for i, st := range statuses {
		args[i] = st
	}
	q := fmt.Sprintf(`SELECT %s FROM %s WHERE status IN (%s) ORDER BY created_at DESC LIMIT %d`,
		newsletterCols, s.t("newsletters"), s.placeholders(1, len(statuses)), limit)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.Newsletter
	for rows.Next() {
		n, err := scanNewsletter(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *SQLStore) GetNewsletter(ctx context.Context, id int64) (*models.Newsletter, error) {
	q := fmt.Sprintf(`SELECT %s FROM %s WHERE id = %s`, newsletterCols, s.t("newsletters"), s.p(1))
	return scanNewsletter(s.db.QueryRowContext(ctx, q, id))
}

func (s *SQLStore) CreateNewsletter(ctx context.Context, n *models.Newsletter) error {
	q := fmt.Sprintf(`INSERT INTO %s
		(subject, from_email, from_name, reply_to, target_list_id, template_id, theme, body_markdown, status, scheduled_at, created_by, sent_by, created_at, updated_at)
		VALUES (%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`,
		s.t("newsletters"), s.p(1), s.p(2), s.p(3), s.p(4), s.p(5), s.p(6), s.p(7), s.p(8), s.p(9), s.p(10), s.p(11), s.p(12))
	args := []any{n.Subject, n.FromEmail, n.FromName, n.ReplyTo, n.TargetListID, n.TemplateID, n.Theme, n.BodyMarkdown, n.Status, n.ScheduledAt, n.CreatedBy, n.SentBy}
	if s.dialect == "postgres" || s.dialect == "postgresql" {
		return s.db.QueryRowContext(ctx, q+" RETURNING id", args...).Scan(&n.ID)
	}
	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return err
	}
	n.ID, err = res.LastInsertId()
	return err
}

func (s *SQLStore) UpdateNewsletter(ctx context.Context, n *models.Newsletter) error {
	q := fmt.Sprintf(`UPDATE %s SET subject=%s, from_email=%s, from_name=%s, reply_to=%s, target_list_id=%s, template_id=%s,
		theme=%s, body_markdown=%s, status=%s, scheduled_at=%s, updated_at=CURRENT_TIMESTAMP WHERE id=%s`,
		s.t("newsletters"), s.p(1), s.p(2), s.p(3), s.p(4), s.p(5), s.p(6), s.p(7), s.p(8), s.p(9), s.p(10), s.p(11))
	_, err := s.db.ExecContext(ctx, q, n.Subject, n.FromEmail, n.FromName, n.ReplyTo, n.TargetListID, n.TemplateID, n.Theme, n.BodyMarkdown, n.Status, n.ScheduledAt, n.ID)
	return err
}

func (s *SQLStore) DeleteNewsletter(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE id=%s`, s.t("newsletters"), s.p(1)), id)
	return err
}

func (s *SQLStore) ListScheduledDue(ctx context.Context, now time.Time) ([]*models.Newsletter, error) {
	q := fmt.Sprintf(`SELECT %s FROM %s WHERE status = %s AND scheduled_at <= %s ORDER BY scheduled_at ASC`,
		newsletterCols, s.t("newsletters"), s.p(1), s.p(2))
	rows, err := s.db.QueryContext(ctx, q, models.NewsletterScheduled, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.Newsletter
	for rows.Next() {
		n, err := scanNewsletter(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *SQLStore) ListLists(ctx context.Context) ([]*models.NewsletterList, error) {
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`SELECT %s FROM %s ORDER BY created_at DESC`, listCols, s.t("newsletter_lists")))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.NewsletterList
	for rows.Next() {
		l, err := scanList(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *SQLStore) GetList(ctx context.Context, id int64) (*models.NewsletterList, error) {
	return scanList(s.db.QueryRowContext(ctx, fmt.Sprintf(`SELECT %s FROM %s WHERE id = %s`, listCols, s.t("newsletter_lists"), s.p(1)), id))
}

func (s *SQLStore) CreateList(ctx context.Context, l *models.NewsletterList) error {
	filter := jsonString(l.FilterJSON)
	q := fmt.Sprintf(`INSERT INTO %s (name, description, kind, filter_json, created_by, created_at, updated_at)
		VALUES (%s,%s,%s,%s,%s,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`, s.t("newsletter_lists"), s.p(1), s.p(2), s.p(3), s.p(4), s.p(5))
	args := []any{l.Name, l.Description, l.Kind, filter, l.CreatedBy}
	if s.dialect == "postgres" || s.dialect == "postgresql" {
		return s.db.QueryRowContext(ctx, q+" RETURNING id", args...).Scan(&l.ID)
	}
	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return err
	}
	l.ID, err = res.LastInsertId()
	return err
}

func (s *SQLStore) UpdateList(ctx context.Context, l *models.NewsletterList) error {
	q := fmt.Sprintf(`UPDATE %s SET name=%s, description=%s, filter_json=%s, updated_at=CURRENT_TIMESTAMP WHERE id=%s`,
		s.t("newsletter_lists"), s.p(1), s.p(2), s.p(3), s.p(4))
	_, err := s.db.ExecContext(ctx, q, l.Name, l.Description, jsonString(l.FilterJSON), l.ID)
	return err
}

func (s *SQLStore) DeleteList(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE id=%s`, s.t("newsletter_lists"), s.p(1)), id)
	return err
}

func (s *SQLStore) AddMember(ctx context.Context, listID, contactID int64, addedBy *models.UserID) error {
	q := fmt.Sprintf(`INSERT INTO %s (list_id, contact_id, added_by, added_at) VALUES (%s,%s,%s,CURRENT_TIMESTAMP) ON CONFLICT (list_id, contact_id) DO NOTHING`,
		s.t("newsletter_list_members"), s.p(1), s.p(2), s.p(3))
	_, err := s.db.ExecContext(ctx, q, listID, contactID, addedBy)
	return err
}

func (s *SQLStore) RemoveMember(ctx context.Context, listID, contactID int64) error {
	q := fmt.Sprintf(`DELETE FROM %s WHERE list_id=%s AND contact_id=%s`, s.t("newsletter_list_members"), s.p(1), s.p(2))
	_, err := s.db.ExecContext(ctx, q, listID, contactID)
	return err
}

func (s *SQLStore) ListMembers(ctx context.Context, listID int64, limit int) ([]*models.NewsletterListMember, error) {
	q := fmt.Sprintf(`SELECT id, list_id, contact_id, added_at, added_by FROM %s WHERE list_id=%s ORDER BY id DESC LIMIT %d`,
		s.t("newsletter_list_members"), s.p(1), limit)
	rows, err := s.db.QueryContext(ctx, q, listID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.NewsletterListMember
	for rows.Next() {
		m := &models.NewsletterListMember{}
		var addedBy sql.NullString
		if err := rows.Scan(&m.ID, &m.ListID, &m.ContactID, &m.AddedAt, &addedBy); err != nil {
			return nil, err
		}
		m.AddedBy = nullableUserID(addedBy)
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *SQLStore) ListMemberIDs(ctx context.Context, listID int64) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`SELECT contact_id FROM %s WHERE list_id=%s ORDER BY contact_id`, s.t("newsletter_list_members"), s.p(1)), listID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *SQLStore) CountListMembers(ctx context.Context, listID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE list_id=%s`, s.t("newsletter_list_members"), s.p(1)), listID).Scan(&n)
	return n, err
}

func (s *SQLStore) CountListOptedOut(ctx context.Context, listID int64) (int, error) {
	q := fmt.Sprintf(`SELECT COUNT(*) FROM %s m JOIN %s c ON c.id = m.contact_id WHERE m.list_id=%s AND c.marketing_opt_out_at IS NOT NULL`,
		s.t("newsletter_list_members"), s.t("contacts"), s.p(1))
	var n int
	err := s.db.QueryRowContext(ctx, q, listID).Scan(&n)
	return n, err
}

func (s *SQLStore) ContactExists(ctx context.Context, id int64) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE id=%s`, s.t("contacts"), s.p(1)), id).Scan(&n)
	return n > 0, err
}

func (s *SQLStore) GetContact(ctx context.Context, id int64) (*models.Contact, error) {
	return scanContact(s.db.QueryRowContext(ctx, fmt.Sprintf(`SELECT %s FROM %s WHERE id=%s`, contactCols, s.t("contacts"), s.p(1)), id))
}

func (s *SQLStore) GetContactByEmail(ctx context.Context, email string) (*models.Contact, error) {
	return scanContact(s.db.QueryRowContext(ctx, fmt.Sprintf(`SELECT %s FROM %s WHERE email=%s`, contactCols, s.t("contacts"), s.p(1)), email))
}

func (s *SQLStore) CreateContact(ctx context.Context, c *models.Contact) error {
	q := fmt.Sprintf(`INSERT INTO %s (email, name, user_id, metadata, marketing_opt_out_at, created_at, updated_at)
		VALUES (%s,%s,%s,%s,%s,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`, s.t("contacts"), s.p(1), s.p(2), s.p(3), s.p(4), s.p(5))
	args := []any{c.Email, c.Name, c.UserID, jsonString(c.Metadata), c.MarketingOptOutAt}
	if s.dialect == "postgres" || s.dialect == "postgresql" {
		return s.db.QueryRowContext(ctx, q+" RETURNING id", args...).Scan(&c.ID)
	}
	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return err
	}
	c.ID, err = res.LastInsertId()
	return err
}

func (s *SQLStore) UpdateContactOptOut(ctx context.Context, id int64, when time.Time) error {
	q := fmt.Sprintf(`UPDATE %s SET marketing_opt_out_at=%s, updated_at=CURRENT_TIMESTAMP WHERE id=%s`, s.t("contacts"), s.p(1), s.p(2))
	_, err := s.db.ExecContext(ctx, q, when, id)
	return err
}

func (s *SQLStore) ContactsByIDs(ctx context.Context, ids []int64) ([]*models.Contact, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	q := fmt.Sprintf(`SELECT %s FROM %s WHERE id IN (%s) ORDER BY id`, contactCols, s.t("contacts"), s.placeholders(1, len(ids)))
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.Contact
	for rows.Next() {
		c, err := scanContact(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *SQLStore) ListTemplates(ctx context.Context) ([]*models.NewsletterTemplate, error) {
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`SELECT %s FROM %s ORDER BY created_at DESC`, templateCols, s.t("newsletter_templates")))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.NewsletterTemplate
	for rows.Next() {
		t, err := scanTemplate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *SQLStore) GetTemplate(ctx context.Context, id int64) (*models.NewsletterTemplate, error) {
	return scanTemplate(s.db.QueryRowContext(ctx, fmt.Sprintf(`SELECT %s FROM %s WHERE id=%s`, templateCols, s.t("newsletter_templates"), s.p(1)), id))
}

func (s *SQLStore) CreateTemplate(ctx context.Context, t *models.NewsletterTemplate) error {
	q := fmt.Sprintf(`INSERT INTO %s (name, theme, subject_template, body_markdown, merge_fields_schema, created_by, created_at, updated_at)
		VALUES (%s,%s,%s,%s,%s,%s,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`, s.t("newsletter_templates"), s.p(1), s.p(2), s.p(3), s.p(4), s.p(5), s.p(6))
	args := []any{t.Name, t.Theme, t.SubjectTemplate, t.BodyMarkdown, jsonString(t.MergeFieldsSchema), t.CreatedBy}
	if s.dialect == "postgres" || s.dialect == "postgresql" {
		return s.db.QueryRowContext(ctx, q+" RETURNING id", args...).Scan(&t.ID)
	}
	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return err
	}
	t.ID, err = res.LastInsertId()
	return err
}

func (s *SQLStore) UpdateTemplate(ctx context.Context, t *models.NewsletterTemplate) error {
	q := fmt.Sprintf(`UPDATE %s SET name=%s, theme=%s, subject_template=%s, body_markdown=%s, merge_fields_schema=%s, updated_at=CURRENT_TIMESTAMP WHERE id=%s`,
		s.t("newsletter_templates"), s.p(1), s.p(2), s.p(3), s.p(4), s.p(5), s.p(6))
	_, err := s.db.ExecContext(ctx, q, t.Name, t.Theme, t.SubjectTemplate, t.BodyMarkdown, jsonString(t.MergeFieldsSchema), t.ID)
	return err
}

func (s *SQLStore) DeleteTemplate(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE id=%s`, s.t("newsletter_templates"), s.p(1)), id)
	return err
}

func (s *SQLStore) InsertDelivery(ctx context.Context, d *models.NewsletterDelivery) error {
	q := fmt.Sprintf(`INSERT INTO %s (newsletter_id, contact_id, email_at_send, status, tracking_token, attempt_count, is_test, created_at)
		VALUES (%s,%s,%s,%s,%s,%s,%s,CURRENT_TIMESTAMP)`, s.t("newsletter_deliveries"), s.p(1), s.p(2), s.p(3), s.p(4), s.p(5), s.p(6), s.p(7))
	args := []any{d.NewsletterID, d.ContactID, d.EmailAtSend, d.Status, d.TrackingToken, d.AttemptCount, d.IsTest}
	if s.dialect == "postgres" || s.dialect == "postgresql" {
		return s.db.QueryRowContext(ctx, q+" RETURNING id", args...).Scan(&d.ID)
	}
	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return err
	}
	d.ID, err = res.LastInsertId()
	return err
}

func (s *SQLStore) GetDelivery(ctx context.Context, id int64) (*models.NewsletterDelivery, error) {
	return scanDelivery(s.db.QueryRowContext(ctx, fmt.Sprintf(`SELECT %s FROM %s WHERE id=%s`, deliveryCols, s.t("newsletter_deliveries"), s.p(1)), id))
}

func (s *SQLStore) GetDeliveryByToken(ctx context.Context, token string) (*models.NewsletterDelivery, error) {
	return scanDelivery(s.db.QueryRowContext(ctx, fmt.Sprintf(`SELECT %s FROM %s WHERE tracking_token=%s`, deliveryCols, s.t("newsletter_deliveries"), s.p(1)), token))
}

func (s *SQLStore) ListDeliveries(ctx context.Context, newsletterID int64, status string, includeTest bool, limit int) ([]*models.NewsletterDelivery, error) {
	args := []any{newsletterID}
	where := fmt.Sprintf("newsletter_id=%s", s.p(1))
	next := 2
	if status != "" {
		where += fmt.Sprintf(" AND status=%s", s.p(next))
		args = append(args, status)
	}
	if !includeTest {
		if s.dialect == "postgres" || s.dialect == "postgresql" {
			where += " AND is_test = FALSE"
		} else {
			where += " AND is_test = 0"
		}
	}
	q := fmt.Sprintf(`SELECT %s FROM %s WHERE %s ORDER BY id DESC LIMIT %d`, deliveryCols, s.t("newsletter_deliveries"), where, limit)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.NewsletterDelivery
	for rows.Next() {
		d, err := scanDelivery(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *SQLStore) UpdateDelivery(ctx context.Context, d *models.NewsletterDelivery) error {
	q := fmt.Sprintf(`UPDATE %s SET status=%s, sent_at=%s, opened_at=%s, last_clicked_at=%s, clicks_count=%s,
		bounce_reason=%s, failure_reason=%s, attempt_count=%s, claimed_at=%s, next_attempt_at=%s WHERE id=%s`,
		s.t("newsletter_deliveries"), s.p(1), s.p(2), s.p(3), s.p(4), s.p(5), s.p(6), s.p(7), s.p(8), s.p(9), s.p(10), s.p(11))
	_, err := s.db.ExecContext(ctx, q, d.Status, d.SentAt, d.OpenedAt, d.LastClickedAt, d.ClicksCount,
		d.BounceReason, d.FailureReason, d.AttemptCount, d.ClaimedAt, d.NextAttemptAt, d.ID)
	return err
}

func (s *SQLStore) IncrementNewsletter(ctx context.Context, id int64, col string, by int) error {
	allowed := map[string]bool{
		"summary_sent": true, "summary_opened": true, "summary_clicked": true,
		"summary_bounced": true, "summary_complained": true,
	}
	if !allowed[col] {
		return fmt.Errorf("unsupported newsletter counter %q", col)
	}
	q := fmt.Sprintf(`UPDATE %s SET %s = %s + %s, updated_at=CURRENT_TIMESTAMP WHERE id=%s`,
		s.t("newsletters"), col, col, s.p(1), s.p(2))
	_, err := s.db.ExecContext(ctx, q, by, id)
	return err
}

func (s *SQLStore) SetNewsletterStatus(ctx context.Context, id int64, status models.NewsletterStatus, sentAt *time.Time) error {
	q := fmt.Sprintf(`UPDATE %s SET status=%s, sent_at=%s, updated_at=CURRENT_TIMESTAMP WHERE id=%s`, s.t("newsletters"), s.p(1), s.p(2), s.p(3))
	_, err := s.db.ExecContext(ctx, q, status, sentAt, id)
	return err
}

func (s *SQLStore) SetNewsletterSummaryTotal(ctx context.Context, id int64, total int) error {
	q := fmt.Sprintf(`UPDATE %s SET summary_total=%s, updated_at=CURRENT_TIMESTAMP WHERE id=%s`, s.t("newsletters"), s.p(1), s.p(2))
	_, err := s.db.ExecContext(ctx, q, total, id)
	return err
}
