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

The pages post with Inertia (`X-Inertia: true`), as JSON or a form. The ticket
form (`POST /tickets`) answers a page the Inertia way: a `303` to the new
ticket, or the form again with an `errors` prop when validation fails. A client
without `X-Inertia` keeps getting JSON (`201` with the ticket, or
`422 {message, errors}`). It takes `priority` as a slug or the integer.

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
- SLA policies have no business-hours mode: `business_hours_only` is always
  `false`.
- Shared props (`auth.user`, `escalated.*`) and Ziggy's `route()` come from the
  host's Inertia setup.
