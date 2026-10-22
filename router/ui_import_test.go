package router_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	escalated "github.com/escalated-dev/escalated-go"
	"github.com/escalated-dev/escalated-go/internal/sqldialect"
	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/services"
	"github.com/escalated-dev/escalated-go/services/email"
)

// The admin Import wizard the shared frontend drives
// (src/pages/Admin/Import/{Index,Connect,Mapping,Review,Progress}.vue), on
// escalated-laravel's paths and props (Admin\ImportController,
// ImportService, ImportJob, ImportContext).
//
// The source is fakeImportAdapter, listed in Config.ImportAdapters the way a
// host lists a Zendesk or Freshdesk adapter.

const importBase = "/escalated/admin/import"
const fakeToken = "secret-token"

var fakeCredentials = map[string]string{"subdomain": "acme", "api_token": fakeToken}

type extractCall struct {
	entityType string
	cursor     string // "" for nil
	importing  bool
}

// fakeImportAdapter serves fixed records: two contacts (one a host user),
// one tag, two custom fields, three tickets two to a page, two replies, two
// attachments and a satisfaction rating. records, when set, replaces them.
type fakeImportAdapter struct {
	mu        sync.Mutex
	calls     []extractCall
	onExtract func(entityType string, cursor *string)
	fieldsCtx map[string]string
	records   map[string][]map[string]any
}

// A real PDF header and PNG signature, so the content checks see the files
// they claim to be.
var (
	fakePDF = []byte("%PDF-1.4\n1 0 obj << >> endobj\ntrailer << >>\n%%EOF\n")
	fakePNG = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x02\x00\x00\x00")
)

var fakeRecords = map[string][]map[string]any{
	"contacts": {
		{"source_id": "c1", "email": "ada@customer.test", "name": "Ada Customer"},
		{"source_id": "c2", "email": "bob@customer.test", "name": "Bob Customer"},
	},
	"tags": {{"source_id": "t1", "name": "Billing"}},
	"custom_fields": {
		{"source_id": "f1", "name": "Plan", "type": "select", "options": []any{"free", "pro"}},
		{"source_id": "f2", "name": "Seats", "type": "number"},
	},
	"tickets": {
		{"source_id": "k1", "title": "Refund please", "description": "Charged twice.", "status": "open", "priority": "high",
			"requester_source_id": "c1", "tag_source_ids": []any{"t1"}, "created_at": "2024-01-02T03:04:05Z",
			"custom_fields": map[string]any{"f1": "pro", "f2": 12.0}},
		{"source_id": "k2", "title": "Login broken", "status": "resolved", "priority": "urgent", "requester_source_id": "c2"},
		{"source_id": "k3", "title": "Feature idea", "status": "not-a-status", "requester_source_id": "c1"},
	},
	"replies": {
		{"source_id": "r1", "ticket_source_id": "k1", "body": "Looking into it", "author_source_id": "c1"},
		{"source_id": "r2", "ticket_source_id": "k2", "body": "Internal note", "is_internal_note": true},
	},
	"attachments": {
		{"source_id": "a1", "parent_type": "ticket", "parent_source_id": "k1", "filename": "receipt.pdf",
			"mime_type": "application/pdf", "content": base64.StdEncoding.EncodeToString(fakePDF)},
		{"source_id": "a2", "parent_source_id": "r1", "filename": "screen.png", "content": fakePNG},
	},
	"satisfaction_ratings": {
		{"source_id": "s1", "ticket_source_id": "k2", "rating": 4.0, "comment": "Quick fix", "created_at": "2024-02-03T04:05:06Z"},
	},
}

func (f *fakeImportAdapter) Name() string        { return "fakedesk" }
func (f *fakeImportAdapter) DisplayName() string { return "FakeDesk" }
func (f *fakeImportAdapter) CredentialFields() []map[string]any {
	return []map[string]any{
		{"name": "subdomain", "label": "Subdomain", "type": "text", "help": "acme"},
		{"name": "api_token", "label": "API token", "type": "password", "help": ""},
	}
}

func (f *fakeImportAdapter) TestConnection(_ context.Context, c map[string]string) (bool, error) {
	if c["subdomain"] == "down" {
		return false, errors.New("FakeDesk is unreachable")
	}
	return c["api_token"] == fakeToken, nil
}

