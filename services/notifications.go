package services

import (
	"context"
	"database/sql"
	"fmt"
	"html"
	"html/template"
	"log"
	"strings"
	"sync"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/services/email"
	"github.com/escalated-dev/escalated-go/ticketaccess"
)

// Notifier emails requesters and agents about ticket activity, following
// escalated-laravel's notification listeners (SendNewTicketNotifications,
// SendReplyNotifications, SendAssignmentNotification,
// SendStatusChangeNotification, SendEscalationNotification,
// SendSlaBreachNotification):
//
//	ticket created   the requester (the thread anchor), and the assignee if any
//	public reply     the requester unless they wrote it; the assignee unless
//	                 they wrote it; followers other than the author, the
//	                 requester and the assignee
//	ticket assigned  the new assignee
//	status changed   the requester and followers, except whoever changed it
//	escalated        the assignee
//	SLA breached     the assignee
//
// Internal notes and live chat messages email nobody, and nobody is emailed
// about their own action. Agents and user requesters are host users: their
// address comes from UserEmail. Guests get the ticket's guest email.
type Notifier struct {
	Mailer MailSender
	From   string
	// Domain is used in Message-IDs and the signed Reply-To.
	Domain string
	// InboundSecret, when set, adds a signed Reply-To (reply+{id}.{hmac}@Domain)
	// so an emailed reply threads back onto the ticket.
	InboundSecret string
	Branding      EmailBranding
	// UserEmail returns a host user's email address ("" when unknown). Nil
	// means host users (agents, followers, user requesters) are not emailed.
	UserEmail func(ctx context.Context, id models.UserID) (string, error)
	// TicketURL returns the link to a ticket for a recipient (agent true for
	// agents and followers). Nil leaves the link out.
	TicketURL func(t *models.Ticket, agent bool) string
	// DB holds the followers table. Nil means no followers are emailed.
	DB *sql.DB
	// Access, when set, skips agents who cannot see the ticket.
	Access *ticketaccess.Access
	// Sync sends inline instead of in a goroutine (tests).
	Sync   bool
	Logger *log.Logger

	emailOnce sync.Once
	email     *EmailService
}

// notifyTarget is one recipient: a host user or a guest address.
type notifyTarget struct {
	userID models.UserID
	agent  bool
	guest  string
}

func (n *Notifier) logger() *log.Logger {
	if n.Logger == nil {
		return log.Default()
	}
	return n.Logger
}

func (n *Notifier) domain() string {
	if n.Domain != "" {
		return n.Domain
	}
	return "escalated.dev"
}

// requesterTarget is the ticket's requester: its host user, or its guest
// address.
func requesterTarget(t *models.Ticket) (notifyTarget, bool) {
	if t.RequesterID != nil && !t.RequesterID.Empty() {
		return notifyTarget{userID: *t.RequesterID}, true
	}
	if t.GuestEmail != nil && strings.TrimSpace(*t.GuestEmail) != "" {
		return notifyTarget{guest: strings.TrimSpace(*t.GuestEmail)}, true
	}
	return notifyTarget{}, false
}

func sameUser(a *models.UserID, b models.UserID) bool {
	return a != nil && !a.Empty() && !b.Empty() && *a == b
}

func (n *Notifier) followers(t *models.Ticket) []models.UserID {
	if n.DB == nil {
		return nil
	}
	ids, err := models.FollowerUserIDs(n.DB, t.ID, "")
	if err != nil {
		n.logger().Printf("escalated notifications: followers of ticket #%d: %v", t.ID, err)
		return nil
	}
	return ids
}

// TicketCreated emails the requester and the assignee.
func (n *Notifier) TicketCreated(t *models.Ticket) {
	var to []notifyTarget
	if r, ok := requesterTarget(t); ok {
		to = append(to, r)
	}
	if t.AssignedTo != nil && !t.AssignedTo.Empty() {
		to = append(to, notifyTarget{userID: *t.AssignedTo, agent: true})
	}
	subject := fmt.Sprintf("[%s] New Ticket: %s", t.Reference, t.Subject)
	body := "<p>A new support ticket has been created.</p>" + n.ticketLines(t)
	n.send(t, to, subject, body, threadAnchor, 0)
}

