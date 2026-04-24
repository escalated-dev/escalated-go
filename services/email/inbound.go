package email

import (
	"context"
	"regexp"
	"strings"

	"github.com/escalated-dev/escalated-go/models"
)

// InboundMessage is the transport-agnostic representation of an
// inbound email, independent of the source adapter (Postmark,
// Mailgun, SES, IMAP, etc.).
//
// Adapters normalize their provider-specific webhook payload into
// this shape; InboundRouter.ResolveTicket then maps it to an
// existing ticket via canonical Message-ID parsing + signed
// Reply-To verification.
type InboundMessage struct {
	FromEmail   string
	FromName    string
	ToEmail     string
	Subject     string
	BodyText    string
	BodyHTML    string
	MessageID   string
	InReplyTo   string
	References  string
	Headers     map[string]string
	Attachments []InboundAttachment
}

// Body returns the best available body content — plain text
// preferred, HTML fallback, empty string otherwise.
func (m InboundMessage) Body() string {
	if m.BodyText != "" {
		return m.BodyText
	}
	return m.BodyHTML
}

// InboundAttachment represents a single attachment on an inbound
// email. Content is either inlined (small attachments) or served
// from a provider URL.
type InboundAttachment struct {
	Name        string
	ContentType string
	SizeBytes   int64
	Content     []byte
	DownloadURL string
}

// InboundEmailParser is the pluggable interface host apps (or the
// package's default adapters) implement to normalize a provider's
// webhook payload into an InboundMessage.
//
// Name() must match the adapter label on the inbound webhook
// request (`?adapter=...` query or `X-Escalated-Adapter` header).
type InboundEmailParser interface {
	Name() string
	Parse(rawPayload interface{}) (InboundMessage, error)
}

// TicketLookup is the minimal store contract the router needs to
// resolve tickets. Host apps with additional state can embed their
// own store; the router only touches these two methods.
type TicketLookup interface {
	GetTicket(ctx context.Context, id int64) (*models.Ticket, error)
	GetTicketByReference(ctx context.Context, ref string) (*models.Ticket, error)
}

// InboundRouter resolves an inbound email to an existing ticket via
// canonical Message-ID parsing + signed Reply-To verification.
//
// # Resolution order (first match wins)
//
//  1. In-Reply-To parsed via ParseTicketIDFromMessageID — cold-start
//     path, no DB lookup on the header value required.
//  2. References parsed via ParseTicketIDFromMessageID, each id in
//     order.
//  3. Signed Reply-To on ToEmail (reply+{id}.{hmac8}@...) verified
//     with VerifyReplyTo. Survives clients that strip threading
//     headers; forged signatures are rejected via hmac.Equal.
//  4. Subject-line reference tag ([{PREFIX}-...]).
//
// Mirrors the NestJS reference and the 5 per-framework inbound-verify
// PRs (Laravel, Rails, Django, Adonis, WordPress) + the greenfield
// .NET / Spring routers.
type InboundRouter struct {
	store        TicketLookup
	domain       string
	secret       string
	subjectRegex *regexp.Regexp
}

// NewInboundRouter constructs a router.
//
// domain is the outbound email domain the package stamps on
// Message-IDs. secret is the HMAC key used for signed Reply-To
// addresses — empty disables the signed Reply-To branch.
func NewInboundRouter(store TicketLookup, domain, secret string) *InboundRouter {
	return &InboundRouter{
		store:        store,
		domain:       domain,
		secret:       secret,
		subjectRegex: regexp.MustCompile(`\[([A-Z]+-[0-9A-Z-]+)\]`),
	}
}

// ResolveTicket returns the ticket matching the inbound email, or
// nil on no match (caller should create a new ticket). Errors from
// the store are returned as-is so the caller can decide whether to
// drop, retry, or dead-letter the message.
func (r *InboundRouter) ResolveTicket(ctx context.Context, message InboundMessage) (*models.Ticket, error) {
	// 1 + 2. Parse canonical Message-IDs out of our own headers.
	for _, raw := range CandidateHeaderMessageIDs(message) {
		ticketID, ok := ParseTicketIDFromMessageID(raw)
		if !ok {
			continue
		}
		ticket, err := r.store.GetTicket(ctx, ticketID)
		if err != nil {
			return nil, err
		}
		if ticket != nil {
			return ticket, nil
		}
	}

	// 3. Signed Reply-To on the recipient address.
	if r.secret != "" && message.ToEmail != "" {
		if ticketID, ok := VerifyReplyTo(message.ToEmail, r.secret); ok {
			ticket, err := r.store.GetTicket(ctx, ticketID)
			if err != nil {
				return nil, err
			}
			if ticket != nil {
				return ticket, nil
			}
		}
	}

	// 4. Subject-line reference tag.
	if match := r.subjectRegex.FindStringSubmatch(message.Subject); match != nil {
		ticket, err := r.store.GetTicketByReference(ctx, match[1])
		if err != nil {
			return nil, err
		}
		if ticket != nil {
			return ticket, nil
		}
	}

	return nil, nil
}

// CandidateHeaderMessageIDs returns every candidate Message-ID from
// the inbound headers in the order the mail client sent them.
// Exported for reuse + easy unit testing.
func CandidateHeaderMessageIDs(message InboundMessage) []string {
	ids := make([]string, 0, 4)
	if message.InReplyTo != "" {
		ids = append(ids, strings.TrimSpace(message.InReplyTo))
	}
	if message.References != "" {
		for _, raw := range strings.Fields(message.References) {
			if raw != "" {
				ids = append(ids, raw)
			}
		}
	}
	return ids
}
