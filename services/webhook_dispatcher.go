package services

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/escalated-dev/escalated-go/models"
)

// SupportedWebhookEvents is the set of domain events an outbound webhook may
// subscribe to. It mirrors the event names dispatched by the ticket/reply
// services and the Laravel reference's DispatchWebhook listener.
var SupportedWebhookEvents = []string{
	"ticket.created",
	"ticket.updated",
	"ticket.status_changed",
	"ticket.resolved",
	"ticket.closed",
	"ticket.reopened",
	"ticket.assigned",
	"ticket.unassigned",
	"ticket.escalated",
	"ticket.priority_changed",
	"ticket.department_changed",
	"reply.created",
	"note.created",
	"sla.breached",
	"sla.warning",
	"ticket.tag_added",
	"ticket.tag_removed",
}

// WebhookDispatcher delivers domain events to active outbound webhooks.
//
// On each supported event it looks up every active webhook subscribed to the
// event name and POSTs a signed JSON envelope to the webhook URL, recording an
// audit row per attempt in escalated_webhook_deliveries. Non-2xx responses and
// transport errors are retried with exponential backoff up to MaxAttempts.
//
// Mirrors the Laravel reference (Escalated\Laravel\Services\WebhookDispatcher).
// Callers should invoke Dispatch in a goroutine so a slow endpoint never blocks
// the request path.
type WebhookDispatcher struct {
	DB     *sql.DB
	Client *http.Client
	Logger *log.Logger
	// MaxAttempts caps the total number of delivery attempts (default 3).
	MaxAttempts int
	// RetryBackoff is the base backoff; attempt N sleeps RetryBackoff*2^N.
	// The Laravel reference uses pow(2, attempt)*30s (120s, 240s). Set to 0
	// to disable sleeping (used in tests).
	RetryBackoff time.Duration
}

// NewWebhookDispatcher constructs a dispatcher with a 10s HTTP client and a
// default logger when logger is nil.
func NewWebhookDispatcher(db *sql.DB, logger *log.Logger) *WebhookDispatcher {
	if logger == nil {
		logger = log.Default()
	}
	return &WebhookDispatcher{
		DB:           db,
		Client:       &http.Client{Timeout: 10 * time.Second},
		Logger:       logger,
		MaxAttempts:  3,
		RetryBackoff: 30 * time.Second,
	}
}

// webhookEnvelope is the JSON body POSTed to each subscriber. The field order
// (event, payload, timestamp) is fixed so the signed bytes are deterministic.
type webhookEnvelope struct {
	Event     string         `json:"event"`
	Payload   map[string]any `json:"payload"`
	Timestamp string         `json:"timestamp"`
}

// Dispatch delivers the event to every active webhook subscribed to it.
//
// It runs synchronously (including retries); callers that must not block the
// request path should invoke it as `go dispatcher.Dispatch(...)`.
func (d *WebhookDispatcher) Dispatch(event string, payload map[string]any) {
	if d == nil || d.DB == nil {
		return
	}

	rows, err := d.DB.Query(
		`SELECT id, url, events, secret, active FROM escalated_webhooks WHERE active = TRUE`,
	)
	if err != nil {
		d.logger().Printf("escalated webhook: list active: %v", err)
		return
	}
	defer rows.Close()

	var webhooks []models.Webhook
	for rows.Next() {
		var w models.Webhook
		var secret sql.NullString
		if err := rows.Scan(&w.ID, &w.URL, &w.Events, &secret, &w.Active); err != nil {
			d.logger().Printf("escalated webhook: scan: %v", err)
			return
		}
		if secret.Valid {
			w.Secret = &secret.String
		}
		webhooks = append(webhooks, w)
	}
	if err := rows.Err(); err != nil {
		d.logger().Printf("escalated webhook: rows: %v", err)
		return
	}

	for _, w := range webhooks {
		if w.SubscribedTo(event) {
			d.send(w, event, payload, 1)
		}
	}
}

// RetryDelivery re-sends the given delivery from a fresh first attempt.
func (d *WebhookDispatcher) RetryDelivery(delivery models.WebhookDelivery) {
	if d == nil || d.DB == nil {
		return
	}
	webhook, err := d.findWebhook(delivery.WebhookID)
	if err != nil {
		d.logger().Printf("escalated webhook: retry lookup webhook #%d: %v", delivery.WebhookID, err)
		return
	}
	payload := map[string]any{}
	if len(delivery.Payload) > 0 {
		_ = json.Unmarshal(delivery.Payload, &payload)
	}
	d.send(*webhook, delivery.Event, payload, 1)
}

