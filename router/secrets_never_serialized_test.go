package router_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	escalated "github.com/escalated-dev/escalated-go"
	"github.com/escalated-dev/escalated-go/internal/sqldialect"
	"github.com/escalated-dev/escalated-go/services"
)

// Stored secrets never reach the browser (escalated-laravel #258).
//
// Import source credentials and webhook signing secrets are kept server-side
// so the package can use them. None of them is shown back to an admin: every
// page and JSON response that carries one of these records is checked here,
// through both routers, for the raw secret anywhere in the body.
//
// This package has no SSO settings, no inbound-email secrets on a settings
// page (they are host config), no API token store and no two-factor page or
// JSON surface; the models that hold secrets are checked in their packages.

const (
	leakWebhookSecret = "whsec-4c8e1a7b9d3f"
	leakImportKey     = "imp-key-7f3a9c2e5b1d"
	leakImportPass    = "imp-pass-q8w7e6r5t4"
)

func assertNoSecrets(t *testing.T, what string, rec *httptest.ResponseRecorder, secrets ...string) {
	t.Helper()
	body := rec.Body.String()
	for _, s := range secrets {
		if s != "" && strings.Contains(body, s) {
			t.Errorf("%s: the response contains the secret %q: %s", what, s, body)
		}
	}
}

func decodeJSON(t *testing.T, what string, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	out := map[string]any{}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("%s: %d not JSON: %v: %s", what, rec.Code, err, rec.Body.String())
	}
	return out
}

func inertiaGet(t *testing.T, h http.Handler, path string) (*httptest.ResponseRecorder, inertiaPage) {
	t.Helper()
	rec := send(h, http.MethodGet, path, uiAgent, "admin", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: %d %s", path, rec.Code, rec.Body.String())
	}
	var page inertiaPage
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("GET %s: not an Inertia page: %v", path, err)
	}
	return rec, page
}

func storedWebhookSecret(t *testing.T, f *uiFixture, id int64) *string {
	t.Helper()
	db := f.esc.Config.DB
	var secret *string
	if err := db.QueryRow(sqldialect.Rebind(db, `SELECT secret FROM escalated_webhooks WHERE id = ?`), id).Scan(&secret); err != nil {
		t.Fatalf("webhook %d: %v", id, err)
	}
	return secret
}

func createWebhookWithSecret(t *testing.T, f *uiFixture, h http.Handler, url, secret string) int64 {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"url": url, "events": []string{"ticket.created"}, "secret": secret, "active": true})
	rec := sendAPI(h, http.MethodPost, "/escalated/admin/webhooks", uiAgent, "admin", string(body))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create webhook: %d %s", rec.Code, rec.Body.String())
	}
	db := f.esc.Config.DB
	var id int64
	if err := db.QueryRow(sqldialect.Rebind(db, `SELECT id FROM escalated_webhooks WHERE url = ?`), url).Scan(&id); err != nil {
		t.Fatalf("stored webhook: %v", err)
	}
	return id
}

