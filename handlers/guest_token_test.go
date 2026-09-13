package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/services"
)

func TestGuestTokenMatches(t *testing.T) {
	stored := "GT-abcdef"
	empty := ""

	tests := []struct {
		name   string
		stored *string
		given  string
		want   bool
	}{
		{name: "equal", stored: &stored, given: "GT-abcdef", want: true},
		{name: "last character differs", stored: &stored, given: "GT-abcdeg", want: false},
		{name: "prefix of the stored token", stored: &stored, given: "GT-abc", want: false},
		{name: "longer than the stored token", stored: &stored, given: "GT-abcdefg", want: false},
		{name: "ticket has no token", stored: nil, given: "GT-abcdef", want: false},
		{name: "empty token given", stored: &stored, given: "", want: false},
		{name: "empty token stored and given", stored: &empty, given: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := guestTokenMatches(tt.stored, tt.given); got != tt.want {
				t.Errorf("guestTokenMatches(%v, %q) = %v, want %v", tt.stored, tt.given, got, tt.want)
			}
		})
	}
}

// chatSessionStore is the widget mock with a chat session behind each ticket
// that has one.
type chatSessionStore struct {
	*widgetMockStore
	sessions map[int64]*models.ChatSession
}

func (m *chatSessionStore) GetChatSessionByTicket(_ context.Context, ticketID int64) (*models.ChatSession, error) {
	return m.sessions[ticketID], nil
}

// A chat started before the token change holds a reference-shaped token. It is
// stored and compared as it is, so the visitor keeps their chat; a new chat's
// token works the same way, and neither opens the other.
func TestWidgetChatGuestTokens(t *testing.T) {
	ctx := context.Background()
	ms := &chatSessionStore{widgetMockStore: newWidgetMockStore(), sessions: map[int64]*models.ChatSession{}}
	sessions := services.NewChatSessionService(ms, services.NewChatRoutingService(ms), services.NewBroadcaster(services.BroadcastConfig{Enabled: false}, nil))

	cfg := DefaultWidgetConfig()
	cfg.Enabled = true
	h := NewWidgetChatHandler(cfg, ms, sessions, nil)

	current, session, err := sessions.StartSession(ctx, services.StartSessionInput{GuestName: "Visitor", GuestEmail: "visitor@example.com"})
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	ms.sessions[current.ID] = session
	currentToken := *current.GuestToken

	oldToken := "GT-2609-A1B2C3"
	channel := models.ChannelChat
	ms.tickets[99] = &models.Ticket{
		ID:         99,
		Reference:  "ESC-2609-4D2C1A",
		Subject:    "Live Chat",
		Status:     models.StatusLive,
		Channel:    &channel,
		GuestToken: &oldToken,
	}
	ms.sessions[99] = &models.ChatSession{ID: 99, TicketID: 99, Status: models.ChatStatusActive}

	tamperedToken := currentToken[:len(currentToken)-1] + "A"
	if tamperedToken == currentToken {
		tamperedToken = currentToken[:len(currentToken)-1] + "B"
	}

	tests := []struct {
		name       string
		ref        string
		token      string
		wantStatus int
	}{
		{name: "new chat, its own token", ref: current.Reference, token: currentToken, wantStatus: http.StatusOK},
		{name: "chat started before the change, its own token", ref: "ESC-2609-4D2C1A", token: oldToken, wantStatus: http.StatusOK},
		{name: "new chat, the older chat's token", ref: current.Reference, token: oldToken, wantStatus: http.StatusNotFound},
		{name: "older chat, the new chat's token", ref: "ESC-2609-4D2C1A", token: currentToken, wantStatus: http.StatusNotFound},
		{name: "new chat, token with its last character changed", ref: current.Reference, token: tamperedToken, wantStatus: http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/widget/chat/sessions/"+tt.ref+"/end", nil)
			req.SetPathValue("ref", tt.ref)
			req.Header.Set("X-Guest-Token", tt.token)
			rec := httptest.NewRecorder()

			h.EndChat(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("EndChat = %d, want %d; body: %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}
