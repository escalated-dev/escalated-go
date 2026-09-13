<p align="center">
  <a href="docs/translations/README.ar.md">العربية</a> •
  <a href="docs/translations/README.de.md">Deutsch</a> •
  <b>English</b> •
  <a href="docs/translations/README.es.md">Español</a> •
  <a href="docs/translations/README.fr.md">Français</a> •
  <a href="docs/translations/README.it.md">Italiano</a> •
  <a href="docs/translations/README.ja.md">日本語</a> •
  <a href="docs/translations/README.ko.md">한국어</a> •
  <a href="docs/translations/README.nl.md">Nederlands</a> •
  <a href="docs/translations/README.pl.md">Polski</a> •
  <a href="docs/translations/README.pt-BR.md">Português (BR)</a> •
  <a href="docs/translations/README.ru.md">Русский</a> •
  <a href="docs/translations/README.tr.md">Türkçe</a> •
  <a href="docs/translations/README.zh-CN.md">简体中文</a>
</p>

# Escalated Go

Embeddable support ticket system for Go applications. Works with standard `net/http`, Chi, and any router that accepts `http.HandlerFunc`.

## Features

- Tickets with statuses, priorities, types, and SLA tracking
- Replies (public, internal notes, system messages)
- Departments and tags
- SLA policies with per-priority response/resolution targets
- Agent dashboard and admin configuration
- Inertia.js UI or headless JSON API mode
- PostgreSQL and SQLite support
- Framework-agnostic HTTP handlers
- Embedded SQL migrations

### Additional Features

- **Ticket splitting** — Split a reply into a new standalone ticket while preserving the original context
- **Ticket snooze** — Snooze tickets with presets (1h, 4h, tomorrow, next week); a background goroutine scheduler auto-wakes them on schedule
- **Saved views / custom queues** — Save, name, and share filter presets as reusable ticket views
- **Embeddable support widget** — Lightweight `<script>` widget with KB search, ticket form, and status check
- **Email threading** — Outbound emails include proper `In-Reply-To` and `References` headers for correct threading in mail clients
- **Branded email templates** — Configurable logo, primary color, and footer text for all outbound emails
- **Real-time updates** — Server-Sent Events (SSE) endpoint for live ticket updates with automatic polling fallback
- **Knowledge base toggle** — Enable or disable the public knowledge base from admin settings
- **Ticket subjects** — Attach host-app entities (Project, Customer, …) a ticket is about; polymorphic links with UI presentation via `TicketSubject` contract

## Installation

```bash
go get github.com/escalated-dev/escalated-go
```

## Quick Start with Chi

```go
package main

import (
    "database/sql"
    "log"
    "net/http"

    "github.com/go-chi/chi/v5"
    _ "github.com/lib/pq"

    escalated "github.com/escalated-dev/escalated-go"
    "github.com/escalated-dev/escalated-go/migrations"
    "github.com/escalated-dev/escalated-go/router"
)

func main() {
    db, err := sql.Open("postgres", "postgres://localhost/myapp?sslmode=disable")
    if err != nil {
        log.Fatal(err)
    }

    // Run migrations
    if err := migrations.Migrate(db, "escalated_"); err != nil {
        log.Fatal(err)
    }

    // Configure
    cfg := escalated.DefaultConfig()
    cfg.DB = db
    cfg.RoutePrefix = "/support"
    cfg.AdminCheck = func(r *http.Request) bool {
        // Your admin check logic
        return r.Header.Get("X-Admin") == "true"
    }
    cfg.AgentCheck = func(r *http.Request) bool {
        // Your agent check logic
        return r.Header.Get("X-Agent") == "true"
    }
    cfg.UserIDFunc = func(r *http.Request) models.UserID {
        // Extract user ID from session/JWT/etc. models.UserID is a string-
        // backed type, so it works for integer and UUID/string user keys alike.
        return models.UserID("")
    }

    esc, err := escalated.New(cfg)
    if err != nil {
        log.Fatal(err)
    }

    // Mount routes
    r := chi.NewRouter()
    router.MountChi(r, esc)

    log.Println("Listening on :8080")
    http.ListenAndServe(":8080", r)
}
```

## Quick Start with Standard Library

