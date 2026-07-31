package models

import (
	"encoding/json"
	"time"
)

// WebhookDelivery is the audit record for a single outbound delivery attempt.
// One row is written per attempt (mirroring the Laravel reference, which
// creates a WebhookDelivery on every send), capturing the response code,
// a truncated response body, the attempt count, and when it was delivered.
type WebhookDelivery struct {
	ID        int64  `json:"id"`
	WebhookID int64  `json:"webhook_id"`
	Event     string `json:"event"`
	// Payload is the JSON payload object sent under the "payload" key.
	Payload json.RawMessage `json:"payload,omitempty"`
	// ResponseCode is the HTTP status, or 0 on transport error. Nil until sent.
	ResponseCode *int       `json:"response_code,omitempty"`
	ResponseBody *string    `json:"response_body,omitempty"`
	Attempts     int        `json:"attempts"`
	DeliveredAt  *time.Time `json:"delivered_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// IsSuccess reports whether the recorded response was a 2xx.
func (d WebhookDelivery) IsSuccess() bool {
	return d.ResponseCode != nil && *d.ResponseCode >= 200 && *d.ResponseCode < 300
}
