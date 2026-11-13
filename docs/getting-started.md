# Getting started

## Installation

```bash
go get github.com/escalated-dev/escalated-go
```

Requires Go 1.26.9 or later, as declared in [go.mod](../go.mod). The examples use
the PostgreSQL and pure-Go SQLite drivers already included by this module.

## Quick Start with Chi

Replace the authentication callbacks below with checks against your host app's
verified session or JWT. Until then they deny access; request headers alone must
not grant an admin or agent role.

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
    "github.com/escalated-dev/escalated-go/models"
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
        // Replace with an admin check using your verified session or JWT.
        return false
    }
    cfg.AgentCheck = func(r *http.Request) bool {
        // Replace with an agent check using your verified session or JWT.
        return false
    }
    cfg.UserIDFunc = func(r *http.Request) models.UserID {
        // Return the user ID from your host app's verified session or JWT.
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
    log.Fatal(http.ListenAndServe(":8080", r))
}
```

## Quick Start with Standard Library

```go
package main

import (
    "database/sql"
    "log"
    "net/http"

    _ "modernc.org/sqlite"

    escalated "github.com/escalated-dev/escalated-go"
    "github.com/escalated-dev/escalated-go/migrations"
    "github.com/escalated-dev/escalated-go/router"
)

func main() {
    db, err := sql.Open("sqlite", "escalated.db")
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

    // Mount on the standard-library mux
    mux := http.NewServeMux()
    router.MountStdlib(mux, esc)

    log.Println("Listening on :8080")
    log.Fatal(http.ListenAndServe(":8080", mux))
}
```

## Embedding the admin in your own admin panel (optional)

Escalated's admin is a standalone panel at `{RoutePrefix}/admin` by default,
and nothing changes unless you configure this. If your app already has an
admin panel, you can show Escalated's admin screens inside it instead: under
your URL prefix and middleware, in your Vue + Inertia layout, in your
server-rendered admin template, or in an iframe on an allow-listed origin.

```go
cfg.EmbedAdmin(escalated.EmbedConfig{
    Prefix:     "admin/support", // the admin moves to /admin/support/...
    Middleware: []func(http.Handler) http.Handler{requireAdminSession},
    Layout:     "Layouts/AdminLayout",
})
```

Route names do not change, Escalated's admin gate stays on unless you
replace it, and pages carry the admin menu (`escalated.navigation`, or
`esc.AdminNavigation(r)`) for your sidebar. See
[docs/embedding-admin.md](embedding-admin.md).
