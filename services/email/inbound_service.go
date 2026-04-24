package email

import (
	"context"
	"log"

	"github.com/escalated-dev/escalated-go/models"
)

// Outcome is the high-level result of processing an inbound email.
type Outcome string

const (
	OutcomeRepliedToExisting Outcome = "replied_to_existing"
	OutcomeCreatedNew        Outcome = "created_new"
	OutcomeSkipped           Outcome = "skipped"
)

// ProcessResult is returned by InboundEmailService.Process. Carries
// enough information for the controller to build its JSON response
// and for a follow-up worker to download any provider-hosted
// attachments (Mailgun etc.) out-of-band.
type ProcessResult struct {
	Outcome                    Outcome
	TicketID                   int64
	ReplyID                    int64
	PendingAttachmentDownloads []PendingAttachment
}

// PendingAttachment represents a provider-hosted attachment that the
// host app should download out-of-band. The parser populates the
// DownloadURL when the provider hosts content (Mailgun behavior);
// inline Postmark attachments come through with Content bytes and
// aren't included here.
type PendingAttachment struct {
	Name        string
	ContentType string
	SizeBytes   int64
	DownloadURL string
}

// TicketWriter is the minimal contract InboundEmailService needs on
// the ticket-write path. Implementations: services.TicketService.
type TicketWriter interface {
	Create(ctx context.Context, in CreateTicketInputShim) (*models.Ticket, error)
	AddReply(ctx context.Context, ticketID int64, body string, authorType *string, authorID *int64, internal bool) (*models.Reply, error)
}

// CreateTicketInputShim mirrors services.CreateTicketInput with a
// minimal subset the inbound flow populates. Host apps pass through
// to services.TicketService.Create via their own adapter. Keeps the
// email package free of a circular dep on services.
type CreateTicketInputShim struct {
	Subject      string
	Description  string
	Priority     int
	GuestName    *string
	GuestEmail   *string
	DepartmentID *int64
}

// InboundEmailService orchestrates the full inbound email pipeline:
//
//	parser output → router resolution → reply-on-existing or
//	create-new-ticket.
//
// Mirrors the NestJS reference InboundRouterService and the .NET /
// Spring ports.
type InboundEmailService struct {
	router  *InboundRouter
	tickets TicketWriter
}

// NewInboundEmailService wires an InboundRouter + a TicketWriter for
// reply/create operations.
func NewInboundEmailService(router *InboundRouter, tickets TicketWriter) *InboundEmailService {
	return &InboundEmailService{router: router, tickets: tickets}
}

// Process executes the full inbound pipeline on a parsed message.
// Returns a ProcessResult carrying the outcome.
//
// Resolution:
//
//   - router.ResolveTicket → ticket found: AddReply(body, "inbound_email")
//     outcome = REPLIED_TO_EXISTING.
//   - router miss + noise (SNS confirmation, empty body+subject):
//     outcome = SKIPPED, no side effects.
//   - router miss + real content: Create(subject, body, guest name/email)
//     outcome = CREATED_NEW.
//
// Attachment persistence is out of scope: provider-hosted attachments
// (Mailgun DownloadURL without inline Content) surface in
// PendingAttachmentDownloads for a follow-up worker.
func (s *InboundEmailService) Process(ctx context.Context, message InboundMessage) (ProcessResult, error) {
	ticket, err := s.router.ResolveTicket(ctx, message)
	if err != nil {
		return ProcessResult{}, err
	}

	if ticket != nil {
		authorType := "inbound_email"
		reply, err := s.tickets.AddReply(ctx, ticket.ID, message.Body(), &authorType, nil, false)
		if err != nil {
			return ProcessResult{}, err
		}
		return ProcessResult{
			Outcome:                    OutcomeRepliedToExisting,
			TicketID:                   ticket.ID,
			ReplyID:                    reply.ID,
			PendingAttachmentDownloads: pendingDownloads(message),
		}, nil
	}

	if isNoiseEmail(message) {
		return ProcessResult{Outcome: OutcomeSkipped}, nil
	}

	subject := message.Subject
	if subject == "" {
		subject = "(no subject)"
	}
	guestName := nilIfEmpty(message.FromName)
	guestEmail := nilIfEmpty(message.FromEmail)

	newTicket, err := s.tickets.Create(ctx, CreateTicketInputShim{
		Subject:     subject,
		Description: message.Body(),
		GuestName:   guestName,
		GuestEmail:  guestEmail,
	})
	if err != nil {
		return ProcessResult{}, err
	}

	log.Printf("[InboundEmailService] created ticket #%d from inbound email", newTicket.ID)

	return ProcessResult{
		Outcome:                    OutcomeCreatedNew,
		TicketID:                   newTicket.ID,
		PendingAttachmentDownloads: pendingDownloads(message),
	}, nil
}

// IsNoiseEmail returns true for messages we should skip rather than
// create a new ticket from: SNS subscription confirmations, bounce
// echoes, and fully-empty bodies. Exported for tests + for host apps
// that want to apply the same filter elsewhere.
func IsNoiseEmail(message InboundMessage) bool { return isNoiseEmail(message) }

func isNoiseEmail(message InboundMessage) bool {
	if message.FromEmail == "no-reply@sns.amazonaws.com" {
		return true
	}
	if message.Body() == "" && message.Subject == "" {
		return true
	}
	return false
}

func pendingDownloads(message InboundMessage) []PendingAttachment {
	var list []PendingAttachment
	for _, a := range message.Attachments {
		if a.DownloadURL != "" && len(a.Content) == 0 {
			list = append(list, PendingAttachment{
				Name:        a.Name,
				ContentType: a.ContentType,
				SizeBytes:   a.SizeBytes,
				DownloadURL: a.DownloadURL,
			})
		}
	}
	return list
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
