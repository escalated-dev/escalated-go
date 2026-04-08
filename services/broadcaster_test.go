package services

import (
	"testing"
	"time"
)

func TestBroadcaster_SubscribeAndPublish(t *testing.T) {
	cfg := BroadcastConfig{Enabled: true, BufferSize: 10, MaxSubscribers: 100}
	b := NewBroadcaster(cfg, nil)

	sub, err := b.Subscribe("user-1", []string{"ticket.1", "agents"})
	if err != nil {
		t.Fatalf("Subscribe error: %v", err)
	}
	defer b.Unsubscribe("user-1")

	if !sub.Channels["ticket.1"] {
		t.Error("expected subscription to ticket.1")
	}
	if !sub.Channels["agents"] {
		t.Error("expected subscription to agents")
	}

	// Publish to subscribed channel
	delivered, err := b.Publish("ticket.1", "ticket.updated", map[string]string{"id": "1"})
	if err != nil {
		t.Fatalf("Publish error: %v", err)
	}
	if delivered != 1 {
		t.Errorf("expected 1 delivery, got %d", delivered)
	}

	// Read event
	select {
	case evt := <-sub.Events:
		if evt.Channel != "ticket.1" {
			t.Errorf("channel = %q, want 'ticket.1'", evt.Channel)
		}
		if evt.EventType != "ticket.updated" {
			t.Errorf("event = %q, want 'ticket.updated'", evt.EventType)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}

	// Publish to unsubscribed channel
	delivered, _ = b.Publish("ticket.999", "ticket.updated", nil)
	if delivered != 0 {
		t.Errorf("expected 0 deliveries to unsubscribed channel, got %d", delivered)
	}
}

func TestBroadcaster_Disabled(t *testing.T) {
	cfg := BroadcastConfig{Enabled: false}
	b := NewBroadcaster(cfg, nil)

	_, err := b.Subscribe("user-1", []string{"test"})
	if err == nil {
		t.Fatal("expected error when disabled")
	}

	_, err = b.Publish("test", "evt", nil)
	if err == nil {
		t.Fatal("expected error when disabled")
	}
}

func TestBroadcaster_MaxSubscribers(t *testing.T) {
	cfg := BroadcastConfig{Enabled: true, BufferSize: 1, MaxSubscribers: 2}
	b := NewBroadcaster(cfg, nil)

	_, err := b.Subscribe("user-1", nil)
	if err != nil {
		t.Fatalf("Subscribe 1: %v", err)
	}
	_, err = b.Subscribe("user-2", nil)
	if err != nil {
		t.Fatalf("Subscribe 2: %v", err)
	}
	_, err = b.Subscribe("user-3", nil)
	if err == nil {
		t.Fatal("expected error for max subscribers")
	}
}

func TestBroadcaster_AuthFunc(t *testing.T) {
	cfg := BroadcastConfig{Enabled: true, BufferSize: 10, MaxSubscribers: 100}
	authFunc := func(subID, channel string) bool {
		// Only allow user-1 to subscribe to private channels
		return channel == "public" || subID == "user-1"
	}
	b := NewBroadcaster(cfg, authFunc)

	sub1, _ := b.Subscribe("user-1", []string{"public", "private"})
	sub2, _ := b.Subscribe("user-2", []string{"public", "private"})

	if !sub1.Channels["private"] {
		t.Error("user-1 should be authorized for private")
	}
	if sub2.Channels["private"] {
		t.Error("user-2 should NOT be authorized for private")
	}
	if !sub2.Channels["public"] {
		t.Error("user-2 should be authorized for public")
	}

	// AddChannel with auth check
	err := b.AddChannel("user-2", "private")
	if err == nil {
		t.Error("expected error adding unauthorized channel")
	}
}

func TestBroadcaster_Unsubscribe(t *testing.T) {
	cfg := BroadcastConfig{Enabled: true, BufferSize: 10, MaxSubscribers: 100}
	b := NewBroadcaster(cfg, nil)

	_, err := b.Subscribe("user-1", []string{"test"})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	if b.SubscriberCount() != 1 {
		t.Errorf("count = %d, want 1", b.SubscriberCount())
	}

	b.Unsubscribe("user-1")

	if b.SubscriberCount() != 0 {
		t.Errorf("count = %d, want 0", b.SubscriberCount())
	}
}

func TestBroadcaster_ResubscribeReplacesOld(t *testing.T) {
	cfg := BroadcastConfig{Enabled: true, BufferSize: 10, MaxSubscribers: 100}
	b := NewBroadcaster(cfg, nil)

	sub1, _ := b.Subscribe("user-1", []string{"ch1"})
	sub2, _ := b.Subscribe("user-1", []string{"ch2"})

	// Old subscriber should be closed
	select {
	case <-sub1.Done():
		// good
	default:
		t.Error("old subscriber should be closed")
	}

	if sub2.Channels["ch1"] {
		t.Error("new subscriber should not have ch1")
	}
	if !sub2.Channels["ch2"] {
		t.Error("new subscriber should have ch2")
	}

	if b.SubscriberCount() != 1 {
		t.Errorf("count = %d, want 1", b.SubscriberCount())
	}
}

func TestChannelNames(t *testing.T) {
	if got := TicketChannel(42); got != "ticket.42" {
		t.Errorf("TicketChannel = %q", got)
	}
	if got := AgentChannel(); got != "agents" {
		t.Errorf("AgentChannel = %q", got)
	}
	if got := UserChannel(5); got != "user.5" {
		t.Errorf("UserChannel = %q", got)
	}
}

func TestBroadcaster_RemoveChannel(t *testing.T) {
	cfg := BroadcastConfig{Enabled: true, BufferSize: 10, MaxSubscribers: 100}
	b := NewBroadcaster(cfg, nil)

	sub, _ := b.Subscribe("user-1", []string{"ch1", "ch2"})
	b.RemoveChannel("user-1", "ch1")

	if sub.Channels["ch1"] {
		t.Error("ch1 should have been removed")
	}
	if !sub.Channels["ch2"] {
		t.Error("ch2 should still be present")
	}
}

func TestBroadcaster_PublishDropsWhenBufferFull(t *testing.T) {
	cfg := BroadcastConfig{Enabled: true, BufferSize: 1, MaxSubscribers: 100}
	b := NewBroadcaster(cfg, nil)

	_, err := b.Subscribe("user-1", []string{"test"})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	// Fill the buffer
	b.Publish("test", "evt1", nil)
	// This should not block (drops the event)
	delivered, _ := b.Publish("test", "evt2", nil)
	if delivered != 0 {
		t.Errorf("expected 0 delivered (buffer full), got %d", delivered)
	}
}