func (f *fakeImportAdapter) EntityTypes() []string {
	return []string{"contacts", "tags", "custom_fields", "tickets", "replies", "attachments", "satisfaction_ratings"}
}

func (f *fakeImportAdapter) DefaultFieldMappings(entityType string) map[string]string {
	if entityType == "tickets" {
		return map[string]string{"subject": "title", "state": "status"}
	}
	return map[string]string{}
}

func (f *fakeImportAdapter) AvailableSourceFields(_ context.Context, entityType string, c map[string]string) ([]map[string]any, error) {
	f.mu.Lock()
	f.fieldsCtx = c
	f.mu.Unlock()
	if entityType != "tickets" {
		return []map[string]any{}, nil
	}
	return []map[string]any{
		{"name": "subject", "label": "Subject", "escalated_options": []string{"title", "description"}},
		{"name": "state", "label": "State", "escalated_options": []string{"status"}},
	}, nil
}

func (f *fakeImportAdapter) Extract(ctx context.Context, entityType string, c map[string]string, cursor *string) (*services.ExtractResult, error) {
	if c["api_token"] != fakeToken {
		return nil, errors.New("bad credentials")
	}
	call := extractCall{entityType: entityType, importing: services.IsImporting(ctx)}
	if cursor != nil {
		call.cursor = *cursor
	}
	f.mu.Lock()
	f.calls = append(f.calls, call)
	hook := f.onExtract
	f.mu.Unlock()
	if hook != nil {
		hook(entityType, cursor)
	}
	f.mu.Lock()
	all := f.records
	f.mu.Unlock()
	if all == nil {
		all = fakeRecords
	}
	records := all[entityType]
	offset := 0
	if cursor != nil {
		offset, _ = strconv.Atoi(*cursor)
	}
	end := offset + 2
	if end > len(records) {
		end = len(records)
	}
	total := len(records)
	res := &services.ExtractResult{Records: records[offset:end], TotalCount: &total}
	if end < len(records) {
		next := strconv.Itoa(end)
		res.Cursor = &next
	}
	return res, nil
}

func (f *fakeImportAdapter) takeCalls() []extractCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.calls
	f.calls = nil
	return out
}

func newImportFixture(t *testing.T, configure ...func(*escalated.Config)) (*uiFixture, *fakeImportAdapter) {
	t.Helper()
	fake := &fakeImportAdapter{}
	storage, err := email.NewLocalFileStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := newUIFixture(t, func(cfg *escalated.Config) {
		cfg.ImportAdapters = []services.ImportAdapter{fake}
		cfg.ImportAttachmentStorage = storage
		for _, c := range configure {
			c(cfg)
		}
		cfg.ImportSecret = "an import secret for the tests"
		cfg.UserIDByEmail = func(_ context.Context, email string) (models.UserID, error) {
			if email == "ada@customer.test" {
				return "501", nil
			}
			return "", nil
		}
	})
	return f, fake
}

type importClient struct {
	t       *testing.T
	h       http.Handler
	role    string
	cookies []*http.Cookie
}

func (c *importClient) do(method, path, body string) *httptest.ResponseRecorder {
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	r.Header.Set("X-Inertia", "true")
	r.Header.Set(headerUser, uiAgent)
	r.Header.Set(headerRole, c.role)
	for _, ck := range c.cookies {
		r.AddCookie(ck)
	}
	rec := httptest.NewRecorder()
	c.h.ServeHTTP(rec, r)
	for _, ck := range rec.Result().Cookies() {
		kept := c.cookies[:0]
		for _, old := range c.cookies {
			if old.Name != ck.Name {
				kept = append(kept, old)
			}
		}
		c.cookies = kept
		if ck.MaxAge >= 0 && ck.Value != "" {
			c.cookies = append(c.cookies, ck)
		}
	}
	return rec
}

func (c *importClient) page(path, component string, except ...string) map[string]any {
	c.t.Helper()
	rec := c.do(http.MethodGet, path, "")
	if rec.Code != http.StatusOK {
		c.t.Fatalf("GET %s: %d %s", path, rec.Code, rec.Body.String())
	}
	var page inertiaPage
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		c.t.Fatalf("GET %s: %v", path, err)
	}
	if page.Component != component {
		c.t.Fatalf("GET %s: component %q", path, page.Component)
	}
	assertDeclaredProps(c.t, component, page.Props, except...)
	return page.Props
}

