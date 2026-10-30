# Access control: roles & permissions, two-factor, SSO

Escalated ships three access features as install-level switches
(escalated-developer-context ADR `2026-10-29-configurable-access-features`;
escalated-laravel is the reference). Each one is either enforced or hidden:
off, its routes answer 404 and the shared `escalated.features` prop says it is
off; on, it is enforced for real.

| Feature | `Config.Access` field | Environment variable | Default |
|---|---|---|---|
| Roles & permissions | `RolesPermissions` | `ESCALATED_ROLES_PERMISSIONS` | off |
| Two-factor authentication | not yet | | off |
| Single sign-on | not yet | | off |

A nil field reads its environment variable (`true`, `1`, `yes` or `on`). The
switches are read once, in `escalated.New`, and never from the settings table:
each one changes who may sign in or what they may do, so a stored setting must
not flip it, and posting a setting with the same name changes nothing.
`Escalated.Features` holds the resolved values.

Two-factor and SSO are not enforced by this package yet, so `features.two_factor`
and `features.sso` are always `false` and their screens are not served.

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
