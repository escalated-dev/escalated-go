# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.2.9] - 2026-10-30

### Upgrading
- Single sign-on is a new install-level switch, off by default
  (`Config.Access.SSO`, else `ESCALATED_SSO`). It needs
  `Config.Access.SSOOptions.SignIn` (you sign the user in; Escalated owns no
  sessions), `Config.UserIDByEmail` and an absolute base URL
  (`SSOOptions.BaseURL`, else `Notifications.BaseURL`); `New` fails without
  them while it is on. Run the migrations first (they add
  `escalated_sso_message_ids`). See docs/access-control.md.
- Two-factor authentication is a new install-level switch, off by default
  (`Config.Access.TwoFactor`, else `ESCALATED_TWO_FACTOR`). It needs
  `Config.Access.SessionID` (your session id, renewed on login) and a secret
  (`Config.Access.Secret`, else `GuestAccess.Secret`); `New` fails
  without them while it is on. Run the migrations first (they add
  `two_factors.last_used_step`). Every page now also carries
  `escalated.two_factor_required`. See docs/access-control.md.
- Roles & permissions is a new install-level switch, off by default
  (`Config.Access.RolesPermissions`, else `ESCALATED_ROLES_PERMISSIONS`).
  Off, nothing changes. To turn it on: run the migrations (they add
  `escalated_roles`, `escalated_permissions`, `escalated_role_permission` and
  `escalated_role_user`), call `esc.Roles().Seed(ctx)`, give every staff user
  a role (at least one `esc.Roles().Assign(ctx, id, "admin")`), then set the
  switch. Staff without a role can use nothing but their own screens once it
  is on. See docs/access-control.md.
- Every page now carries `escalated.features` and `escalated.permissions`
  (`["*"]` for staff while roles & permissions is off).
- SDK plugins' cron schedules run only when the host calls
  `esc.RunPluginCron(ctx)` every minute or starts `esc.StartPluginCron(ctx)`.
- Every page now carries `escalated.plugins` (`{}` with no active plugin).
- Active plugins receive the canonical domain events. A host without plugins
  is unaffected.
- `ticket.tag_added` and `ticket.tag_removed` webhook payloads add the tag's
  `slug` beside `id` and `name`.
- Run the migrations: they add `escalated_plugins` and
  `escalated_plugin_store` (with your table prefix). Nothing else changes for
  a host that registers no plugins: Admin > Plugins lists none, and the plugin
  endpoint and webhook routes answer 404.
- The routers now mount `{RoutePrefix}/admin/plugins` (admin),
  `{RoutePrefix}/api/plugins/` (admin) and `{RoutePrefix}/webhooks/plugins/`
  (public, for plugin webhooks). A host route of its own under those paths
  must move.
- Call `esc.Close()` on shutdown when the plugin runtime is enabled; it stops
  the Node process.

### Added
- Single sign-on, enforced behind its switch (ADR
  2026-10-29-configurable-access-features, as escalated-laravel): SAML through
  gosaml2 (signed assertion against the configured certificate only, Issuer,
  Audience, Destination, Recipient, validity windows with clock skew,
  InResponseTo of a request issued here, assertion replay refused), OAuth 2 /
  OIDC through golang.org/x/oauth2 with a single-use state and PKCE, and JWT
  through golang-jwt (pinned algorithm, required exp, aud and single-use
  jti). Admin > Settings > SSO (`Admin/Settings/SsoSettings`) picks the
  provider, masks the secrets and lists the endpoints for the IdP. Unknown
  emails are refused unless `SSOOptions.CreateUsers`; `SSOOptions.SyncRoles`
  maps the IdP's role onto Escalated roles while roles & permissions is on.
- Two-factor authentication, enforced behind its switch (ADR
  2026-10-29-configurable-access-features, as escalated-laravel): TOTP
  through pquerna/otp with one step of drift and no code reuse, enrolment for
  every staff user at `Admin/Settings/TwoFactor`, the
  `Auth/TwoFactorChallenge` challenge after the host's login (5 attempts a
  minute, bound to the host's session id), eight single-use recovery codes,
  secrets sealed at rest, and "require two-factor"
  (`Config.Access.TwoFactorRequired` / `ESCALATED_TWO_FACTOR_REQUIRED`, or the
  `two_factor_required` setting, which `POST /admin/settings` saves on its
  own). `esc.TwoFactor().Disable` resets a user's enrolment. Token requests
  are not challenged.
- Roles & permissions, enforced behind its switch (ADR
  2026-10-29-configurable-access-features, as escalated-laravel): every
  admin, agent and staff API route needs its permission from the user's
  roles (`access.RoutePermissions`, fail closed), with status changes to
  resolved/closed/reopened needing `ticket.close`, bulk actions checked per
  action, internal replies needing `reply.create_internal` and API tokens
  checked as their owner. Admin > Roles (`Admin/Roles/Index`, `Form`) and the
  users page's role editor (`PUT /admin/users/{user}/roles`) are served while
  it is on and answer 404 while it is off. `esc.Roles()` seeds
  escalated-laravel's permissions and the `admin`, `agent` and `light_agent`
  roles and assigns roles.
- Native plugins get cron schedules and widget badges, as SDK plugins do:
  `plugins.CronPlugin` (`Cron()` maps five-field UTC expressions to
  functions, run by `RunPluginCron`/`StartPluginCron` for active plugins, as
  no user, within 30 s) and `plugins.BadgePlugin` (`Badges()` maps a widget
  component to a function whose answer is the widget's `badge`, asked as the
  viewer within 5 s, `null` when it fails).
- SDK plugin cron schedules run: call `esc.RunPluginCron(ctx)` every minute
  from your scheduler, or start `esc.StartPluginCron(ctx)`, and the due
  five-field (UTC) schedules of active SDK plugins are sent to the runtime as
  `cron`, as no user. SDK plugin widgets with a `badge` function carry its
  answer in `escalated.plugins.dashboardWidgets[].badge`, asked as the viewer
  within 5 s (`null` when it fails).
- SDK plugin calls in flight at once keep their own users: the bridge reads
  the `host_call_id` the plugin runtime (after 0.2.0) stamps on each `ctx`
  call, so `ctx.currentUser` and the actor of a plugin write are the request's
  that the call serves, even while another request is served. With an older
  runtime the bridge behaves as before.
- **Plugins**, per the shared plugin contract (escalated-developer-context
  `domain-model/plugin-contract.md`); see `docs/plugins.md`.
  - Native plugins: Go types implementing `plugins.Plugin` (plus the optional
    `Activator`, `Deactivator`, `ActionPlugin`, `FilterPlugin`,
    `EndpointPlugin` and `WebhookPlugin`), registered in
    `Config.Plugins.Native`. Duplicate slugs or malformed routes fail
    `escalated.New`.
  - SDK plugins (`@escalated-dev/plugin-sdk`) through
    `@escalated-dev/plugin-runtime`, spawned with `os/exec` and spoken to in
    JSON-RPC 2.0 over stdio when `Config.Plugins.Runtime.Enabled` is set:
    handshake, manifest, actions, filters, endpoints and webhooks over HTTP
    contract version 1, activate and deactivate, and the runtime's `ctx.*`
    calls, each acting only for a plugin being served. Lazy start, restart with exponential
    backoff capped at five minutes, and the host keeps serving without Node.
  - Admin > Plugins (`Escalated/Admin/Plugins/Index`, `can_upload: false`):
    activate, deactivate and uninstall, with the contract's lifecycle order
    and `plugin.activated`, `plugin.deactivated` and `plugin.uninstalling`.
  - Plugin pages (`Escalated/Plugin/Page`) at
    `{RoutePrefix}/admin/plugins/{slug}/{route}`, endpoints at
    `{RoutePrefix}/api/plugins/{slug}{path}` and webhooks at
    `{RoutePrefix}/webhooks/plugins/{slug}{path}`, for active plugins only,
    with capabilities checked by `Config.Plugins.Capability` (admins by
    default).
  - `Escalated.Plugins` (`*plugins.Host`): `ApplyFilter` for host filter
    points, `DispatchAction`, `ContextFor` and the lifecycle methods.
  - Every canonical domain event reaches active plugins' actions (native and
    SDK) after the change is saved, with the ticket flat plus `ticket` and the
    event's extras, the reply plus `reply` and `ticket`, or `{session}` for
    chat events. Delivery is in the background and a failing plugin never
    fails the request. Legacy names are never delivered.
  - The plugin context's domain methods, for native plugins and over the
    bridge: `ctx.tickets.find/query/create/update`,
    `ctx.replies.find/query/create` (through the ticket service, so events
    fire; allow-listed keys only; acting as the signed-in user of the request
    or nobody), `ctx.contacts`, `ctx.tags`, `ctx.departments`, `ctx.agents`
    (from `Config.SkillAgentDirectory`) and `ctx.broadcast` (through
    `Config.Plugins.Broadcaster`).
  - `escalated.plugins` (`menuItems`, `pageComponents`, `dashboardWidgets`)
    on every page, for active plugins only, filtered by capability, admin
    menu items for admins only.
  - `TicketService.Plugins` (`services.PluginEventSink`) and
    `TicketService.UpdateMetadata`; `renderer.WithShared` for props every page
    of a request carries.

