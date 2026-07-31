package services

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/escalated-dev/escalated-go/migrations"
)

func newWebhookTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := migrations.MigrateSQLite(db, "escalated_"); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func insertWebhook(t *testing.T, db *sql.DB, url string, events []string, secret *string, active bool) int64 {
	t.Helper()
	ev, _ := json.Marshal(events)
	res, err := db.Exec(
		`INSERT INTO escalated_webhooks (url, events, secret, active, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		url, ev, secret, active, time.Now(), time.Now(),
	)
	if err != nil {
		t.Fatalf("insert webhook: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

// A subscribed, active webhook with a secret must receive the event with a
// valid HMAC-SHA256 signature over the raw body and the expected headers. A
// webhook that is not subscribed to the event must NOT be called.
func TestWebhookDispatcherFiltersAndSigns(t *testing.T) {
	db := newWebhookTestDB(t)

	var gotBody []byte
	var gotSig, gotEvent, gotContentType string
	var hits int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		gotBody, _ = io.ReadAll(r.Body)
		gotSig = r.Header.Get("X-Escalated-Signature")
		gotEvent = r.Header.Get("X-Escalated-Event")
		gotContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	var unsubHits int32
	unsub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&unsubHits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer unsub.Close()

	secret := "s3cr3t"
	subID := insertWebhook(t, db, srv.URL, []string{"ticket.created", "reply.created"}, &secret, true)
	// Subscribed only to a different event — must be skipped.
	insertWebhook(t, db, unsub.URL, []string{"ticket.closed"}, nil, true)

	d := NewWebhookDispatcher(db, discardLogger())
	payload := map[string]any{
		"ticket": map[string]any{"id": 7, "reference": "ESC-7"},
	}
	d.Dispatch("ticket.created", payload)

	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("subscribed webhook hits = %d, want 1", got)
	}
	if got := atomic.LoadInt32(&unsubHits); got != 0 {
		t.Fatalf("unsubscribed webhook hits = %d, want 0", got)
	}
	if gotEvent != "ticket.created" {
		t.Errorf("X-Escalated-Event = %q, want ticket.created", gotEvent)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}

	// Recompute the HMAC over the exact body the server received.
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(gotBody)
	want := hex.EncodeToString(mac.Sum(nil))
	if gotSig != want {
		t.Errorf("X-Escalated-Signature = %q, want %q", gotSig, want)
	}

	// Body must carry the envelope shape {event, payload, timestamp}.
	var env struct {
		Event     string         `json:"event"`
		Payload   map[string]any `json:"payload"`
		Timestamp string         `json:"timestamp"`
	}
	if err := json.Unmarshal(gotBody, &env); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if env.Event != "ticket.created" {
		t.Errorf("body event = %q, want ticket.created", env.Event)
	}
	if env.Timestamp == "" {
		t.Error("body timestamp is empty")
	}
	if env.Payload["ticket"] == nil {
		t.Error("body payload.ticket missing")
	}

	// One successful delivery row must be recorded for the subscribed webhook.
	var count, code int
	if err := db.QueryRow(
		`SELECT COUNT(*), COALESCE(MAX(response_code), 0) FROM escalated_webhook_deliveries WHERE webhook_id = ?`,
		subID,
	).Scan(&count, &code); err != nil {
		t.Fatalf("count deliveries: %v", err)
	}
	if count != 1 {
		t.Errorf("delivery rows = %d, want 1", count)
	}
	if code != http.StatusOK {
		t.Errorf("recorded response_code = %d, want 200", code)
	}
}

// A webhook without a secret must be delivered WITHOUT a signature header.
func TestWebhookDispatcherNoSecretNoSignature(t *testing.T) {
	db := newWebhookTestDB(t)

	var gotSig string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSig = r.Header.Get("X-Escalated-Signature")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	insertWebhook(t, db, srv.URL, []string{"reply.created"}, nil, true)

	d := NewWebhookDispatcher(db, discardLogger())
	d.Dispatch("reply.created", map[string]any{"reply": map[string]any{"id": 1}})

	if gotSig != "" {
		t.Errorf("unexpected X-Escalated-Signature = %q for unsigned webhook", gotSig)
	}
}

// A persistently failing endpoint must be retried up to MaxAttempts, writing
// one delivery row per attempt. An inactive webhook must never be contacted.
func TestWebhookDispatcherRetriesAndSkipsInactive(t *testing.T) {
	db := newWebhookTestDB(t)

	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	var inactiveHits int32
	inactive := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&inactiveHits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer inactive.Close()

	failID := insertWebhook(t, db, srv.URL, []string{"ticket.created"}, nil, true)
	insertWebhook(t, db, inactive.URL, []string{"ticket.created"}, nil, false)

	d := NewWebhookDispatcher(db, discardLogger())
	d.RetryBackoff = 0 // no sleeping in tests
	d.Dispatch("ticket.created", map[string]any{})

	if got := atomic.LoadInt32(&hits); got != 3 {
		t.Errorf("failing endpoint hits = %d, want 3 (MaxAttempts)", got)
	}
	if got := atomic.LoadInt32(&inactiveHits); got != 0 {
		t.Errorf("inactive webhook hits = %d, want 0", got)
	}

	var count int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM escalated_webhook_deliveries WHERE webhook_id = ?`, failID,
	).Scan(&count); err != nil {
		t.Fatalf("count deliveries: %v", err)
	}
	if count != 3 {
		t.Errorf("delivery rows = %d, want 3 (one per attempt)", count)
	}
}

func discardLogger() *log.Logger {
	return log.New(io.Discard, "", 0)
}
