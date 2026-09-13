package services

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/escalated-dev/escalated-go/internal/sqldialect"
)

func lastDelivery(t *testing.T, db *sql.DB, webhookID int64) (int, string) {
	t.Helper()
	var code sql.NullInt64
	var body sql.NullString
	err := db.QueryRow(
		sqldialect.Rebind(db, `SELECT response_code, response_body FROM escalated_webhook_deliveries WHERE webhook_id = ? ORDER BY id DESC LIMIT 1`),
		webhookID,
	).Scan(&code, &body)
	if err != nil {
		t.Fatalf("read delivery: %v", err)
	}
	return int(code.Int64), body.String
}

// A URL stored before create-time validation existed, or a hostname that
// resolves somewhere else by the time a delivery is sent, must still not reach
// a loopback or private address. The check belongs at connect time, on the
// address actually dialled.
func TestWebhookDispatcherRefusesToConnectToPrivateAddresses(t *testing.T) {
	db := newWebhookTestDB(t)

	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	id := insertWebhook(t, db, srv.URL+"/hook", []string{"ticket.created"}, nil, true)

	d := NewWebhookDispatcher(db, discardLogger())
	d.MaxAttempts = 1
	d.RetryBackoff = 0
	d.Dispatch("ticket.created", map[string]any{"ticket": map[string]any{"id": 1}})

	if got := atomic.LoadInt32(&hits); got != 0 {
		t.Fatalf("loopback receiver was called %d time(s), want 0", got)
	}
	if code, body := lastDelivery(t, db, id); code != 0 || !strings.Contains(body, "not allowed") {
		t.Errorf("delivery recorded code=%d body=%q, want code 0 and a refusal", code, body)
	}
}

// Following a redirect would hand the destination choice to the receiver: a
// public URL could answer 302 to the metadata endpoint. The default client
// must return the redirect as the response instead.
func TestWebhookDispatcherDoesNotFollowRedirects(t *testing.T) {
	db := newWebhookTestDB(t)

	var targetHits int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&targetHits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/elsewhere", http.StatusFound)
	}))
	defer origin.Close()

	id := insertWebhook(t, db, origin.URL+"/hook", []string{"ticket.created"}, nil, true)

	d := NewWebhookDispatcher(db, discardLogger())
	d.MaxAttempts = 1
	d.RetryBackoff = 0
	// Both test servers listen on loopback, which the default transport refuses.
	// Swap only the transport, so this exercises the default client's redirect
	// policy and nothing else.
	d.Client.Transport = http.DefaultTransport
	d.Dispatch("ticket.created", map[string]any{"ticket": map[string]any{"id": 1}})

	if got := atomic.LoadInt32(&targetHits); got != 0 {
		t.Fatalf("redirect target was called %d time(s), want 0", got)
	}
	if code, _ := lastDelivery(t, db, id); code != http.StatusFound {
		t.Errorf("delivery response_code = %d, want 302 recorded as the response", code)
	}
}
