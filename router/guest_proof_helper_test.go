package router_test

import (
	"context"
	"fmt"
	"regexp"
	"sync"
	"sync/atomic"
	"testing"

	escalated "github.com/escalated-dev/escalated-go"
	"github.com/escalated-dev/escalated-go/services"
)

// codeMailer is a services.MailSender that keeps the last verification code
// sent to each address, as the guest would read it from their inbox.
type codeMailer struct {
	mu    sync.Mutex
	codes map[string]string
	sent  []services.Mail
}

func newCodeMailer() *codeMailer { return &codeMailer{codes: map[string]string{}} }

var eightDigits = regexp.MustCompile(`\b[0-9]{8}\b`)

func (m *codeMailer) SendMail(_ context.Context, msg services.Mail) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, msg)
	if code := eightDigits.FindString(msg.Text); code != "" {
		m.codes[msg.To] = code
	}
	return nil
}

func (m *codeMailer) code(email string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.codes[email]
}

func (m *codeMailer) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sent)
}

var proofClient atomic.Int64

// withProof adds a fresh email code for body["email"] and purpose, the way the
// widget and the guest form send it. The code is requested from GuestAccess
// directly, not over HTTP, so a route budget under test is not spent on it.
func withProof(t *testing.T, esc *escalated.Escalated, purpose string, body map[string]any) map[string]any {
	t.Helper()
	mailer, ok := esc.Config.Mailer.(*codeMailer)
	if !ok {
		t.Fatal("withProof needs Config.Mailer = newCodeMailer()")
	}
	email, _ := body["email"].(string)
	if email == "" {
		email, _ = body["guest_email"].(string)
	}
	client := fmt.Sprintf("192.0.2.%d", proofClient.Add(1)%250)
	id, err := esc.GuestAccess.Challenge(context.Background(), email, purpose, client)
	if err != nil {
		t.Fatalf("challenge %s: %v", email, err)
	}
	out := map[string]any{"verification_id": id, "verification_code": mailer.code(services.NormalizeGuestEmail(email))}
	for k, v := range body {
		out[k] = v
	}
	return out
}
