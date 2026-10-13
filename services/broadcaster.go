// Package services contains business logic that sits between handlers and the store.
//
// This file implements a channel-based event broadcaster with SSE support.
package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/escalated-dev/escalated-go/ticketaccess"

	"github.com/escalated-dev/escalated-go/models"
)

// Event represents a real-time event that can be broadcast to subscribers.
type Event struct {
	Channel   string          `json:"channel"`
	EventType string          `json:"event"`
	Data      json.RawMessage `json:"data"`
	Timestamp time.Time       `json:"timestamp"`
}

// Subscriber represents a connected client listening to events.
type Subscriber struct {
	ID       string
	Channels map[string]bool
	Events   chan Event
	done     chan struct{}
}

// Close signals this subscriber to stop receiving events.
func (s *Subscriber) Close() {
	select {
	case <-s.done:
	default:
		close(s.done)
	}
}

// Done returns a channel that is closed when the subscriber is closed.
func (s *Subscriber) Done() <-chan struct{} {
	return s.done
}

// BroadcastConfig holds configuration for the broadcasting service.
type BroadcastConfig struct {
	TicketAccess   *ticketaccess.Access `json:"-"`
	Enabled        bool                 `json:"enabled"`
	BufferSize     int                  `json:"buffer_size"`
	MaxSubscribers int                  `json:"max_subscribers"`
}

// DefaultBroadcastConfig returns a BroadcastConfig with sensible defaults.
func DefaultBroadcastConfig() BroadcastConfig {
	return BroadcastConfig{
		Enabled:        false,
		BufferSize:     64,
		MaxSubscribers: 1000,
	}
}

// Broadcaster manages event channels and subscribers for real-time updates.
type Broadcaster struct {
	mu          sync.RWMutex
	config      BroadcastConfig
	subscribers map[string]*Subscriber
	authFunc    func(subscriberID, channel string) bool
}

// NewBroadcaster creates a new Broadcaster.
func NewBroadcaster(cfg BroadcastConfig, authFunc func(subscriberID, channel string) bool) *Broadcaster {
	if cfg.BufferSize <= 0 {
		cfg.BufferSize = 64
	}
	if cfg.MaxSubscribers <= 0 {
		cfg.MaxSubscribers = 1000
	}
	if authFunc == nil {
		authFunc = func(_, _ string) bool { return true }
	}
	return &Broadcaster{
		config:      cfg,
		subscribers: make(map[string]*Subscriber),
		authFunc:    authFunc,
	}
}

// Subscribe creates a new subscriber and subscribes it to the given channels.
// Returns an error if broadcasting is disabled or max subscribers reached.
func (b *Broadcaster) Subscribe(subscriberID string, channels []string) (*Subscriber, error) {
	if !b.config.Enabled {
		return nil, fmt.Errorf("broadcasting is disabled")
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if len(b.subscribers) >= b.config.MaxSubscribers {
		return nil, fmt.Errorf("max subscribers reached")
	}

	// Remove existing subscriber with same ID
	if old, ok := b.subscribers[subscriberID]; ok {
		old.Close()
		delete(b.subscribers, subscriberID)
	}

	sub := &Subscriber{
		ID:       subscriberID,
		Channels: make(map[string]bool),
		Events:   make(chan Event, b.config.BufferSize),
		done:     make(chan struct{}),
	}

	for _, ch := range channels {
		if b.authFunc(subscriberID, ch) && authorizeTicketChannel(b.config.TicketAccess, subscriberID, ch) {
			sub.Channels[ch] = true
		}
	}

	b.subscribers[subscriberID] = sub
	return sub, nil
}

// Unsubscribe removes a subscriber.
func (b *Broadcaster) Unsubscribe(subscriberID string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if sub, ok := b.subscribers[subscriberID]; ok {
		sub.Close()
		delete(b.subscribers, subscriberID)
	}
}

// AddChannel adds a channel to an existing subscriber's subscriptions.
func (b *Broadcaster) AddChannel(subscriberID, channel string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	sub, ok := b.subscribers[subscriberID]
	if !ok {
		return fmt.Errorf("subscriber %q not found", subscriberID)
	}

	if !b.authFunc(subscriberID, channel) || !authorizeTicketChannel(b.config.TicketAccess, subscriberID, channel) {
		return fmt.Errorf("not authorized for channel %q", channel)
	}

	sub.Channels[channel] = true
	return nil
}

// RemoveChannel removes a channel from a subscriber's subscriptions.
func (b *Broadcaster) RemoveChannel(subscriberID, channel string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if sub, ok := b.subscribers[subscriberID]; ok {
		delete(sub.Channels, channel)
	}
}

// Publish sends an event to all subscribers on the given channel.
// Returns the number of subscribers the event was delivered to.
func (b *Broadcaster) Publish(channel, eventType string, data any) (int, error) {
	if !b.config.Enabled {
		return 0, fmt.Errorf("broadcasting is disabled")
	}

	raw, err := json.Marshal(data)
	if err != nil {
		return 0, fmt.Errorf("marshaling event data: %w", err)
	}

	evt := Event{
		Channel:   channel,
		EventType: eventType,
		Data:      raw,
		Timestamp: time.Now(),
	}

	b.mu.RLock()
	access := b.config.TicketAccess
	subscribers := []*Subscriber{}
	for _, sub := range b.subscribers {
		if sub.Channels[channel] {
			subscribers = append(subscribers, sub)
		}
	}
	b.mu.RUnlock()
	delivered := 0
	for _, sub := range subscribers {
		if access.Restricted() && (!b.authFunc(sub.ID, channel) || !canDeliverTicketEvent(access, sub.ID, evt)) {
			continue
		}
		select {
		case sub.Events <- evt:
			delivered++
		case <-sub.Done():
			// Subscriber closed, skip
		default:
			// Buffer full, drop event for this subscriber
		}
	}
	return delivered, nil
}

// SubscriberCount returns the current number of active subscribers.
func (b *Broadcaster) SubscriberCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subscribers)
}

