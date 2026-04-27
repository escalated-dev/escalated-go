// Package email contains pure helpers for RFC 5322 Message-ID threading
// and signed Reply-To addresses.
//
// Mirrors the NestJS reference escalated-nestjs/src/services/email/message-id.ts
// and the Spring / WordPress / .NET / Phoenix / Laravel / Rails /
// Django / Adonis ports.
//
// Message-ID format:
//
//	<ticket-{ticketID}@{domain}>             initial ticket email
//	<ticket-{ticketID}-reply-{replyID}@{domain}>  agent reply
//
// Signed Reply-To format:
//
//	reply+{ticketID}.{hmac8}@{domain}
//
// The signed Reply-To carries ticket identity even when clients strip
// our Message-ID / In-Reply-To headers — the inbound provider webhook
// verifies the 8-char HMAC-SHA256 prefix before routing a reply to its
// ticket.
package email

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	ticketIDPattern   = regexp.MustCompile(`(?i)ticket-(\d+)(?:-reply-\d+)?@`)
	replyLocalPattern = regexp.MustCompile(`(?i)^reply\+(\d+)\.([a-f0-9]{8})$`)
)

// BuildMessageID builds an RFC 5322 Message-ID. Pass 0 for replyID to
// indicate the initial ticket email (only ticket-{id} form); pass a
// positive replyID to get the reply form.
func BuildMessageID(ticketID int64, replyID int64, domain string) string {
	if replyID > 0 {
		return fmt.Sprintf("<ticket-%d-reply-%d@%s>", ticketID, replyID, domain)
	}
	return fmt.Sprintf("<ticket-%d@%s>", ticketID, domain)
}

// ParseTicketIDFromMessageID extracts the ticket id from a Message-ID
// we issued. Accepts the header value with or without angle brackets.
// Returns 0 and ok=false when the input doesn't match our shape.
func ParseTicketIDFromMessageID(raw string) (int64, bool) {
	if raw == "" {
		return 0, false
	}
	match := ticketIDPattern.FindStringSubmatch(raw)
	if match == nil {
		return 0, false
	}
	n, err := strconv.ParseInt(match[1], 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

// BuildReplyTo builds a signed Reply-To address of the form
// reply+{ticketID}.{hmac8}@{domain}.
func BuildReplyTo(ticketID int64, secret, domain string) string {
	return fmt.Sprintf("reply+%d.%s@%s", ticketID, sign(ticketID, secret), domain)
}

// VerifyReplyTo verifies a reply-to address (full local@domain or just
// the local part). Returns the ticket id on match and ok=true, or 0
// and ok=false otherwise. Uses crypto/hmac.Equal for timing-safe
// comparison.
func VerifyReplyTo(address, secret string) (int64, bool) {
	if address == "" {
		return 0, false
	}
	local := address
	if at := strings.IndexByte(address, '@'); at > 0 {
		local = address[:at]
	}
	match := replyLocalPattern.FindStringSubmatch(local)
	if match == nil {
		return 0, false
	}
	ticketID, err := strconv.ParseInt(match[1], 10, 64)
	if err != nil {
		return 0, false
	}
	expected := sign(ticketID, secret)
	if hmac.Equal([]byte(strings.ToLower(expected)), []byte(strings.ToLower(match[2]))) {
		return ticketID, true
	}
	return 0, false
}

// sign returns the 8-character HMAC-SHA256 prefix over the ticket id.
func sign(ticketID int64, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strconv.FormatInt(ticketID, 10)))
	return hex.EncodeToString(mac.Sum(nil))[:8]
}
