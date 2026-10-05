package email

import (
	"context"
	"log"
	"strings"

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
// the ticket-write path. Implementations: services.TicketService
// (AddReply and ChangeStatus match its signatures; Create goes through
// the host's adapter).
type TicketWriter interface {
	Create(ctx context.Context, in CreateTicketInputShim) (*models.Ticket, error)
	AddReply(ctx context.Context, ticketID int64, body string, authorType *string, authorID *models.UserID, internal bool) (*models.Reply, error)
	// ChangeStatus moves a ticket to newStatus. Used to reopen a
	// resolved or closed ticket when its requester replies by email.
	ChangeStatus(ctx context.Context, ticketID int64, newStatus int, causerID *models.UserID) error
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
	router     *InboundRouter
	tickets    TicketWriter
	requesters RequesterEmailResolver
}

// RequesterEmailResolver returns the email address of a ticket's
// host-app requester (RequesterType / RequesterID). The package does
// not own the host's user table, so hosts whose customers reply by
// email register one via WithRequesterEmailResolver. Return "" when
// the requester is unknown.
type RequesterEmailResolver interface {
	RequesterEmail(ctx context.Context, ticket *models.Ticket) (string, error)
}

// NewInboundEmailService wires an InboundRouter + a TicketWriter for
// reply/create operations.
func NewInboundEmailService(router *InboundRouter, tickets TicketWriter) *InboundEmailService {
	return &InboundEmailService{router: router, tickets: tickets}
}

// WithRequesterEmailResolver registers the lookup used to accept
// email replies from a ticket's host-app requester. Without one, only
// guest tickets (GuestEmail) accept email replies; replies to user
// tickets open a new ticket instead. Returns the service for chaining.
func (s *InboundEmailService) WithRequesterEmailResolver(r RequesterEmailResolver) *InboundEmailService {
	s.requesters = r
	return s
}

// Process executes the full inbound pipeline on a parsed message.
// Returns a ProcessResult carrying the outcome.
//
// Resolution:
//
//   - router.ResolveTicket → ticket found and From is the ticket's
//     requester: AddReply posted as that requester (the requester
//     user, or an "inbound_email" guest reply). A resolved or closed
//     ticket is then reopened (ChangeStatus to StatusReopened); a
//     failed reopen is logged and the reply stands. outcome =
//     REPLIED_TO_EXISTING.
//   - noise (SNS confirmation, empty body+subject): outcome = SKIPPED,
//     no side effects.
//   - router miss, or a sender who is not the requester:
//     Create(subject, body, guest name/email) outcome = CREATED_NEW.
//
// A thread match alone never posts a reply: Message-IDs and ticket
// references are guessable, and the From header is unauthenticated,
// so the reply author always comes from the ticket, never from From.
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
		authorType, authorID, accepted, err := s.replyAuthor(ctx, ticket, message)
		if err != nil {
			return ProcessResult{}, err
		}
		if accepted {
			reply, err := s.tickets.AddReply(ctx, ticket.ID, message.Body(), authorType, authorID, false)
			if err != nil {
				return ProcessResult{}, err
			}
			s.reopenIfFinished(ctx, ticket, authorID)
			return ProcessResult{
				Outcome:                    OutcomeRepliedToExisting,
				TicketID:                   ticket.ID,
				ReplyID:                    reply.ID,
				PendingAttachmentDownloads: pendingDownloads(message),
			}, nil
		}
		log.Printf("[InboundEmailService] inbound email matched ticket #%d but not its requester; opening a new ticket", ticket.ID)
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

// replyAuthor decides who a threaded inbound email may post as. It
// accepts the message only when From (case-insensitive) is the
// ticket's guest email or its requester's email, and returns the
// requester as the author. Anyone else — including a staff member
// whose address appears in From — is not accepted.
func (s *InboundEmailService) replyAuthor(ctx context.Context, ticket *models.Ticket, message InboundMessage) (*string, *models.UserID, bool, error) {
	sender := normalizeEmail(message.FromEmail)
	if sender == "" {
		return nil, nil, false, nil
	}

	if ticket.GuestEmail != nil && normalizeEmail(*ticket.GuestEmail) == sender {
		authorType := "inbound_email"
		return &authorType, nil, true, nil
	}

	if ticket.RequesterType != nil && ticket.RequesterID != nil && *ticket.RequesterID != "" && s.requesters != nil {
		email, err := s.requesters.RequesterEmail(ctx, ticket)
		if err != nil {
			return nil, nil, false, err
		}
		if normalizeEmail(email) == sender {
			authorType := *ticket.RequesterType
			authorID := *ticket.RequesterID
			return &authorType, &authorID, true, nil
		}
	}

	return nil, nil, false, nil
}

// reopenIfFinished reopens a resolved or closed ticket after its
// requester replied, matching the Laravel reference. A refused or
// failed transition is logged; the reply has already been posted.
func (s *InboundEmailService) reopenIfFinished(ctx context.Context, ticket *models.Ticket, causerID *models.UserID) {
	if ticket.Status != models.StatusResolved && ticket.Status != models.StatusClosed {
		return
	}
	if err := s.tickets.ChangeStatus(ctx, ticket.ID, models.StatusReopened, causerID); err != nil {
		log.Printf("[InboundEmailService] could not reopen ticket #%d from inbound email reply: %v", ticket.ID, err)
	}
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
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