// importDeclaredProps are the props each Import page declares, as
// @escalated-dev/escalated 0.12.3 publishes them in pages.json (this repo's
// testdata copy carries the page names only).
var importDeclaredProps = map[string][]string{
	"Escalated/Admin/Import/Index":    {"adapters", "jobs"},
	"Escalated/Admin/Import/Connect":  {"credential_fields", "display_name", "platform"},
	"Escalated/Admin/Import/Mapping":  {"credentials", "display_name", "entity_types", "mappings", "platform"},
	"Escalated/Admin/Import/Review":   {"entity_types", "field_mappings", "platform"},
	"Escalated/Admin/Import/Progress": {"job"},
}

// assertDeclaredProps checks the page receives every prop the component
// declares (less `except`) and none it does not.
func assertDeclaredProps(t *testing.T, component string, props map[string]any, except ...string) {
	t.Helper()
	shared := map[string]bool{"errors": true, "auth": true, "current_user": true, "flash": true, "escalated": true, "plugin_ui": true, "page": true}
	skip := map[string]bool{}
	for _, e := range except {
		skip[e] = true
	}
	var declared, sent []string
	for _, p := range importDeclaredProps[component] {
		if !skip[p] {
			declared = append(declared, p)
		}
	}
	for k := range props {
		if !shared[k] {
			sent = append(sent, k)
		}
	}
	sort.Strings(declared)
	sort.Strings(sent)
	if strings.Join(declared, ",") != strings.Join(sent, ",") {
		t.Errorf("%s props: sent %v, declared %v", component, sent, declared)
	}
}

