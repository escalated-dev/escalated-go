# Notification emails

Escalated emails requesters and agents when something happens on a ticket. It
renders each message and hands it to your mailer; nothing is sent until you
configure one.

## Setup

```go
cfg.Mailer = myMailer // services.MailSender, also used for guest verification codes
cfg.MailFrom = "support@example.com"
cfg.UserEmail = func(ctx context.Context, id models.UserID) (string, error) {
    return users.EmailByID(ctx, id) // your users table
}
cfg.Notifications = escalated.NotificationConfig{
    BaseURL:       "https://support.example.com", // links in the emails
    Domain:        "support.example.com",          // Message-IDs; defaults to BaseURL's host
    InboundSecret: os.Getenv("ESCALATED_INBOUND_SECRET"), // signed Reply-To, optional
    Branding:      services.EmailBranding{CompanyName: "Acme Support", LogoURL: "https://example.com/logo.png"},
}
```

`Mailer.SendMail(ctx, services.Mail)` receives `To`, `From`, `ReplyTo`,
`Subject`, `Text`, `HTML` and `Headers` (`Message-ID`, `In-Reply-To`,
`References`). Your implementation owns the transport.

- Delivery runs in a goroutine, so a slow mail server does not hold up the
  request. A mailer error is logged and never fails the ticket operation.
- `Notifications.Disabled` turns these emails off while keeping `Mailer` for
  guest verification codes.
- Escalated owns no users table. Agents, followers and signed-in requesters
  are emailed at the address `UserEmail` returns; without `UserEmail` only
  guest requesters are emailed. When ticket visibility is restricted, agents
  who cannot see the ticket are skipped.

## Who gets what

The rules follow escalated-laravel's notification listeners.

| Event | Recipients |
|---|---|
| Ticket created | The requester (the first message of the thread), and the assignee if the ticket was created assigned |
| Public reply | The requester unless they wrote it; the assignee unless they wrote it; followers other than the author, requester and assignee |
| Ticket assigned | The new assignee, unless they assigned it to themselves |
| Status changed | The requester and the followers, except whoever changed it |
| Ticket escalated | The assignee |
| SLA breached | The assignee (from `Escalated.CheckSLA`) |

Internal notes and live chat messages email nobody. Nobody is emailed about
their own action. The requester is the host user for a signed-in ticket, or
the guest email for a guest ticket. A reply with no author is the guest's own.

The notices go out from the ticket service, so they follow the mounted routes,
macros and escalation rules that change tickets through it, and
`esc.NewTicketService()` in your own code. Replies written straight to the
database by workflow and automation actions send nothing.

## Threading

- The new-ticket email sets `Message-ID: <ticket-{id}@{Domain}>`, the anchor of
  the thread.
- A reply email sets `Message-ID: <ticket-{id}-reply-{reply_id}@{Domain}>` and
  `In-Reply-To` / `References` pointing at the anchor.
- Assignment, status, escalation and SLA emails set `In-Reply-To` /
  `References` to the anchor.

With `InboundSecret` set, every email also has a signed
`Reply-To: reply+{id}.{hmac8}@{Domain}`, which the inbound email handlers
verify to put an emailed reply back on its ticket.

## Links

With `BaseURL` set, agents and followers get
`{BaseURL}{RoutePrefix}/agent/tickets/{id}` and signed-in requesters
`{BaseURL}{RoutePrefix}/tickets/{id}`. Guests get no link: their access is the
grant from [verified guest access](guest-access.md).
