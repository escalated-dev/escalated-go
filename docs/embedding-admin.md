# Embedding the admin in your own admin panel (optional)

Escalated ships a complete standalone admin at `{RoutePrefix}/admin`
(`/support/admin` with `cfg.RoutePrefix = "/support"`), with its own sidebar
and top bar. **That is the default, and it needs no setup.** If your Go app
already has an admin panel, you can instead show Escalated's admin screens
inside it: under your sidebar and header, your URL prefix and your
middleware.

This is an optional alternative to the standalone admin. Nothing on this page
has any effect until you turn it on: with no embed configuration every route,
page, prop, header and gate is exactly what it was before (a test compares
the route table with a snapshot, `testdata/embed/default-routes.json`). The
design is in the ADR
[`2026-11-05-embeddable-admin`](https://github.com/escalated-dev/escalated-developer-context/blob/main/decisions/2026-11-05-embeddable-admin.md);
the frontend side is documented in
[`@escalated-dev/escalated` docs/embedding-admin.md](https://github.com/escalated-dev/escalated/blob/main/docs/embedding-admin.md).

## Contents

- [Pick your setup](#pick-your-setup)
- [Turning it on](#turning-it-on)
- [Configuration reference](#configuration-reference)
- [What changes, and what does not](#what-changes-and-what-does-not)
- [1. Vue + Inertia host](#1-vue--inertia-host)
- [2. Server-rendered host](#2-server-rendered-host)
- [3. iframe](#3-iframe)
- [The admin menu: `escalated.navigation`](#the-admin-menu-escalatednavigation)
- [Security](#security)
- [What does not move](#what-does-not-move)
- [Turning it off](#turning-it-off)

## Pick your setup

| Your admin panel | Use | `Mode` |
|---|---|---|
| Vue + Inertia in this app | [Vue + Inertia host](#1-vue--inertia-host) | `layout` |
| Server-rendered templates (`html/template`, templ, ...) | [Server-rendered host](#2-server-rendered-host) (your template as the root view) | `layout` |
| Another app or origin, a non-Vue SPA, a CMS admin | [iframe](#3-iframe) | `iframe` |

## Turning it on

Call `Config.EmbedAdmin` before `escalated.New` (or set `cfg.UI.Embed`
yourself; the two are equivalent):

```go
cfg := escalated.DefaultConfig()
cfg.RoutePrefix = "/support"
cfg.EmbedAdmin(escalated.EmbedConfig{
    Prefix:     "admin/support",
    Middleware: []func(http.Handler) http.Handler{requireAdminSession},
    Layout:     "Layouts/AdminLayout",
})
esc, err := escalated.New(cfg)
// ...
router.MountChi(r, esc) // or router.MountStdlib(mux, esc)
```

`requireAdminSession` is your own admin middleware. `MountChi` and
`MountStdlib` mount the embedded admin themselves; there is nothing else to
register.

Or, from the environment, with no code change: `ESCALATED_EMBED=true`,
`ESCALATED_EMBED_MODE`, `ESCALATED_EMBED_PREFIX` and
`ESCALATED_EMBED_FRAME_ANCESTORS` (read by `escalated.New` for any field you
left at its zero value).

## Configuration reference

| `EmbedConfig` field | Env var | Default | Meaning |
|---|---|---|---|
| `Enabled` | `ESCALATED_EMBED` | `false` | Master switch (`EmbedAdmin` sets it). While `false`, nothing below has any effect. |
| `Mode` | `ESCALATED_EMBED_MODE` | `"layout"` | `escalated.EmbedModeLayout` (Vue + Inertia or server-rendered host) or `escalated.EmbedModeIframe`. |
| `Prefix` | `ESCALATED_EMBED_PREFIX` | `"admin/support"` | URL path the admin moves under, instead of `{RoutePrefix}/admin`. |
| `Middleware` | | `nil` | Your middleware for the embedded admin routes, outermost first. `nil` adds none: the embedded admin then runs what the standalone one runs (whatever wraps your router, plus Escalated's gates). |
| `ReplaceAdminGate` | | `false` | `true` drops Escalated's admin gate (`AdminCheck`) from the embedded routes, so `Middleware` alone decides. See [Security](#security). |
| `Layout` | | `""` | Name of your Vue admin layout, matched against the frontend plugin's `layouts` option. |
| `RootView` | | `""` | Path of an `html/template` root view for embedded pages ([server-rendered host](#2-server-rendered-host)). |
| `FrameAncestors` | `ESCALATED_EMBED_FRAME_ANCESTORS` (comma-separated) | `[]` | iframe mode: exact origins allowed to frame the admin. `*`, wildcards, paths and anything that is not an http(s) origin are dropped. |

## What changes, and what does not

While embedding is on:

- Every admin route that lived under `{RoutePrefix}/admin` (the admin
  screens, the plugin admin and plugin pages, API tokens, the import wizard,
  reports) is served under your `Prefix`, behind your `Middleware`, followed
  by the same stack as before: API tokens, two-factor, route permissions,
  ticket access, Escalated's admin gate (unless you replace it), the same
  handlers. The old `{RoutePrefix}/admin/...` URLs answer 404.
- **Route names do not change.** The `ziggy` route list the pages' `route()`
  reads points the `escalated.admin.*` names at the new URLs, so the shared
  frontend's links follow. Redirects the admin answers with (after a save, a
  plugin action, a two-factor challenge) go to the new URLs too.
- Embedded pages carry `escalated.embed`, and every Escalated page carries
  `escalated.navigation`, the admin menu for your sidebar.

How the Go routers do it: the embed prefix is one route (`{Prefix}` and
`{Prefix}/*` on chi, `{Prefix}` and `{Prefix}/` on the `ServeMux`). It runs
your `Middleware` on the request as the browser sent it, marks it as
embedded and hands it to Escalated's mount at its standalone path, which the
mount serves only for embedded requests. Inertia's `url` and ziggy's
`location` keep the embedded URL.

`escalated.embed` on an embedded page:

```json
{
  "enabled": true,
  "mode": "layout",
  "layout": "Layouts/AdminLayout",
  "route": "escalated.admin.tickets.index",
  "base_url": "/admin/support",
  "allowed_origins": []
}
```

On every other page `enabled` is `false` and `route` is `null`. `route` is
the route list's name for the current URL.

## 1. Vue + Inertia host

Backend: turn embedding on (above) with your prefix, your admin middleware
and the name of your admin layout.

Frontend: register your admin layout under the name you gave `Layout`:

```js
import { EscalatedPlugin, resolveEscalatedPage } from '@escalated-dev/escalated';
import AdminLayout from '@/Layouts/AdminLayout.vue';
import AuthenticatedLayout from '@/Layouts/AuthenticatedLayout.vue';

createInertiaApp({
    resolve: (name) => resolveEscalatedPage(name, () =>
        resolvePageComponent(`./Pages/${name}.vue`, import.meta.glob('./Pages/**/*.vue'))),
    setup({ el, App, props, plugin }) {
        createApp({ render: () => h(App, props) })
            .use(plugin)
            .use(EscalatedPlugin, {
                layout: AuthenticatedLayout, // customer pages, as before
                layouts: { 'Layouts/AdminLayout': AdminLayout }, // embedded admin pages
                theme: { panel: { mode: 'light' } }, // match your admin
            })
            .mount(el);
    },
});
```

To use one layout for customer and admin pages, pass
`layout: AdminLayout, layoutScope: ['customer', 'admin']` instead of
`layouts`.

Your admin layout needs a `#header` slot (Escalated puts the page title
there) and a default slot. List Escalated's menu with `<EscalatedNavigation />`:

```vue
<!-- Layouts/AdminLayout.vue -->
<script setup>
import { EscalatedNavigation } from '@escalated-dev/escalated';
</script>

<template>
    <div class="flex min-h-screen">
        <aside class="w-64 border-r bg-white p-3">
            <a href="/admin">Dashboard</a>
            <!-- ...your own items... -->
            <p class="mt-4 px-3 text-xs font-semibold uppercase text-gray-500">Support</p>
            <EscalatedNavigation />
        </aside>
        <div class="flex-1">
            <header class="border-b bg-white px-6 py-4"><slot name="header" /></header>
            <main class="p-6"><slot /></main>
        </div>
    </div>
</template>
```

Escalated renders each admin page chrome-free (no Escalated sidebar, top bar,
logo or footer) inside your layout.

Your own Inertia pages get `escalated.navigation` too when you render them
with Escalated's renderer and wrap their routes with
`esc.EmbedShareMiddleware`, so the sidebar can list the menu on every page.

## 2. Server-rendered host

If your admin layout is a server-rendered template, make it the root view of
the embedded pages: a template with your chrome and the Inertia mount point.

```go
cfg.EmbedAdmin(escalated.EmbedConfig{
    Prefix:     "admin/support",
    Middleware: []func(http.Handler) http.Handler{requireAdminSession},
    RootView:   "templates/admin/escalated.html",
})
```

`RootView` is an `html/template` file parsed the way the renderer's own root
template is (`renderer.NewInertiaRenderer`), with the same data: `page`
(render it with `marshal`), `escalatedRouteHelper`, plus
`escalatedNavigation`, the admin menu (`[]escalated.NavigationItem`) for the
request. This is the template the tests render
(`testdata/embed/host-admin.html`):

```html
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>Acme admin</title>
    <script type="module" src="/assets/admin.js"></script>
</head>
<body>
    <header class="host-header">Acme admin</header>
    <div class="host-shell">
        <aside class="host-sidebar">
            <a href="/admin">Dashboard</a>
            <p class="menu-heading">Support</p>
            <ul>
                {{ range .escalatedNavigation }}
                <li><a href="{{ .URL }}"{{ if .Active }} class="active" aria-current="page"{{ end }}>{{ .Label }}</a></li>
                {{ end }}
            </ul>
        </aside>
        <main class="host-main">
            <script data-page="app" type="application/json">{{ marshal .page }}</script>
            <div id="app"></div>
        </main>
    </div>
    <script>{{ .escalatedRouteHelper }}</script>
</body>
</html>
```

Only embedded pages use this root view; every other Escalated page keeps the
renderer's. Your Inertia entry needs no extra option: Escalated sees
`escalated.embed.enabled` and renders the page chrome-free (title + content).
For the menu on your other pages, call `esc.AdminNavigation(r)` in your
handlers: it returns the same list for the request's user, whether or not
embedding is on.

## 3. iframe

For an admin that is not part of this app (another origin, another stack),
frame the embedded admin:

```go
cfg.EmbedAdmin(escalated.EmbedConfig{
    Prefix:         "admin/support",
    Middleware:     []func(http.Handler) http.Handler{requireAdminSession},
    Mode:           escalated.EmbedModeIframe,
    FrameAncestors: []string{"https://admin.example.com"},
})
```

or `ESCALATED_EMBED=true`, `ESCALATED_EMBED_MODE=iframe`,
`ESCALATED_EMBED_FRAME_ANCESTORS=https://admin.example.com`.

Embedded responses then carry
`Content-Security-Policy: frame-ancestors 'self' https://admin.example.com`
and no `X-Frame-Options` (the allow-listed `frame-ancestors` replaces it on
those responses only). The headers are set as the response is written,
after your `Middleware` and any middleware around the router, so a
middleware that adds `X-Frame-Options` to every response cannot undo them. A
CSP you already send is kept and this directive is added as a second policy;
if your own policy has a `frame-ancestors` directive, list the same origins
there too, since browsers enforce both. No other route's headers change.

On the framing page:

```html
<iframe id="support-admin" src="https://support.example.com/admin/support/tickets"
        style="width:100%;border:0" title="Support administration"></iframe>
<script type="module">
    import { connectEscalatedFrame } from '@escalated-dev/escalated/embed';

    const frame = connectEscalatedFrame(document.getElementById('support-admin'), {
        origin: 'https://support.example.com',
        onNavigate: ({ route, title }) => highlightMenu(route),
    });
    frame.setTheme('light');
</script>
```

The frame sizes itself to its content and reports each page change; the
protocol is in the frontend guide.

### Session cookies in a frame

Escalated owns no session: your sign-in cookie is what the frame needs.

- **Same site** (`admin.example.com` framing `support.example.com`): a
  `SameSite=Lax` session cookie works.
- **Cross-site** (different registrable domains): your session cookie must be
  sent in a third-party context: `http.Cookie{SameSite: http.SameSiteNoneMode,
  Secure: true, ...}` (or your session library's `SameSite: None` and
  `Secure` options), and both sites served over HTTPS. Escalated's own
  short-lived cookies (the two-factor pass mark, the import wizard and the
  Plugins page flash) stay `SameSite=Lax`, so two-factor challenges and the
  import wizard need a same-site frame. Browsers that block third-party
  cookies will not keep a session in a cross-site frame at all; use the
  Vue + Inertia or server-rendered setup there.
- CSRF protection is your app's, unchanged.

## The admin menu: `escalated.navigation`

While embedding is on, every Escalated page shares the admin menu, and
`esc.AdminNavigation(r)` returns the same list for the request's user
(`Config.UserIDFunc`, `Config.AdminCheck`):

```json
[
  {
    "key": "tickets",
    "label": "Tickets",
    "route": "escalated.admin.tickets.index",
    "url": "/admin/support/tickets",
    "icon": "tickets",
    "position": 20,
    "active": true,
    "badge": null,
    "plugin": null,
    "children": []
  }
]
```

It is filtered for the user: empty for anyone who fails `AdminCheck` (with
`ReplaceAdminGate`, for anyone not signed in), screens whose feature is off
are left out (Roles while roles & permissions is off, Newsletters while
`EnableNewsletters` is off, any name a `RouteOverrides` entry removed), and
while roles & permissions is on each item needs the permission its route
requires (`access.RoutePermissions`). Active plugins' admin menu items are
included (`key` `plugin:<slug>:<n>`), sorted by `position` with the core
items. Core `icon` keys match the frontend's `escalatedNavIcons`. The
Newsletters item keeps its standalone URL and has no `route` (no name in the
route list).

## Security

- **The admin gate stays on by default.** Your `Middleware` runs *in addition
  to* Escalated's `AdminCheck`. Set `ReplaceAdminGate` only if your middleware
  itself restricts the routes to admins: with it on, your middleware is the
  only check on who reaches the admin. Two-factor and route permissions still
  apply either way, and the agent panel keeps Escalated's gates.
- **Framing is allow-listed.** In iframe mode only the origins you list (plus
  your own) may frame the admin; nothing else, and never `*`.
- **No open `postMessage`.** The frame talks only to an allow-listed parent
  origin and ignores messages from anywhere else.

## What does not move

The agent panel (`{RoutePrefix}/agent`), customer portal, guest pages,
knowledge base, widget, JSON API, SSO, the two-factor enrolment page
(`{RoutePrefix}/admin/settings/two-factor`) and challenge, and the newsletter
admin (`{RoutePrefix}/admin/newsletters`) keep their URLs.

## Turning it off

Remove the `EmbedAdmin` call (and `ESCALATED_EMBED`). The admin returns to
`{RoutePrefix}/admin` with Escalated's own chrome.