func TestSecretsNeverSerializedWebhookPagesAndJSON(t *testing.T) {
	for _, name := range []string{"chi", "stdlib"} {
		t.Run(name, func(t *testing.T) {
			f := newUIFixture(t)
			db := f.esc.Config.DB
			h := f.mounts()[name]
			index := "/escalated/admin/webhooks"
			url := "https://example.test/leak-" + name
			id := createWebhookWithSecret(t, f, h, url, leakWebhookSecret)
			if _, err := db.Exec(sqldialect.Rebind(db, `INSERT INTO escalated_webhook_deliveries (webhook_id, event, payload, response_code, attempts) VALUES (?, ?, ?, ?, ?)`),
				id, "ticket.created", `{"ticket":{"id":1}}`, 200, 1); err != nil {
				t.Fatalf("delivery: %v", err)
			}

			rec, page := inertiaGet(t, h, index)
			assertNoSecrets(t, "index page", rec, leakWebhookSecret)
			row := first(t, list(t, page.Props, "webhooks"), "webhooks")
			if row["has_secret"] != true {
				t.Errorf("index page has_secret = %v, want true", row["has_secret"])
			}
			if _, ok := row["secret"]; ok {
				t.Errorf("index page sends secret: %v", row)
			}

			rec, page = inertiaGet(t, h, index+"/"+itoa(id)+"/edit")
			assertNoSecrets(t, "edit page", rec, leakWebhookSecret)
			webhook := obj(t, page.Props, "webhook")
			if webhook["has_secret"] != true || webhook["url"] != url {
				t.Errorf("edit page webhook = %v, want has_secret true and the url", webhook)
			}
			if _, ok := webhook["secret"]; ok {
				t.Errorf("edit page sends secret: %v", webhook)
			}

			rec, page = inertiaGet(t, h, index+"/"+itoa(id)+"/deliveries")
			assertNoSecrets(t, "delivery-log page", rec, leakWebhookSecret)
			if obj(t, page.Props, "webhook")["has_secret"] != true {
				t.Errorf("delivery-log webhook = %v, want has_secret true", page.Props["webhook"])
			}

			// The JSON API the package serves to non-Inertia clients.
			rec = sendAPI(h, http.MethodGet, index, uiAgent, "admin", "")
			assertNoSecrets(t, "JSON list", rec, leakWebhookSecret)
			hooks, _ := decodeJSON(t, "JSON list", rec)["webhooks"].([]any)
			if len(hooks) != 1 {
				t.Fatalf("JSON list webhooks = %v", hooks)
			}
			apiRow := hooks[0].(map[string]any)
			if apiRow["has_secret"] != true {
				t.Errorf("JSON list has_secret = %v, want true", apiRow["has_secret"])
			}
			if _, ok := apiRow["secret"]; ok {
				t.Errorf("JSON list sends secret: %v", apiRow)
			}

			rec = sendAPI(h, http.MethodGet, index+"/"+itoa(id)+"/deliveries", uiAgent, "admin", "")
			if rec.Code != http.StatusOK {
				t.Fatalf("JSON deliveries: %d %s", rec.Code, rec.Body.String())
			}
			assertNoSecrets(t, "JSON deliveries", rec, leakWebhookSecret)

			// The signing secret is still there for deliveries.
			if s := storedWebhookSecret(t, f, id); s == nil || *s != leakWebhookSecret {
				t.Errorf("stored secret = %v", s)
			}
		})
	}
}