### Fixed
- **Admin > Users roles picker validation now answers like escalated-laravel.**
  A bad `role_ids` on `PUT {prefix}/admin/users/{user}/roles` used to answer
  one generic `role_ids` message, and an Inertia visit was redirected back
  without it (Go keeps no session to flash it). An Inertia visit now gets the
  users page again with `errors` keyed `role_ids` (missing / not an array) or
  `role_ids.N` (not an integer / not a role) in Laravel's wording; a JSON
  client gets Laravel's 422 with the same keys. Refusing to remove your own
  role management is shown on the page the same way.
  `handlers.RoleHandler.UsersPage` (set by the router to
  `UserHandler.RenderWithErrors`) renders it.

## [0.2.8] - 2026-10-28

### Upgrading
- The widget now counts against the same per-IP guest budgets (and store) as
  the guest API, the guest pages and the mobile routes on the same mount,
  instead of a separate in-memory budget of its own.
- `POST verification` (guest API, widget, mobile) refuses a `chat` code with
  403 while live chat is switched off (`chat_enabled` setting).
- Guest responses send `Cache-Control: private, no-store` (was `no-store,
  private`; same directives).
- `POST /widget/chat/start` now answers exactly as escalated-laravel: `201
  {id, session_id, expires_at, ticket_reference, status, agent_name}`, where
  `id` and `session_id` are both the chat token (plus `guest_token`,
  `messages` and `agent`). The numeric `session_id` is no longer returned; use
  `id` for the widget chat routes and `ticket_reference` to name the ticket.
- The startup migration adds nullable `visitor_token_hash` (unique) and
  `visitor_token_expires_at` to the chat sessions table. Chats already in
  progress keep working: the next widget request with their token (still the
  chat ticket's `guest_token`) moves the token's hash to the session with a
  full lifetime and clears the ticket's copy.
- Widget chat tokens now expire `GuestAccessConfig.TTL` after the chat starts
  (default 24 hours, clamped to 5 minutes .. 7 days), as in escalated-laravel.
  After that every `/widget/chat/{id}/...` route, and
  `/widget/chat/sessions/{ref}/...` with it as `X-Guest-Token`, answers 404 and
  the visitor starts a new chat.
- New chat tickets no longer carry the chat token as their `guest_token`.
  `GuestAccess.IssueChatToken` and `GuestAccess.ChatSessionForToken` issue and
  resolve chat tokens for host code.

### Added
- **Mobile guest API** under `{RoutePrefix}/api/mobile/guest` (default
  `/escalated/api/mobile/guest`), per the shared contract
  (escalated-developer-context `domain-model/mobile-guest-api.md`):
  `POST verification`, `POST lookup`, `POST tickets`, `GET tickets/{token}` and
  `POST tickets/{token}/replies`. They reuse the guest API's email codes,
  expiring grants and per-IP guest budgets, answer with the allow-listed mobile
  ticket object, and send `Cache-Control: private, no-store` and
  `Referrer-Policy: no-referrer` on every answer, 429s included. A grant from
  any guest surface opens the others; anything else is a 404. See
  `docs/guest-access.md`.
- `GuestAccess.ResolveGrant` returns a grant's ticket id and expiry (grants
  only, never a legacy token).

### Security
- **Widget chat tokens are stored hashed and expire.** The token returned by
  `POST /widget/chat/start` (`id`, `guest_token`) was the chat ticket's plain
  `guest_token`, never expired, and (with legacy tokens accepted) also opened
  the ticket's guest routes. It is now kept only as a SHA-256 on the chat
  session, the start answer carries `expires_at`, and an unknown or expired
  token is a 404 on every chat route.

## [0.2.7] - 2026-10-22

### Fixed
- Transferring a live chat from the agent ticket page: the shared
  Agent/TicketShow page POSTs to `route('escalated.admin.chat.transfer',
  ticket.chat_session_id)`, a chat SESSION id, but the only transfer route,
  `POST /agent/chat/sessions/{id}/transfer`, takes the chat ticket's id, so the
  transfer reached the wrong chat or none. New `POST /agent/chat/{session}/transfer`
  on both routers takes the session id, with the same body, checks and answers
  (404 `{"message": "Chat session not found."}` for an unknown session).
  Upgrade: map `escalated.admin.chat.transfer` to this path in your `route()`
  helper (see `docs/ui-pages.md`). The ticket-id route is unchanged.

## [0.2.6] - 2026-10-22

### Added
- Ending a live chat by its session id (escalated-laravel
  `escalated.admin.chat.end`): `POST /agent/chat/{session}/end` on both
  routers, behind the agent check like the rest of the chat console. The shared
  Agent/TicketShow page POSTs `{}` to `route('escalated.admin.chat.end',
  ticket.chat_session_id)`, so map that name to this path (see
  `docs/ui-pages.md`). It answers 200 `{"message": "Chat ended."}`, 404 for an
  unknown session and, as in Laravel, 422 `{"message": "Chat is already
  ended."}` for a chat that has already ended. The ticket-id console route
  `POST /agent/chat/sessions/{id}/end` is unchanged (no already-ended check,
  200 `{"status": "ended"}`).
- Live chat transfer (escalated-laravel `escalated.admin.chat.transfer`):
  `POST /agent/chat/sessions/{id}/transfer` on both routers, behind the agent
  check like the rest of the chat console ({id} is the chat ticket's id). The
  body is `{"agent_id", "department_id"}` (either or both); the shared
  `ChatActionBar`'s `{"type": "agent"|"department", "id"}` is accepted too.
  With an agent, the session and ticket are reassigned to them (the agent must
  be able to see the ticket). With only a department, the ticket moves there
  and the chat goes back to the queue (unassigned, waiting) for routing. A
  closed chat, an unknown department or a missing target is a 422. An internal
  system note records the transfer; the visitor's transcript does not show it.
  New `ChatSessionService.TransferSession`.
- `chat.transferred` is sent to outbound webhooks, runs workflows and is
  offered in the webhook form and workflow builder. Its `chat` payload adds
  `from_agent_id`, `to_agent_id` and `to_department_id`, as in Laravel. The
  realtime ticket channel gets `chat.transferred` too.

## [0.2.5] - 2026-10-22

### Upgrading
- Run the migrations: they add `{prefix}api_tokens` (PostgreSQL and SQLite).
- Nothing changes for existing hosts until `Config.APITokensEnabled` is set,
  and with it on, a bearer header the package did not issue is still left to
  `AgentCheck` / `AdminCheck`.

### Added
- API tokens (escalated-laravel `ApiToken`): admins issue, rename, re-scope and
  revoke bearer tokens on `Escalated/Admin/ApiTokens/Index` at
  `GET /admin/api-tokens` (`POST`, `PATCH /{id}`, `DELETE /{id}`;
  `escalated.admin.api-tokens.*`), on both routers. Only each token's SHA-256
  digest is stored; the token is shown once. Tokens carry abilities (`agent`,
  `admin`, `*`), an optional expiry and their last use. With the new
  `Config.APITokensEnabled` they authenticate the REST API's ticket, department
  and tag routes as their owner, narrowed by their abilities; the owner must
  still be staff (`Config.APITokenOwner`, defaulting to `UserDirectory`). See
  docs/api-tokens.md.
- `Escalated/Admin/Settings` at `GET /admin/settings` (`escalated.admin.settings`;
  `POST` saves, `escalated.admin.settings.update`), on both routers. It carries
  only the settings this package stores and honours, named in
  `supported_settings` so the shared page hides the rest:
  `knowledge_base_enabled` and `knowledge_base_public` (read by the widget) and
  `widget_allowed_origins` (as `PUT /admin/settings/widget` saves it). Each is
  optional on save; any other key is ignored. Guest tickets, attachments, the
  reference prefix, inbound mail and the widget's appearance stay in `Config`.
- `Escalated/Admin/Reports/AgentDetail` at `GET /admin/reports/agent/{id}/detail`
  (`escalated.admin.reports.agent-detail`, which the agent ranking links to), on
  both routers: one agent's tickets, response percentiles, productivity (replies
  and notes written, tickets resolved), workload and CSAT breakdown over `days`,
  against the period before. The props are the ones the page declares;
  escalated-laravel's controller passes `agent_id`, `metrics` and `percentiles`,
  which the page does not read.

