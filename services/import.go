package services

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/escalated-dev/escalated-go/internal/sqldialect"
	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/store"
)

// ImportAdapter is a source the admin Import wizard can import from, as
// escalated-laravel's Contracts\ImportAdapter. Escalated ships none: the host
// (or a plugin package it compiles in) lists its adapters in
// Config.ImportAdapters -- a Zendesk or Freshdesk adapter, say.
//
// Extract returns records already normalized ("source_id", "title",
// "requester_source_id", "tag_source_ids", "ticket_source_id", ...), as
// Laravel's adapters do.
type ImportAdapter interface {
	// Name is the unique slug, e.g. "zendesk".
	Name() string
	// DisplayName is the human-readable name, e.g. "Zendesk".
	DisplayName() string
	// CredentialFields describe the form: name, label, type (text, password
	// or url) and help.
	CredentialFields() []map[string]any
	// TestConnection validates credentials with a test call. An error's
	// message is shown to the admin.
	TestConnection(ctx context.Context, credentials map[string]string) (bool, error)
	// EntityTypes are the importable types in dependency order.
	EntityTypes() []string
	// DefaultFieldMappings maps source fields to Escalated fields.
	DefaultFieldMappings(entityType string) map[string]string
	// AvailableSourceFields lists the source fields as the Mapping screen
	// reads them: name, label and escalated_options.
	AvailableSourceFields(ctx context.Context, entityType string, credentials map[string]string) ([]map[string]any, error)
	// Extract returns one batch. A nil Cursor means the type is exhausted.
	Extract(ctx context.Context, entityType string, credentials map[string]string, cursor *string) (*ExtractResult, error)
}

// ExtractResult is one batch from ImportAdapter.Extract.
type ExtractResult struct {
	Records    []map[string]any
	Cursor     *string
	TotalCount *int
}

type importModeKey struct{}

// WithImportMode marks ctx as an import run, as Laravel's ImportContext
// does: SLA deadlines and breach and warning checks stand down for work done
// under it, so imported history does not fire as if it were new. It is a
// context value, so only the import's own goroutine is in import mode.
func WithImportMode(ctx context.Context) context.Context {
	return context.WithValue(ctx, importModeKey{}, true)
}

// IsImporting reports whether ctx is an import run.
func IsImporting(ctx context.Context) bool {
	v, _ := ctx.Value(importModeKey{}).(bool)
	return v
}

// ImportCredentials seals a source's credentials for where they rest: the
// wizard's cookie, from a tested connection until the import starts, and the
// import job's credentials column until the run ends or is cancelled.
// AES-256-GCM under a key derived from Secret.
type ImportCredentials struct {
	Secret string
}

