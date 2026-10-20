# Embedding the widget on other sites

The support widget can run on a site other than the one your Go app serves
Escalated from, such as a store, a marketing site or a customer portal. That
site loads one script from your app. Nothing is granted until you list the
sites allowed to do this.

## 1. Allow the embedding sites

List them in `handlers.WidgetConfig.AllowedOrigins`, comma or newline
separated. `handlers.DefaultWidgetConfig()` reads the
`ESCALATED_WIDGET_ALLOWED_ORIGINS` environment variable into it:

```go
widgetCfg := handlers.DefaultWidgetConfig()
widgetCfg.Enabled = true
widgetCfg.AllowedOrigins = "https://shop.example.com, https://*.example.org"
```

Admins can add more without a redeploy through `PUT /admin/settings/widget`
(the `widget_allowed_origins` setting, one origin per line or comma
separated). The two lists are combined.

Each entry is an origin: `scheme://host[:port]`, http or https, with no path,
query or user. Case and default ports (`:80`, `:443`) are ignored, and a
trailing `/` is allowed. `https://*.example.org` matches every subdomain of
`example.org`, at any depth, but not `example.org` itself and not
`http://`. A bare `*` is never accepted: in the config list it is ignored, and
the admin endpoint rejects it with 422.

## 2. Mount the widget routes

### With MountChi or MountStdlib

Set `EnableWidget` and configure `Widget`:

```go
cfg := escalated.DefaultConfig()
cfg.EnableWidget = true
cfg.Widget.Enabled = true
cfg.Widget.AllowedOrigins = "https://shop.example.com"
cfg.Widget.PrimaryColor = "#4F46E5"
cfg.Widget.WelcomeMessage = "Hi! How can we help?"
```

The routers then mount the widget under `{RoutePrefix}/widget` (for example
`/escalated/widget`): `embed.js` on its own, and every other widget route
behind `handlers.WidgetCORS`, with `esc.TicketAccessMiddleware` inside it.
`EnableWidget` is off by default, so a host that wires the handlers itself
(below) is not affected. `Widget.Enabled` still turns the endpoints on and
off: with it false, `/config` answers 403 `{"enabled": false}` and the other
routes 503.

### Wiring the handlers yourself

Put every widget route behind `handlers.WidgetCORS`, and mount `embed.js`
outside it. WidgetCORS has to see `OPTIONS` requests, so wrap the whole widget
prefix rather than single routes.

With chi:

```go
widget := handlers.NewWidgetHandler(widgetCfg, esc.Store, services.NewTicketService(esc.Store))
widget.DB = esc.Config.DB // the knowledge base; leave nil for none
chat := handlers.NewWidgetChatHandler(widgetCfg, esc.Store, chatSessions, chatAvailability)

r.Get("/support/widget/embed.js", handlers.WidgetScript)
r.Route("/support/widget", func(r chi.Router) {
    r.Use(handlers.WidgetCORS(widgetCfg, esc.Store))
    r.Get("/config", widget.Config)
    r.Get("/articles", widget.SearchArticles)
    r.Get("/articles/{slug}", widget.ShowArticle)
    r.Post("/tickets", widget.CreateTicket)
    r.Get("/tickets/lookup", widget.LookupTicket)
    r.Get("/chat/availability", chat.Availability)
    r.Post("/chat/start", chat.StartChat)
    r.Get("/chat/{id}/messages", chat.Messages)
    r.Post("/chat/{id}/messages", chat.PostMessage)
    r.Post("/chat/{id}/typing", chat.Typing)
    r.Post("/chat/{id}/end", chat.End)
    r.Post("/chat/{id}/rate", chat.Rate)
    // Older routes, reference + X-Guest-Token header:
    r.Post("/chat/sessions/{ref}/messages", chat.SendMessage)
    r.Post("/chat/sessions/{ref}/end", chat.EndChat)
})
```

With `net/http`:

