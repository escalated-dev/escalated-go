package services

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/escalated-dev/escalated-go/models"
)

// ImportAttachmentStorage stores the bytes of an imported attachment and
// returns the path the attachment download route reads them from. It has the
// method set of email.AttachmentStorage, so email.LocalFileStorage (rooted
// outside anything the web server serves) or the host's own storage fits.
type ImportAttachmentStorage interface {
	Put(ctx context.Context, filename string, content io.Reader, contentType string) (path string, err error)
}

// DefaultImportAttachmentMaxBytes is Laravel's live-upload limit
// (escalated.tickets.max_attachment_size_kb, 10240 KB).
const DefaultImportAttachmentMaxBytes int64 = 10240 * 1024

// importAttachmentTypes are the extensions Laravel's live uploads accept
// (CreateTicketRequest / ReplyToTicketRequest `mimes:`), with the type each
// is stored as. The type is taken from the extension, never from the
// source's claim.
var importAttachmentTypes = map[string]string{
	"jpg":  "image/jpeg",
	"jpeg": "image/jpeg",
	"png":  "image/png",
	"gif":  "image/gif",
	"webp": "image/webp",
	"pdf":  "application/pdf",
	"doc":  "application/msword",
	"docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	"xls":  "application/vnd.ms-excel",
	"xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	"csv":  "text/csv",
	"txt":  "text/plain",
	"zip":  "application/zip",
	"mp4":  "video/mp4",
	"mp3":  "audio/mpeg",
}

