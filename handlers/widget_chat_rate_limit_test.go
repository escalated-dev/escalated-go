package handlers

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/services"
)

// NewWidgetChatHandler built its limiter with a zero window, so every earlier
// hit had already expired and chat start was never limited. It now uses the
// same per-minute window as the ticket-create limiter, per client IP (the port
// is ignored, so a new connection does not reset the count).
func TestWidgetChatHandler_StartIsRateLimited(t *testing.T) {
	cfg := DefaultWidgetConfig()
	cfg.Enabled = true
	cfg.RateLimitPerMin = 2
	ms := &chatSessionStore{widgetMockStore: newWidgetMockStore(), sessions: map[int64]*models.ChatSession{}}
	sessions := services.NewChatSessionService(ms, services.NewChatRoutingService(ms), nil)
	h := NewWidgetChatHandler(cfg, ms, sessions, nil)

	start := func(remote string) int {
		req := httptest.NewRequest(http.MethodPost, "/widget/chat/start",
			bytes.NewBufferString(`{"name":"Visitor","email":"visitor@example.com"}`))
		req.RemoteAddr = remote
		rec := httptest.NewRecorder()
		h.StartChat(rec, req)
		return rec.Code
	}

	for i := 0; i < 2; i++ {
		if code := start(fmt.Sprintf("10.0.0.1:%d", 4000+i)); code != http.StatusCreated {
			t.Fatalf("start %d: got %d, want 201", i, code)
		}
	}
	if code := start("10.0.0.1:5000"); code != http.StatusTooManyRequests {
		t.Fatalf("third start from the same IP: got %d, want 429", code)
	}
	if code := start("10.0.0.2:4000"); code != http.StatusCreated {
		t.Fatalf("another IP: got %d, want 201", code)
	}
}

// A zero RateLimitPerMin falls back to the default instead of refusing every
// chat (a limit of 0 would make the limiter answer 429 to everyone).
func TestWidgetChatHandler_ZeroLimitUsesDefault(t *testing.T) {
	cfg := DefaultWidgetConfig()
	cfg.Enabled = true
	cfg.RateLimitPerMin = 0
	ms := &chatSessionStore{widgetMockStore: newWidgetMockStore(), sessions: map[int64]*models.ChatSession{}}
	h := NewWidgetChatHandler(cfg, ms, services.NewChatSessionService(ms, services.NewChatRoutingService(ms), nil), nil)

	req := httptest.NewRequest(http.MethodPost, "/widget/chat/start",
		bytes.NewBufferString(`{"name":"Visitor","email":"visitor@example.com"}`))
	req.RemoteAddr = "10.0.0.1:4000"
	rec := httptest.NewRecorder()
	h.StartChat(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("got %d, want 201", rec.Code)
	}
}
