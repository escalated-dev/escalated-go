# Verified guest access

Guests (people without an account in your app) create tickets, start live
chats and look tickets up only after proving they can read the mailbox they
give. This follows escalated-laravel's `docs/guest-access.md`. It covers the
guest JSON API under `{RoutePrefix}/api/guest`, the mobile guest API under
`{RoutePrefix}/api/mobile/guest`, the guest pages under `{RoutePrefix}/guest`
and the widget under `{RoutePrefix}/widget`.

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
  against submissions, `lookup` against replies. One limiter (and store) per
  mount serves the guest API, the guest pages, the widget and the mobile
  routes, so a client IP has one budget across them.
- A `chat` code is refused with 403 while live chat is off (`chat_enabled`).

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

Guest responses send `Cache-Control: private, no-store` and
`Referrer-Policy: no-referrer`. Redact grant path segments and
`Authorization` headers from your access logs.

`esc.GuestAccess.Revoke(ctx, ticketID)` invalidates every grant for a ticket.
Call it when your own code changes a ticket's guest email.

## Mobile guest API

The routes a mobile app uses for guests, under `{RoutePrefix}/api/mobile/guest`
(default `/escalated/api/mobile/guest`). The canonical contract is
<https://github.com/escalated-dev/escalated-developer-context/blob/main/domain-model/mobile-guest-api.md>.

| Method | Path | Per-IP guest budget |
|---|---|---|
| POST | `/verification` | requests + submissions |
| POST | `/lookup` | requests + replies |
| POST | `/tickets` | requests + submissions |
| GET | `/tickets/{token}` | requests |
| POST | `/tickets/{token}/replies` | requests + replies |

- `verification` and `lookup` are the same handlers as on the guest API and the
  widget (see above).
- `POST tickets` takes JSON or a form: `name`, `email`, `subject`,
  `description`, optional `priority` (`low`, `medium`, `high`, `urgent`,
  `critical`; default `medium`) and `department_id`, plus `verification_id`
  (a UUID) and `verification_code`. Field errors are 422 before the code is
  checked; a wrong, spent, expired or other-purpose code is 422 on
  `verification_code` and creates nothing. Other fields (status, assignee,
  metadata, ...) are ignored. 201 `{data: <mobile ticket>, message}` with
  `guest_access_token` and `guest_access_expires_at`.
- `GET tickets/{token}`: 200 `{data: <mobile ticket>}` with the public replies,
  newest first. Staff appear as `{id: 0, name, email: ""}`; internal notes
  never appear.
- `POST tickets/{token}/replies` with `{email, body}`: a closed ticket is 422,
  missing fields 422, an email (trimmed, case-insensitive) other than the
  ticket's guest email 403, else 201 `{message: "Reply sent."}`.
- `{token}` must be a live ticket grant from any guest surface. An expired,
  rotated, malformed or over-long grant, a chat token or a legacy guest token
  is a 404.
- The routes are registered with the rest of the JSON API, which this package
  always mounts: there is no switch to turn the API off.

## Lookup

After a `lookup` code, `POST lookup` with
`{email, reference, verification_id, verification_code}`. The answer is
`{data: [{reference, subject, guest_access_token, expires_at}]}`, at most 20
tickets whose reference and guest email both match. Each match gets a fresh
grant. The result may be empty, and the code is consumed either way.

## Live chat

`POST /widget/chat/start` needs a `chat` code for the visitor's email. Its `id`
is a random chat token that works like Laravel's chat grant: only its SHA-256
is stored (on the chat session), it expires `GuestAccessConfig.TTL` after the
chat starts (`expires_at`), and after that every `/chat/{id}/...` route, and
`/chat/sessions/{ref}/...` with it as `X-Guest-Token`, answers 404. The chat's
ticket keeps no guest token, so a chat token cannot open ticket routes. Chats
started before this release keep working: their next request moves the
token's hash to the session with a full lifetime.

## Legacy guest tokens

Tickets created before this change carry a permanent `guest_token`. During the
transition such a token keeps opening its ticket on the guest API and on the
widget's `GET /tickets/lookup?ref=&token=`, until a grant is issued for the
ticket (which clears it). Set `GuestAccess.RejectLegacyTokens = true` to stop
accepting them now; Laravel no longer accepts them at all.

## Differences from Laravel

- Legacy tokens are not cut off at once (see above).
- The chat token is a random token rather than a sealed grant, so a ticket link does not rotate it.
- Consuming a code and the ticket write are not one database transaction: the
  code is consumed first and handed back if the write fails.
- There is no `external_reference`; lookup matches the ticket reference.
- There is no API-layer rate limiter (no `X-RateLimit-*` headers); the per-IP
  guest budgets apply. The mobile routes cannot be switched off with the API.
- Guest tickets have no on/off switch, so the contract's 403 for disabled guest
  tickets never applies.
- Uploads are not stored yet: a mobile create or reply that sends files is a
  422 on `attachments`. Reply attachments (from email or imports) are listed
  with `url: null`, as there is no guest-authorized download route.
- There is no guest identity policy and no ticket channel column: guest
  tickets get no policy applied, and the mobile ticket reports channel `web`.
