package models

import (
	"encoding/json"
	"strings"
	"testing"
)

// Models that hold a secret never put it in their JSON encoding, so no
// handler that writes one of them to a response can send it
// (escalated-laravel #258: hidden attributes).
func marshalString(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal %T: %v", v, err)
	}
	return string(b)
}

func TestWebhookJSONHidesTheSecretAndReportsHasSecret(t *testing.T) {
	secret := "whsec-4c8e1a7b9d3f"
	out := marshalString(t, Webhook{ID: 7, URL: "https://hooks.example.com", Events: JSONText(`["ticket.created"]`), Secret: &secret, Active: true})
	if strings.Contains(out, secret) || strings.Contains(out, `"secret"`) {
		t.Errorf("webhook JSON carries the secret: %s", out)
	}
	var m map[string]any
	_ = json.Unmarshal([]byte(out), &m)
	if m["has_secret"] != true || m["url"] != "https://hooks.example.com" || m["id"] != float64(7) {
		t.Errorf("webhook JSON = %s", out)
	}

	// A pointer encodes the same way, and no (or a blank) secret is false.
	if out := marshalString(t, &Webhook{Secret: &secret}); !strings.Contains(out, `"has_secret":true`) {
		t.Errorf("*Webhook JSON = %s", out)
	}
	blank := ""
	for _, w := range []Webhook{{}, {Secret: &blank}} {
		if out := marshalString(t, w); !strings.Contains(out, `"has_secret":false`) {
			t.Errorf("unset secret JSON = %s", out)
		}
	}
}

func TestTwoFactorJSONHidesTheSeedAndRecoveryCodes(t *testing.T) {
	seed := "TOTPSECRETBASE32XX"
	out := marshalString(t, TwoFactor{UserID: "1", Method: "totp", Secret: &seed, RecoveryCodes: []string{"rc-aaaa-bbbb"}, IsEnabled: true})
	for _, leak := range []string{seed, "rc-aaaa-bbbb", `"secret"`, `"recovery_codes"`} {
		if strings.Contains(out, leak) {
			t.Errorf("two-factor JSON carries %q: %s", leak, out)
		}
	}
	if !strings.Contains(out, `"is_enabled":true`) {
		t.Errorf("two-factor JSON = %s", out)
	}
}

func TestEmailChannelJSONHidesTheSmtpPassword(t *testing.T) {
	password := "smtp-pass-1a2b3c"
	out := marshalString(t, EmailChannel{EmailAddress: "help@example.com", SmtpPassword: &password})
	if strings.Contains(out, password) || strings.Contains(out, `"smtp_password"`) {
		t.Errorf("email channel JSON carries the password: %s", out)
	}
}

func TestTicketJSONHidesTheGuestToken(t *testing.T) {
	token := "guest-token-5e6f7a8b9c"
	out := marshalString(t, Ticket{Subject: "Hi", GuestToken: &token})
	if strings.Contains(out, token) || strings.Contains(out, `"guest_token"`) {
		t.Errorf("ticket JSON carries the guest token: %s", out)
	}
}