## [0.2.4] - 2026-10-22

### Upgrading
- Updating a webhook with the secret left blank (or left out) now keeps the
  stored secret, because the form no longer receives it; it used to remove
  it. Send `clear_secret: true` to remove it. A non-blank `secret` replaces it,
  and create is unchanged. This applies to page visits and API clients alike.
- `models.Webhook` encodes as JSON with `has_secret` in place of `secret`, so
  the `GET /admin/webhooks` JSON list no longer carries the secret. Code that
  read `secret` from that JSON should keep its own copy of the secret.
- `models.Webhook.Secret`, `models.TwoFactor.Secret` / `RecoveryCodes`,
  `models.EmailChannel.SmtpPassword`, `models.Ticket.GuestToken` and
  `services.ImportJob.Credentials` are tagged `json:"-"`; the fields are still
  there in Go. The guest flows that hand a guest its token name it explicitly
  and are unchanged.
- Data retention now deletes closed tickets. `POST /admin/data-retention/purge`
  used to only count closed tickets past `retention_closed_tickets`; it now
  deletes them with their replies, attachment rows, followers, tags, subjects,
  activities, ratings, links, workflow logs, delayed actions, chat sessions and
  messages, side conversations and custom field values (tickets merged into one
  are kept and unlinked). Tickets have no soft delete here, so there is no 30-day
  grace period. The setting defaults to `never`, which deletes nothing. The
  purge also deletes expired guest verification codes, and now uses
  `Config.TablePrefix` instead of `escalated_`. Its JSON adds
  `closed_tickets_deleted` and `guest_verifications_deleted`.
- The public tickets page save is validated like Laravel's for a page visit: a
  user id below 1 or a signup template over 500 characters is refused (it was
  truncated). API clients (`PUT` without `X-Inertia`) keep the old handling.
  The page's `settings.guest_policy_user_id` is now the stored string, or null.
- Migrate (or restart, which migrates) to create `escalated_import_jobs` and `escalated_import_source_maps`. Set `Config.ImportSecret` (16 bytes or more) or `GuestAccess.Secret` before using the import wizard: it seals source credentials with it.
- To import attachments, set `Config.ImportAttachmentStorage` (for example `email.NewLocalFileStorage` on a directory the web server does not serve); adapters must send each file's bytes as `content`.
- Custom ticket statuses (additive): `Migrate` / `MigrateSQLite` create
  `ticket_statuses`, seed one system row per built-in status (`open` ...
  `snoozed`, `live`), and add a nullable `tickets.ticket_status_id`
  (`ON DELETE SET NULL`). The integer `status` column and its values are
  unchanged and existing tickets get no custom status, so nothing changes
  until an admin adds one. Webhook and workflow payloads keep their keys and
  values; `ticket.status_changed` (and the lifecycle events fired with it)
  gain `old_custom_status`, `new_custom_status`, `old_category` and
  `new_category`. A macro run without the ticket service now rejects an
  unknown `set_status` value instead of storing 0 (open).
- Custom fields (additive): `Migrate` / `MigrateSQLite` add the nullable
  `custom_fields.placeholder`, `validation_rules` and `conditions` columns.
  Existing fields and values are kept. With no custom fields defined, every
  ticket request behaves as before. Once an active ticket field is
  `required`, ticket creates (`POST /api/tickets`, `POST /tickets`) must send
  it in `custom_fields`, or they get a validation error and no ticket.
- Business hours (additive, nothing changes until you opt in): `Migrate` /
  `MigrateSQLite` add `sla_policies.business_hours_only` (default false),
  `sla_policies.business_schedule_id` and `departments.business_schedule_id`
  (nullable). A policy with `business_hours_only` counts its targets in
  business time against its own schedule, else the ticket department's, else
  the default schedule; with no schedule configured it keeps calendar hours,
  so existing installs see no change. The SLA policy form's
  `business_hours_only` switch is now stored (it was ignored) and
  `business_schedule_id` is accepted on SLA policy and department writes; a
  write that leaves the key out leaves the stored value alone. The
  `Admin/SlaPolicies/Form` and `Admin/Departments/Form` pages receive a
  `schedules` prop (`[{id, name}]`, by name) for the shared frontend's
  **Business hours schedule** picker (omitted when the store keeps no
  business schedules).
- `models.BusinessSchedule` now serialises its hours as `schedule` and
  holidays as `schedule_id` / `recurring` / `YYYY-MM-DD` dates (Laravel's
  keys). `DaySchedule.Enabled` is now `*bool`: a day is open when it has a
  start and end, unless `enabled` is explicitly false.
- Automation `change_status` / `change_priority` actions and `status` /
  `priority` conditions read slugs (`closed`, `urgent`), as the shared editor
  sends them. Actions stored 0 for a slug; conditions compared the slug with
  the integer column and never matched (an error on PostgreSQL).
- Department, tag and public-ticket settings writes from a page visit
  (`X-Inertia`) now get a 303 instead of JSON.
- Workflow `change_status` and `change_priority` actions now read the slug the
  shared builder sends (`resolved`, `high`); they used to store 0 for a slug,
  silently reopening tickets or dropping them to low priority. An unknown
  value now fails the run (logged) instead. Workflows stored with a flat list
  of conditions are evaluated as `all` instead of never matching. New
  workflows go to the end of the list.
- The agent and customer ticket writes now answer a page visit
  (`X-Inertia`) with a 303 back to the page, or the page again with an
  `errors` prop, instead of JSON. API clients (no `X-Inertia`) still get JSON,
  now with `422 {message, errors}` for validation errors where some answered
  400 before. The agent status write validates the status.
- A new SLA policy sent without `first_response_hours` or
  `resolution_hours` is stored with `{}` instead of failing; hours may be
  sent as numbers or numeric strings.
- A webhook saved with an empty `secret` stores none.
- `POST /tickets/{id}/rate` is served by the customer handler: the same
  requester-only rule, with JSON errors.