func (c *importClient) connect(creds map[string]string) (int, map[string]any) {
	body, _ := json.Marshal(map[string]any{"credentials": creds})
	rec := c.do(http.MethodPost, importBase+"/fakedesk/test-connection", string(body))
	out := map[string]any{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func (c *importClient) start(f *uiFixture) *services.ImportJob {
	c.t.Helper()
	return c.startWith(f, `{"tickets":{"subject":"title","state":"status"}}`)
}

// startWith starts an import with the given field_mappings JSON.
func (c *importClient) startWith(f *uiFixture, fieldMappings string) *services.ImportJob {
	c.t.Helper()
	if code, body := c.connect(fakeCredentials); code != 200 || body["success"] != true {
		c.t.Fatalf("test-connection: %d %v", code, body)
	}
	rec := c.do(http.MethodPost, importBase+"/fakedesk/start", `{"field_mappings":`+fieldMappings+`}`)
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), importBase+"/progress/") {
		c.t.Fatalf("start: %d %q %s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	f.esc.Imports().Wait()
	id := strings.TrimPrefix(rec.Header().Get("Location"), importBase+"/progress/")
	job, err := f.esc.Imports().Get(context.Background(), id)
	if err != nil || job == nil {
		c.t.Fatalf("job %s: %v", id, err)
	}
	return job
}

func importedTickets(t *testing.T, f *uiFixture, jobID string) []*models.Ticket {
	t.Helper()
	db := f.esc.Config.DB
	rows, err := db.Query(sqldialect.Rebind(db, "SELECT escalated_id FROM escalated_import_source_maps WHERE import_job_id = ? AND entity_type = 'tickets' ORDER BY id"), jobID)
	if err != nil {
		t.Fatalf("maps: %v", err)
	}
	// Read every id before loading a ticket: the SQLite test database has
	// one connection, which the open rows hold.
	var ids []string
	for rows.Next() {
		var id string
		_ = rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	var out []*models.Ticket
	for _, id := range ids {
		n, _ := strconv.ParseInt(id, 10, 64)
		ticket, err := f.esc.Store.GetTicket(context.Background(), n)
		if err != nil || ticket == nil {
			t.Fatalf("ticket %s: %v", id, err)
		}
		out = append(out, ticket)
	}
	return out
}

func TestImportWizardPages(t *testing.T) {
	f, fake := newImportFixture(t)
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			c := &importClient{t: t, h: h, role: "admin"}

			index := c.page(importBase, "Escalated/Admin/Import/Index")
			adapters := list(t, index, "adapters")
			if len(adapters) != 1 || adapters[0].(map[string]any)["name"] != "fakedesk" || adapters[0].(map[string]any)["display_name"] != "FakeDesk" {
				t.Errorf("adapters %v", adapters)
			}
			hasKeys(t, "adapter", adapters[0].(map[string]any), "credential_fields")

			connect := c.page(importBase+"/fakedesk/connect", "Escalated/Admin/Import/Connect")
			if connect["platform"] != "fakedesk" || len(list(t, connect, "credential_fields")) != 2 {
				t.Errorf("connect %v", connect)
			}
			if rec := c.do(http.MethodGet, importBase+"/nowhere/connect", ""); rec.Code != http.StatusNotFound {
				t.Errorf("unknown platform: %d", rec.Code)
			}

			if code, body := c.connect(map[string]string{"subdomain": "acme", "api_token": "wrong"}); code != 200 || body["success"] != false || len(body) != 1 {
				t.Errorf("refused: %d %v", code, body)
			}
			if code, body := c.connect(map[string]string{"subdomain": "down"}); code != 422 || body["error"] != "FakeDesk is unreachable" {
				t.Errorf("raising: %d %v", code, body)
			}

			// Mapping wants a tested connection.
			fresh := &importClient{t: t, h: h, role: "admin"}
			if rec := fresh.do(http.MethodGet, importBase+"/fakedesk/mapping", ""); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != importBase+"/fakedesk/connect" {
				t.Errorf("mapping without credentials: %d %q", rec.Code, rec.Header().Get("Location"))
			}

			c.connect(fakeCredentials)
			mapping := c.page(importBase+"/fakedesk/mapping", "Escalated/Admin/Import/Mapping", "credentials")
			if fmt.Sprint(mapping["entity_types"]) != "[contacts tags custom_fields tickets replies attachments satisfaction_ratings]" {
				t.Errorf("entity types %v", mapping["entity_types"])
			}
			tickets := obj(t, obj(t, mapping, "mappings"), "tickets")
			if fmt.Sprint(tickets["defaults"]) != "map[state:status subject:title]" || len(list(t, tickets, "source_fields")) != 2 {
				t.Errorf("ticket mappings %v", tickets)
			}
			if fake.fieldsCtx["api_token"] != fakeToken {
				t.Errorf("source fields read with %v", fake.fieldsCtx)
			}
			if rec := c.do(http.MethodGet, importBase+"/fakedesk/mapping", ""); strings.Contains(rec.Body.String(), fakeToken) {
				t.Errorf("the page carries the credentials")
			}

			// Review: echo, nothing written.
			before := f.count(t, "import_jobs", "1 = 1")
			rec := c.do(http.MethodPost, importBase+"/fakedesk/review", `{"field_mappings":{"tickets":{"subject":"title"}},"entity_types":["tickets","replies"]}`)
			var review inertiaPage
			_ = json.Unmarshal(rec.Body.Bytes(), &review)
			if review.Component != "Escalated/Admin/Import/Review" || review.Props["platform"] != "fakedesk" ||
				fmt.Sprint(review.Props["entity_types"]) != "[tickets replies]" || fmt.Sprint(review.Props["field_mappings"]) != "map[tickets:map[subject:title]]" {
				t.Errorf("review %d %s", rec.Code, rec.Body.String())
			}
			assertDeclaredProps(t, "Escalated/Admin/Import/Review", review.Props)
			if f.count(t, "import_jobs", "1 = 1") != before {
				t.Errorf("review wrote a job")
			}

			// Start refuses without a tested connection.
			if rec := fresh.do(http.MethodPost, importBase+"/fakedesk/start", `{}`); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != importBase+"/fakedesk/connect" {
				t.Errorf("start without credentials: %d %q", rec.Code, rec.Header().Get("Location"))
			}
		})
	}
}

