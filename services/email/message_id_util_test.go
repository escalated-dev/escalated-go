package email

import (
	"regexp"
	"strings"
	"testing"
)

const (
	testDomain = "support.example.com"
	testSecret = "test-secret-long-enough-for-hmac"
)

func TestBuildMessageID_InitialTicket(t *testing.T) {
	got := BuildMessageID(42, 0, testDomain)
	want := "<ticket-42@support.example.com>"
	if got != want {
		t.Fatalf("BuildMessageID(42, 0) = %q, want %q", got, want)
	}
}

func TestBuildMessageID_ReplyForm(t *testing.T) {
	got := BuildMessageID(42, 7, testDomain)
	want := "<ticket-42-reply-7@support.example.com>"
	if got != want {
		t.Fatalf("BuildMessageID(42, 7) = %q, want %q", got, want)
	}
}

func TestParseTicketIDFromMessageID_RoundTripsInitial(t *testing.T) {
	id := BuildMessageID(42, 0, testDomain)
	got, ok := ParseTicketIDFromMessageID(id)
	if !ok || got != 42 {
		t.Fatalf("ParseTicketIDFromMessageID(%q) = (%d, %v), want (42, true)", id, got, ok)
	}
}

func TestParseTicketIDFromMessageID_RoundTripsReply(t *testing.T) {
	id := BuildMessageID(42, 7, testDomain)
	got, ok := ParseTicketIDFromMessageID(id)
	if !ok || got != 42 {
		t.Fatalf("ParseTicketIDFromMessageID(%q) = (%d, %v), want (42, true)", id, got, ok)
	}
}

func TestParseTicketIDFromMessageID_AcceptsValueWithoutBrackets(t *testing.T) {
	got, ok := ParseTicketIDFromMessageID("ticket-99@example.com")
	if !ok || got != 99 {
		t.Fatalf("got = (%d, %v), want (99, true)", got, ok)
	}
}

func TestParseTicketIDFromMessageID_UnrelatedInput(t *testing.T) {
	cases := []string{"", "<random@mail.com>", "ticket-abc@example.com"}
	for _, c := range cases {
		if _, ok := ParseTicketIDFromMessageID(c); ok {
			t.Errorf("ParseTicketIDFromMessageID(%q): ok = true, want false", c)
		}
	}
}

func TestBuildReplyTo_IsStable(t *testing.T) {
	first := BuildReplyTo(42, testSecret, testDomain)
	again := BuildReplyTo(42, testSecret, testDomain)
	if first != again {
		t.Fatalf("BuildReplyTo not stable: %q vs %q", first, again)
	}
	matched, _ := regexp.MatchString(`^reply\+42\.[a-f0-9]{8}@support\.example\.com$`, first)
	if !matched {
		t.Fatalf("BuildReplyTo output %q does not match expected shape", first)
	}
}

func TestBuildReplyTo_DiffersAcrossTickets(t *testing.T) {
	a := BuildReplyTo(42, testSecret, testDomain)
	b := BuildReplyTo(43, testSecret, testDomain)
	aLocal := strings.SplitN(a, "@", 2)[0]
	bLocal := strings.SplitN(b, "@", 2)[0]
	if aLocal == bLocal {
		t.Fatalf("expected different local parts, both = %q", aLocal)
	}
}

func TestVerifyReplyTo_RoundTrips(t *testing.T) {
	address := BuildReplyTo(42, testSecret, testDomain)
	got, ok := VerifyReplyTo(address, testSecret)
	if !ok || got != 42 {
		t.Fatalf("VerifyReplyTo(%q) = (%d, %v), want (42, true)", address, got, ok)
	}
}

func TestVerifyReplyTo_AcceptsLocalPartOnly(t *testing.T) {
	address := BuildReplyTo(42, testSecret, testDomain)
	local := strings.SplitN(address, "@", 2)[0]
	got, ok := VerifyReplyTo(local, testSecret)
	if !ok || got != 42 {
		t.Fatalf("VerifyReplyTo local %q = (%d, %v), want (42, true)", local, got, ok)
	}
}

func TestVerifyReplyTo_RejectsTampered(t *testing.T) {
	address := BuildReplyTo(42, testSecret, testDomain)
	at := strings.IndexByte(address, '@')
	local := address[:at]
	last := local[len(local)-1]
	flip := byte('0')
	if last == '0' {
		flip = '1'
	}
	tampered := local[:len(local)-1] + string(flip) + address[at:]
	if _, ok := VerifyReplyTo(tampered, testSecret); ok {
		t.Fatalf("tampered address %q should not verify", tampered)
	}
}

func TestVerifyReplyTo_RejectsWrongSecret(t *testing.T) {
	address := BuildReplyTo(42, testSecret, testDomain)
	if _, ok := VerifyReplyTo(address, "different-secret"); ok {
		t.Fatalf("wrong secret should not verify")
	}
}

func TestVerifyReplyTo_RejectsMalformedInput(t *testing.T) {
	cases := []string{
		"",
		"alice@example.com",
		"reply@example.com",
		"reply+abc.deadbeef@example.com",
	}
	for _, c := range cases {
		if _, ok := VerifyReplyTo(c, testSecret); ok {
			t.Errorf("VerifyReplyTo(%q) = ok, want not-ok", c)
		}
	}
}

func TestVerifyReplyTo_CaseInsensitiveHex(t *testing.T) {
	address := BuildReplyTo(42, testSecret, testDomain)
	got, ok := VerifyReplyTo(strings.ToUpper(address), testSecret)
	if !ok || got != 42 {
		t.Fatalf("upper-case %q = (%d, %v), want (42, true)", strings.ToUpper(address), got, ok)
	}
}