- The agent dashboard, ticket queue and ticket screen, the customer ticket
  list and ticket screen, and the SLA policy list now render the props the
  shared Vue pages read (escalated-laravel's shapes) instead of raw models:
  `tickets` is a paginator (`tickets.data`, 15 a page), status and priority
  are slugs, relations are nested, and the ticket screens nest replies,
  attachments and activities inside `ticket`. Hosts that read the old props
  (`total`, `my_tickets`, top-level `replies`, `sla_policies`) from these
  routes must switch. See [docs/ui-pages.md](docs/ui-pages.md).
- Set `Config.UserName` to show people by name on the pages. Without it staff
  show as "Support".
- `POST /tickets` (customer) defaults priority to medium, validates subject,
  description, priority, department and ticket type (422, or the form again
  for a page visit), and redirects a page visit to the new ticket.

### Added
- The admin Settings sub-pages, on escalated-laravel's paths, props and
  validation: `Admin/Settings/CsatSettings` (`/admin/settings/csat`),
  `Admin/Settings/DataRetention` (`/admin/settings/data-retention`, with the
  `purgePreview` counts), `Admin/Settings/EmailSettings`
  (`/admin/settings/email`; `email_addresses` and `default_reply_address`,
  stored only) and a read-only `Admin/Settings/DatabaseConnection`
  (`/admin/settings/database`, `/database/test`). Public tickets also accepts
  `POST`, which the page sends. All values live in the settings store.
- `Escalated.PurgeExpired(ctx, dryRun)` and `StartRetentionPurger(ctx,
  interval)` (daily by default) apply the data retention periods, as Laravel's
  `escalated:purge-expired`.
- The admin Import wizard, as escalated-laravel's Admin\ImportController, on its paths and props: `Escalated/Admin/Import/Index` (`GET {prefix}/admin/import`), `Connect` (`GET /admin/import/{platform}/connect`, with `POST .../test-connection` answering `{success[, error]}`, 422 when the source fails), `Mapping` (`GET .../mapping`), `Review` (`POST .../review`, which writes nothing), `start` (`POST .../start`) and `Progress` (`GET /admin/import/progress/{job}`, polled through `GET /admin/import/status/{job}`, with `POST /admin/import/{pause,resume,cancel}/{job}`). Admin only.
  - Sources are `services.ImportAdapter` values in `Config.ImportAdapters`; Escalated ships none, so a Zendesk or Freshdesk adapter is the host's or a plugin package's. `Config.UserIDByEmail` finds host users: agents must exist; contacts that do import as that user, others as Escalated contacts on guest tickets.
  - `services.ImportService` (`Escalated.Imports()`) keeps the jobs and runs them as Laravel's ImportService does: entity types in order, records already in the source map skipped (so a paused, failed or interrupted run resumes without duplicates), references resolved through the map, progress saved per batch, and a stop between batches when the job is paused or cancelled. It imports contacts, agents, tags, departments, custom fields, tickets (with their custom field values), replies, attachments and satisfaction ratings.
  - Tested credentials wait, sealed, in an HttpOnly cookie scoped to the import paths (Escalated has no session) until the import starts, and are never sent in a page; a completed or cancelled job drops them.
  - A started import runs in a goroutine; `Config.ImportDispatcher` hands the job id to the host's queue instead, whose worker calls `Escalated.Imports().Run`.
  - Import mode (`services.WithImportMode`, a context value) holds back SLA deadlines and breach and warning checks for the run.
  - Custom fields: a `custom_fields` record (`name`, `type`, `options`) is found by slug or created, as Laravel's `persistCustomField`; the type must be one the custom field admin accepts and a select needs options. A ticket record's `custom_fields` object (`{key: value}`) is stored as the ticket's values: the wizard's ticket field mapping decides each key (`"custom_fields.<slug>"` lands on that field, `""` skips it), else the key is a custom field source id from the same import, else a slug. Values are checked as a ticket update checks them (type, options, active ticket fields); one that fails is left out and logged under `custom_field_values` with the ticket's source id, and the ticket is kept. Fields a record does not carry are not required.
  - Attachments: an `attachments` record (`parent_type` `ticket` or `reply`, default `reply`; `parent_source_id`, `filename`, `content` as bytes or base64) is checked as a live upload is (Laravel's 10240 KB limit, `Config.ImportAttachmentMaxBytes` to change it; Laravel's allowed extensions; HTML/XML content refused whatever the name), stored under its base name through `Config.ImportAttachmentStorage` and served only by the authorised download route. Unlike Laravel, a source `path` is never trusted: a record without `content` fails, and with no storage configured every attachment fails.
  - Satisfaction ratings: a `satisfaction_ratings` record (`ticket_source_id`, `rating` or `score`, `comment`, `created_at`) is written on its imported ticket as a 1-5 integer, one per ticket, with no rater and none of a live rating's rules or effects.
- Custom ticket statuses, per the shared statuses spec. Admins manage them at
  `/admin/statuses` (`Admin/Statuses/Index` and `Form`,
  `escalated.admin.statuses.index|create|store|edit|update|destroy`, admin
  only) with escalated-laravel's props and validation (`label`, `category`,
  `color`, `description`, `position`, `is_default`); `is_default` is
  exclusive within a category, and the slug comes from the label
  (`waiting_on_vendor`, then `_2`, `_3`; never a built-in's name). Each status
  belongs to a category (`new`, `open`, `pending`, `on_hold`, `solved`); a
  ticket given one takes the category's anchor (`open`, `open`,
  `waiting_on_customer`, `in_progress`, `resolved`) through
  `TicketService.ChangeStatus`'s path, so `resolved_at`/`closed_at`, SLA,
  activity, `ticket.status_changed` (also on a move within one category),
  `ticket.resolved` etc., notifications and workflows behave as for the
  built-in, and `ticket_status_id` records the sub-status. New
  `TicketService.SetCustomStatus` and `ChangeStatusValue`. The agent and admin
  status writes, bulk `status`, `PATCH /api/tickets/{id}`, macro
  `set_status`, and workflow and automation `change_status` take a custom
  slug as well as a built-in value; unknown values are rejected as before.
  Setting a built-in status by any path (including assignment's
  open-to-in-progress move, snooze, unsnooze, merge and escalation) clears
  the custom status. System rows keep their slug and category and cannot be
  deleted (`422`, or the index with an `errors` prop for a page visit).
  Deleting a custom status leaves its tickets in their built-in status;
  changing a status's category does not move tickets already in it.
- Agent and admin ticket pages and the JSON API's ticket list and detail
  carry `custom_status` (`{id, label, slug, color, category}` or null) and
  `status_category`; the agent and admin queues' `status` filter takes a
  custom slug. Customer and guest views show the built-in status only.
