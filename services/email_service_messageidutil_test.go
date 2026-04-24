package services

import (
	"regexp"
	"strings"
	"testing"

	"github.com/escalated-dev/escalated-go/services/email"
)

// Tests for the new MessageIdUtil wire-up — verify the ByID helpers
// produce the canonical <ticket-{id}@{domain}> format and that
// BuildSignedReplyTo round-trips through email.VerifyReplyTo.

const (
	testWireDomain = "support.example.com"
	testWireSecret = "test-secret-for-hmac"
)

func TestGenerateTicketMessageIDByID(t *testing.T) {
	got := GenerateTicketMessageIDByID(42, testWireDomain)
	want := "<ticket-42@support.example.com>"
	if got != want {
		t.Fatalf("GenerateTicketMessageIDByID(42) = %q, want %q", got, want)
	}
}

func TestGenerateMessageIDByID_Initial(t *testing.T) {
	got := GenerateMessageIDByID(42, 0, testWireDomain)
	want := "<ticket-42@support.example.com>"
	if got != want {
		t.Fatalf("GenerateMessageIDByID(42, 0) = %q, want %q", got, want)
	}
}

func TestGenerateMessageIDByID_Reply(t *testing.T) {
	got := GenerateMessageIDByID(42, 7, testWireDomain)
	want := "<ticket-42-reply-7@support.example.com>"
	if got != want {
		t.Fatalf("GenerateMessageIDByID(42, 7) = %q, want %q", got, want)
	}
}

func TestBuildSignedReplyTo_EmptySecretReturnsEmpty(t *testing.T) {
	if got := BuildSignedReplyTo(42, "", testWireDomain); got != "" {
		t.Fatalf("BuildSignedReplyTo with empty secret = %q, want empty", got)
	}
}

func TestBuildSignedReplyTo_RoundTripsThroughVerify(t *testing.T) {
	address := BuildSignedReplyTo(42, testWireSecret, testWireDomain)
	matched, _ := regexp.MatchString(`^reply\+42\.[a-f0-9]{8}@support\.example\.com$`, address)
	if !matched {
		t.Fatalf("BuildSignedReplyTo output %q has unexpected shape", address)
	}
	id, ok := email.VerifyReplyTo(address, testWireSecret)
	if !ok || id != 42 {
		t.Fatalf("VerifyReplyTo(%q) = (%d, %v), want (42, true)", address, id, ok)
	}
}

func TestBuildSignedReplyTo_DifferentTicketsProduceDifferentSignatures(t *testing.T) {
	a := BuildSignedReplyTo(42, testWireSecret, testWireDomain)
	b := BuildSignedReplyTo(43, testWireSecret, testWireDomain)
	aLocal := strings.SplitN(a, "@", 2)[0]
	bLocal := strings.SplitN(b, "@", 2)[0]
	if aLocal == bLocal {
		t.Fatalf("expected different local parts, both = %q", aLocal)
	}
}

func TestBuildThreadingHeadersByID_InitialTicket(t *testing.T) {
	headers := BuildThreadingHeadersByID(42, 0, testWireDomain, "", nil)
	if headers.MessageID != "<ticket-42@support.example.com>" {
		t.Fatalf("MessageID = %q", headers.MessageID)
	}
	if headers.InReplyTo != "" {
		t.Fatalf("InReplyTo should be empty for initial, got %q", headers.InReplyTo)
	}
}

func TestBuildThreadingHeadersByID_Reply(t *testing.T) {
	parent := GenerateTicketMessageIDByID(42, testWireDomain)
	headers := BuildThreadingHeadersByID(42, 7, testWireDomain, parent, nil)

	if headers.MessageID != "<ticket-42-reply-7@support.example.com>" {
		t.Fatalf("MessageID = %q", headers.MessageID)
	}
	if headers.InReplyTo != parent {
		t.Fatalf("InReplyTo = %q, want %q", headers.InReplyTo, parent)
	}
	if len(headers.References) != 1 || headers.References[0] != parent {
		t.Fatalf("References = %v, want [%q]", headers.References, parent)
	}
}
