# The shared pages

With `UIEnabled` (the default), the routes under `/agent`, `/admin` and
`/tickets` render the Vue pages of `@escalated-dev/escalated` through Inertia.
Each page gets the props escalated-laravel's controller passes it, with the same
key names and shapes, so the shared frontend works here as it does on Laravel:

- Lists are Laravel's paginator: `tickets.data`, `current_page`, `last_page`,
  `per_page` (15 by default, `?per_page=` up to 100), `total`, `links`, `from`,
  `to`, `path` and the page URLs. `?page=` picks the page.
- Status and priority are slugs (`"open"`, `"urgent"`), not the integers the
  JSON API uses. List filters accept either.
- Relations are nested objects: `requester {name, email}`, `assignee {id, name,
  email}`, `department {id, name}`, `tags [{id, name, color}]`.
- A ticket page path takes the ticket's reference, which is what the pages put
  in their links, or its id.

Props are built field by field, so nothing a model holds for internal use
(guest tokens, storage paths) reaches a page.

## Names and emails

Escalated owns no users table. Set `Config.UserName` to show people by name, and
`Config.UserEmail` (also used by the notification emails) to show their address:

```go
cfg.UserName = func(ctx context.Context, id models.UserID) string {
    u, err := users.Find(ctx, string(id))
    if err != nil {
        return ""
    }
    return u.Name
}
```

Without them, staff show as "Support", a signed-in requester as "Unknown", and
no email is shown. Guests are named from their ticket. A customer never sees a
staff member's email or id, only a display name.

## Writes from the pages

The pages post with Inertia (`X-Inertia: true`), as JSON or a form, to the
routes escalated-laravel names. A page visit is answered the Inertia way: a
`303` back to the page it came from (the `Referer`, same host only), or the
page again with an `errors` prop when validation fails. A client without
`X-Inertia` keeps getting JSON (`422 {message, errors}` on validation errors).
Status and priority are taken as slugs or integers. File uploads are refused
with an `attachments` error: this package stores no uploads yet.

| Laravel route name | Route |
|---|---|
| `escalated.agent.tickets.update` | `PUT /agent/tickets/{ticket}` (subject, description, ticket_type) |
| `escalated.agent.tickets.reply` / `.note` | `POST /agent/tickets/{ticket}/reply`, `/note` |
| `escalated.agent.tickets.assign` | `POST /agent/tickets/{ticket}/assign` |
| `escalated.agent.tickets.status` / `.priority` | `POST /agent/tickets/{ticket}/status`, `/priority` |
| `escalated.agent.tickets.tags` | `POST /agent/tickets/{ticket}/tags` (`tag_ids` is the whole set) |
| `escalated.agent.tickets.department` | `POST /agent/tickets/{ticket}/department` |
| `escalated.agent.tickets.macro` | `POST /agent/tickets/{ticket}/macro` |
| `escalated.agent.tickets.follow` | `POST /agent/tickets/{ticket}/follow` (toggles) |
| `escalated.agent.tickets.pin` | `POST /agent/tickets/{ticket}/replies/{reply}/pin` (internal notes) |
| `escalated.agent.tickets.custom-action` | `POST /agent/tickets/{ticket}/actions/{action}` |
| `escalated.customer.tickets.store` | `POST /tickets` (303 to the new ticket) |
| `escalated.customer.tickets.reply` | `POST /tickets/{ticket}/reply` |
| `escalated.customer.tickets.close` / `.reopen` | `POST /tickets/{ticket}/close`, `/reopen` |
| `escalated.customer.tickets.rate` | `POST /tickets/{ticket}/rate` |

The admin ticket screen has the same writes under `/admin/tickets/{ticket}`
(`escalated.admin.tickets.*`), plus `split` (`reply_id`, optional `subject`;
a page visit goes to the new ticket) and `merge` (`target_reference`). Bulk
actions are at `POST /agent/tickets/bulk` and `POST /admin/tickets/bulk`
(`ticket_ids`, `action` = status, priority, assign, tags, department or
delete, and `value`); only admins may delete. The ticket panels (subjects,
links, side conversations) are mounted under both.

The admin pages list agents for their assignee controls from
`Config.SkillAgentDirectory`; without it the list is empty.