func TestImportRunsEndToEnd(t *testing.T) {
	f, fake := newImportFixture(t)
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			c := &importClient{t: t, h: h, role: "admin"}
			fake.takeCalls()

			job := c.start(f)

			if job.Status != "completed" || job.CompletedAt == nil || job.Credentials != nil || len(job.ErrorLog) != 0 {
				t.Fatalf("job %+v", job)
			}
			if job.UserID == nil || string(*job.UserID) != uiAgent {
				t.Errorf("user_id %v", job.UserID)
			}
			if fmt.Sprint(job.FieldMappings) != "map[tickets:map[state:status subject:title]]" {
				t.Errorf("field mappings %v", job.FieldMappings)
			}
			p := job.Progress["tickets"]
			if fmt.Sprintf("%v %v %v %v %v", p["total"], p["processed"], p["skipped"], p["failed"], p["cursor"]) != "3 3 0 0 2" {
				t.Errorf("progress %v", job.Progress)
			}

			tickets := importedTickets(t, f, job.ID)
			if len(tickets) != 3 {
				t.Fatalf("tickets %d", len(tickets))
			}
			var subjects, statuses, priorities []string
			for _, tk := range tickets {
				subjects = append(subjects, tk.Subject)
				statuses = append(statuses, tk.StatusString())
				priorities = append(priorities, tk.PriorityString())
				if tk.SLAPolicyID != nil {
					t.Errorf("imported ticket %d has an SLA policy", tk.ID)
				}
			}
			if fmt.Sprint(subjects, statuses, priorities) != "[Refund please Login broken Feature idea] [open resolved open] [high urgent medium]" {
				t.Errorf("tickets %v %v %v", subjects, statuses, priorities)
			}
			// Ada is a host user; Bob becomes an Escalated contact on a guest ticket.
			if tickets[0].RequesterID == nil || string(*tickets[0].RequesterID) != "501" {
				t.Errorf("requester %v", tickets[0].RequesterID)
			}
			if tickets[1].GuestEmail == nil || *tickets[1].GuestEmail != "bob@customer.test" || tickets[1].ContactID == nil {
				t.Errorf("guest ticket %+v", tickets[1])
			}
			if !tickets[0].CreatedAt.Equal(time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)) {
				t.Errorf("created_at %v", tickets[0].CreatedAt)
			}
			if n := f.count(t, "ticket_tags", "ticket_id = ?", tickets[0].ID); n != 1 {
				t.Errorf("tags %d", n)
			}
			if n := f.count(t, "replies", "ticket_id IN (?, ?)", tickets[0].ID, tickets[1].ID); n != 2 {
				t.Errorf("replies %d", n)
			}
			if n := f.count(t, "replies", "ticket_id = ? AND is_internal = ?", tickets[1].ID, true); n != 1 {
				t.Errorf("internal notes %d", n)
			}
			assertImportedRecords(t, f, h, job, tickets)

			// Import mode: in the run only.
			calls := fake.takeCalls()
			if len(calls) == 0 {
				t.Fatal("no extract calls")
			}
			for _, call := range calls {
				if !call.importing {
					t.Errorf("extract outside import mode: %+v", call)
				}
			}

			// The credentials are sealed at rest and gone from the cookie.
			var stored string
			db := f.esc.Config.DB
			_ = db.QueryRow(sqldialect.Rebind(db, "SELECT COALESCE(credentials, '') FROM escalated_import_jobs WHERE id = ?"), job.ID).Scan(&stored)
			if strings.Contains(stored, fakeToken) {
				t.Errorf("credentials stored in clear")
			}
			if rec := c.do(http.MethodGet, importBase+"/fakedesk/mapping", ""); rec.Code != http.StatusSeeOther {
				t.Errorf("credentials outlived the start: %d", rec.Code)
			}

			// Progress and the status poll.
			progress := c.page(importBase+"/progress/"+job.ID, "Escalated/Admin/Import/Progress")
			sent := obj(t, progress, "job")
			hasKeys(t, "job", sent, "id", "platform", "status", "progress", "error_log", "field_mappings", "created_at", "started_at", "completed_at")
			if _, has := sent["credentials"]; has || sent["id"] != job.ID || sent["status"] != "completed" {
				t.Errorf("progress job %v", sent)
			}
			rec := c.do(http.MethodGet, importBase+"/status/"+job.ID, "")
			var status map[string]any
			_ = json.Unmarshal(rec.Body.Bytes(), &status)
			keys := keysOf(status)
			sort.Strings(keys)
			if fmt.Sprint(keys) != "[completed_at error_log progress started_at status]" || status["status"] != "completed" {
				t.Errorf("status %v", status)
			}
			if rec := c.do(http.MethodGet, importBase+"/status/nope", ""); rec.Code != http.StatusNotFound {
				t.Errorf("unknown job: %d", rec.Code)
			}
		})
	}
}

