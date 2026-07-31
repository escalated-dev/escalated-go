package models

import (
	"encoding/json"
	"time"
)

// Webhook is an outbound HTTP subscription. When a supported domain event
// fires, every active webhook whose Events list contains the event name
// receives a signed POST to URL.
//
// Mirrors the Laravel reference (Escalated\Laravel\Models\Webhook): the
// events column is a JSON array of event-name strings, active gates
// delivery, and the optional secret enables HMAC-SHA256 request signing.
type Webhook struct {
	ID  int64  `json:"id"`
	URL string `json:"url"`
	// Events is a JSON array of subscribed event names (e.g. ["ticket.created"]).
	Events json.RawMessage `json:"events"`
	// Secret, when set, signs each delivery via X-Escalated-Signature.
	Secret    *string   `json:"secret,omitempty"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SubscribedTo reports whether this webhook is registered for the event.
func (w Webhook) SubscribedTo(event string) bool {
	for _, e := range w.EventNames() {
		if e == event {
			return true
		}
	}
	return false
}

// EventNames returns the parsed list of subscribed event names. A malformed
// or empty Events column yields an empty slice.
func (w Webhook) EventNames() []string {
	if len(w.Events) == 0 {
		return nil
	}
	var events []string
	_ = json.Unmarshal(w.Events, &events)
	return events
}
