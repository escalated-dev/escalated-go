# Features

## Features

- Tickets with statuses, priorities, types, and SLA tracking
- Replies (public, internal notes, system messages)
- Departments and tags
- SLA policies with per-priority response/resolution targets
- Agent dashboard and admin configuration
- Inertia.js UI or headless JSON API mode
- PostgreSQL and SQLite support
- Framework-agnostic HTTP handlers
- Embedded SQL migrations

### Additional Features

- **Ticket splitting** — Split a reply into a new standalone ticket while preserving the original context
- **Ticket snooze** — Snooze tickets with presets (1h, 4h, tomorrow, next week); a background goroutine scheduler auto-wakes them on schedule
- **Saved views / custom queues** — Save, name, and share filter presets as reusable ticket views
- **Notification emails** — Requesters, assignees and followers are emailed about new tickets, replies, assignments, status changes, escalations and SLA breaches through your mailer, threaded by `Message-ID` (see [notification emails](notification-emails.md))
- **Embeddable support widget** — Lightweight `<script>` widget with KB search, ticket form and live chat, mounted by `Config.EnableWidget`; other sites you list can embed it (see [embedding the widget](embedding-the-widget.md))
- **Email threading** — Outbound emails include proper `In-Reply-To` and `References` headers for correct threading in mail clients
- **Inbound email replies** — `email.InboundEmailService` turns provider webhooks into replies or new tickets. With an inbound secret set, only the signed Reply-To address links mail to a ticket; without one, the `In-Reply-To` / `References` headers and the `[ESC-...]` subject reference are used. A matched email becomes a reply only when `From` is the ticket's requester (the guest email, or the requester's email via `WithRequesterEmailResolver`), and it is posted as that requester. A requester's reply to a resolved or closed ticket reopens it. Anyone else, including an address belonging to an agent, gets a new ticket
- **Branded email templates** — Configurable logo, primary color, and footer text for all outbound emails
- **Real-time updates** — Server-Sent Events (SSE) endpoint for live ticket updates with automatic polling fallback
- **Knowledge base toggle** — Enable or disable the public knowledge base from admin settings
- **Roles & permissions** — Optional, per install (`Config.Access.RolesPermissions`): every admin, agent and API route then needs its permission from the user's Escalated roles, managed on Admin > Roles (see [access control](access-control.md))
- **Two-factor authentication** — Optional, per install (`Config.Access.TwoFactor`): staff enrol a TOTP app and are challenged after your login, with recovery codes and an admin "require two-factor" setting (see [access control](access-control.md))
- **Single sign-on** — Optional, per install (`Config.Access.SSO`): SAML 2.0, OAuth 2 / OIDC or JWT, picked in Admin > Settings > SSO and signed in through your session (see [access control](access-control.md))
- **Plugins** — Native Go plugins registered in `Config.Plugins.Native`, and `@escalated-dev/plugin-sdk` plugins through the Node plugin runtime, managed on Admin > Plugins (see [plugins](plugins.md))
- **Ticket subjects** — Attach host-app entities (Project, Customer, …) a ticket is about; polymorphic links with UI presentation via `TicketSubject` contract
