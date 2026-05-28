// Package actions provides the host-defined custom ticket action registry.
//
// Host applications register actions on escalated.Config.TicketActions. Each
// visible action renders as a button on the agent ticket screen; triggering it
// records an internal note and invokes the optional Config.OnCustomAction
// callback. Mirrors the Laravel TicketActionRegistry / NestJS reference.
package actions

import "github.com/escalated-dev/escalated-go/models"

// TicketAction is a host-defined custom action button.
type TicketAction struct {
	// Key is the stable identifier, used in the action URL and the event.
	Key string
	// Label is the button text shown to the agent.
	Label string
	// Variant is the button style: "primary" | "secondary" | "danger".
	Variant string
	// Confirmation, when non-empty, is shown as a prompt before the action fires.
	Confirmation string
	// Visible reports whether the action appears for this ticket/user.
	// A nil func means always visible.
	Visible func(t *models.Ticket, userID int64) bool
	// Enabled reports whether the button is clickable (vs shown but disabled).
	// A nil func means always enabled.
	Enabled func(t *models.Ticket, userID int64) bool
	// Metadata is arbitrary data passed through to the UI and the event.
	Metadata map[string]any
}

// CustomActionEvent is passed to Config.OnCustomAction when an action triggers.
type CustomActionEvent struct {
	Ticket   *models.Ticket
	Action   string
	UserID   int64
	Payload  map[string]any
	Metadata map[string]any
}

// Registry holds the registered actions and resolves availability.
type Registry struct {
	ordered []TicketAction
	byKey   map[string]TicketAction
}

// NewRegistry builds a registry from a slice of actions, skipping entries that
// are missing a Key or Label.
func NewRegistry(actions []TicketAction) *Registry {
	r := &Registry{byKey: make(map[string]TicketAction)}
	for _, a := range actions {
		if a.Key == "" || a.Label == "" {
			continue
		}
		r.ordered = append(r.ordered, a)
		r.byKey[a.Key] = a
	}
	return r
}

// Find returns the action with the given key.
func (r *Registry) Find(key string) (TicketAction, bool) {
	a, ok := r.byKey[key]
	return a, ok
}

// Visible reports whether the action is visible for the ticket/user.
func (r *Registry) Visible(a TicketAction, t *models.Ticket, userID int64) bool {
	return a.Visible == nil || a.Visible(t, userID)
}

// Enabled reports whether the action is enabled for the ticket/user.
func (r *Registry) Enabled(a TicketAction, t *models.Ticket, userID int64) bool {
	return a.Enabled == nil || a.Enabled(t, userID)
}

// ForTicket returns the visible actions for a ticket/user, serialized for the
// UI. The caller adds the "url" and "method" before sending to the client.
func (r *Registry) ForTicket(t *models.Ticket, userID int64) []map[string]any {
	out := make([]map[string]any, 0, len(r.ordered))
	for _, a := range r.ordered {
		if !r.Visible(a, t, userID) {
			continue
		}

		variant := a.Variant
		if variant == "" {
			variant = "secondary"
		}

		var confirmation any
		if a.Confirmation != "" {
			confirmation = a.Confirmation
		}

		metadata := a.Metadata
		if metadata == nil {
			metadata = map[string]any{}
		}

		out = append(out, map[string]any{
			"key":          a.Key,
			"label":        a.Label,
			"variant":      variant,
			"confirmation": confirmation,
			"disabled":     !r.Enabled(a, t, userID),
			"metadata":     metadata,
		})
	}
	return out
}