```go
w := http.NewServeMux()
w.HandleFunc("GET /support/widget/config", widget.Config)
w.HandleFunc("POST /support/widget/tickets", widget.CreateTicket)
// ... the other widget routes

mux.HandleFunc("GET /support/widget/embed.js", handlers.WidgetScript)
mux.Handle("/support/widget/", handlers.WidgetCORS(widgetCfg, esc.Store)(w))
```

Put WidgetCORS outside anything that can answer early, such as a rate limiter
(`middleware.NewGuestRateLimiter`), so those answers carry the CORS headers
too. If you scope the widget with `esc.TicketAccessMiddleware`, put that
inside WidgetCORS.

### What the widget calls

Paths are relative to the widget prefix.

| Route | Notes |
|---|---|
| `GET /config` | `enabled`, `color`, `position`, `greeting`, `departments` (`[{id, name}]`, active only), `kb_enabled`, `guest_tickets_enabled`, `status_lookup_enabled: false`, `accepts_metadata: true`, plus the original `title`, `subtitle`, `primary_color`, `logo_url`, `welcome_message`. |
| `GET /articles?q=` | Published knowledge-base articles matching the title or body: `{articles: [{title, slug, excerpt}]}`, at most 10. Fewer than two characters, or no knowledge base, returns none. |
| `GET /articles/{slug}` | A published article: `{title, slug, body, category}`. Drafts and unknown slugs are 404. |
| `POST /tickets` | `{name, email, subject, description, department_id?, metadata?}`. 201 `{message, reference, ticket_reference, guest_token}`. An unknown or inactive department is 422. |
| `GET /tickets/lookup?ref=&token=` | Unchanged: needs the ticket's guest token. |
| `GET /chat/availability` | `{available, queue_length}`. |
| `POST /chat/start` | `{name, email, department_id?, message?, subject?, metadata?}`. 201 `{id, session_id, status, messages, agent?}`. |
| `GET /chat/{id}/messages` | `{messages: [{id, body, is_agent, author: {name}, created_at}], typing: null, agent, ended}`. The opening message comes first. |
| `POST /chat/{id}/messages` | `{body}`. 422 once the chat has ended. |
| `POST /chat/{id}/typing`, `/end` | The visitor's typing indicator (broadcast, not stored); ending the chat. |
| `POST /chat/{id}/rate` | `{rating: 1-5, comment?}`, once, after the chat ended. Stored on the chat ticket's metadata as `chat_rating`. |

The chat `id` is the chat ticket's guest token: a random string, never a
sequential number. It is the only credential the `/chat/{id}/...` routes take,
so treat it like a password. Unknown ids, and the guest token of a ticket that
is not a chat, are 404.

Validation errors are 422 `{message, errors: {field: [message]}}` (the
original `error` key stays). Every widget route counts against a general
per-client-IP budget of `WidgetConfig.RateLimitPerMin` a minute (when zero,
`Config.GuestRateLimit.RequestsPerMinute`, default 60). Ticket creation and chat
start also count against the guest submission limit (default 5 a minute);
chat messages, chat ratings and ticket lookups against the guest reply limit
(default 10). Over a limit the answer is 429 with `Retry-After`. The widget
uses `Config.GuestRateLimit`'s `ClientIP`, `Store` and `Disabled`, so behind a
proxy set `ClientIP` or every visitor shares the proxy's budget.

There is no mailbox verification in this backend, so `/config` sends
`status_lookup_enabled: false` and no `guest_verification_required`: the
widget hides its Status tab. Nothing looks a ticket up by email address.
Agent names in the chat transcript show as "Support" unless you set
`WidgetChatHandler.AgentName`; the routers leave it unset.

## 3. Add the script to the other site

```html
<script src="https://help.example.com/support/widget/embed.js" async></script>
<!-- with MountChi/MountStdlib and the default prefix: /escalated/widget/embed.js -->
```

