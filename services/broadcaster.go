// Package services contains business logic that sits between handlers and the store.
//
// This file implements a channel-based event broadcaster with SSE support.
package services

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
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
	Enabled        bool `json:"enabled"`
	BufferSize     int  `json:"buffer_size"`
	MaxSubscribers int  `json:"max_subscribers"`
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
		if b.authFunc(subscriberID, ch) {
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
	b.mu.RLock()
	defer b.mu.RUnlock()

	sub, ok := b.subscribers[subscriberID]
	if !ok {
		return fmt.Errorf("subscriber %q not found", subscriberID)
	}

	if !b.authFunc(subscriberID, channel) {
		return fmt.Errorf("not authorized for channel %q", channel)
	}

	sub.Channels[channel] = true
	return nil
}

// RemoveChannel removes a channel from a subscriber's subscriptions.
func (b *Broadcaster) RemoveChannel(subscriberID, channel string) {
	b.mu.RLock()
	defer b.mu.RUnlock()

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
	defer b.mu.RUnlock()

	delivered := 0
	for _, sub := range b.subscribers {
		if !sub.Channels[channel] {
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
func UserChannel(userID int64) string {
	return fmt.Sprintf("user.%d", userID)
}