// ReplyCreated emails about a public reply. A reply with no author is the
// guest requester's own.
func (n *Notifier) ReplyCreated(t *models.Ticket, r *models.Reply) {
	if r == nil || r.IsInternal {
		return
	}
	var to []notifyTarget
	requester, hasRequester := requesterTarget(t)
	requesterWrote := false
	if t.RequesterID != nil && !t.RequesterID.Empty() {
		requesterWrote = sameUser(r.AuthorID, *t.RequesterID)
	} else {
		requesterWrote = r.AuthorID == nil || r.AuthorID.Empty()
	}
	if hasRequester && !requesterWrote {
		to = append(to, requester)
	}
	var assignee models.UserID
	if t.AssignedTo != nil {
		assignee = *t.AssignedTo
	}
	if !assignee.Empty() && !sameUser(r.AuthorID, assignee) {
		to = append(to, notifyTarget{userID: assignee, agent: true})
	}
	for _, f := range n.followers(t) {
		if sameUser(r.AuthorID, f) || f == assignee || (t.RequesterID != nil && *t.RequesterID == f) {
			continue
		}
		to = append(to, notifyTarget{userID: f, agent: true})
	}
	subject := fmt.Sprintf("Re: [%s] %s", t.Reference, t.Subject)
	body := "<p>A new reply has been added to your ticket.</p><blockquote>" + paragraphs(r.Body) + "</blockquote>"
	n.send(t, to, subject, body, threadReply, r.ID)
}

// TicketAssigned emails the new assignee, unless they assigned it to
// themselves.
func (n *Notifier) TicketAssigned(t *models.Ticket, agentID models.UserID, causer *models.UserID) {
	if agentID.Empty() || sameUser(causer, agentID) {
		return
	}
	subject := fmt.Sprintf("[%s] Ticket Assigned to You", t.Reference)
	body := "<p>A ticket has been assigned to you.</p>" + n.ticketLines(t)
	n.send(t, []notifyTarget{{userID: agentID, agent: true}}, subject, body, threadNotice, 0)
}

// StatusChanged emails the requester and followers, except the causer.
func (n *Notifier) StatusChanged(t *models.Ticket, oldStatus, newStatus int, causer *models.UserID) {
	if oldStatus == newStatus {
		return
	}
	var to []notifyTarget
	if r, ok := requesterTarget(t); ok && (r.userID.Empty() || !sameUser(causer, r.userID)) {
		to = append(to, r)
	}
	for _, f := range n.followers(t) {
		if sameUser(causer, f) || (t.RequesterID != nil && *t.RequesterID == f) {
			continue
		}
		to = append(to, notifyTarget{userID: f, agent: true})
	}
	label := statusLabel(newStatus)
	subject := fmt.Sprintf("[%s] Status Updated: %s", t.Reference, label)
	body := "<p>The status of your ticket has been updated.</p>" +
		"<p><strong>From:</strong> " + html.EscapeString(statusLabel(oldStatus)) + "<br><strong>To:</strong> " + html.EscapeString(label) + "</p>"
	n.send(t, to, subject, body, threadNotice, 0)
}

// Escalated emails the assignee.
func (n *Notifier) Escalated(t *models.Ticket) {
	if t.AssignedTo == nil || t.AssignedTo.Empty() {
		return
	}
	subject := fmt.Sprintf("[Escalated] [%s] %s", t.Reference, t.Subject)
	body := "<p>A ticket has been escalated.</p>" + n.ticketLines(t)
	n.send(t, []notifyTarget{{userID: *t.AssignedTo, agent: true}}, subject, body, threadNotice, 0)
}

// SLABreached emails the assignee. kind is first_response or resolution.
func (n *Notifier) SLABreached(t *models.Ticket, kind string) {
	if t.AssignedTo == nil || t.AssignedTo.Empty() {
		return
	}
	label := "Resolution"
	if kind == "first_response" {
		label = "First Response"
	}
	subject := fmt.Sprintf("[SLA Breach] [%s] %s SLA Breached", t.Reference, label)
	body := "<p>An SLA has been breached on ticket " + html.EscapeString(t.Reference) + ".</p>" +
		"<p><strong>Type:</strong> " + label + " SLA</p>" + n.ticketLines(t)
	n.send(t, []notifyTarget{{userID: *t.AssignedTo, agent: true}}, subject, body, threadNotice, 0)
}

