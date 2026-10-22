package router_test

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	escalated "github.com/escalated-dev/escalated-go"
	"github.com/escalated-dev/escalated-go/internal/sqldialect"
	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/services"
)

// Attachments, custom fields (definitions and ticket values) and
// satisfaction ratings, imported through the wizard and the fake adapter, as
// escalated-laravel's ImportService persists them, with live-upload rules for
// attachment bytes and the custom field rules for values.

func sourceMapped(t *testing.T, f *uiFixture, jobID, entityType, sourceID string) string {
	t.Helper()
	db := f.esc.Config.DB
	var id string
	_ = db.QueryRow(sqldialect.Rebind(db, "SELECT escalated_id FROM escalated_import_source_maps WHERE import_job_id = ? AND entity_type = ? AND source_id = ?"),
		jobID, entityType, sourceID).Scan(&id)
	return id
}

func ticketFieldValues(t *testing.T, f *uiFixture, ticketID int64) map[string]string {
	t.Helper()
	db := f.esc.Config.DB
	rows, err := db.Query(sqldialect.Rebind(db, "SELECT cf.slug, v.value FROM escalated_custom_field_values v JOIN escalated_custom_fields cf ON cf.id = v.custom_field_id WHERE v.entity_type = 'ticket' AND v.entity_id = ?"), ticketID)
	if err != nil {
		t.Fatalf("values: %v", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var slug, value string
		_ = rows.Scan(&slug, &value)
		out[slug] = value
	}
	return out
}

type importedAttachment struct {
	ticketID int64
	replyID  *int64
	name     string
	mime     string
	size     int64
	path     string
}

func attachmentRow(t *testing.T, f *uiFixture, id string) importedAttachment {
	t.Helper()
	db := f.esc.Config.DB
	var a importedAttachment
	err := db.QueryRow(sqldialect.Rebind(db, "SELECT ticket_id, reply_id, original_filename, mime_type, size, storage_path FROM escalated_attachments WHERE id = ?"), id).
		Scan(&a.ticketID, &a.replyID, &a.name, &a.mime, &a.size, &a.path)
	if err != nil {
		t.Fatalf("attachment %s: %v", id, err)
	}
	return a
}

// errorsByType collects the job's error log as entity type -> source id ->
// message.
func errorsByType(job *services.ImportJob) map[string]map[string]string {
	out := map[string]map[string]string{}
	for _, e := range job.ErrorLog {
		et, _ := e["entity_type"].(string)
		if out[et] == nil {
			out[et] = map[string]string{}
		}
		sid := fmt.Sprint(e["source_id"])
		out[et][sid] = strings.TrimPrefix(out[et][sid]+"; "+fmt.Sprint(e["error"]), "; ")
	}
	return out
}

func progressOf(job *services.ImportJob, entityType string) string {
	p := job.Progress[entityType]
	return fmt.Sprintf("%v %v", p["processed"], p["failed"])
}

// assertImportedRecords checks what the default fake records leave behind.
func assertImportedRecords(t *testing.T, f *uiFixture, h http.Handler, job *services.ImportJob, tickets []*models.Ticket) {
	t.Helper()
	db := f.esc.Config.DB

	// Field definitions, as Laravel's persistCustomField: found or created by
	// slug, with the source's name, type and options.
	for _, sid := range []string{"f1", "f2"} {
		if sourceMapped(t, f, job.ID, "custom_fields", sid) == "" {
			t.Errorf("custom field %s not mapped", sid)
		}
	}
	var fieldType, options string
	if err := db.QueryRow(sqldialect.Rebind(db, "SELECT field_type, COALESCE(options, '') FROM escalated_custom_fields WHERE slug = 'plan'")).Scan(&fieldType, &options); err != nil {
		t.Fatalf("plan field: %v", err)
	}
	if fieldType != "select" || !strings.Contains(options, "free") || !strings.Contains(options, "pro") {
		t.Errorf("plan field %s %s", fieldType, options)
	}
	if n := f.count(t, "custom_fields", "slug IN ('plan', 'seats')"); n != 2 {
		t.Errorf("fields created %d (a second import must reuse them)", n)
	}

	// Ticket values, keyed by the field's source id.
	if got := ticketFieldValues(t, f, tickets[0].ID); fmt.Sprint(got) != "map[plan:pro seats:12]" {
		t.Errorf("ticket values %v", got)
	}

	// Attachments: bytes stored through the attachment storage, on the ticket
	// and on a reply.
	a1 := attachmentRow(t, f, sourceMapped(t, f, job.ID, "attachments", "a1"))
	if a1.ticketID != tickets[0].ID || a1.replyID != nil || a1.name != "receipt.pdf" || a1.mime != "application/pdf" || a1.size != int64(len(fakePDF)) {
		t.Errorf("ticket attachment %+v", a1)
	}
	if b, err := os.ReadFile(a1.path); err != nil || !bytes.Equal(b, fakePDF) {
		t.Errorf("stored bytes %q %v", b, err)
	}
	// Served only through the authorised download route.
	download := "/escalated/attachments/" + sourceMapped(t, f, job.ID, "attachments", "a1") + "/download"
	r := httptest.NewRequest(http.MethodGet, download, nil)
	r.Header.Set(headerUser, uiAgent)
	r.Header.Set(headerRole, "agent")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), fakePDF) || rec.Header().Get("Content-Type") != "application/octet-stream" {
		t.Errorf("agent download: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, download, nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous download: %d", rec.Code)
	}
	a2 := attachmentRow(t, f, sourceMapped(t, f, job.ID, "attachments", "a2"))
	replyID := sourceMapped(t, f, job.ID, "replies", "r1")
	if a2.replyID == nil || strconv.FormatInt(*a2.replyID, 10) != replyID || a2.ticketID != tickets[0].ID || a2.mime != "image/png" {
		t.Errorf("reply attachment %+v (reply %s)", a2, replyID)
	}

	// The rating, on its ticket, with the source's time and no rater.
	var rating int
	var comment string
	var ratedBy *string
	var createdAt time.Time
	if err := db.QueryRow(sqldialect.Rebind(db, "SELECT rating, comment, rated_by_id, created_at FROM escalated_satisfaction_ratings WHERE ticket_id = ?"), tickets[1].ID).
		Scan(&rating, &comment, &ratedBy, &createdAt); err != nil {
		t.Fatalf("rating: %v", err)
	}
	if rating != 4 || comment != "Quick fix" || ratedBy != nil || !createdAt.Equal(time.Date(2024, 2, 3, 4, 5, 6, 0, time.UTC)) {
		t.Errorf("rating %d %q %v %v", rating, comment, ratedBy, createdAt)
	}
	for _, et := range []string{"custom_fields", "attachments", "satisfaction_ratings"} {
		if p := job.Progress[et]; fmt.Sprint(p["failed"]) != "0" {
			t.Errorf("%s progress %v", et, p)
		}
	}
}