```go
package main

import (
    "database/sql"
    "log"
    "net/http"

    _ "github.com/mattn/go-sqlite3"

    escalated "github.com/escalated-dev/escalated-go"
    "github.com/escalated-dev/escalated-go/migrations"
    "github.com/escalated-dev/escalated-go/router"
)

func main() {
    db, err := sql.Open("sqlite3", "escalated.db")
    if err != nil {
        log.Fatal(err)
    }

    // Run SQLite migrations
    if err := migrations.MigrateSQLite(db, "escalated_"); err != nil {
        log.Fatal(err)
    }

    // Configure (headless API mode)
    cfg := escalated.DefaultConfig()
    cfg.DB = db
    cfg.UIEnabled = false

    // NewSQLite says it explicitly; escalated.New would detect the same thing
    // from the connection.
    esc, err := escalated.NewSQLite(cfg)
    if err != nil {
        log.Fatal(err)
    }

    // Mount on stdlib mux (Go 1.22+)
    mux := http.NewServeMux()
    router.MountStdlib(mux, esc)

    log.Println("Listening on :8080")
    http.ListenAndServe(":8080", mux)
}
```

## Configuration

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `RoutePrefix` | `string` | `/escalated` | URL prefix for all routes |
| `UIEnabled` | `bool` | `true` | Mount Inertia UI routes; `false` for JSON API only |
| `TablePrefix` | `string` | `escalated_` | Database table name prefix |
| `AdminCheck` | `func(*http.Request) bool` | `false` | Returns true for admin users |
| `AgentCheck` | `func(*http.Request) bool` | `false` | Returns true for agent users |
| `UserIDFunc` | `func(*http.Request) models.UserID` | `""` | Extracts current user's ID from request |
| `DB` | `*sql.DB` | required | The connection Escalated's own tables live on — see [Separate databases](#separate-databases) |
| `UserDirectory` | `handlers.UserDirectory` | nil | Your bridge to your own users table, for the admin users page. When nil that page lists nothing and the role endpoint responds 501 |
| `SkillAgentDirectory` | `handlers.SkillAgentDirectory` | nil | Lists agents for the Skills form. When nil, `available_agents` is empty |
| `TicketSubjectResolver` | `func(type, id string) (models.TicketSubject, bool)` | nil | Loads host models for subject presentation |
| `TicketSubjectTypes` | `[]string` | nil | Allowlist of `subject_type` values for API attach; empty disables API attach |

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

## API Routes

All routes are prefixed with `RoutePrefix` (default `/escalated`).

### JSON API (always mounted)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/tickets` | List tickets (with filters) |
| `POST` | `/api/tickets` | Create a ticket |
| `GET` | `/api/tickets/{id}` | Get ticket with replies and activities |
| `PATCH` | `/api/tickets/{id}` | Update a ticket |
| `POST` | `/api/tickets/{id}/replies` | Add a reply |
| `POST` | `/api/tickets/{id}/subjects` | Attach a ticket subject (`type`, `id`, optional `role`) |
| `DELETE` | `/api/tickets/{id}/subjects/{subject}` | Detach a subject link by join-row id |
| `GET` | `/api/departments` | List departments |
| `GET` | `/api/tags` | List tags |

The ticket, department and tag routes above require `AgentCheck` or
`AdminCheck` and return 403 to anyone else. The `/api/auth/*`, `/api/guest/*`
and `/api/kb/*` routes are public.

### Customer UI (when `UIEnabled: true`)

These routes require a signed-in user, meaning `UserIDFunc` returns a
non-empty id, and return 401 otherwise. A customer can view and reply only to
tickets they requested. Any other ticket returns 403.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/tickets` | My tickets |
| `POST` | `/tickets` | Submit a ticket |
| `GET` | `/tickets/{id}` | View ticket |
| `POST` | `/tickets/{id}/replies` | Reply to ticket |

### Attachments (always mounted)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/attachments/{id}/download` | Download an attachment |

Agents and admins can download any attachment. A signed-in customer can
download attachments on tickets they requested, but not attachments on
internal notes. Anyone else gets 401 or 403.

### Agent UI (requires `AgentCheck`)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/agent/` | Agent dashboard |
| `GET` | `/agent/tickets` | Ticket queue |
| `GET` | `/agent/tickets/{id}` | Ticket detail |
| `POST` | `/agent/tickets/{id}/assign` | Assign ticket |
| `POST` | `/agent/tickets/{id}/replies` | Reply / internal note |
| `POST` | `/agent/tickets/{id}/status` | Change status |
| `POST` | `/agent/tickets/{id}/subjects` | Attach a ticket subject |
| `DELETE` | `/agent/tickets/{id}/subjects/{subject}` | Detach a ticket subject |

### Admin UI (requires `AdminCheck`)

