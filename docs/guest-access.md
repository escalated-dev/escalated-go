# Verified guest access

Guests (people without an account in your app) create tickets, start live
chats and look tickets up only after proving they can read the mailbox they
give. This follows escalated-laravel's `docs/guest-access.md`. It covers the
guest JSON API under `{RoutePrefix}/api/guest` and the widget under
`{RoutePrefix}/widget`.

## What you must configure

```go
cfg.Mailer = myMailer              // services.MailSender: sends the codes
cfg.MailFrom = "support@example.com"
cfg.GuestAccess.Secret = os.Getenv("ESCALATED_GUEST_SECRET") // long, random, the same on every instance
```

- **Mailer.** Escalated renders each email and calls
  `Mailer.SendMail(ctx, services.Mail{To, From, Subject, Text, ...})`; your
  implementation owns the transport (SMTP, an API, a queue). With no Mailer,
  `services.LogMailSender` logs the recipient and subject and sends nothing,
  so no guest can complete verification. The code is never logged.
- **Secret.** It keys the stored code digests and seals the grants. When it is
  empty a random key is generated at startup and a warning is logged: grants
  then stop working on restart and do not work across instances.
- **Migrations.** `Migrate` / `MigrateSQLite` add the
  `guest_verifications` table and four nullable ticket columns
  (`guest_verified_email`, `guest_email_verified_at`, `guest_access_hash`,
  `guest_access_expires_at`). Existing tickets are untouched.
- **Clients.** Guest clients must run the code step. The bundled widget
  (`@escalated-dev/escalated` 0.12.3, shipped with this package) does.

## Email codes

`POST verification` with `{ "email": "...", "purpose": "ticket" }`. The other
purposes are `chat` and `lookup`. The answer is 202
`{ verification_id, expires_in: 600, message }`; only the email holds the
eight-digit code.

- A code is bound to the normalised email and the purpose, expires after ten
  minutes, allows five guesses and is consumed once. Wrong guesses stay
  counted. If the write a code authorised fails, the code is handed back.
- Only an HMAC digest of the code is stored.
- Code delivery is budgeted: 3 an hour per mailbox from one client IP, 10 an
  hour per mailbox across clients (`CodesPerClientPerHour`,
  `CodesPerMailboxPerHour`). Over budget is 429 with `Retry-After`. The
  counters use `Config.GuestRateLimit.Store` (in memory per process when nil)
  and its `ClientIP`.
- The routes also count against the per-IP guest limits: `verification`
  against submissions, `lookup` against replies.

## Tickets and grants

Send `verification_id` and `verification_code` with the ticket.

| Surface | Request | Answer |
|---|---|---|
| `POST /api/guest/tickets` | `{name, email, subject, description, priority?}` (`guest_name` / `guest_email` still accepted) | 201 `{data: {..., guest_access_token, guest_access_expires_at, guest_token}}`. `guest_token` carries the same grant for older clients. |
| `POST /widget/tickets` | `{name, email, subject, description, department_id?, metadata?}` | 201 `{message, reference, ticket_reference, guest_access_token, expires_at}` |

The grant is an opaque, sealed (AES-256-GCM), expiring token. Do not parse it.
`GuestAccess.TTL` sets its lifetime: 24 hours by default, clamped to 5 minutes
through 7 days. The ticket stores only a SHA-256 of the grant's nonce, so
issuing a new grant (for example through lookup) invalidates the earlier ones.

Where a grant is read:

- `GET /api/guest/tickets/{grant}`: the ticket and its public replies
  (`body`, `author`, `is_agent`, `created_at`; no author ids).
- `POST /api/guest/tickets/{grant}/replies`: `{body, email}`. The email must
  be the ticket's guest email (403 otherwise); a closed ticket is 422.
- `POST /api/guest/tickets/{grant}/rate`: CSAT.
- `GET /widget/tickets/{reference}` with `Authorization: Bearer <grant>`:
  `{reference, subject, status, status_label, created_at, department,
  replies}`. The grant must belong to that reference; anything else is 404.

Guest responses send `Cache-Control: no-store, private` and
`Referrer-Policy: no-referrer`. Redact grant path segments and
`Authorization` headers from your access logs.

`esc.GuestAccess.Revoke(ctx, ticketID)` invalidates every grant for a ticket.
Call it when your own code changes a ticket's guest email.

## Lookup

After a `lookup` code, `POST lookup` with
`{email, reference, verification_id, verification_code}`. The answer is
`{data: [{reference, subject, guest_access_token, expires_at}]}`, at most 20
tickets whose reference and guest email both match. Each match gets a fresh
grant. The result may be empty, and the code is consumed either way.

## Live chat

`POST /widget/chat/start` needs a `chat` code for the visitor's email. The chat
keeps its server-issued id (the chat ticket's random guest token) as the
credential for `/chat/{id}/...`; it is not an expiring grant as in Laravel.

## Legacy guest tokens

Tickets created before this change carry a permanent `guest_token`. During the
transition such a token keeps opening its ticket on the guest API and on the
widget's `GET /tickets/lookup?ref=&token=`, until a grant is issued for the
ticket (which clears it). Set `GuestAccess.RejectLegacyTokens = true` to stop
accepting them now; Laravel no longer accepts them at all.

## Differences from Laravel

- Legacy tokens are not cut off at once (see above).
- Chat keeps its server-issued token rather than an expiring chat grant.
- Consuming a code and the ticket write are not one database transaction: the
  code is consumed first and handed back if the write fails.
- There is no `external_reference`; lookup matches the ticket reference.
- There is no browser guest page; the guest surface is the JSON API.