`{ticket}` is the reference or the id. Escalated ships no Ziggy route list;
point your `route()` names at these paths under your `RoutePrefix`.

## Pages rendered

| Page | Route |
|------|-------|
| `Agent/Dashboard` | `GET /agent/` |
| `Agent/TicketIndex` | `GET /agent/tickets` |
| `Agent/TicketShow` | `GET /agent/tickets/{reference}` |
| `Customer/Index` | `GET /tickets` |
| `Customer/Create` | `GET /tickets/create` |
| `Customer/Show` | `GET /tickets/{reference}` |
| `Admin/SlaPolicies/Index` | `GET /admin/sla-policies` |
| `Admin/Tickets/Index` | `GET /admin/tickets` |
| `Admin/Tickets/Show` | `GET /admin/tickets/{reference}` |
| `Admin/SlaPolicies/Form` | `GET /admin/sla-policies/create`, `/{id}/edit` |
| `Admin/EscalationRules/Index`, `Form` | `GET /admin/escalation-rules`, `/create`, `/{id}/edit` |
| `Admin/KnowledgeBase/Articles/Index`, `Form` | `GET /admin/kb-articles`, `/create`, `/{id}/edit` |
| `Admin/KnowledgeBase/Categories/Index` | `GET /admin/kb-categories` |
| `Customer/KnowledgeBase/Index`, `Article` | `GET /kb`, `/kb/{slug}` (and `POST /kb/{slug}/feedback`) |
| `Admin/Users/Index` | `GET /admin/users` (needs `Config.UserDirectory`) |
| `Admin/Macros/Index` | `GET /admin/macros` |
| `Admin/CannedResponses/Index` | `GET /admin/canned-responses` |
| `Admin/Webhooks/Index`, `Form`, `DeliveryLog` | `GET /admin/webhooks`, `/create`, `/{id}/edit`, `/{id}/deliveries` |

The admin config lists (escalation rules, macros, canned responses, webhooks
and the delivery log) answer the same URL two ways: the page for a page visit
(`X-Inertia`) or a browser asking for HTML, and the JSON they always returned
for anyone else. Their writes (`store`, `update` as `PUT` or `PATCH`,
`destroy`, delivery `retry`) answer a page visit with a 303 to the list (or
back, for a retry), or the page again with an `errors` prop; API clients get
the answers they always had.

The knowledge base admin pages are at Laravel's resource paths
(`/admin/kb-articles`, `/admin/kb-categories`); the older `/admin/kb/...` JSON
routes stay. Their forms send ids as `""` when unset, which is read as none.
The reader pages follow the `knowledge_base_enabled` (404 when off) and
`knowledge_base_public` (403 for a visitor when off) settings, and
`knowledge_base_feedback_enabled` switches the helpfulness vote.

Macros created in the shared editor use its action names (`status`,
`priority`, `assign`, `tags`, `department`, `reply`, `note`), which now run
alongside this package's own (`change_status`, `add_note`, ...). Tags and
departments are taken by id or by name.

The others this package renders are the departments, tags, skills, users, audit
log and newsletter screens. `notYetRendered` in `page_name_parity_test.go` lists
every shipped page not rendered yet.

## Not supported yet

- The queue's `following` and `tag_ids` filters, and the customer list's `tag`,
  `has_attachments`, `created_after` and `created_before` filters, are echoed
  back but not applied: the store has no such filters.
- Tickets keep no channel column, so a ticket shows `channel: "chat"` only
  while it is a live chat (or, on the ticket screen, when it has a chat
  session).
- Roles and API tokens: authentication and users stay with the host, so the
  `Admin/Roles/*` and `Admin/ApiTokens/Index` pages are not rendered.
- SLA policies have no business-hours mode: `business_hours_only` is always
  `false`.
- Shared props (`auth.user`, `escalated.*`) and Ziggy's `route()` come from the
  host's Inertia setup.
- Presence (`escalated.*.tickets.presence`) and typing indicators are not
  served yet.
- Snooze and saved views: the store has methods for them, but the migrations
  create no columns or table for them, so `savedViews` is always empty and the
  snooze and saved-view routes are not mounted.