| Method | Path | Description |
|--------|------|-------------|
| `GET/POST/PATCH/DELETE` | `/admin/departments` | Manage departments |
| `GET/POST/DELETE` | `/admin/tags` | Manage tags |
| `GET/POST/DELETE` | `/admin/sla-policies` | Manage SLA policies |
| `GET/PUT` | `/admin/settings/public-tickets` | Runtime guest-policy mode (unassigned / guest_user / prompt_signup). See [docs.escalated.dev/public-tickets](https://docs.escalated.dev/public-tickets). |

## Custom Ticket Actions

Add host-defined action buttons to the agent ticket screen via
`Config.TicketActions`. Each visible action is exposed on the ticket responses
(`custom_actions`, plus a top-level `customActions` prop on the agent screen),
and triggering it records an internal note and invokes `Config.OnCustomAction`:

```go
import "github.com/escalated-dev/escalated-go/actions"

cfg := escalated.DefaultConfig()
cfg.TicketActions = []actions.TicketAction{
    {
        Key:          "sync-crm",
        Label:        "Sync CRM",
        Variant:      "primary", // primary | secondary | danger
        Confirmation: "Sync this ticket to the CRM?",
        Metadata:     map[string]any{"icon": "refresh-cw"},
        // Visible/Enabled are optional; nil means always visible/enabled.
        Enabled: func(t *models.Ticket, userID int64) bool { return t.ResolvedAt == nil },
    },
}
cfg.OnCustomAction = func(ctx context.Context, e actions.CustomActionEvent) error {
    if e.Action == "sync-crm" {
        // e.Ticket, e.UserID, e.Payload, e.Metadata
    }
    return nil
}
```

Triggering an action (`POST {prefix}/agent/tickets/{id}/actions/{key}` or the
`/api` equivalent) returns 404 if the action is unknown or not visible, 403 if
it is disabled, otherwise records the audit note and calls `OnCustomAction`.

## Custom Store

Implement the `store.Store` interface to use a different database:

```go
esc, _ := escalated.New(cfg)
esc.Store = myCustomStore // satisfies store.Store interface
```

## Ticket Statuses

| Value | Name |
|-------|------|
| 0 | open |
| 1 | in_progress |
| 2 | waiting_on_customer |
| 3 | waiting_on_agent |
| 4 | escalated |
| 5 | resolved |
| 6 | closed |
| 7 | reopened |

## Priorities

| Value | Name |
|-------|------|
| 0 | low |
| 1 | medium |
| 2 | high |
| 3 | urgent |
| 4 | critical |

## Translations

Strings come from the central [`escalated-locale`](https://github.com/escalated-dev/escalated-locale)
Go module so they stay in sync with every other Escalated plugin. The thin
loader at `internal/i18n` deep-merges optional local overrides on top of the
upstream data:

```go
import "github.com/escalated-dev/escalated-go/internal/i18n"

label := i18n.T("ticket.status.open", "fr", nil)
msg   := i18n.T("validation.required", "en", map[string]any{"field": "Email"})
```

To override a single key without forking the locale file, drop a JSON file at
`internal/i18n/overrides/{locale}.json` — only the keys you list there are
overridden, everything else falls through to the central package. See
`internal/i18n/overrides/README.md` for the full pattern.

## Newsletters (optional, partial port)

This PR establishes the schema, models, and renderer for the newsletter system. The DB-bound planner / dispatcher / tracker services need integration with the host's `store/` layer, which is a follow-up.

```go
import (
    "github.com/escalated-dev/escalated-go/services/newsletter"
    "github.com/yuin/goldmark"
    "bytes"
)

cfg := newsletter.Config{
    BaseURL:             "https://support.example.com",
    DefaultTheme:        "default",
    TrackingEnabled:     true,
    ThemesDir:           "/path/to/templates/newsletter_themes",
    MarkdownToHTML:      func(md string) string {
        var buf bytes.Buffer
        _ = goldmark.Convert([]byte(md), &buf)
        return buf.String()
    },
    Brand: newsletter.Brand{
        Name:            "Acme",
        Accent:          "#2563eb",
        PhysicalAddress: "Acme Inc. · 123 Main St",
    },
    EnableNewsletters:   true,
}

r := newsletter.NewRenderer(cfg)
html, _ := r.Render(&delivery, &n, &contact, nil)
```

The package ships:
- `models/newsletter.go` — 5 entity structs + Contact gains `MarketingOptOutAt`
- `services/newsletter/renderer.go` — Markdown → theme → click rewrite → pixel injection
- `migrations/20260522000001_create_escalated_newsletter_system.sql` — goose SQL migration

Follow-up PR will add the Store interfaces for newsletters (postgres + sqlite) and the planner/dispatcher/tracker services that consume them.

## License

MIT
