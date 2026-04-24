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
- **Inbound email** — Single webhook endpoint with Postmark + Mailgun + AWS SES parsers, signed Reply-To verification, and Message-ID-based ticket resolution
- **Branded email templates** — Configurable logo, primary color, and footer text for all outbound emails
- **Real-time updates** — Server-Sent Events (SSE) endpoint for live ticket updates with automatic polling fallback
- **Knowledge base toggle** — Enable or disable the public knowledge base from admin settings

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
    cfg.UserIDFunc = func(r *http.Request) int64 {
        // Extract user ID from session/JWT/etc.
        return 0
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

## Inbound email

Point your Postmark, Mailgun, or AWS SES (via SNS HTTP subscription) inbound webhook at:

```
POST /escalated/webhook/email/inbound?adapter=postmark
POST /escalated/webhook/email/inbound?adapter=mailgun
POST /escalated/webhook/email/inbound?adapter=ses
```

The adapter can be selected via the query parameter or the `X-Escalated-Adapter` header. Your provider must attach the shared secret as an `X-Escalated-Inbound-Secret` header, which is compared with `hmac.Equal` (timing-safe).

Configure the symmetric secret + mail domain (used for signed `Reply-To` + canonical `Message-ID` headers):

```go
cfg := email.Config{
    MailDomain:    os.Getenv("ESCALATED_MAIL_DOMAIN"),
    InboundSecret: os.Getenv("ESCALATED_INBOUND_SECRET"),
}
```

The service resolves inbound messages to existing tickets via, in order: canonical `Message-ID` headers, signed `Reply-To` verification, and subject-reference tags. Unmatched messages with real content create a new ticket; SNS subscription confirmations and empty body+subject messages are skipped.

See the [inbound email docs](https://docs.escalated.dev/inbound-email) for provider setup, the response shape, and a ready-to-paste curl test recipe.

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
| `UserIDFunc` | `func(*http.Request) int64` | `0` | Extracts current user's ID from request |
| `DB` | `*sql.DB` | required | Database connection |

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
| `GET` | `/api/departments` | List departments |
| `GET` | `/api/tags` | List tags |

### Customer UI (when `UIEnabled: true`)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/tickets` | My tickets |
| `POST` | `/tickets` | Submit a ticket |
| `GET` | `/tickets/{id}` | View ticket |
| `POST` | `/tickets/{id}/replies` | Reply to ticket |

### Agent UI (requires `AgentCheck`)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/agent/` | Agent dashboard |
| `GET` | `/agent/tickets` | Ticket queue |
| `GET` | `/agent/tickets/{id}` | Ticket detail |
| `POST` | `/agent/tickets/{id}/assign` | Assign ticket |
| `POST` | `/agent/tickets/{id}/replies` | Reply / internal note |
| `POST` | `/agent/tickets/{id}/status` | Change status |

### Admin UI (requires `AdminCheck`)

| Method | Path | Description |
|--------|------|-------------|
| `GET/POST/PATCH/DELETE` | `/admin/departments` | Manage departments |
| `GET/POST/DELETE` | `/admin/tags` | Manage tags |
| `GET/POST/DELETE` | `/admin/sla-policies` | Manage SLA policies |

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

## License

MIT
