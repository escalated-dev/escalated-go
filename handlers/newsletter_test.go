package handlers

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/escalated-dev/escalated-go/migrations"
	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/renderer"
	"github.com/escalated-dev/escalated-go/services/newsletter"
	_ "modernc.org/sqlite"
)

func newsletterHandlerTest(t *testing.T, permission func(*http.Request, string) bool) (*NewsletterHandler, *sql.DB, *newsletter.SQLStore) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := migrations.MigrateSQLite(db, "escalated_"); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "default.html"), []byte(`<!doctype html><body>{{.Body}}</body>`), 0o600); err != nil {
		t.Fatal(err)
	}
	store := newsletter.NewSQLStore(db, "escalated_", "sqlite")
	bounces := newsletter.NewBounceSuppressionStore(store)
	segments := newsletter.NewContactSegmentResolver(store)
	planner := newsletter.NewNewsletterPlanner(store, segments, bounces)
	newsRender := newsletter.NewRenderer(newsletter.Config{BaseURL: "https://app.test", ThemesDir: dir, DefaultTheme: "default", TrackingEnabled: false})
	tracker := newsletter.NewNewsletterTracker(store, bounces)
	h := NewNewsletterHandler(store, renderer.NewJSONRenderer(), newsRender, planner, tracker, nil,
		func(*http.Request) models.UserID { return "user-1" }, permission,
		NewsletterHandlerConfig{Enabled: true, DefaultTheme: "default", ThemesDir: dir, TrackingEnabled: true, BatchSize: 50, RateLimitPerMinute: 60})
	return h, db, store
}

func TestNewsletterHandler_PermissionDenied(t *testing.T) {
	h, db, _ := newsletterHandlerTest(t, func(*http.Request, string) bool { return false })
	defer db.Close()
	rec := httptest.NewRecorder()
	h.CampaignIndex(rec, httptest.NewRequest(http.MethodGet, "/admin/newsletters", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("want 403 got %d", rec.Code)
	}
}

func TestNewsletterHandler_ViewUnknownAlways200(t *testing.T) {
	h, db, _ := newsletterHandlerTest(t, nil)
	defer db.Close()
	req := httptest.NewRequest(http.MethodGet, "/escalated/n/v/missing", nil)
	req.SetPathValue("token", "missing")
	rec := httptest.NewRecorder()
	h.ViewInBrowser(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200 got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Email unavailable") {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
}

func TestNewsletterHandler_ClickRejectsUnsafeURL(t *testing.T) {
	h, db, _ := newsletterHandlerTest(t, nil)
	defer db.Close()
	req := httptest.NewRequest(http.MethodGet, "/escalated/n/c/tok?u=amF2YXNjcmlwdDphbGVydCgxKQ", nil)
	req.SetPathValue("token", "tok")
	rec := httptest.NewRecorder()
	h.Click(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400 got %d", rec.Code)
	}
}

func TestNewsletterHandler_SendgridWebhookRecordsHardBounce(t *testing.T) {
	h, db, store := newsletterHandlerTest(t, nil)
	defer db.Close()
	ctx := context.Background()
	list := &models.NewsletterList{Name: "List", Kind: models.NewsletterListStatic}
	if err := store.CreateList(ctx, list); err != nil {
		t.Fatal(err)
	}
	res, err := db.Exec(`INSERT INTO escalated_contacts (email, metadata, created_at, updated_at) VALUES ('a@example.test', '{}', ?, ?)`, time.Now(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	contactID, _ := res.LastInsertId()
	nid := int64(0)
	if err := db.QueryRow(`INSERT INTO escalated_newsletters (subject, from_email, target_list_id, status, created_at, updated_at) VALUES ('S', 'from@example.test', ?, 'sending', ?, ?) RETURNING id`, list.ID, time.Now(), time.Now()).Scan(&nid); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertDelivery(ctx, &models.NewsletterDelivery{NewsletterID: nid, ContactID: contactID, EmailAtSend: "a@example.test", Status: models.DeliverySent, TrackingToken: "abc123"}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/escalated/webhooks/newsletter/sendgrid", strings.NewReader(`[{"event":"dropped","smtp-id":"<n-1-abc123@app.test>","reason":"bad"}]`))
	rec := httptest.NewRecorder()
	h.WebhookSendgrid(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200 got %d", rec.Code)
	}
	d, _ := store.GetDeliveryByToken(ctx, "abc123")
	if d.Status != models.DeliveryBounced {
		t.Fatalf("want bounced got %s", d.Status)
	}
}