func TestImportRecordsAreChecked(t *testing.T) {
	f, fake := newImportFixture(t, func(cfg *escalated.Config) {
		cfg.ImportAttachmentMaxBytes = 1024
	})
	pdf := func(extra int) string {
		return base64.StdEncoding.EncodeToString(append(append([]byte{}, fakePDF...), bytes.Repeat([]byte("x"), extra)...))
	}
	fake.records = map[string][]map[string]any{
		"contacts": {{"source_id": "c1", "email": "ada@customer.test"}},
		"custom_fields": {
			{"source_id": "f1", "name": "Plan", "type": "select", "options": []any{"free", "pro"}},
			{"source_id": "f2", "name": "Seats", "type": "number"},
			{"source_id": "f8", "name": "Hologram", "type": "hologram"},
			{"source_id": "f9", "name": "Broken", "type": "select"},
		},
		"tickets": {
			{"source_id": "k1", "title": "One", "requester_source_id": "c1",
				"custom_fields": map[string]any{"plan_code": "pro", "f2": "many", "skipped_src": "x", "nope": "y"}},
			{"source_id": "k2", "title": "Two"},
		},
		"replies": {{"source_id": "r1", "ticket_source_id": "k1", "body": "Hi"}},
		"attachments": {
			{"source_id": "a1", "parent_type": "ticket", "parent_source_id": "k1", "filename": "x.pdf", "path": "/etc/passwd"},
			{"source_id": "a2", "parent_type": "ticket", "parent_source_id": "k1", "filename": "big.pdf", "content": pdf(2000)},
			{"source_id": "a3", "parent_type": "ticket", "parent_source_id": "k1", "filename": "run.exe", "content": base64.StdEncoding.EncodeToString([]byte("MZ\x90\x00"))},
			{"source_id": "a4", "parent_type": "ticket", "parent_source_id": "k1", "filename": "notes.txt", "content": base64.StdEncoding.EncodeToString([]byte("<html><script>alert(1)</script></html>"))},
			{"source_id": "a5", "parent_type": "ticket", "parent_source_id": "zz", "filename": "y.pdf", "content": pdf(0)},
			{"source_id": "a6", "parent_type": "ticket", "parent_source_id": "k1", "filename": "../../evil.pdf", "content": pdf(0)},
			{"source_id": "a7", "parent_type": "ticket", "parent_source_id": "k1", "filename": "z.pdf", "content": "!!not base64!!"},
		},
		"satisfaction_ratings": {
			{"source_id": "s1", "ticket_source_id": "k1", "rating": 9.0},
			{"source_id": "s2", "ticket_source_id": "k1", "rating": "x"},
			{"source_id": "s3", "ticket_source_id": "zz", "rating": 3.0},
			{"source_id": "s4", "ticket_source_id": "k2", "score": 5.0},
			{"source_id": "s5", "ticket_source_id": "k2", "rating": 3.0},
			{"source_id": "s6", "ticket_source_id": "k1", "rating": 2.5},
		},
	}
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			c := &importClient{t: t, h: h, role: "admin"}
			job := c.startWith(f, `{"tickets":{"plan_code":"custom_fields.plan","skipped_src":""}}`)
			if job.Status != "completed" {
				t.Fatalf("job %+v", job)
			}
			errs := errorsByType(job)

			// Definitions: an unknown type and an option-less select fail.
			if progressOf(job, "custom_fields") != "2 2" || errs["custom_fields"]["f8"] == "" || errs["custom_fields"]["f9"] == "" {
				t.Errorf("custom fields %v %v", job.Progress["custom_fields"], errs["custom_fields"])
			}

			// Values: the mapped source field lands on its field, a skipped one
			// is ignored, an invalid or unknown one is logged, and the ticket
			// is imported regardless.
			if progressOf(job, "tickets") != "2 0" {
				t.Errorf("tickets %v", job.Progress["tickets"])
			}
			k1, _ := strconv.ParseInt(sourceMapped(t, f, job.ID, "tickets", "k1"), 10, 64)
			if got := ticketFieldValues(t, f, k1); fmt.Sprint(got) != "map[plan:pro]" {
				t.Errorf("values %v", got)
			}
			valueErrs := errs["custom_field_values"]["k1"]
			if !strings.Contains(valueErrs, "custom_fields.seats") || !strings.Contains(valueErrs, "nope") || strings.Contains(valueErrs, "skipped_src") {
				t.Errorf("value errors %q", valueErrs)
			}

			// Attachments: only the well-formed one under the limit is stored,
			// under its base name, inside the storage root.
			if progressOf(job, "attachments") != "1 6" {
				t.Errorf("attachments %v %v", job.Progress["attachments"], errs["attachments"])
			}
			for sid, want := range map[string]string{
				"a1": "content", "a2": "exceeds", "a3": "not allowed", "a4": "not allowed",
				"a5": "parent ticket not found", "a7": "base64",
			} {
				if !strings.Contains(errs["attachments"][sid], want) {
					t.Errorf("attachment %s: %q, want %q", sid, errs["attachments"][sid], want)
				}
			}
			a6 := attachmentRow(t, f, sourceMapped(t, f, job.ID, "attachments", "a6"))
			if a6.name != "evil.pdf" || strings.Contains(a6.path, "..") || !strings.HasSuffix(filepath.Base(a6.path), "-evil.pdf") {
				t.Errorf("a6 %+v", a6)
			}
			if b, err := os.ReadFile(a6.path); err != nil || !bytes.HasPrefix(b, []byte("%PDF")) {
				t.Errorf("a6 bytes %v", err)
			}

			// Ratings: 1-5 integers on an imported ticket, one per ticket.
			if progressOf(job, "satisfaction_ratings") != "1 5" {
				t.Errorf("ratings %v %v", job.Progress["satisfaction_ratings"], errs["satisfaction_ratings"])
			}
			for _, sid := range []string{"s1", "s2", "s3", "s5", "s6"} {
				if errs["satisfaction_ratings"][sid] == "" {
					t.Errorf("rating %s accepted", sid)
				}
			}
			k2, _ := strconv.ParseInt(sourceMapped(t, f, job.ID, "tickets", "k2"), 10, 64)
			if n := f.count(t, "satisfaction_ratings", "ticket_id = ? AND rating = 5", k2); n != 1 {
				t.Errorf("score rating %d", n)
			}
			if n := f.count(t, "satisfaction_ratings", "ticket_id = ?", k1); n != 0 {
				t.Errorf("bad ratings stored %d", n)
			}
		})
	}
}

func TestImportAttachmentsNeedStorage(t *testing.T) {
	f, _ := newImportFixture(t, func(cfg *escalated.Config) {
		cfg.ImportAttachmentStorage = nil
	})
	h := f.mounts()
	names := make([]string, 0, len(h))
	for name := range h {
		names = append(names, name)
	}
	sort.Strings(names)
	c := &importClient{t: t, h: h[names[0]], role: "admin"}
	before := f.count(t, "attachments", "1 = 1")
	job := c.start(f)
	if job.Status != "completed" || progressOf(job, "attachments") != "0 2" {
		t.Fatalf("job %+v", job)
	}
	if msg := errorsByType(job)["attachments"]["a1"]; !strings.Contains(msg, "attachment storage") {
		t.Errorf("error %q", msg)
	}
	if n := f.count(t, "attachments", "1 = 1"); n != before {
		t.Errorf("attachments stored without storage: %d", n-before)
	}
}