- Custom field admin (Laravel's routes and props):
  `Escalated/Admin/CustomFields/Index` and `Form` at `/admin/custom-fields`
  (`escalated.admin.custom-fields.*`, including `reorder`). Fields have a
  type (text, textarea, select, multi_select, checkbox, date, number), a
  context (ticket, user, organization), options, required, placeholder,
  description, validation rules, position and active. A slug is generated
  from the name and de-duplicated (`_2`, `_3`). A select or multi-select
  field needs at least one option. Deleting a field deletes its values.
- Ticket custom field values: the ticket create and update endpoints
  (`POST /api/tickets`, `PATCH /api/tickets/{id}`, the customer `POST
  /tickets`, the agent and admin `PUT /tickets/{id}`) accept a
  `custom_fields` object keyed by field slug (form posts can use
  `custom_fields[slug]` and `custom_fields[slug][]`). Only active
  ticket-context fields are accepted, and unknown keys are rejected. Values
  are checked by type, with errors keyed `custom_fields.<slug>`. On update,
  only the keys sent are checked, and a blank optional value clears it. The
  customer create page gets a `custom_fields` prop (the definitions). The
  agent, admin and customer ticket pages get `custom_fields` (the definitions
  with each decoded `value`), and `GET /api/tickets/{id}` adds
  `ticket.custom_fields`. The guest and widget ticket forms do not take
  custom fields yet.
- Custom objects and records: `Escalated/Admin/CustomObjects/Index`, `Form`
  and `Records` at `/admin/custom-objects` (`escalated.admin.custom-objects.*`,
  `records`, `records.store`, `records.update`, `records.destroy`). Records
  are validated against the object's `fields_schema`, with errors keyed
  `data.<name>`. Deleting an object deletes its records. A record id from
  another object returns 404.
- Every custom field and custom object query builds its table names from
  `Config.TablePrefix`.
- Business hours and holidays (Laravel `BusinessHoursCalculator`,
  `Admin\BusinessHoursController`): `Escalated/Admin/BusinessHours/Index`
  and `Form` at `/admin/business-hours` (`escalated.admin.business-hours.*`),
  with per-weekday hours in an IANA time zone and fixed or recurring
  holidays. `BusinessSchedule.AddBusinessMinutes` / `IsWithinBusinessHours`
  handle overnight windows (end before start), a `24:00` end, holidays on
  the local date a window opens, and daylight-saving days (time is counted in
  real minutes). `store.BusinessSchedulesFor` exposes the schedules on the
  built-in stores; custom stores without it keep calendar hours.
- `Admin/Departments/Form`, `Admin/Automations/Index` and `Form`,
  `Admin/Capacity/Index` and `Admin/Settings/PublicTickets`, and the
  departments and tags lists now carry `tickets_count`. Departments, tags,
  automations and capacity accept `PUT` (Laravel's routes) and tags gain an
  update. Their writes answer a page visit with a 303 or the page again with
  errors; API clients keep their JSON. Automations run the editor's
  `set_ticket_type`.
- Report pages: `Admin/Reports` and `Reports/Dashboard`, `AgentMetrics`,
  `SlaReport`, `CsatReport`, `SlaTrends`, `ResponseTimes`, `ResolutionTimes`,
  `AgentRanking`, `Cohorts` and `Comparison`, at Laravel's
  `/admin/reports/...` paths. The existing JSON report endpoints keep
  answering API clients.
- Workflow pages per the workflow admin contract: `Admin/Workflows/Index`,
  `Form` (create at `/admin/workflows/create`, edit at `GET
  /admin/workflows/{id}`, with `trigger_events`, `action_types` and
  `operators`) and `Logs` (`/admin/workflows/logs`, `?workflow=`), plus
  `PUT /admin/workflows/{id}`, `POST /{id}/toggle` and `POST /reorder`. The
  JSON list and `/{id}/logs` keep answering API clients.
- Workflow conditions on `tags`, `hours_since_created` and
  `hours_since_updated`, and the `insert_canned_reply` action, from the
  contract's core catalog.
- Guest pages on the verified guest flow: `Escalated/Guest/Create` at
  `GET /guest/create` (with `verification_url` and `lookup_url`) posting to
  `POST /guest` with the email code, and `Escalated/Guest/Show` at
  `GET /guest/{token}` opened by the ticket's grant, with replies
  (`/guest/{token}/reply`) and the rating (`/guest/{token}/rate`). Behind the
  guest rate limits, never cached.
- Knowledge base pages: `Admin/KnowledgeBase/Articles/Index` and `Form` and
  `Categories/Index` at Laravel's `/admin/kb-articles` and
  `/admin/kb-categories` resource routes (the `/admin/kb/...` JSON routes
  keep working and render the pages for a page visit), and the reader pages
  `Customer/KnowledgeBase/Index` and `Article` at `/kb` and `/kb/{slug}`, with
  the helpfulness vote at `POST /kb/{slug}/feedback`
  (`knowledge_base_feedback_enabled`, on by default).
- Article and category writes take `category_id` / `parent_id` as a number, a
  numeric string or `""` (none), as the forms send them.
- `Admin/Users/Index` gets Laravel's full paginator (`users.links`, `from`,
  `to`, page URLs), which its pager reads; a role change from the page
  answers with a 303 back, and demoting yourself shows the refusal as
  `errors.role`. API clients get the same JSON as before.
- The admin config pages: `Admin/SlaPolicies/Form` (with `PUT`/`PATCH`
  update, which SLA policies lacked), `Admin/EscalationRules/Index` and
  `Form`, `Admin/Macros/Index`, `Admin/CannedResponses/Index`,
  `Admin/Webhooks/Index`, `Form` and `DeliveryLog`, with Laravel's props. Their
  list URLs render the page for a page visit or an HTML request and keep
  answering JSON otherwise; their writes accept `PUT` as well as `PATCH` and
  answer a page visit with a 303 or the page again with errors.
- Macros run the shared editor's action names (`status`, `priority`, `tags`,
  `department`, `reply`, `note`); tags and departments by id or name.
- `Escalated/Admin/Tickets/Index` and `Escalated/Admin/Tickets/Show` at
  `GET /admin/tickets` and `/admin/tickets/{ticket}`, with Laravel's props
  (`agents` from `Config.SkillAgentDirectory`, `savedViews`, `is_snoozed`)
  and the ticket screen's writes under `/admin/tickets/{ticket}`, plus
  `split`.
- Bulk actions at `POST /agent/tickets/bulk` and `/admin/tickets/bulk`
  (Laravel BulkActionController).
- The ticket panels (subjects, links, side conversations) accept the ticket
  reference and are mounted under `/admin/tickets` too. Merge takes a form
  as well as JSON, and answers a page visit with a 303.
- The ticket screens' writes at escalated-laravel's routes: agent `reply`,
  `note`, `priority`, `tags`, `department`, `macro`, `follow`,
  `replies/{reply}/pin` and `PUT /agent/tickets/{ticket}`; customer `reply`,
  `close` and `reopen`. All take the reference or the id, JSON or a form, and
  status and priority as slugs.
- `Escalated/Customer/Create` at `GET /tickets/create`, with the active
  departments and the priority slugs.
- Ticket page paths take the ticket's reference as well as its id.
- `Config.UserName`, and `docs/ui-pages.md`.
- `page_name_parity_test.go` keeps the list of shipped pages not rendered yet.

### Security
- The agent ticket queue and dashboard no longer send guest tokens to the
  browser.
- **Stored secrets are no longer sent to admin pages or JSON responses**
  (escalated-laravel #258). The webhook edit page received each webhook's
  signing secret, and the `GET /admin/webhooks` JSON list sent it for every
  webhook. The webhook index, edit and delivery-log pages and the JSON list
  now carry `has_secret` instead. Saving the shared edit form (which posts
  `secret: ''`) silently removed the stored secret; a blank secret now keeps
  it. `ImportJob.Credentials` (sealed), `TwoFactor`'s seed and recovery codes,
  `EmailChannel`'s SMTP password and a ticket's guest token are left out of
  their models' JSON encoding, so no handler that encodes one of them can send
  it. The import pages' jobs are now Laravel's `toPageProps()` field list
  (`user_id` and `updated_at` dropped; `error_log` is the last 50 entries); they
  never carried the credentials.

## [0.2.3] - 2026-10-21

### Upgrading
- Hosts that set `Config.Mailer` (for guest verification) now also send
  ticket notification emails. Set `Config.UserEmail` so agents and signed-in
  requesters are reached, or `Config.Notifications.Disabled` to keep them off.
- Guest ticket creation and widget chat start now need the email-code step.
  Set `Config.Mailer`, `Config.MailFrom` and `Config.GuestAccess.Secret`
  (the same on every instance), run the migrations (new
  `guest_verifications` table and four nullable ticket columns), and use a
  client that runs the code step (the bundled widget does). Guest API clients
  read `data.guest_access_token`; `data.guest_token` carries the same grant.
- Permanent guest tokens on existing tickets keep working until a grant is
  issued for the ticket, or until you set
  `GuestAccess.RejectLegacyTokens = true`.
- Nothing runs the SLA check for you. Call `esc.StartSLAChecker(ctx, time.Minute)`
  once at startup, or `esc.CheckSLA` from your own scheduler. Like Laravel,
  `sla.warning` repeats on every run while a deadline is inside the window.
- A workflow stored as `ticket.tagged` now runs on tag additions only, as
  `ticket.tag_added`. No migration is needed.

### Security
- Verified guest access (escalated-laravel `docs/guest-access.md`): guests
  prove their email with a one-time code before creating a ticket
  (`/api/guest/tickets`, `/widget/tickets`), starting a widget chat or looking
  a ticket up. Codes are 8 digits, sent only by email, bound to the email and
  purpose, expire in 10 minutes, allow 5 guesses and are consumed once; only
  an HMAC digest is stored. Code delivery is budgeted per mailbox (3 an hour
  per client IP, 10 across clients).
- Tickets are opened with expiring, sealed grants
  (`guest_access_token`, 24 h by default) instead of a permanent token. A new
  grant invalidates older ones; `esc.GuestAccess.Revoke` invalidates them all.
- Guest and widget ticket responses send `Cache-Control: no-store` and
  `Referrer-Policy: no-referrer`; the legacy widget lookup no longer returns
  reply author ids.

### Added
- Live chat events (ADR 2026-10-21): `chat.started`, `chat.assigned`,
  `chat.message` and `chat.ended` are now sent to outbound webhooks and run
  workflows (they only reached the realtime channel), and the webhook form and
  the workflow builder offer them. There is no chat transfer in this package,
  so `chat.transferred` is neither offered nor sent. The webhook payload is
  `{"chat": {"session_id", "ticket_id", "status", "agent_id",
  "department_id"}}`, plus `reply_id`/`author_type` (`agent` or `visitor`) for
  a message and `ended_by` (`agent`, `customer` or `system` for an idle
  close) for an end. It never carries the visitor's chat token, the visitor's
  email or the message body. Workflows on chat events run against the chat's
  ticket. The mounted routes wire this up; a host that builds its own
  `ChatSessionService` sets `ChatSessionService.Tickets` (for example to
  `esc.NewTicketService()`). Webhooks and workflows saved under
  `chat.session_started` / `chat.session_ended` receive `chat.started` /
  `chat.ended`. The realtime channel event names are unchanged.
- The agent live-chat console is mounted: `GET /agent/chat/sessions`,
  `POST /agent/chat/sessions/{id}/accept`, `/message` and `/end` ({id} is the
  chat ticket). Visitors' chats could not be taken through the package routes.
- `Config.ChatAgentOnline` and `Config.ChatAgentName` reach the widget's chat
  availability and transcript. Without a presence hook the widget reported
  chat as available around the clock, and agents always showed as "Support".
- Audit log: department, tag and SLA policy changes made through the admin
  routes are recorded (who, IP, user agent, old and new values with secrets
  redacted) in `escalated_audit_logs`, and `GET /admin/audit-log` renders
  `Escalated/Admin/AuditLog/Index` with Laravel's filters (`user_id`,
  `action`, `auditable_type`, `date_from`, `date_to`), 50 a page.
  `services.SQLAuditLogStore` backs `AuditLogService`.