// send performs a single delivery attempt, records it, and schedules a retry
// on failure while attempts remain.
func (d *WebhookDispatcher) send(w models.Webhook, event string, payload map[string]any, attempt int) {
	env := webhookEnvelope{
		Event:     event,
		Payload:   payload,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	body, err := json.Marshal(env)
	if err != nil {
		d.logger().Printf("escalated webhook: marshal body: %v", err)
		return
	}

	payloadJSON, _ := json.Marshal(payload)
	deliveryID := d.recordAttempt(w.ID, event, payloadJSON, attempt)

	req, err := http.NewRequest(http.MethodPost, w.URL, bytes.NewReader(body))
	if err != nil {
		d.recordResult(deliveryID, 0, err.Error(), attempt, false)
		d.maybeRetry(w, event, payload, attempt)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Escalated-Event", event)
	if w.Secret != nil && *w.Secret != "" {
		mac := hmac.New(sha256.New, []byte(*w.Secret))
		mac.Write(body)
		req.Header.Set("X-Escalated-Signature", hex.EncodeToString(mac.Sum(nil)))
	}

	resp, err := d.Client.Do(req)
	if err != nil {
		d.recordResult(deliveryID, 0, err.Error(), attempt, false)
		d.logger().Printf("escalated webhook delivery failed: webhook=%d event=%s attempt=%d err=%v",
			w.ID, event, attempt, err)
		d.maybeRetry(w, event, payload, attempt)
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	success := resp.StatusCode >= 200 && resp.StatusCode < 300
	d.recordResult(deliveryID, resp.StatusCode, truncateBody(string(respBody), 2000), attempt, true)
	if !success {
		d.maybeRetry(w, event, payload, attempt)
	}
}

// maybeRetry sleeps for the exponential backoff and re-sends while attempts
// remain. A zero RetryBackoff skips the sleep.
func (d *WebhookDispatcher) maybeRetry(w models.Webhook, event string, payload map[string]any, attempt int) {
	if attempt >= d.maxAttempts() {
		return
	}
	next := attempt + 1
	if d.RetryBackoff > 0 {
		time.Sleep(d.RetryBackoff * time.Duration(int64(1)<<uint(next)))
	}
	d.send(w, event, payload, next)
}

// recordAttempt inserts a delivery row for this attempt and returns its id.
func (d *WebhookDispatcher) recordAttempt(webhookID int64, event string, payloadJSON []byte, attempt int) int64 {
	now := time.Now()
	res, err := d.DB.Exec(
		`INSERT INTO escalated_webhook_deliveries (webhook_id, event, payload, attempts, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		webhookID, event, payloadJSON, attempt, now, now,
	)
	if err != nil {
		d.logger().Printf("escalated webhook: record attempt: %v", err)
		return 0
	}
	id, _ := res.LastInsertId()
	return id
}

// recordResult updates a delivery row with the response outcome. When delivered
// is true the delivered_at timestamp is set.
func (d *WebhookDispatcher) recordResult(deliveryID int64, code int, respBody string, attempt int, delivered bool) {
	if deliveryID == 0 {
		return
	}
	now := time.Now()
	var deliveredAt any
	if delivered {
		deliveredAt = now
	}
	if _, err := d.DB.Exec(
		`UPDATE escalated_webhook_deliveries
		    SET response_code = ?, response_body = ?, attempts = ?, delivered_at = ?, updated_at = ?
		  WHERE id = ?`,
		code, respBody, attempt, deliveredAt, now, deliveryID,
	); err != nil {
		d.logger().Printf("escalated webhook: record result: %v", err)
	}
}

func (d *WebhookDispatcher) findWebhook(id int64) (*models.Webhook, error) {
	var w models.Webhook
	var secret sql.NullString
	err := d.DB.QueryRow(
		`SELECT id, url, events, secret, active FROM escalated_webhooks WHERE id = ?`,
		id,
	).Scan(&w.ID, &w.URL, &w.Events, &secret, &w.Active)
	if err != nil {
		return nil, err
	}
	if secret.Valid {
		w.Secret = &secret.String
	}
	return &w, nil
}

func (d *WebhookDispatcher) maxAttempts() int {
	if d.MaxAttempts <= 0 {
		return 3
	}
	return d.MaxAttempts
}

func (d *WebhookDispatcher) logger() *log.Logger {
	if d.Logger == nil {
		return log.Default()
	}
	return d.Logger
}

// truncateBody caps a response body at max bytes (Laravel substr(..., 0, 2000)).
func truncateBody(s string, max int) string {
	if len(s) > max {
		return s[:max]
	}
	return s
}