func TestWebhookUpdateKeepsReplacesAndClearsTheSecret(t *testing.T) {
	for _, name := range []string{"chi", "stdlib"} {
		t.Run(name, func(t *testing.T) {
			f := newUIFixture(t)
			h := f.mounts()[name]
			index := "/escalated/admin/webhooks"
			url := "https://example.test/rotate-" + name
			id := createWebhookWithSecret(t, f, h, url, leakWebhookSecret)
			path := index + "/" + itoa(id)
			form := func(extra string) string {
				return `{"url":"` + url + `","events":["ticket.created","ticket.assigned"],"active":true` + extra + `}`
			}
			want := func(what string, expected *string) {
				t.Helper()
				got := storedWebhookSecret(t, f, id)
				switch {
				case expected == nil && got != nil:
					t.Errorf("%s: secret = %q, want none", what, *got)
				case expected != nil && (got == nil || *got != *expected):
					t.Errorf("%s: secret = %v, want %q", what, got, *expected)
				}
			}

			// The shared form posts a blank secret and clear_secret false.
			wantRedirect(t, pageJSON(h, http.MethodPut, path, form(`,"secret":"","clear_secret":false`)), "blank secret", index)
			want("blank secret keeps", ptr(leakWebhookSecret))
			var events string
			db := f.esc.Config.DB
			_ = db.QueryRow(sqldialect.Rebind(db, `SELECT events FROM escalated_webhooks WHERE id = ?`), id).Scan(&events)
			if !strings.Contains(events, "ticket.assigned") {
				t.Errorf("the rest of the form was not saved: events %s", events)
			}

			// An API client that leaves the secret out keeps it too.
			if rec := sendAPI(h, http.MethodPatch, path, uiAgent, "admin", `{"active":false}`); rec.Code != http.StatusOK {
				t.Fatalf("API patch: %d %s", rec.Code, rec.Body.String())
			}
			want("missing secret keeps", ptr(leakWebhookSecret))

			wantRedirect(t, pageJSON(h, http.MethodPut, path, form(`,"secret":"whsec-new","clear_secret":false`)), "new secret", index)
			want("a new secret replaces", ptr("whsec-new"))

			wantRedirect(t, pageJSON(h, http.MethodPut, path, form(`,"secret":"","clear_secret":true`)), "clear secret", index)
			want("clear_secret clears", nil)

			_, page := inertiaGet(t, h, path+"/edit")
			if webhook := obj(t, page.Props, "webhook"); webhook["has_secret"] != false {
				t.Errorf("after clearing, has_secret = %v, want false", webhook["has_secret"])
			}

			// clear_secret wins over a secret sent alongside it, and the API
			// takes it too.
			if rec := sendAPI(h, http.MethodPatch, path, uiAgent, "admin", `{"secret":"whsec-api"}`); rec.Code != http.StatusOK {
				t.Fatalf("API set: %d %s", rec.Code, rec.Body.String())
			}
			want("API sets", ptr("whsec-api"))
			if rec := sendAPI(h, http.MethodPatch, path, uiAgent, "admin", `{"secret":"ignored","clear_secret":true}`); rec.Code != http.StatusOK {
				t.Fatalf("API clear: %d %s", rec.Code, rec.Body.String())
			}
			want("API clear_secret clears", nil)
		})
	}
}

func TestWebhookCreateStoresTheSecret(t *testing.T) {
	f := newUIFixture(t)
	h := f.mounts()["chi"]
	id := createWebhookWithSecret(t, f, h, "https://example.test/create", leakWebhookSecret)
	if s := storedWebhookSecret(t, f, id); s == nil || *s != leakWebhookSecret {
		t.Errorf("created secret = %v", s)
	}
	blank := createWebhookWithSecret(t, f, h, "https://example.test/create-blank", "")
	if s := storedWebhookSecret(t, f, blank); s != nil {
		t.Errorf("a blank secret on create stored %q", *s)
	}
}

// importPageJobKeys is Laravel's ImportJob::toPageProps() field list.
var importPageJobKeys = []string{"completed_at", "created_at", "error_log", "field_mappings", "id", "platform", "progress", "started_at", "status"}