The host parameters (tabs, department, prefill, metadata, title, the
JavaScript API) are described in the frontend package's
[embedding guide](https://github.com/escalated-dev/escalated/blob/main/docs/embedding-the-widget.md).

## What the server does

A request is cross-origin when its `Origin` header is present, is not `null`,
and differs from the request's own scheme and host. The scheme comes from
`r.TLS`, so behind a TLS-terminating proxy a same-site request can count as
cross-origin. It then gets the CORS grant only if its origin is listed.

For a cross-origin request from a listed origin, WidgetCORS:

- answers a preflight (`OPTIONS` with `Access-Control-Request-Method`) with
  204, `Access-Control-Allow-Origin: <origin>`, methods `GET, POST, OPTIONS`,
  headers `Accept, Authorization, Content-Type, X-Requested-With,
  X-Guest-Token`, `Access-Control-Max-Age: 600` and `Vary: Origin`;
- adds `Access-Control-Allow-Origin: <origin>`,
  `Access-Control-Expose-Headers: Retry-After` and `Vary: Origin` to every
  other response before the handler runs, so errors (403, 404, 422, 429) carry
  them too. `Vary` is appended to, not replaced;
- marks the request as embedded (`handlers.WidgetCrossOrigin(ctx)` returns the
  origin). The widget handlers never read a signed-in user, and
  `Escalated.TicketAccessMiddleware` scopes such a request as a guest even
  when the browser is signed in to your app. If your own middleware resolves a
  user on these routes, check `handlers.WidgetCrossOrigin` and treat the
  request as signed out.

It never sends `Access-Control-Allow-Credentials`. Preflights from other
origins get 204 without a grant. Other requests from unlisted origins, and
same-origin requests, pass through unchanged. The widget routes have no CSRF
check, so there is nothing to skip. Rate limits and `WidgetConfig.Enabled`
apply as before.

### Host parameters

`GET /config` returns `"accepts_metadata": true`. `POST /tickets` and
`POST /chat/start` accept an optional `metadata` object. The values come from
the visitor's browser, so only a flat map is kept:

- at most 25 entries;
- keys of letters, digits and `_ . : -`, 64 characters at most;
- string values (trimmed, cut to 500 characters), numbers or booleans. Arrays,
  objects and `null` are dropped;
- `page_url` only when it is an http(s) URL;
- `embedded_on` is always set by the server from the allowed `Origin`, and a
  value sent by the page is discarded.

`metadata` that is not an object, or has more than 50 keys, is rejected with
422. Ticket metadata goes in `tickets.metadata`. For a chat it is stored in the
chat ticket's `metadata` and merged into its `ChatMetadata`. Treat all of it as
unverified visitor input.

### The script

`handlers.WidgetScript` serves the bundle embedded in the binary from
`handlers/assets/widget/escalated-widget.js` (`@escalated-dev/escalated`
0.12.1, `dist/widget/escalated-widget.js`). It sends
`Content-Type: application/javascript; charset=utf-8`,
`Cache-Control: public, max-age=3600`, an `ETag` (answering 304 to a matching
`If-None-Match`), `X-Content-Type-Options: nosniff`,
`Cross-Origin-Resource-Policy: cross-origin` and
`Access-Control-Allow-Origin: *`. If you serve the bundle from a CDN, skip the
route; pages then need `data-base-url`.

## Upgrading

Before this change the widget handlers sent `Access-Control-Allow-Origin: *`
on every response when `AllowedOrigins` was empty, and the raw
`AllowedOrigins` string otherwise, even with several origins. Now the handlers
send no CORS headers themselves. Wrap the routes in `handlers.WidgetCORS` and
list the embedding sites. A config of `*` no longer grants anything.

## Troubleshooting

- **The browser reports a CORS error.** The page's origin is not listed, or it
  differs in scheme or port (`http://` vs `https://`, `:8080`). Check that
  WidgetCORS wraps the whole widget prefix, so `OPTIONS` reaches it.
- **A preflight answers 405.** The router matched the method before
  WidgetCORS ran. Wrap the prefix (chi `r.Route` + `r.Use`, or a sub-mux
  handed to `mux.Handle`) instead of single routes.
