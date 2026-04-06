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
