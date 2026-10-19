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

The library does not mount the widget for you. Put every widget route behind
`handlers.WidgetCORS`, and mount `embed.js` outside it. WidgetCORS has to see
`OPTIONS` requests, so wrap the whole widget prefix rather than single routes.

With chi:

```go
widget := handlers.NewWidgetHandler(widgetCfg, esc.Store, services.NewTicketService(esc.Store))
chat := handlers.NewWidgetChatHandler(widgetCfg, esc.Store, chatSessions, chatAvailability)

r.Get("/support/widget/embed.js", handlers.WidgetScript)
r.Route("/support/widget", func(r chi.Router) {
    r.Use(handlers.WidgetCORS(widgetCfg, esc.Store))
    r.Get("/config", widget.Config)
    r.Get("/articles", widget.SearchArticles)
    r.Get("/articles/{id}", widget.ShowArticle)
    r.Post("/tickets", widget.CreateTicket)
    r.Get("/tickets/lookup", widget.LookupTicket)
    r.Get("/chat/availability", chat.Availability)
    r.Post("/chat/start", chat.StartChat)
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

## 3. Add the script to the other site

```html
<script src="https://help.example.com/support/widget/embed.js" async></script>
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
