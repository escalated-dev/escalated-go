package services

import (
	"bytes"
	"fmt"
	"html/template"
	"strings"

	"github.com/escalated-dev/escalated-go/services/email"
)

// EmailBranding holds branding configuration for outbound emails.
type EmailBranding struct {
	LogoURL     string
	AccentColor string
	FooterText  string
	CompanyName string
}

// DefaultEmailBranding returns branding with sensible defaults.
func DefaultEmailBranding() EmailBranding {
	return EmailBranding{
		AccentColor: "#4F46E5",
		FooterText:  "Powered by Escalated",
		CompanyName: "Support",
	}
}

// EmailThreadingHeaders holds Message-ID, In-Reply-To, and References headers
// for email threading compliance (RFC 2822).
type EmailThreadingHeaders struct {
	MessageID  string
	InReplyTo  string
	References []string
}

// GenerateMessageID creates a unique Message-ID for a ticket reply.
// Format: <ticket-{ticketRef}-reply-{replyID}@{domain}>
func GenerateMessageID(ticketRef string, replyID int64, domain string) string {
	return fmt.Sprintf("<ticket-%s-reply-%d@%s>", strings.ToLower(ticketRef), replyID, domain)
}

// GenerateTicketMessageID creates the root Message-ID for a ticket.
// Format: <ticket-{ticketRef}@{domain}>
//
// Deprecated: prefer GenerateTicketMessageIDByID so the Message-ID
// matches the canonical MessageIdUtil format used by all other
// Escalated frameworks and by the inbound-routing adapters.
func GenerateTicketMessageID(ticketRef string, domain string) string {
	return fmt.Sprintf("<ticket-%s@%s>", strings.ToLower(ticketRef), domain)
}

// GenerateMessageIDByID wraps email.BuildMessageID — pass 0 for
// replyID on the initial ticket form.
func GenerateMessageIDByID(ticketID int64, replyID int64, domain string) string {
	return email.BuildMessageID(ticketID, replyID, domain)
}

// GenerateTicketMessageIDByID returns the canonical ticket-root
// Message-ID for a given ticket id.
func GenerateTicketMessageIDByID(ticketID int64, domain string) string {
	return email.BuildMessageID(ticketID, 0, domain)
}

// BuildSignedReplyTo returns a canonical reply+{id}.{hmac8}@{domain}
// address, or "" when secret is empty (signing disabled).
//
// Inbound provider webhooks verify the HMAC prefix via
// email.VerifyReplyTo to route replies back to the correct ticket
// even when mail clients strip the Message-ID chain.
func BuildSignedReplyTo(ticketID int64, secret, domain string) string {
	if secret == "" {
		return ""
	}
	return email.BuildReplyTo(ticketID, secret, domain)
}

// BuildThreadingHeadersByID is the int64-keyed counterpart to
// BuildThreadingHeaders. Emits canonical <ticket-{id}(-reply-{replyId})?@{domain}>
// Message-IDs. Pass parentMessageID="" and previousRefs=nil for the
// initial ticket-root form.
func BuildThreadingHeadersByID(ticketID, replyID int64, domain, parentMessageID string, previousRefs []string) EmailThreadingHeaders {
	messageID := email.BuildMessageID(ticketID, replyID, domain)

	headers := EmailThreadingHeaders{
		MessageID: messageID,
		InReplyTo: parentMessageID,
	}

	seen := make(map[string]bool)
	for _, ref := range previousRefs {
		if !seen[ref] {
			headers.References = append(headers.References, ref)
			seen[ref] = true
		}
	}
	if parentMessageID != "" && !seen[parentMessageID] {
		headers.References = append(headers.References, parentMessageID)
	}
	return headers
}

// BuildThreadingHeaders constructs the threading headers for a reply email.
// parentMessageID is the Message-ID of the message being replied to.
// previousRefs are all prior Message-IDs in the thread.
func BuildThreadingHeaders(ticketRef string, replyID int64, domain string, parentMessageID string, previousRefs []string) EmailThreadingHeaders {
	messageID := GenerateMessageID(ticketRef, replyID, domain)

	headers := EmailThreadingHeaders{
		MessageID: messageID,
		InReplyTo: parentMessageID,
	}

	// References = all previous references + the parent
	seen := make(map[string]bool)
	for _, ref := range previousRefs {
		if !seen[ref] {
			headers.References = append(headers.References, ref)
			seen[ref] = true
		}
	}
	if parentMessageID != "" && !seen[parentMessageID] {
		headers.References = append(headers.References, parentMessageID)
	}

	return headers
}

