package services

import (
	"context"
	"encoding/base64"
	"regexp"
	"strings"
	"testing"
)

// The guest token is the only credential a live-chat visitor holds: the widget
// sends it with the ticket reference to read, post to and end the chat. It used
// to come from GenerateReference("GT"), a prefix, the year and month, and 3
// random bytes, so a visitor's chat could be taken over by trying 2^24 tokens
// against a reference the visitor was shown.
func TestStartSessionIssuesAGuestTokenWithAtLeast128Bits(t *testing.T) {
	const prefix = "GT-"
	referenceShape := regexp.MustCompile(`^[A-Z]+-\d{4}-[0-9A-Z]+$`)

	ms := newMockStore()
	svc := NewChatSessionService(ms, NewChatRoutingService(ms), NewBroadcaster(BroadcastConfig{Enabled: false}, nil))

	seen := map[string]bool{}
	for i := 0; i < 2; i++ {
		ticket, _, err := svc.StartSession(context.Background(), StartSessionInput{
			GuestName:  "Visitor",
			GuestEmail: "visitor@example.com",
		})
		if err != nil {
			t.Fatalf("StartSession: %v", err)
		}
		if ticket.GuestToken == nil {
			t.Fatal("StartSession left the guest token unset")
		}
		token := *ticket.GuestToken

		if referenceShape.MatchString(token) {
			t.Errorf("guest token %q has the shape of a ticket reference: it comes from the reference generator", token)
		}
		if !strings.HasPrefix(token, prefix) {
			t.Fatalf("guest token %q, want the %q prefix", token, prefix)
		}

		random, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(token, prefix))
		if err != nil {
			t.Fatalf("guest token %q is not %q plus URL-safe base64: %v", token, prefix, err)
		}
		if bits := len(random) * 8; bits < 128 {
			t.Errorf("guest token %q decodes to %d bits, want at least 128 random bits", token, bits)
		}

		if seen[token] {
			t.Fatalf("two sessions got the same guest token %q", token)
		}
		seen[token] = true
	}
}