// ChannelName helpers for common Escalated channels.

// TicketChannel returns the channel name for a specific ticket.
func TicketChannel(ticketID int64) string {
	return fmt.Sprintf("ticket.%d", ticketID)
}

// AgentChannel returns the channel name for agent-wide events.
func AgentChannel() string {
	return "agents"
}

// UserChannel returns the private channel name for a specific user.
func UserChannel(userID models.UserID) string {
	return fmt.Sprintf("user.%s", userID)
}

// ConfigureTicketAccess installs access checks for host-managed broadcasters.
func (b *Broadcaster) ConfigureTicketAccess(access *ticketaccess.Access) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.config.TicketAccess = access
}

// CanDeliver rechecks buffered events immediately before a transport writes them.
func (b *Broadcaster) CanDeliver(subscriberID string, event Event) bool {
	b.mu.RLock()
	access := b.config.TicketAccess
	b.mu.RUnlock()
	return !access.Restricted() || (b.authFunc(subscriberID, event.Channel) && canDeliverTicketEvent(access, subscriberID, event))
}
func authorizeTicketChannel(access *ticketaccess.Access, subscriberID, channel string) bool {
	if !access.Restricted() {
		return true
	}
	if !strings.HasPrefix(subscriberID, "user-") || access.Options.ResolveUser == nil {
		return false
	}
	id := models.UserID(strings.TrimPrefix(subscriberID, "user-"))
	if strings.HasPrefix(channel, "ticket.") {
		ticketID, err := strconv.ParseInt(strings.TrimPrefix(channel, "ticket."), 10, 64)
		return err == nil && access.CanViewUser(context.Background(), id, ticketID, false)
	}
	user, err := access.Options.ResolveUser(context.Background(), id)
	if err != nil || user == nil || user.ID != id {
		return false
	}
	if channel == UserChannel(id) {
		return true
	}
	return channel == AgentChannel() && (user.Agent || user.Admin)
}
func canDeliverTicketEvent(access *ticketaccess.Access, subscriberID string, event Event) bool {
	if !access.Restricted() {
		return true
	}
	if !authorizeTicketChannel(access, subscriberID, event.Channel) {
		return false
	}
	var ticketID int64
	if strings.HasPrefix(event.Channel, "ticket.") {
		ticketID, _ = strconv.ParseInt(strings.TrimPrefix(event.Channel, "ticket."), 10, 64)
	} else {
		var payload struct {
			TicketID int64 `json:"ticket_id"`
			Ticket   struct {
				ID int64 `json:"id"`
			} `json:"ticket"`
		}
		if json.Unmarshal(event.Data, &payload) != nil {
			return false
		}
		ticketID = payload.TicketID
		if ticketID == 0 {
			ticketID = payload.Ticket.ID
		}
	}
	return ticketID > 0 && access.CanViewUser(context.Background(), models.UserID(strings.TrimPrefix(subscriberID, "user-")), ticketID, event.Channel == AgentChannel())
}