type threadKind int

const (
	threadAnchor threadKind = iota
	threadReply
	threadNotice
)

func (n *Notifier) ticketLines(t *models.Ticket) string {
	return "<p><strong>Subject:</strong> " + html.EscapeString(t.Subject) +
		"<br><strong>Priority:</strong> " + html.EscapeString(statusLabel2(models.PriorityName[t.Priority])) + "</p>"
}

func statusLabel(status int) string {
	return statusLabel2(models.StatusName[status])
}

func statusLabel2(slug string) string {
	words := strings.Split(slug, "_")
	for i, w := range words {
		if w != "" {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

// paragraphs escapes a reply body and keeps its line breaks.
func paragraphs(body string) string {
	return strings.ReplaceAll(html.EscapeString(body), "\n", "<br>")
}

func (n *Notifier) send(t *models.Ticket, targets []notifyTarget, subject, body string, kind threadKind, replyID int64) {
	if n == nil || n.Mailer == nil || len(targets) == 0 {
		return
	}
	run := func() {
		defer func() {
			if rec := recover(); rec != nil {
				n.logger().Printf("escalated notifications: panic on ticket #%d: %v", t.ID, rec)
			}
		}()
		ctx := context.Background()
		seen := map[string]bool{}
		for _, target := range targets {
			address := target.guest
			if !target.userID.Empty() {
				if target.agent && n.Access != nil && !n.Access.CanViewUser(ctx, target.userID, t.ID, true) {
					continue
				}
				if n.UserEmail == nil {
					continue
				}
				a, err := n.UserEmail(ctx, target.userID)
				if err != nil {
					n.logger().Printf("escalated notifications: email of user %s: %v", target.userID, err)
					continue
				}
				address = a
			}
			address = strings.TrimSpace(address)
			key := strings.ToLower(address)
			if address == "" || seen[key] {
				continue
			}
			seen[key] = true
			if err := n.Mailer.SendMail(ctx, n.message(t, address, subject, body, kind, replyID, target.agent)); err != nil {
				n.logger().Printf("escalated notifications: send %q for ticket #%d: %v", subject, t.ID, err)
			}
		}
	}
	if n.Sync {
		run()
		return
	}
	go run()
}

func (n *Notifier) message(t *models.Ticket, to, subject, body string, kind threadKind, replyID int64, agent bool) Mail {
	root := email.BuildMessageID(t.ID, 0, n.domain())
	headers := map[string]string{}
	switch kind {
	case threadAnchor:
		headers["Message-ID"] = root
	case threadReply:
		headers["Message-ID"] = email.BuildMessageID(t.ID, replyID, n.domain())
		headers["In-Reply-To"] = root
		headers["References"] = root
	default:
		headers["In-Reply-To"] = root
		headers["References"] = root
	}
	url := ""
	if n.TicketURL != nil {
		url = n.TicketURL(t, agent)
	}
	n.emailOnce.Do(func() { n.email = NewEmailService(n.Branding, n.domain()) })
	htmlBody, err := n.email.RenderHTML(EmailData{
		Subject:   subject,
		Body:      template.HTML(body), //nolint:gosec // every dynamic part is escaped above
		TicketRef: t.Reference,
		TicketURL: url,
	})
	if err != nil {
		htmlBody = ""
	}
	text := htmlToText(body)
	if url != "" {
		text += "\n\nView the ticket: " + url
	}
	return Mail{
		To:      to,
		From:    n.From,
		ReplyTo: BuildSignedReplyTo(t.ID, n.InboundSecret, n.domain()),
		Subject: subject,
		Text:    text,
		HTML:    htmlBody,
		Headers: headers,
	}
}

// htmlToText turns the small notification bodies above into plain text.
func htmlToText(s string) string {
	r := strings.NewReplacer("<br>", "\n", "</p>", "\n\n", "<blockquote>", "", "</blockquote>", "\n", "<p>", "", "<strong>", "", "</strong>", "")
	return html.UnescapeString(strings.TrimSpace(r.Replace(s)))
}