// logImportError appends one entry to the job's error log, capped as
// Laravel's appendError is.
func logImportError(job *ImportJob, entityType, sourceID, message string) {
	if len(job.ErrorLog) >= importMaxErrors {
		return
	}
	job.ErrorLog = append(job.ErrorLog, map[string]any{
		"entity_type": entityType, "source_id": sourceID, "error": message,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *ImportService) customFields() *CustomFieldService {
	return &CustomFieldService{DB: s.DB, Prefix: s.Prefix}
}

// stringList reads a JSON array of strings.
func stringList(v any) []string {
	var out []string
	switch list := v.(type) {
	case []string:
		out = append(out, list...)
	case []any:
		for _, item := range list {
			if str, ok := item.(string); ok && strings.TrimSpace(str) != "" {
				out = append(out, strings.TrimSpace(str))
			}
		}
	}
	return out
}

// persistCustomField is Laravel's persistCustomField: the field definition is
// found by its slug or created with the source's name, type and options.
// Unlike Laravel, the type must be one the custom field admin accepts, and a
// select needs options, as the admin form requires.
func (s *ImportService) persistCustomField(ctx context.Context, record map[string]any) (string, error) {
	name := str(record, "name")
	if name == "" {
		return "", errors.New("custom field has no name")
	}
	slug := FieldSlug(name)
	var id int64
	err := s.DB.QueryRowContext(ctx, s.q("SELECT id FROM "+s.t("custom_fields")+" WHERE slug = ?"), slug).Scan(&id)
	if err == nil {
		return strconv.FormatInt(id, 10), nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	fieldType := str(record, "type")
	if fieldType == "" {
		fieldType = models.FieldTypeText
	}
	if !slices.Contains(models.TicketFieldTypes, fieldType) {
		return "", fmt.Errorf("unknown custom field type '%s'", fieldType)
	}
	options := stringList(record["options"])
	if (fieldType == models.FieldTypeSelect || fieldType == models.FieldTypeMultiSelect) && len(options) == 0 {
		return "", fmt.Errorf("a %s custom field needs at least one option", fieldType)
	}
	f := &models.CustomField{
		Name: name, Slug: slug, FieldType: fieldType, Options: options,
		EntityType: TicketEntity, IsActive: true,
	}
	if err := s.customFields().CreateField(ctx, f); err != nil {
		return "", err
	}
	return strconv.FormatInt(f.ID, 10), nil
}

// fieldSlugFor finds the custom field a ticket record's custom_fields key
// names. The wizard's field mapping for tickets decides first: a key mapped
// to "custom_fields.<slug>" lands on that field, a key mapped to "" (Skip
// field) is dropped. Otherwise the key is a custom field's source id from
// this import, or a field slug.
func (s *ImportService) fieldSlugFor(ctx context.Context, job *ImportJob, mappings map[string]any, key string) (slug string, skip bool, problem string) {
	if target, mapped := mappings[key]; mapped {
		t, _ := target.(string)
		t = strings.TrimSpace(t)
		if t == "" {
			return "", true, ""
		}
		if strings.HasPrefix(t, "custom_fields.") {
			return strings.TrimPrefix(t, "custom_fields."), false, ""
		}
		return "", false, fmt.Sprintf("%s: mapped to '%s', which is not a custom field", ErrorKey(key), t)
	}
	if id := s.ref(ctx, job, "custom_fields", key); id != "" {
		var found string
		if s.DB.QueryRowContext(ctx, s.q("SELECT slug FROM "+s.t("custom_fields")+" WHERE id = ?"), id).Scan(&found) == nil {
			return found, false, ""
		}
	}
	return key, false, ""
}

// importFieldValues stores a ticket record's custom_fields values. Each value
// is checked as a ticket update checks it (active ticket fields only, by
// type and options); one that fails is left out and logged under
// custom_field_values with the ticket's source id, and the ticket is kept.
// Fields the record does not carry are not required: imported history
// predates them.
func (s *ImportService) importFieldValues(ctx context.Context, job *ImportJob, ticketID int64, record map[string]any) error {
	raw, ok := record["custom_fields"].(map[string]any)
	if !ok || len(raw) == 0 {
		return nil
	}
	mappings, _ := job.FieldMappings["tickets"].(map[string]any)
	sourceID := strings.TrimSpace(fmt.Sprint(record["source_id"]))
	keys := make([]string, 0, len(raw))
	for key := range raw {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	cf := s.customFields()
	values := FieldValues{}
	var problems []string
	for _, key := range keys {
		slug, skip, problem := s.fieldSlugFor(ctx, job, mappings, key)
		if skip {
			continue
		}
		if problem != "" {
			problems = append(problems, problem)
			continue
		}
		valid, errs, err := cf.ValidateTicketValues(ctx, map[string]any{slug: raw[key]}, false)
		if err != nil {
			return err
		}
		if msg := errs[ErrorKey(slug)]; msg != "" {
			problems = append(problems, ErrorKey(slug)+": "+msg)
			continue
		}
		for id, v := range valid {
			values[id] = v
		}
	}
	if err := cf.SaveTicketValues(ctx, ticketID, values); err != nil {
		return err
	}
	for _, problem := range problems {
		logImportError(job, "custom_field_values", sourceID, problem)
	}
	return nil
}

// importFilename keeps the base name of a source filename, without control
// characters, as the inbound-mail downloader does.
func importFilename(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	name = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, name))
	if name == "" || name == "." || name == ".." {
		return "attachment"
	}
	if len(name) > 255 {
		ext := filepath.Ext(name)
		if len(ext) > 16 {
			ext = ""
		}
		name = name[:255-len(ext)] + ext
	}
	return name
}

// attachmentContent reads a record's content: bytes, or a base64 string.
func attachmentContent(v any) ([]byte, error) {
	switch c := v.(type) {
	case nil:
		return nil, nil
	case []byte:
		return c, nil
	case string:
		if c == "" {
			return nil, nil
		}
		b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(c))
		if err != nil {
			return nil, errors.New("attachment content is not valid base64")
		}
		return b, nil
	}
	return nil, fmt.Errorf("attachment content must be bytes or a base64 string, not %T", v)
}

// persistAttachment is Laravel's persistAttachment (parent_type ticket or
// reply, default reply; parent_source_id), with the rules of a live upload:
// the record carries the file's bytes (content), which are checked against
// the upload limit and allowed types and written through the attachment
// storage. A storage path from the source is never trusted.
func (s *ImportService) persistAttachment(ctx context.Context, job *ImportJob, record map[string]any) (string, error) {
	parentType := str(record, "parent_type")
	if parentType == "" {
		parentType = "reply"
	}
	a := &models.Attachment{}
	switch parentType {
	case "ticket":
		id := s.ref(ctx, job, "tickets", record["parent_source_id"])
		if id == "" {
			return "", errors.New("parent ticket not found for attachment")
		}
		a.TicketID, _ = strconv.ParseInt(id, 10, 64)
	case "reply":
		id := s.ref(ctx, job, "replies", record["parent_source_id"])
		if id == "" {
			return "", errors.New("parent reply not found for attachment")
		}
		replyID, _ := strconv.ParseInt(id, 10, 64)
		if err := s.DB.QueryRowContext(ctx, s.q("SELECT ticket_id FROM "+s.t("replies")+" WHERE id = ?"), replyID).Scan(&a.TicketID); err != nil {
			return "", errors.New("parent reply not found for attachment")
		}
		a.ReplyID = &replyID
	default:
		return "", fmt.Errorf("unknown attachment parent type '%s'", parentType)
	}
	if s.AttachmentStorage == nil {
		return "", errors.New("no attachment storage is configured (Config.ImportAttachmentStorage)")
	}
	content, err := attachmentContent(record["content"])
	if err != nil {
		return "", err
	}
	if len(content) == 0 {
		return "", errors.New("attachment has no content: imports store the file's bytes, never a source path")
	}
	limit := s.AttachmentMaxBytes
	if limit <= 0 {
		limit = DefaultImportAttachmentMaxBytes
	}
	if int64(len(content)) > limit {
		return "", fmt.Errorf("attachment exceeds the %d KB upload limit", int64(math.Ceil(float64(limit)/1024)))
	}
	name := importFilename(str(record, "filename"))
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
	mimeType, allowed := importAttachmentTypes[ext]
	if !allowed {
		return "", fmt.Errorf("attachment type '%s' is not allowed", ext)
	}
	// As Laravel's mimes rule reads the content, markup is refused whatever
	// the file is called.
	if sniffed := http.DetectContentType(content); strings.HasPrefix(sniffed, "text/html") || strings.HasPrefix(sniffed, "text/xml") {
		return "", fmt.Errorf("attachment content (%s) is not allowed", strings.Split(sniffed, ";")[0])
	}
	path, err := s.AttachmentStorage.Put(ctx, name, bytes.NewReader(content), mimeType)
	if err != nil {
		return "", fmt.Errorf("store attachment: %w", err)
	}
	a.OriginalFilename, a.MimeType, a.Size, a.StoragePath = name, mimeType, int64(len(content)), path
	a.CreatedAt = time.Now().UTC()
	if at, err := time.Parse(time.RFC3339, str(record, "created_at")); err == nil {
		a.CreatedAt = at.UTC()
	}
	if err := s.Store.CreateAttachment(ctx, a); err != nil {
		return "", err
	}
	return strconv.FormatInt(a.ID, 10), nil
}

// ratingOf reads a 1-5 integer rating.
func ratingOf(v any) (int, bool) {
	var n float64
	switch r := v.(type) {
	case float64:
		n = r
	case int:
		n = float64(r)
	case int64:
		n = float64(r)
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(r))
		if err != nil {
			return 0, false
		}
		n = float64(i)
	default:
		return 0, false
	}
	if n != math.Trunc(n) || n < 1 || n > 5 {
		return 0, false
	}
	return int(n), true
}

