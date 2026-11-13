# API routes

## API Routes

All routes are prefixed with `RoutePrefix` (default `/escalated`).

### JSON API (always mounted)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/tickets` | List tickets (with filters) |
| `POST` | `/api/tickets` | Create a ticket |
| `GET` | `/api/tickets/{id}` | Get ticket with replies and activities |
| `PATCH` | `/api/tickets/{id}` | Update a ticket |
| `POST` | `/api/tickets/{id}/replies` | Add a reply |
| `POST` | `/api/tickets/{id}/subjects` | Attach a ticket subject (`type`, `id`, optional `role`) |
| `DELETE` | `/api/tickets/{id}/subjects/{subject}` | Detach a subject link by join-row id |
| `GET` | `/api/departments` | List departments |
| `GET` | `/api/tags` | List tags |

The ticket, department and tag routes above require `AgentCheck` or
`AdminCheck` and return 403 to anyone else. With `Config.APITokensEnabled` they also
accept an admin-issued API token as `Authorization: Bearer <token>` (see
[API tokens](api-tokens.md)). The `/api/auth/*`, `/api/guest/*`
and `/api/kb/*` routes are public.

Guests verify their email with a code first: `POST /api/guest/verification`,
then `POST /api/guest/tickets` with the code returns an expiring grant that
opens `GET /api/guest/tickets/{grant}`, its `/replies` and `/rate`;
`POST /api/guest/lookup` finds tickets by reference. Configure `Config.Mailer`
and `Config.GuestAccess` (see [verified guest access](guest-access.md)).

`POST /api/guest/tickets` is rate-limited per client IP: 5 tickets a minute by
default, then 429 with `Retry-After`. Configure it with `Config.GuestRateLimit`
(`TicketsPerMinute`, `RepliesPerMinute`, `Disabled`, a shared `Store` for
several instances, or a `ClientIP` func). The client IP is `r.RemoteAddr`, so
behind a proxy every guest shares one limit unless a real-IP middleware that
trusts only your proxies runs first, or `ClientIP` is set. If you mount
`handlers.WidgetHandler.CreateTicket` yourself, wrap it with
`middleware.NewGuestRateLimiter(cfg).Middleware(middleware.GuestTicketScope)`.

For Chi behind a reverse proxy, register its client-IP middleware with your
actual proxy CIDRs before `router.MountChi`, and configure `ClientIP` before
constructing Escalated:

```go
import chimiddleware "github.com/go-chi/chi/v5/middleware"

// Replace these loopback ranges with your own trusted proxy ranges.
r.Use(chimiddleware.ClientIPFromXFF("127.0.0.1/32", "::1/128"))
cfg.GuestRateLimit.ClientIP = func(r *http.Request) string {
    return chimiddleware.GetClientIP(r.Context())
}
```

Chi 5.4 requires explicit trusted CIDRs for `ClientIPFromXFF`. A direct client
outside those ranges is counted by its connection address, even if it sends
`X-Forwarded-For`. The default Escalated configuration also uses the connection
address and does not read forwarded headers.

### Customer UI (when `UIEnabled: true`)

The UI routes render the pages of `@escalated-dev/escalated` with the props
escalated-laravel passes them; see [the shared pages](ui-pages.md).
Every page carries the route names the pages call (a `ziggy` prop), so no
hand-written `route()` mapping is needed: add
`<script>{{ .escalatedRouteHelper }}</script>` to your root template (see
[route names](ui-pages.md#route-names)).

These routes require a signed-in user, meaning `UserIDFunc` returns a
non-empty id, and return 401 otherwise. A customer can view and reply only to
tickets they requested. Any other ticket returns 403.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/tickets` | My tickets |
| `GET` | `/tickets/create` | New ticket form |
| `POST` | `/tickets` | Submit a ticket |
| `GET` | `/tickets/{id}` | View ticket (`{id}` is the reference or the id) |
| `POST` | `/tickets/{id}/replies` | Reply to ticket |
| `POST` | `/tickets/{id}/reply`, `/close`, `/reopen`, `/rate` | The ticket screen's writes |

### Attachments (always mounted)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/attachments/{id}/download` | Download an attachment |

Agents and admins can download any attachment. A signed-in customer can
download attachments on tickets they requested, but not attachments on
internal notes. Anyone else gets 401 or 403.

### Agent UI (requires `AgentCheck`)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/agent/` | Agent dashboard |
| `GET` | `/agent/tickets` | Ticket queue |
| `GET` | `/agent/tickets/{id}` | Ticket detail |
| `POST` | `/agent/tickets/{id}/assign` | Assign ticket |
| `POST` | `/agent/tickets/{id}/replies` | Reply / internal note |
| `POST` | `/agent/tickets/{id}/status` | Change status |
| `POST` | `/agent/tickets/{id}/reply`, `/note`, `/priority`, `/tags`, `/department`, `/macro`, `/follow`, `/replies/{reply}/pin`; `PUT /agent/tickets/{id}` | The ticket screen's writes; see [the shared pages](ui-pages.md) |
| `POST` | `/agent/tickets/{id}/subjects` | Attach a ticket subject |
| `DELETE` | `/agent/tickets/{id}/subjects/{subject}` | Detach a ticket subject |

Live chat console (agents): `GET /agent/chat/sessions`, `POST /agent/chat/sessions/{id}/accept`, `/message`, `/end`, where `{id}` is the chat ticket. Set `Config.ChatAgentOnline` (presence) and `Config.ChatAgentName` for the widget.

### Admin UI (requires `AdminCheck`)

| Method | Path | Description |
|--------|------|-------------|
| `GET/POST/PATCH/DELETE` | `/admin/departments` | Manage departments |
| `GET/POST/DELETE` | `/admin/tags` | Manage tags |
| `GET/POST/DELETE` | `/admin/sla-policies` | Manage SLA policies |
| `GET` | `/admin/audit-log` | Audit log of department, tag and SLA policy changes (filters: `user_id`, `action`, `auditable_type`, `date_from`, `date_to`) |
| `GET` | `/admin/tickets`, `/admin/tickets/{ticket}` | Admin ticket queue and screen, with the agent screen's writes plus `split`; see [the shared pages](ui-pages.md) |
| `POST` | `/agent/tickets/bulk`, `/admin/tickets/bulk` | Bulk status, priority, assign, tags, department; delete (admins) |
| `GET` | `/admin/tickets/merge-search?q=` | Unmerged tickets to merge into (reference or subject, at most 10) |
| `POST` | `/admin/tickets/{ticket}/merge` | Merge `{ticket}` (id or reference) into `{target_reference}`: replies move to the target, both get a system note, the source is closed with `merged_into_id` |
| `GET/PUT` | `/admin/settings/public-tickets` | Runtime guest-policy mode (unassigned / guest_user / prompt_signup). See [docs.escalated.dev/public-tickets](https://docs.escalated.dev/public-tickets). |
| `GET/PUT` | `/admin/settings/widget` | Sites allowed to embed the widget (`widget_allowed_origins`). See [embedding the widget](embedding-the-widget.md). |