func TestImportPausesResumesAndCancels(t *testing.T) {
	f, fake := newImportFixture(t)
	db := f.esc.Config.DB
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			c := &importClient{t: t, h: h, role: "admin"}
			fake.takeCalls()
			fake.onExtract = func(entityType string, cursor *string) {
				if entityType == "tickets" && cursor != nil && *cursor == "2" {
					_, _ = db.Exec(sqldialect.Rebind(db, "UPDATE escalated_import_jobs SET status = 'paused' WHERE status = 'importing'"))
				}
			}

			job := c.start(f)
			if job.Status != "paused" || len(importedTickets(t, f, job.ID)) != 3 || job.Credentials == nil {
				t.Fatalf("paused job %+v", job)
			}
			if _, has := job.Progress["replies"]; has {
				t.Errorf("replies imported before the pause")
			}

			fake.onExtract = nil
			fake.takeCalls()
			rec := c.do(http.MethodPost, importBase+"/resume/"+job.ID, `{}`)
			f.esc.Imports().Wait()
			wantRedirect(t, rec, "resume", importBase+"/progress/"+job.ID)
			job, _ = f.esc.Imports().Get(context.Background(), job.ID)
			if job.Status != "completed" || len(importedTickets(t, f, job.ID)) != 3 {
				t.Fatalf("resumed job %+v", job)
			}
			// The run picked tickets up from its saved cursor, not from the start.
			calls := fake.takeCalls()
			var fromStart, fromCursor bool
			for _, call := range calls {
				if call.entityType == "tickets" {
					fromStart = fromStart || call.cursor == ""
					fromCursor = fromCursor || call.cursor == "2"
				}
			}
			if fromStart || !fromCursor {
				t.Errorf("resume calls %+v", calls)
			}

			// Pause an importing job; cancel drops the credentials.
			running := &services.ImportJob{Platform: "fakedesk", Status: "importing"}
			if err := f.esc.Imports().Create(context.Background(), running, fakeCredentials); err != nil {
				t.Fatal(err)
			}
			wantRedirect(t, c.do(http.MethodPost, importBase+"/pause/"+running.ID, `{}`), "pause", importBase+"/progress/"+running.ID)
			if got, _ := f.esc.Imports().Get(context.Background(), running.ID); got.Status != "paused" {
				t.Errorf("pause: %s", got.Status)
			}
			wantRedirect(t, c.do(http.MethodPost, importBase+"/cancel/"+running.ID, `{}`), "cancel", importBase+"/progress/"+running.ID)
			if got, _ := f.esc.Imports().Get(context.Background(), running.ID); got.Status != "failed" || got.CompletedAt == nil || got.Credentials != nil {
				t.Errorf("cancel: %+v", got)
			}

			// A failed job resumes through mapping.
			failed := &services.ImportJob{Platform: "fakedesk", Status: "failed"}
			if err := f.esc.Imports().Create(context.Background(), failed, fakeCredentials); err != nil {
				t.Fatal(err)
			}
			c.do(http.MethodPost, importBase+"/resume/"+failed.ID, `{}`)
			f.esc.Imports().Wait()
			if got, _ := f.esc.Imports().Get(context.Background(), failed.ID); got.Status != "completed" {
				t.Errorf("failed resume: %s", got.Status)
			}
		})
	}
}

func TestImportWizardIsAdminOnly(t *testing.T) {
	f, _ := newImportFixture(t)
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			c := &importClient{t: t, h: h, role: "agent"}
			if rec := c.do(http.MethodGet, importBase, ""); rec.Code == http.StatusOK {
				t.Errorf("an agent reached the import wizard")
			}
		})
	}
}

func TestImportModeHoldsBackSLA(t *testing.T) {
	f, _ := newImportFixture(t)
	ctx := services.WithImportMode(context.Background())
	sla := services.NewSLAService(f.esc.Store)
	if n, err := sla.CheckBreaches(ctx); err != nil || n != 0 {
		t.Errorf("breaches %d %v", n, err)
	}
	f.ticket.SLAPolicyID = nil
	if err := f.esc.Store.UpdateTicket(context.Background(), f.ticket); err != nil {
		t.Fatal(err)
	}
	if err := sla.ApplyDefaultPolicy(ctx, f.ticket.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := f.esc.Store.GetTicket(context.Background(), f.ticket.ID)
	if got.SLAPolicyID != nil {
		t.Errorf("import mode attached SLA policy %d", *got.SLAPolicyID)
	}
	if services.IsImporting(context.Background()) {
		t.Error("import mode leaked")
	}
}