- Ticket notification emails through `Config.Mailer`, with the recipients of
  escalated-laravel's notification listeners: ticket created (requester,
  assignee), public reply (requester, assignee, followers, never the author),
  assigned (the assignee), status changed (requester and followers except the
  causer), escalated and SLA breached (the assignee). Internal notes and chat
  messages email nobody. Threaded (`Message-ID` anchor, `In-Reply-To`,
  `References`) with an optional signed `Reply-To`. New `Config.UserEmail`
  (host user addresses) and `Config.Notifications` (`BaseURL`, `Domain`,
  `InboundSecret`, `Branding`, `Disabled`). See docs/notification-emails.md.
- Ticket merge, as escalated-laravel's `TicketMergeService`:
  `POST /admin/tickets/{ticket}/merge` with `{target_reference}` moves every
  reply to the target, adds an internal system note to both tickets and
  closes the source with `merged_into_id`, in one transaction, then records
  the activity and fires `ticket.updated` for both. `GET
  /admin/tickets/merge-search?q=` lists unmerged targets. Admin only.
- `POST /api/guest/verification`, `POST /api/guest/lookup`,
  `POST /api/guest/tickets/{token}/replies`, and on the widget
  `POST /verification`, `POST /lookup` and
  `GET /tickets/{reference}` (Bearer grant). Widget `/config` sends
  `guest_verification_required: true` and `status_lookup_enabled: true`, so
  the widget's Status tab works.
- `Config.Mailer` (`services.MailSender`) and `Config.MailFrom`: the host's
  mail transport. Without one, `services.LogMailSender` logs and sends nothing.
- `Config.GuestAccess` (`Secret`, `TTL`, code budgets, `RejectLegacyTokens`).
- Event vocabulary (escalated-developer-context ADR 2026-10-21): tag changes
  fire `ticket.tag_added` / `ticket.tag_removed` (payload carries `tag`), from
  the new `TicketService.AddTag` / `RemoveTag` and from workflow, automation
  and macro tag actions.
- `Escalated.CheckSLA(ctx, warningMinutes)` and
  `Escalated.StartSLAChecker(ctx, interval)`: mark SLA breaches and fire
  `sla.breached`, then fire `sla.warning` for deadlines in the next 30 minutes,
  to webhooks and workflows. `SLAService.CheckBreaches` had no caller and
  announced nothing. `Escalated.NewTicketService` returns the wired ticket
  service the routers use.

### Changed
- Webhooks and workflows saved under a legacy event name keep firing:
  `ticket.replied`, `internal_note.added`/`created`, `tag.added`,
  `ticket.tagged`, `tag.removed`, `ticket.untagged`, `chat.session_started`
  and `chat.session_ended` are read as their canonical names. The admin
  endpoints store and list canonical names only.
- The workflow builder offers exactly the events workflows run on. Removed:
  `ticket.tagged` (now `ticket.tag_added`) and `reply.agent_reply` (never
  fired). Added: `ticket.resolved`, `ticket.closed`, `ticket.escalated`,
  `ticket.unassigned`, `ticket.tag_added`, `ticket.tag_removed`,
  `note.created`.

### Fixed
- `POST /agent/tickets/{id}/assign` saved the assignee without firing
  `ticket.assigned` / `ticket.updated` to webhooks and workflows.

## [0.2.2] - 2026-10-20

### Changed
- The bundled widget is now `@escalated-dev/escalated` 0.12.3. It keeps its script settings when `embed.js` is placed in the page `<head>`, and Enter no longer picks a stale mention suggestion.

### Security
- Attachment downloads are always `attachment` / `application/octet-stream`
  with `X-Content-Type-Options: nosniff`, `Cache-Control: private, no-store`
  and an RFC 6266/5987 filename. They were served inline with the sender's
  MIME type, so an emailed HTML or SVG attachment rendered on the app origin.
- `POST /tickets/{id}/rate` accepts a rating only from the ticket's requester
  (403 otherwise) and records `rated_by`.

### Fixed
- Agent and customer ticket screens and the SLA policy list rendered page
  names the frontend does not ship (blank panels). They now render
  `Escalated/Agent/TicketIndex`, `TicketShow`, `Escalated/Customer/Index`,
  `Show` and `Escalated/Admin/SlaPolicies/Index`; every page name carries the
  `Escalated/` prefix, and the parity test reads every `Render` call.
- Widget `department_id` sent as a numeric string (embed.js `data-department`)
  is accepted instead of answering 400.
- `ticket.department_changed`, `ticket.priority_changed`, `ticket.unassigned`
  and `ticket.updated` are emitted (webhooks and workflows) through new
  `TicketService.ChangeDepartment/ChangePriority/Unassign/UpdateDetails`.
  `PATCH /api/tickets/{id}` validates status/priority (number or slug, 422
  otherwise) and goes through the service; macros and escalation rules do too.
- The `sla_breached` escalation condition works on PostgreSQL.
- The `knowledge_base_enabled` / `knowledge_base_public` settings are enforced
  on `/api/kb/*` and the widget (404 when off, 403 signed out when private).

### Upgrading
- **Every widget route is rate limited with `Config.GuestRateLimit`.** The
  widget used its own limiter keyed on `RemoteAddr` (one shared bucket behind a
  proxy). It now uses `GuestRateLimit`'s `ClientIP`, `Store` and `Disabled`: a
  general budget of `WidgetConfig.RateLimitPerMin` (now default 0, meaning
  `GuestRateLimit.RequestsPerMinute`, default 60) on every route, plus the
  submission limit (5) on ticket creation and chat start and the reply limit
  (10) on chat messages, ratings and lookups.

## [0.2.1] - 2026-10-19

### Changed
- The bundled widget is now `@escalated-dev/escalated` 0.12.2. It hides its Check Status tab when `/config` returns `status_lookup_enabled: false`.

### Upgrading

- **The widget no longer sends `Access-Control-Allow-Origin: *` by default.**
  The widget handlers used to send `*` when `WidgetConfig.AllowedOrigins` was
  empty, and the raw configured string otherwise, which was invalid with more
  than one origin. They now send no CORS headers themselves. Wrap the widget
  routes in `handlers.WidgetCORS` and list the embedding sites in
  `AllowedOrigins`, `ESCALATED_WIDGET_ALLOWED_ORIGINS` or the
  `widget_allowed_origins` setting. A bare `*` is ignored. See
  [embedding the widget](docs/embedding-the-widget.md).