func sortedKeys(m map[string]any) string {
	keys := keysOf(m)
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

func TestSecretsNeverSerializedImportPagesAndJSON(t *testing.T) {
	for _, name := range []string{"chi", "stdlib"} {
		t.Run(name, func(t *testing.T) {
			f, _ := newImportFixture(t)
			h := f.mounts()[name]
			ctx := context.Background()
			svc := f.esc.Imports()

			job := &services.ImportJob{Platform: "fakedesk", Status: "importing",
				Progress: map[string]map[string]any{"tickets": {"total": 10, "processed": 4}}}
			if err := svc.Create(ctx, job, map[string]string{"api_token": leakImportKey, "password": leakImportPass}); err != nil {
				t.Fatalf("create job: %v", err)
			}
			for i := 0; i < 60; i++ {
				job.ErrorLog = append(job.ErrorLog, map[string]any{"entity_type": "tickets", "source_id": itoa(int64(i)), "error": "bad"})
			}
			if err := svc.Save(ctx, job); err != nil {
				t.Fatalf("save job: %v", err)
			}
			sealed := *job.Credentials
			secrets := []string{leakImportKey, leakImportPass, sealed}

			rec, page := inertiaGet(t, h, importBase)
			assertNoSecrets(t, "import index", rec, secrets...)
			row := first(t, list(t, page.Props, "jobs"), "jobs")
			if got := sortedKeys(row); got != strings.Join(importPageJobKeys, ",") {
				t.Errorf("index job keys = %s, want %v", got, importPageJobKeys)
			}

			rec, page = inertiaGet(t, h, importBase+"/progress/"+job.ID)
			assertNoSecrets(t, "import progress", rec, secrets...)
			props := obj(t, page.Props, "job")
			if got := sortedKeys(props); got != strings.Join(importPageJobKeys, ",") {
				t.Errorf("progress job keys = %s, want %v", got, importPageJobKeys)
			}
			if props["id"] != job.ID || props["status"] != "importing" || props["platform"] != "fakedesk" {
				t.Errorf("progress job = %v", props)
			}
			errs := list(t, props, "error_log")
			if len(errs) != 50 || errs[49].(map[string]any)["source_id"] != "59" {
				t.Errorf("progress error_log has %d entries, want the last 50", len(errs))
			}

			rec = sendAPI(h, http.MethodGet, importBase+"/status/"+job.ID, uiAgent, "admin", "")
			if decodeJSON(t, "import status", rec)["status"] != "importing" {
				t.Errorf("import status: %s", rec.Body.String())
			}
			assertNoSecrets(t, "import status", rec, secrets...)

			// The wizard steps, with the adapter's own (valid) credentials.
			client := &importClient{t: t, h: h, role: "admin"}
			wizardSecrets := append([]string{fakeToken}, secrets...)
			assertNoSecrets(t, "connect", client.do(http.MethodGet, importBase+"/fakedesk/connect", ""), wizardSecrets...)
			body, _ := json.Marshal(map[string]any{"credentials": fakeCredentials})
			test := client.do(http.MethodPost, importBase+"/fakedesk/test-connection", string(body))
			if decodeJSON(t, "test-connection", test)["success"] != true {
				t.Fatalf("test-connection: %s", test.Body.String())
			}
			assertNoSecrets(t, "test-connection", test, wizardSecrets...)
			mapping := client.do(http.MethodGet, importBase+"/fakedesk/mapping", "")
			if mapping.Code != http.StatusOK {
				t.Fatalf("mapping: %d %s", mapping.Code, mapping.Body.String())
			}
			assertNoSecrets(t, "mapping", mapping, wizardSecrets...)
			review := client.do(http.MethodPost, importBase+"/fakedesk/review", `{"entity_types":["tickets"],"field_mappings":{}}`)
			if review.Code != http.StatusOK {
				t.Fatalf("review: %d %s", review.Code, review.Body.String())
			}
			assertNoSecrets(t, "review", review, wizardSecrets...)

			// The run still opens the stored credentials server-side.
			stored, err := svc.Get(ctx, job.ID)
			if err != nil || stored == nil || stored.Credentials == nil {
				t.Fatalf("stored job: %v %v", stored, err)
			}
			if opened := svc.Credentials().Open(*stored.Credentials); opened["api_token"] != leakImportKey || opened["password"] != leakImportPass {
				t.Errorf("opened credentials = %v", opened)
			}
		})
	}
}

// Inbound-email, import and guest-access secrets are host config here, not
// settings values; no settings page shows them.
func TestSecretsNeverSerializedSettingsPages(t *testing.T) {
	const inbound, importKey, guestKey = "inbound-secret-9d8c7b6a5f4e", "import-secret-8f7e6d5c4b3a", "guest-secret-0123456789abcdef"
	f := newUIFixture(t, func(cfg *escalated.Config) {
		cfg.Notifications.InboundSecret = inbound
		cfg.ImportSecret = importKey
		cfg.GuestAccess.Secret = guestKey
	})
	for name, h := range f.mounts() {
		for _, path := range []string{"email", "csat", "data-retention", "database", "public-tickets", "widget"} {
			rec := send(h, http.MethodGet, "/escalated/admin/settings/"+path, uiAgent, "admin", "")
			if rec.Code != http.StatusOK {
				t.Fatalf("%s %s: %d %s", name, path, rec.Code, rec.Body.String())
			}
			assertNoSecrets(t, name+" settings/"+path, rec, inbound, importKey, guestKey)
		}
	}
}
