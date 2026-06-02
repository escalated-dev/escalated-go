package newsletter

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/escalated-dev/escalated-go/migrations"
	"github.com/escalated-dev/escalated-go/models"
	_ "modernc.org/sqlite"
)

type fakeMailer struct {
	sent int
	err  error
}

func (m *fakeMailer) SendNewsletter(context.Context, MailMessage) error {
	m.sent++
	return m.err
}

func newsletterTestDB(t *testing.T) (*sql.DB, *SQLStore, string) {
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
	return db, NewSQLStore(db, "escalated_", "sqlite"), dir
}

func seedContact(t *testing.T, db *sql.DB, email string, optedOut bool) int64 {
	t.Helper()
	var opt any
	if optedOut {
		opt = time.Now()
	}
	res, err := db.Exec(`INSERT INTO escalated_contacts (email, metadata, marketing_opt_out_at, created_at, updated_at) VALUES (?, '{}', ?, ?, ?)`, email, opt, time.Now(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

func seedNewsletter(t *testing.T, db *sql.DB, listID int64, status string) int64 {
	t.Helper()
	res, err := db.Exec(`INSERT INTO escalated_newsletters (subject, from_email, target_list_id, status, created_at, updated_at) VALUES ('Hello', 'from@example.test', ?, ?, ?, ?)`, listID, status, time.Now(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

func TestPlannerSkipsOptOutAndSuppressed(t *testing.T) {
	db, store, _ := newsletterTestDB(t)
	defer db.Close()
	ctx := context.Background()
	list := &models.NewsletterList{Name: "Static", Kind: models.NewsletterListStatic}
	if err := store.CreateList(ctx, list); err != nil {
		t.Fatal(err)
	}
	c1 := seedContact(t, db, "a@example.test", false)
	c2 := seedContact(t, db, "b@example.test", true)
	c3 := seedContact(t, db, "c@example.test", false)
	_ = store.AddMember(ctx, list.ID, c1, nil)
	_ = store.AddMember(ctx, list.ID, c2, nil)
	_ = store.AddMember(ctx, list.ID, c3, nil)
	nid := seedNewsletter(t, db, list.ID, "draft")
	n, _ := store.GetNewsletter(ctx, nid)
	bounces := NewBounceSuppressionStore(store)
	if err := bounces.MarkBounced(ctx, "c@example.test"); err != nil {
		t.Fatal(err)
	}
	planner := NewNewsletterPlanner(store, NewContactSegmentResolver(store), bounces)
	if err := planner.Plan(ctx, n); err != nil {
		t.Fatal(err)
	}
	var total, rows int
	if err := db.QueryRow(`SELECT summary_total FROM escalated_newsletters WHERE id=?`, nid).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM escalated_newsletter_deliveries WHERE newsletter_id=?`, nid).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if total != 1 || rows != 1 {
		t.Fatalf("want one planned delivery, summary=%d rows=%d", total, rows)
	}
}

func TestDispatcherRateLimitBackoffAndFinalize(t *testing.T) {
	db, store, themes := newsletterTestDB(t)
	defer db.Close()
	ctx := context.Background()
	list := &models.NewsletterList{Name: "Static", Kind: models.NewsletterListStatic}
	_ = store.CreateList(ctx, list)
	c1 := seedContact(t, db, "a@example.test", false)
	c2 := seedContact(t, db, "b@example.test", false)
	nid := seedNewsletter(t, db, list.ID, "sending")
	for _, cid := range []int64{c1, c2} {
		token, _ := randomToken()
		_ = store.InsertDelivery(ctx, &models.NewsletterDelivery{NewsletterID: nid, ContactID: cid, EmailAtSend: "x@example.test", Status: models.DeliveryPending, TrackingToken: token})
	}
	mailer := &fakeMailer{}
	dispatcher := NewNewsletterDispatcher(store, NewRenderer(Config{BaseURL: "https://app.test", ThemesDir: themes, DefaultTheme: "default", TrackingEnabled: false}), mailer, DispatcherConfig{
		EnableNewsletters: true, BatchSize: 10, RateLimitPerMinute: 1, AutoPauseThreshold: 100,
	})
	if err := dispatcher.DispatchBatch(ctx); err != nil {
		t.Fatal(err)
	}
	if mailer.sent != 1 {
		t.Fatalf("first tick should send one due to rate limit, got %d", mailer.sent)
	}
	if err := dispatcher.DispatchBatch(ctx); err != nil {
		t.Fatal(err)
	}
	if mailer.sent != 1 {
		t.Fatalf("second same-minute tick should be rate limited, got %d", mailer.sent)
	}

	failing := &fakeMailer{err: errors.New("smtp down")}
	dispatcher = NewNewsletterDispatcher(store, NewRenderer(Config{BaseURL: "https://app.test", ThemesDir: themes, DefaultTheme: "default", TrackingEnabled: false}), failing, DispatcherConfig{
		EnableNewsletters: true, BatchSize: 10, RateLimitPerMinute: 10, AutoPauseThreshold: 100,
	})
	dispatcher.minute = "old"
	if err := dispatcher.DispatchBatch(ctx); err != nil {
		t.Fatal(err)
	}
	var nextAttempt sql.NullTime
	if err := db.QueryRow(`SELECT next_attempt_at FROM escalated_newsletter_deliveries WHERE status='pending' AND attempt_count=1`).Scan(&nextAttempt); err != nil {
		t.Fatal(err)
	}
	if !nextAttempt.Valid || time.Until(nextAttempt.Time) < 30*time.Second {
		t.Fatalf("expected retry backoff in the future, got %#v", nextAttempt)
	}
}

func TestTrackerOpenClickBounceComplaint(t *testing.T) {
	db, store, _ := newsletterTestDB(t)
	defer db.Close()
	ctx := context.Background()
	list := &models.NewsletterList{Name: "Static", Kind: models.NewsletterListStatic}
	_ = store.CreateList(ctx, list)
	cid := seedContact(t, db, "a@example.test", false)
	nid := seedNewsletter(t, db, list.ID, "sending")
	_ = store.InsertDelivery(ctx, &models.NewsletterDelivery{NewsletterID: nid, ContactID: cid, EmailAtSend: "a@example.test", Status: models.DeliverySent, TrackingToken: "tok"})
	tracker := NewNewsletterTracker(store, NewBounceSuppressionStore(store))
	tracker.RecordOpen(ctx, "tok")
	tracker.RecordOpen(ctx, "tok")
	tracker.RecordClick(ctx, "tok", "https://example.test")
	tracker.RecordClick(ctx, "tok", "https://example.test/2")
	var opened, clicked, clicks int
	_ = db.QueryRow(`SELECT summary_opened, summary_clicked FROM escalated_newsletters WHERE id=?`, nid).Scan(&opened, &clicked)
	_ = db.QueryRow(`SELECT clicks_count FROM escalated_newsletter_deliveries WHERE tracking_token='tok'`).Scan(&clicks)
	if opened != 1 || clicked != 1 || clicks != 2 {
		t.Fatalf("bad counters opened=%d clicked=%d clicks=%d", opened, clicked, clicks)
	}
	reason := "bad"
	tracker.RecordBounce(ctx, "tok", "hard", &reason)
	suppressed, _ := NewBounceSuppressionStore(store).IsBounced(ctx, "A@EXAMPLE.TEST")
	if !suppressed {
		t.Fatal("bounce should suppress email")
	}
}

func TestSegmentResolverAllowlistsRules(t *testing.T) {
	db, store, _ := newsletterTestDB(t)
	defer db.Close()
	ctx := context.Background()
	seedContact(t, db, "a@example.test", false)
	resolver := NewContactSegmentResolver(store)
	count, err := resolver.CountMatches(ctx, map[string]any{"rules": []any{
		map[string]any{"field": "email", "op": "=", "value": "a@example.test"},
		map[string]any{"field": "email; DROP TABLE escalated_contacts; --", "op": "=", "value": "x"},
		map[string]any{"field": "name", "op": "IN (SELECT 1)", "value": "x"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("want one safe match, got %d", count)
	}
}
