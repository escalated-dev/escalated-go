package services

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/escalated-dev/escalated-go/internal/testdb"
)

type captureMailer struct{ last Mail }

func (m *captureMailer) SendMail(_ context.Context, msg Mail) error {
	m.last = msg
	return nil
}

func guestAccessFixture(t *testing.T) (*GuestAccess, *captureMailer, *time.Time, *TicketService) {
	t.Helper()
	db := testdb.Open(t)
	m := &captureMailer{}
	g := NewGuestAccess(db, testdb.Prefix, GuestAccessConfig{Secret: "s", TTL: time.Hour}, m, "support@example.com", nil, discardLogger())
	now := time.Now()
	g.now = func() time.Time { return now }
	return g, m, &now, NewTicketService(testdb.Store(t, db))
}

func TestGuestCodesExpireAfterTenMinutes(t *testing.T) {
	g, m, now, _ := guestAccessFixture(t)
	ctx := context.Background()
	id, err := g.Challenge(ctx, "a@example.com", GuestPurposeTicket, "1.2.3.4")
	if err != nil {
		t.Fatal(err)
	}
	if m.last.From != "support@example.com" || m.last.To != "a@example.com" {
		t.Fatalf("mail = %+v", m.last)
	}
	code := regexp.MustCompile(`[0-9]{8}`).FindString(m.last.Text)
	*now = now.Add(11 * time.Minute)
	if err := g.Consume(ctx, id, code, "a@example.com", GuestPurposeTicket); err != ErrGuestCodeInvalid {
		t.Fatalf("expired code: %v", err)
	}
}

func TestGuestGrantsExpireAndRevoke(t *testing.T) {
	g, _, now, ts := guestAccessFixture(t)
	ctx := context.Background()
	email := "b@example.com"
	tk, err := ts.Create(ctx, CreateTicketInput{Subject: "S", Description: "D", GuestEmail: &email})
	if err != nil {
		t.Fatal(err)
	}
	token, expires, err := g.Issue(ctx, tk.ID, "B@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got := expires.Sub(*now); got < 59*time.Minute || got > time.Hour {
		t.Fatalf("expires in %v, want the 1h TTL", got)
	}
	if id, err := g.Resolve(ctx, token); err != nil || id != tk.ID {
		t.Fatalf("Resolve = %d, %v", id, err)
	}
	// The legacy token is cleared by the first grant.
	if _, err := g.ResolveTicketID(ctx, *tk.GuestToken); err == nil {
		t.Errorf("legacy token still opens the ticket after a grant")
	}

	*now = now.Add(61 * time.Minute)
	if _, err := g.Resolve(ctx, token); err == nil {
		t.Errorf("expired grant still resolves")
	}
	*now = now.Add(-61 * time.Minute)

	if err := g.Revoke(ctx, tk.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Resolve(ctx, token); err == nil {
		t.Errorf("revoked grant still resolves")
	}

	// A grant for a ticket whose guest email is not the verified one is refused.
	if _, _, err := g.Issue(ctx, tk.ID, "other@example.com"); err == nil {
		t.Errorf("issued a grant for an email that is not the ticket's")
	}

	// A grant sealed with another secret does not open anything.
	other := NewGuestAccess(g.DB, g.Prefix, GuestAccessConfig{Secret: "different"}, &captureMailer{}, "", nil, discardLogger())
	fresh, _, err := g.Issue(ctx, tk.ID, email)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Resolve(ctx, fresh); err == nil {
		t.Errorf("a grant opened with the wrong secret")
	}
}

func TestGuestGrantTTLIsClamped(t *testing.T) {
	for _, c := range []struct{ in, want time.Duration }{
		{0, 24 * time.Hour}, {time.Minute, 5 * time.Minute}, {30 * 24 * time.Hour, 7 * 24 * time.Hour}, {2 * time.Hour, 2 * time.Hour},
	} {
		g := &GuestAccess{Config: GuestAccessConfig{TTL: c.in}}
		if got := g.grantTTL(); got != c.want {
			t.Errorf("TTL %v -> %v, want %v", c.in, got, c.want)
		}
	}
}
