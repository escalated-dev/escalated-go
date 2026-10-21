package services

import (
	"context"
	"log"
)

// Mail is one outbound email Escalated asks the host to deliver.
type Mail struct {
	To      string
	From    string
	ReplyTo string
	Subject string
	// Text is the plain-text body. HTML, when set, is an alternative part.
	Text string
	HTML string
	// Headers are extra headers to set (Message-ID, In-Reply-To, References).
	Headers map[string]string
}

// MailSender delivers Escalated's outbound email: guest verification codes
// and ticket notifications. Escalated renders the message; the host owns the
// transport (SMTP, an API, a queue). Set it as Config.Mailer.
type MailSender interface {
	SendMail(ctx context.Context, m Mail) error
}

// LogMailSender is the default MailSender when the host configures none. It
// sends nothing and logs the recipient and subject (never the body, which can
// hold a verification code), so a missing mailer shows up in the logs instead
// of failing requests. Guest verification cannot complete with it: configure
// a real MailSender before enabling guest tickets.
type LogMailSender struct {
	Logger *log.Logger
}

// SendMail logs the dropped message and reports success.
func (l LogMailSender) SendMail(_ context.Context, m Mail) error {
	logger := l.Logger
	if logger == nil {
		logger = log.Default()
	}
	logger.Printf("escalated: no Config.Mailer configured; not sending %q to %s", m.Subject, m.To)
	return nil
}