// persistSatisfactionRating is Laravel's persistSatisfactionRating: the
// rating (or score) and comment, on the imported ticket, at the source's
// time. It writes the row only: no rater, no status rule, nothing a live
// rating would set off. A ticket keeps one rating, as live ratings do.
func (s *ImportService) persistSatisfactionRating(ctx context.Context, job *ImportJob, record map[string]any) (string, error) {
	ticket := s.ref(ctx, job, "tickets", record["ticket_source_id"])
	if ticket == "" {
		return "", errors.New("ticket not found for satisfaction rating")
	}
	ticketID, _ := strconv.ParseInt(ticket, 10, 64)
	raw, present := record["rating"]
	if !present || raw == nil {
		raw = record["score"]
	}
	rating, ok := ratingOf(raw)
	if !ok {
		return "", errors.New("rating must be an integer between 1 and 5")
	}
	var existing int
	if err := s.DB.QueryRowContext(ctx, s.q("SELECT COUNT(1) FROM "+s.t("satisfaction_ratings")+" WHERE ticket_id = ?"), ticketID).Scan(&existing); err != nil {
		return "", err
	}
	if existing > 0 {
		return "", errors.New("the ticket already has a satisfaction rating")
	}
	var comment *string
	if c := str(record, "comment"); c != "" {
		comment = &c
	}
	createdAt := time.Now().UTC()
	if at, err := time.Parse(time.RFC3339, str(record, "created_at")); err == nil {
		createdAt = at.UTC()
	}
	var id int64
	if err := s.DB.QueryRowContext(ctx, s.q("INSERT INTO "+s.t("satisfaction_ratings")+" (ticket_id, rating, comment, created_at) VALUES (?, ?, ?, ?) RETURNING id"),
		ticketID, rating, comment, createdAt).Scan(&id); err != nil {
		return "", err
	}
	return strconv.FormatInt(id, 10), nil
}
