# Configuration

## Configuration

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `RoutePrefix` | `string` | `/escalated` | URL prefix for all routes |
| `UIEnabled` | `bool` | `true` | Mount Inertia UI routes; `false` for JSON API only |
| `Debug` | `bool` | `false` | A 500 on an Inertia visit goes out as the handler wrote it (a panic reaches your recoverer) instead of the generic Error page; development only (see `docs/ui-pages.md`) |
| `RouteOverrides` | `map[string]escalated.NamedRoute` | nil | Replace, add (e.g. `login`) or remove names in the built-in route list the pages' `route()` reads. See [the shared pages](ui-pages.md#route-names) |
| `TablePrefix` | `string` | `escalated_` | Database table name prefix |
| `AdminCheck` | `func(*http.Request) bool` | `false` | Returns true for admin users |
| `AgentCheck` | `func(*http.Request) bool` | `false` | Returns true for agent users |
| `UserIDFunc` | `func(*http.Request) models.UserID` | `""` | Extracts current user's ID from request |
| `DB` | `*sql.DB` | required | The connection Escalated's own tables live on — see [Separate databases](#separate-databases) |
| `UserDirectory` | `handlers.UserDirectory` | nil | Your bridge to your own users table, for the admin users page. When nil that page lists nothing and the role endpoint responds 501 |
| `SkillAgentDirectory` | `handlers.SkillAgentDirectory` | nil | Lists agents for the Skills form. When nil, `available_agents` is empty |
| `TicketSubjectResolver` | `func(type, id string) (models.TicketSubject, bool)` | nil | Loads host models for subject presentation |
| `TicketSubjectTypes` | `[]string` | nil | Allowlist of `subject_type` values for API attach; empty disables API attach |
| `UserName` | `func(ctx, models.UserID) string` | nil | Display names for the shared pages (requesters, assignees, authors). See [the shared pages](ui-pages.md) |
| `Plugins` | `plugins.Config` | none | Native plugins, the SDK plugin runtime and the capability check. See [plugins](plugins.md) |
| `UI.Embed` | `escalated.EmbedConfig` | off | Embed the admin in your own admin panel (`cfg.EmbedAdmin`). See [embedding the admin](embedding-admin.md) |
| `Access` | `access.Config` | all off | The install-level access switches (roles & permissions, two-factor, single sign-on). See [access control](access-control.md) |
| `GuestRoutes` | `*bool` | nil (on) | Mount the guest routes (guest pages, `/api/guest/*`, `/api/mobile/guest/*`). Nil reads `ESCALATED_GUEST_ROUTES` (unset means on). See [Staff-only installs](#staff-only-installs) |
| `EnableWidget` | `bool` | `false` | Mount the support widget's public routes under `{RoutePrefix}/widget`. See [embedding the widget](embedding-the-widget.md) |
| `CustomerMiddleware` | `[]func(http.Handler) http.Handler` | empty | Extra middleware on the customer routes (`/tickets/...`) and knowledge base pages (`/kb/...`), after the package's own. See [Staff-only installs](#staff-only-installs) |
| `Scheduling.AutoStart` | `bool` | `false` | Start `esc.StartScheduler` in `New` (stopped by `Close`). See [Recurring jobs](#recurring-jobs) |

### Staff-only installs

By default any signed-in user of your app can open the customer portal
(`{RoutePrefix}/tickets`), list their tickets and raise new ones, and the guest
pages and guest API are open to anyone. When only your staff use Escalated,
close those doors:

```go
off := false
cfg.GuestRoutes = &off   // no guest pages, guest API or mobile guest API
cfg.EnableWidget = false // the default: no widget routes
cfg.CustomerMiddleware = []func(http.Handler) http.Handler{
    middleware.RequireAgentOrAdmin(cfg.AgentCheck, cfg.AdminCheck),
}
```

- `GuestRoutes` false (or `ESCALATED_GUEST_ROUTES=false` while the field is
  nil) leaves every guest route unregistered on both routers, and their
  `escalated.guest.*` names leave the route list the pages read.
- `CustomerMiddleware` runs, in order, after the package's own middleware on
  the customer routes (signed-in user, customer presence) and the knowledge
  base reader pages (knowledge base enabled and public settings). Any
  middleware works: refuse with 403, or redirect. Set `cfg.AgentCheck` and
  `cfg.AdminCheck` before building the guard from them, as above.
- `EnableWidget` is already off unless you turn it on; with it off no widget
  route is mounted.

### Recurring jobs

See [Required: schedule the recurring jobs](../README.md#required-schedule-the-recurring-jobs).
`esc.StartScheduler(ctx)` runs the SLA check and snooze wake-ups every minute
and escalation rules and automations every five minutes (each once straight
away), on one instance; `Scheduling.AutoStart` has `New` start it. A host with
its own scheduler calls `esc.CheckSLA`, `esc.WakeSnoozedTickets`,
`esc.RunEscalations` and `esc.RunAutomations` instead. Every successful SLA
check stores a heartbeat (the `scheduler_heartbeat_at` setting); the admin
Settings page, the Reports overview and the report dashboard carry it as the `scheduler` prop
(`{healthy, last_run_at, message, commands}`), unhealthy when the heartbeat is
missing or older than 15 minutes, and `esc.SchedulerStatus(ctx)` returns it for
a health endpoint.

### Setup check

`New` logs every callback it needs that is still unset (`AgentCheck`,
`AdminCheck`, `UserIDFunc`), with how to set it; `esc.SetupCheck()` returns the
same list, empty when nothing is missing. Escalated owns no users table, so
there is no user model to implement: those three callbacks are the whole
contract.

### Which database you are on

`escalated.New` works out whether `Config.DB` is connected to PostgreSQL or
SQLite and picks the matching store. You do not tell it, and you cannot tell it
wrong:

```go
db, _ := sql.Open("sqlite", "escalated.db")   // or "postgres", "pgx", ...

cfg := escalated.DefaultConfig()
cfg.DB = db

esc, err := escalated.New(cfg)                // SQLite store, no flag needed
```

Detection reads the driver's import path first — `github.com/lib/pq`,
`github.com/jackc/pgx/...`, `modernc.org/sqlite`, `github.com/mattn/go-sqlite3`
and the rest are recognised without touching the database. A driver it does not
recognise, such as a tracing or proxy wrapper, is asked directly with one
statement.

A database Escalated has no store for — MySQL, say — is named in the error
rather than guessed at, because a wrong guess does not fail at startup. It fails
on the first query whose SQL happens to differ.

To skip detection, set the dialect yourself:

```go
cfg.DatabaseDialect = escalated.DialectPostgres
```

`escalated.DetectDialect(db)` is exported if you want the answer for your own
code — an installer choosing which migrations to run, for instance.

### Separate databases

`Config.DB` is a `*sql.DB` you open and hand over, so Escalated's tables go
wherever that connection points. Nothing requires it to be your application's
database — it can be a schema shared with a legacy system, a separate reporting
store, or simply a database you would rather not mix support data into:

```go
support, err := sql.Open("postgres", os.Getenv("SUPPORT_DATABASE_URL"))
if err != nil {
    log.Fatal(err)
}

cfg := escalated.DefaultConfig()
cfg.DB = support             // Escalated's tables
cfg.UserDirectory = myUsers  // your users, on your own connection
```

Run Escalated's migrations against that same connection. Your application's own
migrations stay where they are.

#### Your users are never on `Config.DB`

This package issues no SQL against a table it does not own. It never queries a
`users` table, and a test fails the build if any statement in the package
references one.

Host user data arrives through two interfaces you implement — `UserDirectory`
(the admin users page: list, fetch, flip role flags) and `SkillAgentDirectory`
(the agent dropdown on the Skills form). Both run on **your** connection, with
your query, against your schema. Escalated only ever holds ids.

That is also why there is no foreign key from `escalated_tickets.requester_id`
to your users table: it is a plain unconstrained column, so the two databases
need never meet. No query joins across them, because no database can join across
two connections.

The trade is that Escalated cannot filter or sort its tables by a user's own
columns. Skill routing, agent load and assignment all work because they resolve
ids from Escalated's tables first and then ask your directory for the people.

### Ticket subjects

A ticket has a **requester** (who raised it) and a **subject line** (free text).
Tickets can also be *about* host-app entities — a Project, Customer, asset — via
polymorphic **ticket subjects**. Implement `models.TicketSubject` on your host
types and wire `TicketSubjectResolver` plus `TicketSubjectTypes` (for API safety).

```go
type Project struct { ID, Name string }

func (p Project) TicketSubjectTitle() string { return p.Name }
func (p Project) TicketSubjectSubtitle() *string { s := "Project"; return &s }
func (p Project) TicketSubjectURL() *string {
    s := "/projects/" + p.ID
    return &s
}
func (p Project) TicketSubjectColor() *string { s := "#2563eb"; return &s }
func (p Project) TicketSubjectIcon() *string  { s := "folder"; return &s }

cfg.TicketSubjectTypes = []string{"Project"}
cfg.TicketSubjectResolver = func(subjectType, id string) (models.TicketSubject, bool) {
    if subjectType != "Project" {
        return nil, false
    }
    p, err := loadProject(id)
    if err != nil {
        return nil, false
    }
    return p, true
}
```

Ticket JSON includes `subjects[]` with `{type,id,role,title,subtitle,url,color,icon,missing}`.
Attach/detach via `POST` / `DELETE` … `/api/tickets/{id}/subjects` (and agent UI routes).
`subject_id` is stored as a string (integer, UUID, or other host keys).

Use `services.TicketSubjectService` for programmatic `AttachSubject`, `DetachSubject`, and `SyncSubjects`.

### Host user key type (UUID / string users)

Host user ids are stored as `models.UserID` (a string-backed type that accepts
integers and UUID/strings, and JSON-encodes numeric ids as numbers for
back-compat). The DB column type defaults to `BIGINT`. If your host app's user
primary key is a **UUID or other string**, set `ESCALATED_USER_KEY_TYPE` before
running migrations so the host-user columns are created as `VARCHAR(255)`:

```bash
# one of: int (default) | bigint | uuid | string
export ESCALATED_USER_KEY_TYPE=uuid
```

Existing integer-keyed installs need no change — the default produces `BIGINT`
columns and numeric JSON ids exactly as before.