// EmailService handles outbound email rendering with branding and threading.
type EmailService struct {
	branding EmailBranding
	domain   string
	tmpl     *template.Template
}

// NewEmailService creates a new EmailService with the given branding and mail domain.
func NewEmailService(branding EmailBranding, domain string) *EmailService {
	tmpl := template.Must(template.New("email").Parse(emailTemplate))
	return &EmailService{
		branding: branding,
		domain:   domain,
		tmpl:     tmpl,
	}
}

// EmailData holds the data passed to the email template.
type EmailData struct {
	Subject     string
	Body        template.HTML
	TicketRef   string
	Branding    EmailBranding
	UnsubURL    string
	TicketURL   string
	ReplyPrompt string
}

// RenderHTML renders a branded HTML email.
func (es *EmailService) RenderHTML(data EmailData) (string, error) {
	data.Branding = es.branding
	var buf bytes.Buffer
	if err := es.tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("rendering email template: %w", err)
	}
	return buf.String(), nil
}

// ThreadingHeadersForReply generates threading headers for a reply.
func (es *EmailService) ThreadingHeadersForReply(ticketRef string, replyID int64, parentMessageID string, previousRefs []string) EmailThreadingHeaders {
	return BuildThreadingHeaders(ticketRef, replyID, es.domain, parentMessageID, previousRefs)
}

// ThreadingHeadersForTicket generates the initial Message-ID for a new ticket.
func (es *EmailService) ThreadingHeadersForTicket(ticketRef string) EmailThreadingHeaders {
	return EmailThreadingHeaders{
		MessageID: GenerateTicketMessageID(ticketRef, es.domain),
	}
}

const emailTemplate = `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>{{.Subject}}</title>
</head>
<body style="margin:0;padding:0;background-color:#f4f4f5;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;">
<table role="presentation" width="100%" cellspacing="0" cellpadding="0" style="background-color:#f4f4f5;">
<tr><td align="center" style="padding:24px 16px;">
<table role="presentation" width="600" cellspacing="0" cellpadding="0" style="background-color:#ffffff;border-radius:8px;overflow:hidden;">

{{if .Branding.LogoURL}}
<tr><td style="padding:24px 32px 0 32px;text-align:center;">
<img src="{{.Branding.LogoURL}}" alt="{{.Branding.CompanyName}}" style="max-height:48px;max-width:200px;">
</td></tr>
{{end}}

<tr><td style="padding:8px 32px 0 32px;">
<div style="height:4px;background-color:{{.Branding.AccentColor}};border-radius:2px;"></div>
</td></tr>

<tr><td style="padding:24px 32px;">
<h2 style="margin:0 0 8px 0;color:#18181b;font-size:18px;">{{.Subject}}</h2>
{{if .TicketRef}}<p style="margin:0 0 16px 0;color:#71717a;font-size:13px;">Ticket: {{.TicketRef}}</p>{{end}}
<div style="color:#3f3f46;font-size:15px;line-height:1.6;">{{.Body}}</div>
</td></tr>

{{if .TicketURL}}
<tr><td style="padding:0 32px 24px 32px;">
<a href="{{.TicketURL}}" style="display:inline-block;padding:10px 24px;background-color:{{.Branding.AccentColor}};color:#ffffff;text-decoration:none;border-radius:6px;font-size:14px;font-weight:500;">View Ticket</a>
</td></tr>
{{end}}

{{if .ReplyPrompt}}
<tr><td style="padding:0 32px 24px 32px;color:#71717a;font-size:13px;font-style:italic;">
{{.ReplyPrompt}}
</td></tr>
{{end}}

<tr><td style="padding:16px 32px;background-color:#fafafa;text-align:center;border-top:1px solid #e4e4e7;">
<p style="margin:0;color:#a1a1aa;font-size:12px;">{{.Branding.FooterText}}</p>
{{if .UnsubURL}}<p style="margin:4px 0 0 0;"><a href="{{.UnsubURL}}" style="color:#a1a1aa;font-size:12px;">Unsubscribe</a></p>{{end}}
</td></tr>

</table>
</td></tr>
</table>
</body>
</html>`
