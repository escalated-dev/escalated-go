# Plugins

Escalated runs two kinds of plugin, listed together on **Admin > Plugins**
(`{RoutePrefix}/admin/plugins`, the shared `Escalated/Admin/Plugins/Index`
screen):

- **Native plugins**: Go code in your build, implementing `plugins.Plugin` and
  listed in `Config.Plugins.Native`.
- **SDK plugins**: plugins written with
  [`@escalated-dev/plugin-sdk`](https://github.com/escalated-dev/escalated-plugin-sdk)
  (`definePlugin()`), run by the Node plugin runtime, which this package starts
  and talks to over its stdin and stdout.

The behaviour every Escalated backend shares is in the
[plugin contract](https://github.com/escalated-dev/escalated-developer-context/blob/main/domain-model/plugin-contract.md);
this page covers what is specific to Go.

Every plugin starts inactive. Nothing in it runs until an admin activates it,
and deactivating it stops everything again: no hooks, no pages, no endpoints or
webhooks (404), no menus or widgets.

## Tables

`migrations.Migrate` (PostgreSQL) and `migrations.MigrateSQLite` create two
tables, with your table prefix:

- `escalated_plugins`: one row per plugin that has been activated (`slug`,
  `is_active`, `activated_at`, `deactivated_at`).
- `escalated_plugin_store`: what `ctx.store` and `ctx.config` write, keyed by
  the plugin's name, collection and key. `data` is `JSONB` on PostgreSQL and
  `TEXT` on SQLite.

## Native plugins

A native plugin is any type with a `Manifest() plugins.Manifest` method. The
optional interfaces add behaviour: `Activator`, `Deactivator`, `ActionPlugin`,
`FilterPlugin`, `EndpointPlugin`, `WebhookPlugin`.

```go
type CRM struct{}

func (CRM) Manifest() plugins.Manifest {
	return plugins.Manifest{
		Name:    "@acme/escalated-plugin-crm",
		Version: "1.0.0",
		Pages: []plugins.Page{
			{Route: "settings", Component: "CrmSettings", Menu: &plugins.Menu{Label: "CRM"}},
		},
	}
}

func (CRM) Endpoints() []plugins.Route {
	return []plugins.Route{{Method: "GET", Path: "/CrmSettings",
		Handler: func(ctx context.Context, pc *plugins.Context, req *plugins.Request) (any, error) {
			key, err := pc.Config.Get(ctx, "api_key")
			return map[string]any{"api_key_set": key != nil}, err
		}}}
}

func (CRM) Actions() map[string]plugins.ActionFunc {
	return map[string]plugins.ActionFunc{
		"plugin.activated": func(ctx context.Context, event map[string]any, pc *plugins.Context) error {
			pc.Log.Info("CRM plugin is on")
			return nil
		},
	}
}

cfg.Plugins.Native = []plugins.Plugin{CRM{}}
```

The plugin's **slug** is its name without the leading `@`, with `/` as `--` and
anything outside `[A-Za-z0-9_-]` as `-`: `@acme/escalated-plugin-crm` is
`acme--escalated-plugin-crm`. Two plugins with one slug, or a malformed route,
make `escalated.New` fail and name them.

Native plugins are listed with `source: "registered"`. Deleting one on the
screen uninstalls it: it is deactivated, its activation row is deleted, and it
stays listed (inactive), because its code is still in your build. Its stored
data is kept, so activating it again finds it.

## SDK plugins

Install the runtime and the plugins in your project with npm, then switch the
bridge on:

```sh
npm install @escalated-dev/plugin-runtime @escalated-dev/plugin-slack
```

```go
cfg.Plugins.Runtime = plugins.RuntimeConfig{
	Enabled: true,
	Dir:     "/srv/support", // the directory holding node_modules
}
```

This runs `node node_modules/@escalated-dev/plugin-runtime/build/bin/escalated-plugins.js`
in `Dir`; `Command` and `Args` change that. The runtime is started on first use
(the plugins screen, a hook, an endpoint) and spoken to in JSON-RPC 2.0, one
message per line, at most 10 MB. It must answer the handshake as compatible
with protocol 1.0; endpoints and webhooks also need it to support HTTP contract
version 1, and answer 503 otherwise.

When Node or the package is missing, or the runtime dies, the host carries on:
SDK actions are dropped and logged, filters return their input, and SDK
endpoints answer 503. It is started again on next use with exponential backoff,
capped at five minutes. Call `esc.Close()` on shutdown to stop it.

SDK plugins are listed with `source: "composer"` (the contract's name for a
package-manager install): the screen refuses to delete them, because npm
removes them. An SDK plugin whose slug a native plugin already has is not
loaded, and the clash is logged.

The runtime today loads and dispatches to every installed plugin. This package
only sends `endpoint`, `webhook`, `activate` and `deactivate` for the plugin
concerned, sends `action` and `filter` only when an active SDK plugin declares
the hook, with `plugins: [active names]`, and refuses any `ctx.*` call the
runtime makes for a plugin it is not serving at that moment. An inactive SDK
plugin can therefore neither read nor write through `ctx`.

## What a plugin gets

| Plugin declares | Served at | Who |
|---|---|---|
| `Pages` | `GET {RoutePrefix}/admin/plugins/{slug}/{route}`, rendered as `Escalated/Plugin/Page` with the props of the plugin's own `GET /{component}` endpoint (`{}` when it has none or it fails) | `AdminCheck`, plus the page's `Capability` (403) |
| endpoints | `{RoutePrefix}/api/plugins/{slug}{path}` | `AdminCheck`, plus the endpoint's capability (403) |
| webhooks | `{RoutePrefix}/webhooks/plugins/{slug}{path}` | Anyone. The plugin must authenticate the sender. |
| `OnActivate`, `OnDeactivate` | Called when an admin activates or deactivates it | |
| actions | `plugin.activated`, `plugin.deactivated`, `plugin.uninstalling` (`{plugin, slug}`), and whatever active plugins emit | |
| filters | `esc.Plugins.ApplyFilter(ctx, hook, value)`, for host code that wants a filter point | |

- A **capability** is a permission slug. `Config.Plugins.Capability` decides
  who holds one; left nil, admins hold every capability and nobody else holds
  any.
- Endpoint calls that change state (anything but GET, HEAD and OPTIONS) must
  carry `X-Requested-With` or `X-Inertia`, which a cross-site form cannot send,
  so another site cannot use an admin's session.
- Handlers receive `params` from `:name` segments of the declared path,
  `query`, the parsed `body` (JSON or a form), the exact bytes (`RawBody`;
  base64 to the runtime), the client address (through
  `GuestRateLimit.ClientIP` when you set it), and lower-case `headers` without
  `Cookie`, `Authorization`, `X-XSRF-TOKEN`, `X-CSRF-TOKEN` or `Proxy-*`. A
  body is at most 3 MB.
- A handler's value is a JSON 200. A `plugins.Response` (native) or the SDK's
  `httpResponse()` chooses the status, headers and format; transport headers
  and `Set-Cookie` are refused with 502. A handler that fails is a 500 whose
  body says nothing about the error; the error goes to the log.
- Timeouts: actions and endpoints 30 s, filters 5 s, webhooks 60 s. A failing,
  panicking or slow plugin callback is logged with the plugin's name and never
  fails the request that caused it.

The lifecycle runs as the contract orders it: activating writes the row, then
calls `OnActivate` (the runtime's `activate`), then fires `plugin.activated`;
deactivating fires `plugin.deactivated` while the plugin is still active, calls
`OnDeactivate` (`deactivate`), then switches the row off; uninstalling fires
`plugin.uninstalling` first. A callback that fails does not undo the change.
Every POST and DELETE answers with a 303 to the index and a `success` or
`error` flash, carried in a one-minute cookie (this package keeps no session).
An unknown slug is an `error` flash, or a 404 for a request that asks for JSON.
There is no upload (`can_upload: false`).

## The context

`*plugins.Context` is everything a plugin may touch, scoped to its own name.
Native plugins get it in every callback; for SDK plugins the bridge answers the
runtime's `ctx.*` calls through the same methods.

- `Config.All/Get/Set`: the plugin's settings, one JSON object. `Set` merges.
  Settings may hold secrets, so they never reach the plugins screen or a shared
  prop.
- `Store.Get/Set/Query/Insert/Update/Delete`: records by collection and key.
  `Query` filters in Go, not SQL: equality (an array field matches when it
  contains the value), `$gt $gte $lt $lte $ne $in $nin` on dotted field names,
  with `OrderBy`, `Order` and `Limit`. A field name that is not a dotted path
  is an error. Each record is its data with the row id as `_id`.
- `Log.Info/Warn/Error/Debug`: the host log, with the plugin's name.
- `Emit(ctx, hook, data)`: fires an action to every active plugin. The
  canonical domain event names, their legacy names and the `plugin.*`
  lifecycle names belong to the host and are dropped.
- `CurrentUser(ctx)`: the signed-in user of the request being served (named
  through `Config.UserName` and `Config.UserEmail`), or nil.

`esc.Plugins.ContextFor(slug)` gives host code the same context, to seed a
plugin's settings at deploy time.

## Trust

A native plugin is your code, with your process's privileges. The runtime keeps
SDK plugin code out of your process and database connection, but every SDK
plugin shares one Node process and the network: it isolates the host from
plugins, not plugins from each other. Only install plugins you would merge into
your own code.
