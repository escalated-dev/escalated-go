<p align="center">
  <a href="README.ar.md">العربية</a> •
  <b>Deutsch</b> •
  <a href="../../README.md">English</a> •
  <a href="README.es.md">Español</a> •
  <a href="README.fr.md">Français</a> •
  <a href="README.it.md">Italiano</a> •
  <a href="README.ja.md">日本語</a> •
  <a href="README.ko.md">한국어</a> •
  <a href="README.nl.md">Nederlands</a> •
  <a href="README.pl.md">Polski</a> •
  <a href="README.pt-BR.md">Português (BR)</a> •
  <a href="README.ru.md">Русский</a> •
  <a href="README.tr.md">Türkçe</a> •
  <a href="README.zh-CN.md">简体中文</a>
</p>

# Escalated Go

Einbettbares Support-Ticket-System für Go-Anwendungen. Funktioniert mit Standard `net/http`, Chi und jedem Router, der `http.HandlerFunc` akzeptiert.

## Funktionen

- Tickets mit Status, Prioritäten, Typen und SLA-Tracking
- Antworten (öffentlich, interne Notizen, Systemnachrichten)
- Abteilungen und Tags
- SLA-Richtlinien mit Antwort-/Lösungszielen pro Priorität
- Agenten-Dashboard und Admin-Konfiguration
- Inertia.js-UI oder Headless-JSON-API-Modus
- PostgreSQL- und SQLite-Unterstützung
- Framework-unabhängige HTTP-Handler
- Eingebettete SQL-Migrationen

### Funktionen

- **Ticket splitting** — Eine Antwort in ein neues eigenständiges Ticket aufteilen und den ursprünglichen Kontext beibehalten
- **Ticket snooze** — Tickets mit Voreinstellungen schlummern (1h, 4h, morgen, nächste Woche); ein Hintergrund-Goroutine-Scheduler weckt sie automatisch planmäßig
- **Saved views / custom queues** — Filter-Voreinstellungen als wiederverwendbare Ticket-Ansichten speichern, benennen und teilen
- **Embeddable support widget** — Leichtgewichtiges `<script>`-Widget mit KB-Suche, Ticketformular und Statusprüfung
- **Email threading** — Ausgehende E-Mails enthalten korrekte `In-Reply-To`- und `References`-Header für richtiges Threading in Mail-Clients
- **Branded email templates** — Konfigurierbares Logo, Primärfarbe und Fußzeilentext für alle ausgehenden E-Mails
- **Real-time updates** — Server-Sent Events (SSE)-Endpunkt für Live-Ticket-Updates mit automatischem Polling-Fallback
- **Knowledge base toggle** — Öffentliche Wissensdatenbank in den Admin-Einstellungen aktivieren oder deaktivieren

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

## Schnellstart mit der Standardbibliothek

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

## Konfiguration

| Feld | Typ | Standard | Beschreibung |
|-------|------|---------|-------------|
| `RoutePrefix` | `string` | `/escalated` | URL-Präfix für alle Routen |
| `UIEnabled` | `bool` | `true` | Inertia-UI-Routen einbinden; `false` für nur JSON-API |
| `TablePrefix` | `string` | `escalated_` | Datenbanktabellen-Namenspräfix |
| `AdminCheck` | `func(*http.Request) bool` | `false` | Gibt true für Admin-Benutzer zurück |
| `AgentCheck` | `func(*http.Request) bool` | `false` | Gibt true für Agenten-Benutzer zurück |
| `UserIDFunc` | `func(*http.Request) int64` | `0` | Extrahiert die ID des aktuellen Benutzers aus der Anfrage |
| `DB` | `*sql.DB` | required | Datenbankverbindung |

## API-Routen

Alle Routen sind mit `RoutePrefix` (Standard `/escalated`) versehen.

### JSON API (immer eingebunden)

| Methode | Pfad | Beschreibung |
|--------|------|-------------|
| `GET` | `/api/tickets` | Tickets auflisten (mit Filtern) |
| `POST` | `/api/tickets` | Ein Ticket erstellen |
| `GET` | `/api/tickets/{id}` | Ticket mit Antworten und Aktivitäten abrufen |
| `PATCH` | `/api/tickets/{id}` | Ein Ticket aktualisieren |
| `POST` | `/api/tickets/{id}/replies` | Eine Antwort hinzufügen |
| `GET` | `/api/departments` | Abteilungen auflisten |
| `GET` | `/api/tags` | Tags auflisten |

### Kunden-UI (wenn `UIEnabled: true`)

| Methode | Pfad | Beschreibung |
|--------|------|-------------|
| `GET` | `/tickets` | Meine Tickets |
| `POST` | `/tickets` | Ein Ticket einreichen |
| `GET` | `/tickets/{id}` | Ticket anzeigen |
| `POST` | `/tickets/{id}/replies` | Auf Ticket antworten |

### Agenten-UI (erfordert `AgentCheck`)

| Methode | Pfad | Beschreibung |
|--------|------|-------------|
| `GET` | `/agent/` | Agenten-Dashboard |
| `GET` | `/agent/tickets` | Ticket-Warteschlange |
| `GET` | `/agent/tickets/{id}` | Ticket-Details |
| `POST` | `/agent/tickets/{id}/assign` | Ticket zuweisen |
| `POST` | `/agent/tickets/{id}/replies` | Antwort / interne Notiz |
| `POST` | `/agent/tickets/{id}/status` | Status ändern |

### Admin-UI (erfordert `AdminCheck`)

| Methode | Pfad | Beschreibung |
|--------|------|-------------|
| `GET/POST/PATCH/DELETE` | `/admin/departments` | Abteilungen verwalten |
| `GET/POST/DELETE` | `/admin/tags` | Tags verwalten |
| `GET/POST/DELETE` | `/admin/sla-policies` | SLA-Richtlinien verwalten |

## Benutzerdefinierter Store

Implementieren Sie das `store.Store`-Interface, um eine andere Datenbank zu verwenden:

```go
esc, _ := escalated.New(cfg)
esc.Store = myCustomStore // satisfies store.Store interface
```

## Ticket-Status

| Wert | Name |
|-------|------|
| 0 | open |
| 1 | in_progress |
| 2 | waiting_on_customer |
| 3 | waiting_on_agent |
| 4 | escalated |
| 5 | resolved |
| 6 | closed |
| 7 | reopened |

## Prioritäten

| Wert | Name |
|-------|------|
| 0 | low |
| 1 | medium |
| 2 | high |
| 3 | urgent |
| 4 | critical |

## Lizenz

MIT