func (c ImportCredentials) aead() (cipher.AEAD, error) {
	if len(c.Secret) < 16 {
		return nil, errors.New("escalated: imports need a secret (Config.ImportSecret or GuestAccess.Secret) of 16 bytes or more")
	}
	key := sha256.Sum256([]byte("escalated import credentials\x00" + c.Secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// Seal encrypts credentials.
func (c ImportCredentials) Seal(credentials map[string]string) (string, error) {
	aead, err := c.aead()
	if err != nil {
		return "", err
	}
	plain, err := json.Marshal(credentials)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(aead.Seal(nonce, nonce, plain, []byte("escalated-import"))), nil
}

// Open decrypts what Seal produced; nil when absent, tampered with or sealed
// under another secret.
func (c ImportCredentials) Open(sealed string) map[string]string {
	if sealed == "" {
		return nil
	}
	aead, err := c.aead()
	if err != nil {
		return nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(sealed)
	if err != nil || len(raw) < aead.NonceSize() {
		return nil
	}
	plain, err := aead.Open(nil, raw[:aead.NonceSize()], raw[aead.NonceSize():], []byte("escalated-import"))
	if err != nil {
		return nil
	}
	out := map[string]string{}
	if json.Unmarshal(plain, &out) != nil {
		return nil
	}
	return out
}

// ImportJob is one import, as Laravel's ImportJob. Credentials holds the
// sealed source credentials and is never serialised.
type ImportJob struct {
	ID            string
	UserID        *models.UserID
	Platform      string
	Status        string
	Credentials   *string `json:"-"` // sealed; read server-side only, never serialized
	FieldMappings map[string]any
	Progress      map[string]map[string]any
	ErrorLog      []map[string]any
	StartedAt     *time.Time
	CompletedAt   *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Resumable reports whether the job may be resumed: paused or failed.
func (j *ImportJob) Resumable() bool { return j.Status == "paused" || j.Status == "failed" }

const importMaxErrors = 10000

// ImportService keeps import jobs and runs them, as Laravel's ImportService:
// it walks the adapter's entity types in order under import mode, resumes each
// from its saved cursor and counters, writes every record not yet in the
// source map, logs failures, saves progress after each batch, and stops
// between batches when the job has been paused or cancelled. A completed job
// drops its credentials.
//
// Contacts the host has as users (UserIDByEmail) import as those users;
// others become Escalated contacts on guest tickets: Escalated owns no users
// table.
type ImportService struct {
	DB       *sql.DB
	Prefix   string
	Store    store.Store
	Adapters []ImportAdapter
	Secret   string

	// UserIDByEmail finds a host user by email: agents must exist, contacts
	// may. Nil treats every contact as a guest and fails every agent.
	UserIDByEmail func(ctx context.Context, email string) (models.UserID, error)
	// UserType is the requester type stored for a host user ("User").
	UserType string
	// Dispatch hands a started job to the host's queue, which calls Run.
	// Nil runs it in a goroutine.
	Dispatch func(jobID string)
	// AttachmentStorage stores imported attachment bytes. Nil fails every
	// imported attachment.
	AttachmentStorage ImportAttachmentStorage
	// AttachmentMaxBytes caps one imported attachment; zero is
	// DefaultImportAttachmentMaxBytes.
	AttachmentMaxBytes int64

	wg sync.WaitGroup
}

func (s *ImportService) t(name string) string  { return s.Prefix + name }
func (s *ImportService) q(query string) string { return sqldialect.Rebind(s.DB, query) }

// Credentials is the sealer for this service's secret.
func (s *ImportService) Credentials() ImportCredentials { return ImportCredentials{Secret: s.Secret} }

// Adapter returns the adapter named platform, or nil.
func (s *ImportService) Adapter(platform string) ImportAdapter {
	for _, a := range s.Adapters {
		if a.Name() == platform {
			return a
		}
	}
	return nil
}

const importJobColumns = "id, user_id, platform, status, credentials, field_mappings, progress, error_log, started_at, completed_at, created_at, updated_at"

func scanImportJob(row interface{ Scan(...any) error }) (*ImportJob, error) {
	j := &ImportJob{}
	var mappings, progress, errs sql.NullString
	var started, completed sql.NullTime
	if err := row.Scan(&j.ID, &j.UserID, &j.Platform, &j.Status, &j.Credentials, &mappings, &progress, &errs,
		&started, &completed, &j.CreatedAt, &j.UpdatedAt); err != nil {
		return nil, err
	}
	j.FieldMappings = map[string]any{}
	j.Progress = map[string]map[string]any{}
	j.ErrorLog = []map[string]any{}
	if mappings.Valid && mappings.String != "" {
		_ = json.Unmarshal([]byte(mappings.String), &j.FieldMappings)
	}
	if progress.Valid && progress.String != "" {
		_ = json.Unmarshal([]byte(progress.String), &j.Progress)
	}
	if errs.Valid && errs.String != "" {
		_ = json.Unmarshal([]byte(errs.String), &j.ErrorLog)
	}
	if started.Valid {
		t := started.Time
		j.StartedAt = &t
	}
	if completed.Valid {
		t := completed.Time
		j.CompletedAt = &t
	}
	return j, nil
}

func jsonText(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// Get returns the job with id, or nil.
func (s *ImportService) Get(ctx context.Context, id string) (*ImportJob, error) {
	j, err := scanImportJob(s.DB.QueryRowContext(ctx, s.q("SELECT "+importJobColumns+" FROM "+s.t("import_jobs")+" WHERE id = ?"), id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return j, err
}

// Recent returns the newest jobs.
func (s *ImportService) Recent(ctx context.Context, limit int) ([]*ImportJob, error) {
	rows, err := s.DB.QueryContext(ctx, s.q("SELECT "+importJobColumns+" FROM "+s.t("import_jobs")+" ORDER BY created_at DESC, id DESC LIMIT "+strconv.Itoa(limit)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*ImportJob{}
	for rows.Next() {
		j, err := scanImportJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// Create inserts a job with sealed credentials.
func (s *ImportService) Create(ctx context.Context, j *ImportJob, credentials map[string]string) error {
	sealed, err := s.Credentials().Seal(credentials)
	if err != nil {
		return err
	}
	id, err := newUUID()
	if err != nil {
		return err
	}
	j.ID = id
	j.Credentials = &sealed
	now := time.Now().UTC()
	j.CreatedAt, j.UpdatedAt = now, now
	if j.FieldMappings == nil {
		j.FieldMappings = map[string]any{}
	}
	if j.Progress == nil {
		j.Progress = map[string]map[string]any{}
	}
	if j.ErrorLog == nil {
		j.ErrorLog = []map[string]any{}
	}
	_, err = s.DB.ExecContext(ctx, s.q("INSERT INTO "+s.t("import_jobs")+" ("+importJobColumns+") VALUES (?,?,?,?,?,?,?,?,?,?,?,?)"),
		j.ID, j.UserID, j.Platform, j.Status, j.Credentials, jsonText(j.FieldMappings), jsonText(j.Progress), jsonText(j.ErrorLog),
		j.StartedAt, j.CompletedAt, j.CreatedAt, j.UpdatedAt)
	return err
}

// Save writes every column back.
func (s *ImportService) Save(ctx context.Context, j *ImportJob) error {
	j.UpdatedAt = time.Now().UTC()
	_, err := s.DB.ExecContext(ctx, s.q("UPDATE "+s.t("import_jobs")+` SET status = ?, credentials = ?, field_mappings = ?, progress = ?,
		error_log = ?, started_at = ?, completed_at = ?, updated_at = ? WHERE id = ?`),
		j.Status, j.Credentials, jsonText(j.FieldMappings), jsonText(j.Progress), jsonText(j.ErrorLog), j.StartedAt, j.CompletedAt, j.UpdatedAt, j.ID)
	return err
}

// saveProgress writes the counters and error log only. The status is the
// admin's to change while a run is going (pause, cancel), so the run never
// writes it back from its own copy.
func (s *ImportService) saveProgress(ctx context.Context, j *ImportJob) error {
	j.UpdatedAt = time.Now().UTC()
	_, err := s.DB.ExecContext(ctx, s.q("UPDATE "+s.t("import_jobs")+" SET progress = ?, error_log = ?, updated_at = ? WHERE id = ?"),
		jsonText(j.Progress), jsonText(j.ErrorLog), j.UpdatedAt, j.ID)
	return err
}

func (s *ImportService) currentStatus(ctx context.Context, id string) string {
	var status string
	_ = s.DB.QueryRowContext(ctx, s.q("SELECT status FROM "+s.t("import_jobs")+" WHERE id = ?"), id).Scan(&status)
	return status
}

// Queue runs the job in the background: Dispatch, or a goroutine.
func (s *ImportService) Queue(jobID string) {
	if s.Dispatch != nil {
		s.Dispatch(jobID)
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		if err := s.Run(context.Background(), jobID); err != nil {
			log.Printf("escalated: import %s failed: %v", jobID, err)
		}
	}()
}

// Wait blocks until every goroutine run started by Queue has finished.
func (s *ImportService) Wait() { s.wg.Wait() }

// Run runs (or resumes) the job. A run that fails marks the job failed.
func (s *ImportService) Run(ctx context.Context, jobID string) error {
	job, err := s.Get(ctx, jobID)
	if err != nil || job == nil || job.Status == "completed" {
		return err
	}
	if err := s.run(WithImportMode(ctx), job); err != nil {
		job.Status = "failed"
		_ = s.Save(ctx, job)
		return err
	}
	return nil
}

func (s *ImportService) run(ctx context.Context, job *ImportJob) error {
	adapter := s.Adapter(job.Platform)
	if adapter == nil {
		return fmt.Errorf("no adapter found for platform %q", job.Platform)
	}
	job.Status = "importing"
	if job.StartedAt == nil {
		now := time.Now().UTC()
		job.StartedAt = &now
	}
	if err := s.Save(ctx, job); err != nil {
		return err
	}
	credentials := map[string]string{}
	if job.Credentials != nil {
		credentials = s.Credentials().Open(*job.Credentials)
	}

	for _, entityType := range adapter.EntityTypes() {
		if s.stopped(ctx, job) {
			return nil
		}
		stopped, err := s.importEntityType(ctx, job, adapter, entityType, credentials)
		if err != nil {
			return err
		}
		if stopped {
			return nil
		}
	}
	if s.stopped(ctx, job) {
		return nil
	}
	now := time.Now().UTC()
	job.Status = "completed"
	job.CompletedAt = &now
	job.Credentials = nil
	return s.Save(ctx, job)
}

// stopped re-reads the status the admin may have changed: paused, or
// cancelled (failed).
func (s *ImportService) stopped(ctx context.Context, job *ImportJob) bool {
	status := s.currentStatus(ctx, job.ID)
	if status == "paused" || status == "failed" {
		job.Status = status
		return true
	}
	return false
}

func intOf(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	}
	return 0
}

func (s *ImportService) importEntityType(ctx context.Context, job *ImportJob, adapter ImportAdapter, entityType string, credentials map[string]string) (bool, error) {
	saved := job.Progress[entityType]
	if saved == nil {
		saved = map[string]any{"total": 0, "processed": 0, "skipped": 0, "failed": 0, "cursor": nil}
		job.Progress[entityType] = saved
	}
	processed, skipped, failed := intOf(saved["processed"]), intOf(saved["skipped"]), intOf(saved["failed"])
	var cursor *string
	if c, ok := saved["cursor"].(string); ok {
		cursor = &c
	}

	for {
		result, err := adapter.Extract(ctx, entityType, credentials, cursor)
		if err != nil {
			return false, err
		}
		for _, record := range result.Records {
			sourceID := strings.TrimSpace(fmt.Sprint(record["source_id"]))
			if record["source_id"] == nil || sourceID == "" {
				failed++
				continue
			}
			if id, _ := s.resolve(ctx, job.ID, entityType, sourceID); id != "" {
				skipped++
				continue
			}
			escalatedID, err := s.persist(ctx, job, entityType, record)
			if err == nil {
				_, err = s.DB.ExecContext(ctx, s.q("INSERT INTO "+s.t("import_source_maps")+" (import_job_id, entity_type, source_id, escalated_id, created_at) VALUES (?,?,?,?,?)"),
					job.ID, entityType, sourceID, escalatedID, time.Now().UTC())
			}
			if err != nil {
				failed++
				if len(job.ErrorLog) < importMaxErrors {
					job.ErrorLog = append(job.ErrorLog, map[string]any{
						"entity_type": entityType, "source_id": sourceID, "error": err.Error(),
						"timestamp": time.Now().UTC().Format(time.RFC3339),
					})
				}
				continue
			}
			processed++
		}

		progress := job.Progress[entityType]
		if result.TotalCount != nil {
			progress["total"] = *result.TotalCount
		}
		progress["processed"], progress["skipped"], progress["failed"] = processed, skipped, failed
		// As Laravel's: an exhausted extract keeps the last cursor, so a
		// resume re-reads one page, not the whole type.
		if result.Cursor != nil {
			progress["cursor"] = *result.Cursor
		}
		if err := s.saveProgress(ctx, job); err != nil {
			return false, err
		}
		if s.stopped(ctx, job) {
			return true, nil
		}
		if result.Cursor == nil {
			return false, nil
		}
		cursor = result.Cursor
	}
}

func (s *ImportService) resolve(ctx context.Context, jobID, entityType, sourceID string) (string, error) {
	var id string
	err := s.DB.QueryRowContext(ctx, s.q("SELECT escalated_id FROM "+s.t("import_source_maps")+" WHERE import_job_id = ? AND entity_type = ? AND source_id = ?"),
		jobID, entityType, sourceID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}

func (s *ImportService) ref(ctx context.Context, job *ImportJob, entityType string, sourceID any) string {
	if sourceID == nil {
		return ""
	}
	sid := strings.TrimSpace(fmt.Sprint(sourceID))
	if sid == "" {
		return ""
	}
	id, _ := s.resolve(ctx, job.ID, entityType, sid)
	return id
}

const importContactPrefix = "contact:"

func str(record map[string]any, key string) string {
	if v, ok := record[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func (s *ImportService) persist(ctx context.Context, job *ImportJob, entityType string, record map[string]any) (string, error) {
	switch entityType {
	case "agents":
		email := str(record, "email")
		if s.UserIDByEmail != nil && email != "" {
			if id, err := s.UserIDByEmail(ctx, email); err == nil && id != "" {
				return string(id), nil
			}
		}
		return "", fmt.Errorf("agent with email '%s' not found in host application", email)
	case "contacts":
		return s.persistContact(ctx, record)
	case "tags":
		return s.persistTag(ctx, record)
	case "departments":
		return s.persistDepartment(ctx, record)
	case "tickets":
		return s.persistTicket(ctx, job, record)
	case "replies":
		return s.persistReply(ctx, job, record)
	case "custom_fields":
		return s.persistCustomField(ctx, record)
	case "attachments":
		return s.persistAttachment(ctx, job, record)
	case "satisfaction_ratings":
		return s.persistSatisfactionRating(ctx, job, record)
	}
	return "", fmt.Errorf("unknown entity type: %s", entityType)
}

func (s *ImportService) persistContact(ctx context.Context, record map[string]any) (string, error) {
	email := str(record, "email")
	if email == "" {
		return "", errors.New("contact has no email")
	}
	if s.UserIDByEmail != nil {
		if id, err := s.UserIDByEmail(ctx, email); err == nil && id != "" {
			return string(id), nil
		}
	}
	normalized := models.NormalizeEmail(email)
	contact, err := s.Store.GetContactByEmail(ctx, normalized)
	if err != nil {
		return "", err
	}
	if contact == nil {
		contact = &models.Contact{Email: normalized, Metadata: map[string]any{}}
		if name := str(record, "name"); name != "" {
			contact.Name = &name
		}
		if err := s.Store.CreateContact(ctx, contact); err != nil {
			return "", err
		}
	}
	return importContactPrefix + strconv.FormatInt(contact.ID, 10), nil
}

var importSlug = regexp.MustCompile(`[^a-z0-9]+`)

func importSlugify(name string) string {
	return strings.Trim(importSlug.ReplaceAllString(strings.ToLower(name), "-"), "-")
}

func (s *ImportService) persistTag(ctx context.Context, record map[string]any) (string, error) {
	name := str(record, "name")
	slug := importSlugify(name)
	tags, err := s.Store.ListTags(ctx)
	if err != nil {
		return "", err
	}
	for _, tag := range tags {
		if tag.Slug == slug {
			return strconv.FormatInt(tag.ID, 10), nil
		}
	}
	tag := &models.Tag{Name: name, Slug: slug}
	if err := s.Store.CreateTag(ctx, tag); err != nil {
		return "", err
	}
	return strconv.FormatInt(tag.ID, 10), nil
}

func (s *ImportService) persistDepartment(ctx context.Context, record map[string]any) (string, error) {
	name := str(record, "name")
	slug := importSlugify(name)
	departments, err := s.Store.ListDepartments(ctx, false)
	if err != nil {
		return "", err
	}
	for _, d := range departments {
		if d.Slug == slug {
			return strconv.FormatInt(d.ID, 10), nil
		}
	}
	d := &models.Department{Name: name, Slug: slug, IsActive: true}
	if err := s.Store.CreateDepartment(ctx, d); err != nil {
		return "", err
	}
	return strconv.FormatInt(d.ID, 10), nil
}

func lookupCode(names map[int]string, value string, fallback int) int {
	for code, name := range names {
		if name == value {
			return code
		}
	}
	return fallback
}

func (s *ImportService) persistTicket(ctx context.Context, job *ImportJob, record map[string]any) (string, error) {
	subject := str(record, "title")
	if subject == "" {
		subject = str(record, "subject")
	}
	if subject == "" {
		subject = "Imported ticket"
	}
	description := str(record, "description")
	if description == "" {
		description = str(record, "body")
	}
	if description == "" {
		description = subject
	}
	t := &models.Ticket{
		Reference:   models.GenerateReference("ESC"),
		Subject:     subject,
		Description: description,
		// As Laravel's TicketStatus::tryFrom / TicketPriority::tryFrom.
		Status:     lookupCode(models.StatusName, str(record, "status"), models.StatusOpen),
		Priority:   lookupCode(models.PriorityName, str(record, "priority"), models.PriorityMedium),
		TicketType: "question",
	}
	if metadata, ok := record["metadata"].(map[string]any); ok {
		t.Metadata = models.JSONText(jsonText(metadata))
	}
	if requester := s.ref(ctx, job, "contacts", record["requester_source_id"]); requester != "" {
		if strings.HasPrefix(requester, importContactPrefix) {
			id, _ := strconv.ParseInt(strings.TrimPrefix(requester, importContactPrefix), 10, 64)
			t.ContactID = &id
			var email string
			var name sql.NullString
			if s.DB.QueryRowContext(ctx, s.q("SELECT email, name FROM "+s.t("contacts")+" WHERE id = ?"), id).Scan(&email, &name) == nil {
				t.GuestEmail = &email
				if name.Valid {
					t.GuestName = &name.String
				}
			}
		} else {
			userType := s.UserType
			if userType == "" {
				userType = "User"
			}
			uid := models.UserID(requester)
			t.RequesterType, t.RequesterID = &userType, &uid
		}
	}
	if assignee := s.ref(ctx, job, "agents", record["assignee_source_id"]); assignee != "" {
		uid := models.UserID(assignee)
		t.AssignedTo = &uid
	}
	if department := s.ref(ctx, job, "departments", record["department_source_id"]); department != "" {
		id, _ := strconv.ParseInt(department, 10, 64)
		t.DepartmentID = &id
	}
	if err := s.Store.CreateTicket(ctx, t); err != nil {
		return "", err
	}
	if tags, ok := record["tag_source_ids"].([]any); ok {
		for _, sourceID := range tags {
			if tagID := s.ref(ctx, job, "tags", sourceID); tagID != "" {
				id, _ := strconv.ParseInt(tagID, 10, 64)
				if err := s.Store.AddTagToTicket(ctx, t.ID, id); err != nil {
					return "", err
				}
			}
		}
	}
	if err := s.importFieldValues(ctx, job, t.ID, record); err != nil {
		return "", err
	}
	if err := s.keepTimestamps(ctx, "tickets", t.ID, record); err != nil {
		return "", err
	}
	return strconv.FormatInt(t.ID, 10), nil
}

func (s *ImportService) persistReply(ctx context.Context, job *ImportJob, record map[string]any) (string, error) {
	ticket := s.ref(ctx, job, "tickets", record["ticket_source_id"])
	if ticket == "" {
		return "", errors.New("parent ticket not found for reply")
	}
	ticketID, _ := strconv.ParseInt(ticket, 10, 64)
	internal, _ := record["is_internal_note"].(bool)
	r := &models.Reply{TicketID: ticketID, Body: str(record, "body"), IsInternal: internal}
	author := s.ref(ctx, job, "agents", record["author_source_id"])
	if author == "" {
		author = s.ref(ctx, job, "contacts", record["author_source_id"])
	}
	if author != "" && !strings.HasPrefix(author, importContactPrefix) {
		userType := s.UserType
		if userType == "" {
			userType = "User"
		}
		uid := models.UserID(author)
		r.AuthorType, r.AuthorID = &userType, &uid
	}
	if err := s.Store.CreateReply(ctx, r); err != nil {
		return "", err
	}
	if err := s.keepTimestamps(ctx, "replies", r.ID, record); err != nil {
		return "", err
	}
	return strconv.FormatInt(r.ID, 10), nil
}

// keepTimestamps writes the source's created_at / updated_at over the ones
// set on insert.
func (s *ImportService) keepTimestamps(ctx context.Context, table string, id int64, record map[string]any) error {
	for _, column := range []string{"created_at", "updated_at"} {
		value := str(record, column)
		if value == "" {
			continue
		}
		at, err := time.Parse(time.RFC3339, value)
		if err != nil {
			continue
		}
		if _, err := s.DB.ExecContext(ctx, s.q("UPDATE "+s.t(table)+" SET "+column+" = ? WHERE id = ?"), at.UTC(), id); err != nil {
			return err
		}
	}
	return nil
}