- **`GET /widget/config` answers 403 `{"enabled": false}` when the widget is
  disabled** (was 503), matching the other backends. The other widget routes
  still answer 503.
- **`NewWidgetChatHandler` now rate-limits chat start.** Its limiter was built
  with a zero window and never limited anything; it now allows
  `WidgetConfig.RateLimitPerMin` chats a minute per client IP, like ticket
  creation.

### Added

- `Config.EnableWidget` mounts the widget in `MountChi` and `MountStdlib`
  under `{RoutePrefix}/widget` (`embed.js`, then every other route behind
  `WidgetCORS`), configured by `Config.Widget`. Off by default, so hosts that
  wire the handlers themselves are unaffected.
- The widget API matches the shared widget: `/config` adds `enabled`, `color`,
  `position`, `greeting`, `departments`, `kb_enabled`,
  `guest_tickets_enabled` and `status_lookup_enabled: false` beside the
  existing keys; `/articles?q=` and `/articles/{slug}` search and show
  published knowledge-base articles; `POST /tickets` accepts `department_id`
  and returns `reference` and `message`; `POST /chat/start` returns `id` (the
  chat's unguessable guest token) and `messages`; and
  `GET|POST /chat/{id}/messages`, `POST /chat/{id}/typing`, `/end` and `/rate`
  are new. Validation errors carry `message` and `errors`.
- Migrations create the `chat_sessions`, `chat_messages` and
  `chat_routing_rules` tables the chat stores already used; without them
  starting a chat failed.

- Cross-origin widget embedding. `handlers.WidgetCORS` answers preflights and
  grants CORS per request only to listed origins (`https://*.example.com`
  subdomain patterns supported), including on 403/404/422/429 answers, and
  runs those requests as guests (`Escalated.TicketAccessMiddleware` ignores the
  signed-in user for them).
- `handlers.WidgetScript` serves the standalone widget bundle
  (`@escalated-dev/escalated` 0.12.1) at `{widget prefix}/embed.js` with an
  ETag and a one-hour cache.
- Widget tickets and live chats store sanitised host `metadata`, including the
  server-recorded `embedded_on` origin. `GET /widget/config` returns
  `accepts_metadata: true`.
- `GET/PUT /admin/settings/widget` reads and validates the
  `widget_allowed_origins` setting.

- Opt-in department/account ticket visibility across both routers, built-in SQL stores, direct handlers, reports, bulk operations, widget and related records.
- Nullable indexed ticket account identity with host requester/subject resolvers, account/type rule conditions and targeted department/type SLAs that preserve routing clocks.
- Assignment and recipient checks, buffered live-event reauthorization, additive migrations and SQLite/PostgreSQL regressions. See [configuration and upgrade notes](docs/ticket-access-and-routing.md).

### Fixed

- **Live chat availability.** `GET /widget/chat/availability` always answered
  `available: false`, so the shared widget never showed its Chat tab: the
  SQLite and PostgreSQL stores never loaded a routing rule's `agent_ids` (and
  SQLite never stored them). Both stores now save and load them (existing
  PostgreSQL `{1,2}` values still read), and `ListActiveChatRoutingRules`
  honours its department argument: a department's own rules first, then the
  global ones. Chat is available when the `chat_enabled` setting is not false
  and an agent on an applicable active rule is online and under the rule's
  `max_concurrent_chats` (5 when unset). The endpoint takes `department_id`
  (or `departmentId`). Hosts that track presence can set
  `ChatAvailabilityService.AgentOnline`; without it every agent a rule names
  counts as accepting chats. Chat routing now assigns those agents too, and no
  longer picks an agent from another department's rule.

## [0.2.0] - 2026-10-08

### Upgrading

- **Custom `email.TicketWriter` implementations must add `ChangeStatus`.**
  The interface gains `ChangeStatus(ctx, ticketID, newStatus, causerID) error`,
  used to reopen a resolved or closed ticket when its requester replies. An
  adapter over `services.TicketService` can forward to its `ChangeStatus`.
- **Register `WithRequesterEmailResolver` if your users reply by email.**
  Inbound email becomes a reply only when `From` is the ticket's requester.
  Without a resolver, only guest tickets (`GuestEmail`) accept email replies;
  replies to user tickets open a new ticket.
- **Agents reply in the app.** Mail from anyone other than the requester, an
  agent's address included, opens a new ticket for the sender.
- **Pass an inbound secret to `NewInboundRouter`.** With one set, only the
  signed Reply-To address links mail to a ticket; `In-Reply-To` / `References`
  and subject references are used only without a secret.
- **Guest endpoints are rate-limited per client IP** (5 tickets and 10 replies
  a minute). Behind a proxy, run a trusted real-IP middleware first or every
  guest shares one limit; set `Config.GuestRateLimit.Store` for a shared
  counter across instances, or `Disabled` if you throttle upstream.

### Security
- **The guest ticket endpoint was not rate-limited.** `POST /api/guest/tickets`
  now allows 5 tickets per client IP per minute and answers 429 with
  `Retry-After` beyond that. `Config.GuestRateLimit` sets the limits
  (`TicketsPerMinute`, `RepliesPerMinute`), switches it off (`Disabled`), and
  takes a shared `Store` for multi-instance deployments and a `ClientIP` func.
  `middleware.GuestRateLimiter` counts ticket submissions and replies in
  separate buckets and runs before the handler, so a guest-token check behind
  it counts refused requests too. Behind a proxy, run a trusted real-IP
  middleware first or every guest shares one IP.
- **Inbound email could post on any ticket.** `InboundEmailService` added a
  reply to whichever ticket the `In-Reply-To` / `References` headers, the
  signed Reply-To or the subject reference pointed at, whoever sent it. Ticket
  Message-IDs and references are guessable. Now, when the router has an inbound
  secret, only the signed Reply-To address links mail to a ticket. A matched
  email becomes a reply only when `From` (case-insensitive) is the ticket's
  guest email or its requester's email, and the reply is posted as that
  requester; the author is never taken from `From`. Anything else opens a new
  ticket for the sender, so no mail is dropped. Hosts whose user requesters
  reply by email register `WithRequesterEmailResolver`; without one, replies to
  user tickets open a new ticket.

### Changed
- **A requester's email reply reopens a resolved or closed ticket.** When
  `InboundEmailService` accepts a reply from the ticket's requester and the
  ticket is resolved or closed, it now moves the ticket to `reopened`, as the
  Laravel, .NET, Spring and Phoenix ports do. Mail from anyone else still opens
  a new ticket and leaves the matched one alone. A failed reopen is logged; the
  reply is kept. **Breaking for custom writers:** `email.TicketWriter` gains
  `ChangeStatus(ctx, ticketID, newStatus, causerID) error`, the same signature
  as `services.TicketService.ChangeStatus`, so an adapter over `TicketService`
  can forward it directly.

## [0.1.3] - 2026-09-13

### Fixed
- **A new ticket could fail because its reference was already taken.** The
  reference's random part was 3 bytes, 24 bits for each month, and the
  `reference` column is unique, so a busy site lost a ticket whenever a draw
  matched a stored one. The random part is now 8 characters of Crockford base32
  (40 bits), e.g. `ESC-2609-7KQ2M9XH`. When an insert still hits a taken
  reference, `CreateTicket` inserts again under a fresh one, up to three
  attempts, on SQLite and PostgreSQL. A reference the caller sets is never
  replaced, other unique violations fail at once, and older six-character
  references keep resolving.

### Security
- **A live-chat guest token could be guessed.** `ChatSessionService.StartSession`
  issued the token with `GenerateReference("GT")`: `GT-`, the year and month,
  and 3 random bytes, so 24 random bits. On a host that serves live chat, the
  token is the visitor's only credential: the public
  `/api/guest/tickets/{token}` route opens the ticket with it alone, and the
  widget's chat and lookup handlers accept it with the ticket reference. All of
  a month's chat tokens could be enumerated. New chats get their token
  from `GenerateGuestToken`, 32 bytes from `crypto/rand` (256 bits), as guest
  tickets already did. The widget now compares tokens in constant time. Tokens
  already stored keep working, and are no stronger than before.

