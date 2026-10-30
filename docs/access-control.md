# Access control: roles & permissions, two-factor, SSO

Escalated ships three access features as install-level switches
(escalated-developer-context ADR `2026-10-29-configurable-access-features`;
escalated-laravel is the reference). Each one is either enforced or hidden:
off, its routes answer 404 and the shared `escalated.features` prop says it is
off; on, it is enforced for real.

| Feature | `Config.Access` field | Environment variable | Default |
|---|---|---|---|
| Roles & permissions | `RolesPermissions` | `ESCALATED_ROLES_PERMISSIONS` | off |
| Two-factor authentication | `TwoFactor` | `ESCALATED_TWO_FACTOR` | off |
| Single sign-on | not yet | | off |

A nil field reads its environment variable (`true`, `1`, `yes` or `on`). The
switches are read once, in `escalated.New`, and never from the settings table:
each one changes who may sign in or what they may do, so a stored setting must
not flip it, and posting a setting with the same name changes nothing.
`Escalated.Features` holds the resolved values.

SSO is not enforced by this package yet, so `features.sso` is always `false` and
its screens are not served.

## Roles & permissions

```go
on := true
cfg.Access.RolesPermissions = &on // or ESCALATED_ROLES_PERMISSIONS=true
```

### What it enforces

- A user's permissions are the union of their roles' permissions
  (`escalated_role_user`, `escalated_role_permission`). Nothing else grants a
  permission: not `AdminCheck`, not the API token's abilities. A user who
  should have everything holds the `admin` role.
- `AdminCheck` and `AgentCheck` still decide who reaches the admin and agent
  areas at all. With the switch on, every admin, agent and staff API route
  **also** needs its permission. Both must pass; a missing permission answers
  **403** (`{"message"}` for a JSON client).
- Every admin, agent and staff API route is in one route-permission map,
  `access.RoutePermissions` (escalated-laravel's `RoutePermissions`, keyed by
  method and path under `RoutePrefix`). A route that matches no entry is
  refused, and a test walks the router so a new route cannot ship without one.
  Reads map to `*.view`, writes to `*.manage` or the finer
  `create`/`edit`/`delete` slugs. The auth, guest, mobile guest and public
  knowledge base API routes are not staff routes and are not checked.
- Actions decided by the request body are checked per action:
  - a status change (ticket screen, `PUT`/`PATCH`, the API) needs
    `ticket.edit`, and also `ticket.close` when the new status is resolved,
    closed or reopened, by slug, number or a custom status in those
    categories;
  - bulk actions need the action's own permission (`assign` needs
    `ticket.assign`, `delete` needs `ticket.delete`, `status` as above, the
    others `ticket.edit`);
  - applying a macro needs `macro.view` and `ticket.edit`;
  - an internal reply (`is_internal`) needs `reply.create_internal` instead of
    `reply.create`.
- API tokens act as their owner: the staff API checks the owner's permissions
  as well as the token's abilities.
- The newsletter screens need `newsletters.manage` (and `newsletters.send` to
  send), on top of `Config.NewsletterPermissionCheck` if you set one.
- Plugin pages and endpoints are left to the plugin's own capability check.
- Users with no role have no permissions. Turning the switch on before
  assigning roles locks staff out.

With the switch off, the gates (`AdminCheck`, `AgentCheck` and your
`NewsletterPermissionCheck`) are the only authority, exactly as before.

### Seeding and the first admin

The migrations create `escalated_roles`, `escalated_permissions`,
`escalated_role_permission` and `escalated_role_user` (with your table prefix;
`role_user.user_id` follows `ESCALATED_USER_KEY_TYPE`). Seed escalated-laravel's
permissions and the system roles `admin` (every permission), `agent` and
`light_agent`, then give the first admin a role:

```go
roles := esc.Roles()
if err := roles.Seed(ctx); err != nil { // idempotent; syncs the system roles
    log.Fatal(err)
}
if err := roles.Assign(ctx, models.UserID("1"), "admin"); err != nil {
    log.Fatal(err)
}
```

`Assign` is escalated-laravel's `escalated:roles:assign`. System roles cannot
be deleted.

### Screens

While the switch is on:

- Admin > Roles renders `Escalated/Admin/Roles/Index` and `Form` at
  `GET /admin/roles`, `/create`, `/{id}/edit`, with `POST /admin/roles`,
  `PUT`/`PATCH /admin/roles/{id}` (`name`, `description`, `permissions` as
  permission ids) and `DELETE /admin/roles/{id}`
  (`escalated.admin.roles.*`).
- The users page carries `roles` (`{id, name, slug}`) and each user's `roles`
  (their role slugs), and `PUT /admin/users/{user}/roles` with `role_ids`
  replaces a user's roles (`escalated.admin.users.roles`). You cannot remove
  your own `role.manage`.

With it off, all of those answer 404 and the users page has no role column.

### Shared props

`AccessMiddleware`, which both routers mount under the route prefix, adds to
every page:

- `escalated.features`: `{roles_permissions, two_factor, sso}`.
- `escalated.permissions`: with the switch on, the signed-in user's slugs;
  with it off, `["*"]` for staff (`AgentCheck` or `AdminCheck`) and `[]` for
  anyone else.

### Turning it on in an existing install

1. Run the migrations.
2. Call `esc.Roles().Seed(ctx)`.
3. Give every staff user a role, at least one `admin`
   (`esc.Roles().Assign`).
4. Set `Config.Access.RolesPermissions` (or `ESCALATED_ROLES_PERMISSIONS=true`).

## Two-factor authentication

```go
on := true
cfg.Access.TwoFactor = &on // or ESCALATED_TWO_FACTOR=true
cfg.Access.Secret = os.Getenv("ESCALATED_ACCESS_SECRET")
cfg.Access.SessionID = func(r *http.Request) string {
    return sessions.ID(r) // your session library's id for this request
}
```

`escalated.New` refuses to start with two-factor on and no `SessionID`, or no
secret (`Secret`, else `GuestAccess.Secret`).

### Escalated owns no sessions

Escalated does not own your login form or your sessions. The challenge sits
between your session and the panel: after your login, a staff user with a
confirmed enrolment is challenged before any admin, agent or staff API page,
and passing it sets an `escalated_two_factor` cookie (HttpOnly, SameSite=Lax,
`Path` the route prefix) that is an HMAC of their user id and **your session
id**. So:

- `SessionID` must return an id that changes when someone signs in. Most
  session libraries renew it on login (they should, against session
  fixation); then every new login is challenged again, and one user's pass
  never counts for another.
- Set `RegenerateSession` if you can renew the session id on demand: it is
  called after a passed challenge, as Laravel's `session()->regenerate()`, and
  the pass is bound to the id it returns.
- `esc.TwoFactorGate().Forget(w, r)` clears the cookie, for your logout
  handler.

### What it enforces

- **TOTP** is RFC 6238 (SHA-1, 6 digits, 30 s) through
  [`pquerna/otp`](https://github.com/pquerna/otp), with one step of drift
  either side. The accepted time step is recorded (a conditional update), and
  that step or an earlier one is refused afterwards, so a code works once.
- **Enrolment** is `Escalated/Admin/Settings/TwoFactor` at
  `GET /admin/settings/two-factor`, open to every staff user (`AgentCheck` or
  `AdminCheck`), not only admins, and needing no permission with roles &
  permissions on. `POST .../setup` creates a pending secret and answers with
  the page carrying `flash.two_factor_setup {qr_uri, recovery_codes}`;
  `POST .../confirm` with `code` confirms it, answers with
  `flash.two_factor_confirmed {recovery_codes}` and counts as passing the
  challenge for this session; `POST .../disable` removes it. With no session
  to flash through, setup and confirm answer with the page itself
  (`Cache-Control: no-store`). Secrets and recovery codes are stored sealed
  (AES-256-GCM under a key derived from the secret) and are never in props.
- **The challenge**: a staff user with a confirmed enrolment who has not
  passed it in this session is redirected from any admin, agent or staff API
  route to `GET /two-factor/challenge`, which renders
  `Escalated/Auth/TwoFactorChallenge` with `action`. A JSON client gets `403
  {"message", "two_factor_required": true}`. `POST /two-factor/challenge` takes
  `code` or `recovery_code`; success goes to the page that was asked for (or
  the agent dashboard / admin reports), failure is a validation error on the
  field sent. Five attempts a minute per user, then 429.
- **Recovery codes**: eight single-use codes, issued at setup. A code that
  signs someone in is removed.
- **Requiring it**: `Config.Access.TwoFactorRequired`
  (`ESCALATED_TWO_FACTOR_REQUIRED`) or the `two_factor_required` setting
  (Admin > Settings, or the enrolment page's toggle; `POST /admin/settings`
  accepts it on its own). The setting wins when an admin has saved it. When
  required, staff without a confirmed enrolment are redirected to the
  enrolment page and can use nothing else. `escalated.two_factor_required` is
  shared with every page.
- **Lost device**: `esc.TwoFactor().Disable(ctx, userID)` clears a user's
  enrolment (escalated-laravel's `escalated:two-factor:reset`).
- API token requests are not challenged: the token API is not session-based.

With the switch off, nobody is challenged, the enrolment and challenge routes
answer 404, the settings page does not offer `two_factor_required`, and
enrolments are kept for when it is turned on.

### Turning it on in an existing install

1. Run the migrations (they add `two_factors.last_used_step`).
2. Set `Secret` (or `GuestAccess.Secret`) and `SessionID`.
3. Set `Config.Access.TwoFactor` (or `ESCALATED_TWO_FACTOR=true`).
4. Require it (`TwoFactorRequired` or the setting) only after telling staff:
   everyone without an enrolment is sent to enrol.