## [0.1.2] - 2026-09-13

### Security
- **The JSON API, customer ticket pages and attachment downloads had no access
  check.** Only `/agent` and `/admin` were guarded, on both routers. Anyone could
  list every ticket through `/api/tickets`, read internal notes, change status
  and priority, open or reply to another customer's ticket, and download any
  attachment by id. Now:
  - The API's ticket, department and tag routes require `AgentCheck` or
    `AdminCheck`. The auth, guest and knowledge-base routes stay public.
  - Customer routes require a signed-in user (`UserIDFunc` returning an id). Show
    and Reply are limited to the ticket's requester.
  - Attachments download for agents and admins, or for the requester of the
    owning ticket, and never from an internal note.

  **Hosts must wire `AgentCheck`/`AdminCheck` for API clients and `UserIDFunc`
  for customer pages.** Requests without them now get 401 or 403 (#113).
- **Webhook URLs could point at internal addresses.** Creating or updating a
  webhook now refuses loopback, private, link-local, carrier-grade NAT and
  reserved destinations (422). The dispatcher's default client checks the
  address it actually connects to, doesn't follow redirects or use a proxy, and
  doesn't retry a refused destination. A host that sets its own
  `WebhookDispatcher.Client` owns that policy (#114).

### Fixed
- **Tagging and following failed on PostgreSQL.** Workflow, automation and macro
  `add_tag`, and `AddFollower`, used SQLite's `INSERT OR IGNORE`, which
  PostgreSQL rejects as a syntax error. They now use `ON CONFLICT DO NOTHING`
  (#111).
- **Escalation rules couldn't be created or toggled on PostgreSQL.**
  `escalation_rules.is_active` shipped as `INTEGER` while the handlers bind a
  bool. A PostgreSQL upgrade step converts the column to `BOOLEAN DEFAULT TRUE`,
  keeping stored values, and is a no-op once applied (#112).

## [0.1.1] - 2026-09-13

### Fixed
- **Automation and macro actions that write a reply always failed.** The
  `add_note` automation action, and the `add_reply`, `add_note` and
  `insert_canned_reply` macro actions, inserted into `is_internal_note` (and the
  automation into `metadata` as well). `escalated_replies` has `is_internal` and
  `is_system` and never had either column, so every one of them failed on
  insert, on SQLite and PostgreSQL alike. Automation notes are now written
  internal and system-authored, the way workflow notes already were; macro
  notes and replies keep the agent who applied the macro as their author.

## [0.1.0] - 2026-09-12

### Fixed
- **The PostgreSQL migrations could not create the schema.** `escalated_replies`
  and `escalated_ticket_activities` passed their `Sprintf` arguments in the
  wrong order, so the statements asked PostgreSQL for `REFERENCES BIGINT(id)`
  and a column typed `escalated_tickets`. Both are refused outright, which means
  `migrations.Migrate` has never completed against a real PostgreSQL server.
  Every test in the package opened SQLite, so nothing had ever said so.

- **Everything outside the store only worked on SQLite.** Handlers, services, the
  newsletter store and the workflow runners build SQL inline with `?`
  placeholders, which PostgreSQL rejects as a syntax error. 196 statements now
  go through `sqldialect.Rebind`, which rewrites them as `$1, $2, …` for a
  PostgreSQL driver and leaves them alone for everything else — including `?`
  inside a string literal, where it is data rather than a placeholder.

- **Inserts could not read back the new row's id.** PostgreSQL's driver does not
  implement `LastInsertId`; the value comes back from a `RETURNING` clause.
  `sqldialect.ExecInsert` adds one and returns a result of the same shape, so
  every call site reads the id exactly as before.

- **Every JSON column failed to read on PostgreSQL.** `json.RawMessage` is a
  `[]byte`, and `database/sql` will only scan a `[]byte` into it. SQLite's driver
  hands back `[]byte`; lib/pq hands back a `string`, and the read failed with
  "unsupported Scan". The 23 JSON columns are now `models.JSONText`, which scans
  from either and marshals identically.

- **Timestamps came back shifted by the server's time zone.** The PostgreSQL
  migrations declared `TIMESTAMP`, which has no time zone, so the driver wrote
  the local wall clock and read it back labelled UTC. A newsletter retry
  scheduled a minute ahead came back hours in the past. They are `TIMESTAMPTZ`
  now, which is what every one of those columns means; SQLite keeps `TIMESTAMP`,
  having no time zones to get wrong.

### Added
- **The test suite can run against PostgreSQL.** `internal/testdb` opens the
  database the suite was told to use — SQLite by default, so running it locally
  still needs nothing installed. PostgreSQL gets a schema per fixture, pinned
  through `search_path`, so tests stay isolated on a shared server without a
  database each.

  An unrecognised `ESCALATED_TEST_DRIVER` fails the test rather than falling
  back: a CI leg that quietly ran SQLite would report green having tested
  nothing the matrix exists for.

  `migrations/postgres_test.go` asserts the schema PostgreSQL actually ends up
  with — the tables, the foreign keys that the swapped arguments turned into
  references to a type, and the column types they mistyped. The whole suite now
  runs on both: **PostgreSQL went from 215 failures to none.**
- **`escalated.New` detects the database it was given.** It picks the PostgreSQL
  or SQLite store from the connection in `Config.DB`, so a host that opened a
  SQLite connection no longer has to know `NewSQLite` exists. `DetectDialect` is
  exported for hosts that want the answer for their own code.

  Detection reads the driver's import path first — lib/pq, pgx, modernc.org/sqlite,
  mattn/go-sqlite3 and the rest are recognised without touching the database — and
  falls back to a single statement for a driver it does not recognise, which is
  what a tracing or proxy wrapper looks like.

### Fixed
- **`New` assumed PostgreSQL.** Handing it a SQLite connection was not an error
  anyone saw at startup; the wrong SQL reached the database on the first query
  that happened to differ. `Config.DatabaseDialect` now defaults to empty rather
  than `"postgres"`, and a dialect Escalated has no store for is named in an
  error instead of being silently treated as PostgreSQL.
- **Configurable database connection, documented and guarded.** `Config.DB` has
  always been a `*sql.DB` the host opens and hands over, so Escalated's tables go
  wherever that connection points — including a database the host application
  otherwise never touches. That was never written down, and nothing stopped it
  from quietly rotting.

  The README now covers it, and a test fails the build if any statement in the
  package references a table the host owns. On a separate database such a query
  does not error; it returns no rows, which reads as users who do not exist.

  Host user data continues to arrive through the `UserDirectory` and
  `SkillAgentDirectory` interfaces, which run on the host's own connection.
  `escalated_tickets.requester_id` is a plain unconstrained column with no
  foreign key, so no query joins the two — no database can join across two
  connections.
- Admin users-management page (`GET/PATCH /admin/users`) backed by a host-supplied `handlers.UserDirectory` hook on `Config.UserDirectory`, mirroring escalated-laravel#94 (admin/agent role toggles, self-demote guard, name+email search)
- Central translation loader at `internal/i18n` consuming `github.com/escalated-dev/escalated-locale/packages/go`, with deep-merge override support at `internal/i18n/overrides/{locale}.json` and a `T(key, locale, params)` helper
- Attachment model, store, handler, and download endpoint (#20)
- Parity with Laravel reference across tickets, workflows, chat, KB, reports (#19)

### Fixed
- Include computed ticket fields in serialization (#21)
- Include chat, context panel, and activity fields for ticket detail serialization (#22)
- Include missing workflow and workflow log computed fields in serialization (#23)

### Internal
- One-command Docker dev/demo environment under `docker/` with click-to-login agent picker (#24)
- `gofmt` cleanup across ticket model and handler files (style commits)

## Initial release

Go port of `escalated` reaching feature parity with the Laravel reference: tickets, workflow engine, chat, KB, reports, SLA tracking, and Inertia-driven Vue frontend served through the shared `@escalated-dev/escalated` package. Versioning follows Go module semantics — consumers pin via `go.mod` against commit SHAs until the first tagged release.
