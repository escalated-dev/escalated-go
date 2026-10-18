<p align="center">
  <a href="README.ar.md">العربية</a> •
  <a href="README.de.md">Deutsch</a> •
  <a href="../../README.md">English</a> •
  <a href="README.es.md">Español</a> •
  <a href="README.fr.md">Français</a> •
  <a href="README.it.md">Italiano</a> •
  <a href="README.ja.md">日本語</a> •
  <a href="README.ko.md">한국어</a> •
  <b>Nederlands</b> •
  <a href="README.pl.md">Polski</a> •
  <a href="README.pt-BR.md">Português (BR)</a> •
  <a href="README.ru.md">Русский</a> •
  <a href="README.tr.md">Türkçe</a> •
  <a href="README.zh-CN.md">简体中文</a>
</p>

# Escalated Go

Inbedbaar support-ticketsysteem voor Go-applicaties. Werkt met standaard `net/http`, Chi en elke router die `http.HandlerFunc` accepteert.

## Functies

- Tickets met statussen, prioriteiten, typen en SLA-tracking
- Antwoorden (openbaar, interne notities, systeemberichten)
- Afdelingen en tags
- SLA-beleid met respons-/oplossingsdoelen per prioriteit
- Agent-dashboard en admin-configuratie
- Inertia.js-UI of headless JSON API-modus
- PostgreSQL- en SQLite-ondersteuning
- Framework-onafhankelijke HTTP-handlers
- Ingebedde SQL-migraties

### Functies

- **Ticket splitting** — Een antwoord opsplitsen in een nieuw zelfstandig ticket met behoud van de oorspronkelijke context
- **Ticket snooze** — Tickets snoozen met voorinstellingen (1u, 4u, morgen, volgende week); een achtergrond-goroutine-scheduler wekt ze automatisch op schema
- **Saved views / custom queues** — Filter-voorinstellingen opslaan, benoemen en delen als herbruikbare ticketweergaven
- **Embeddable support widget** — Lichtgewicht `<script>`-widget met KB-zoeken, ticketformulier en statuscontrole
- **Email threading** — Uitgaande e-mails bevatten juiste `In-Reply-To`- en `References`-headers voor correct threading in mailclients
- **Branded email templates** — Configureerbaar logo, primaire kleur en voettekst voor alle uitgaande e-mails
- **Real-time updates** — Server-Sent Events (SSE)-endpoint voor live ticketupdates met automatische polling-fallback
- **Knowledge base toggle** — Openbare kennisbank in- of uitschakelen vanuit admin-instellingen

## Installatie

```bash
go get github.com/escalated-dev/escalated-go
```

Vereist Go 1.26.9 of nieuwer met de beveiligingsupdates voor de standaardbibliotheek.

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

## Snelle Start met de Standaardbibliotheek

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

## Configuratie

| Veld | Type | Standaard | Beschrijving |
|-------|------|---------|-------------|
| `RoutePrefix` | `string` | `/escalated` | URL-voorvoegsel voor alle routes |
| `UIEnabled` | `bool` | `true` | Inertia UI-routes aankoppelen; `false` voor alleen JSON API |
| `TablePrefix` | `string` | `escalated_` | Voorvoegsel voor databasetabelnaam |
| `AdminCheck` | `func(*http.Request) bool` | `false` | Geeft true terug voor admin-gebruikers |
| `AgentCheck` | `func(*http.Request) bool` | `false` | Geeft true terug voor agent-gebruikers |
| `UserIDFunc` | `func(*http.Request) models.UserID` | `""` | Haalt de ID van de huidige gebruiker uit het verzoek |
| `DB` | `*sql.DB` | required | Databaseverbinding |

## API-routes

Alle routes zijn voorzien van het voorvoegsel `RoutePrefix` (standaard `/escalated`).

### JSON API (altijd aangekoppeld)

| Methode | Pad | Beschrijving |
|--------|------|-------------|
| `GET` | `/api/tickets` | Tickets weergeven (met filters) |
| `POST` | `/api/tickets` | Een ticket aanmaken |
| `GET` | `/api/tickets/{id}` | Ticket ophalen met antwoorden en activiteiten |
| `PATCH` | `/api/tickets/{id}` | Een ticket bijwerken |
| `POST` | `/api/tickets/{id}/replies` | Een antwoord toevoegen |
| `GET` | `/api/departments` | Afdelingen weergeven |
| `GET` | `/api/tags` | Tags weergeven |

### Klant-UI (wanneer `UIEnabled: true`)

| Methode | Pad | Beschrijving |
|--------|------|-------------|
| `GET` | `/tickets` | Mijn tickets |
| `POST` | `/tickets` | Een ticket indienen |
| `GET` | `/tickets/{id}` | Ticket bekijken |
| `POST` | `/tickets/{id}/replies` | Antwoorden op ticket |

### Agent-UI (vereist `AgentCheck`)

| Methode | Pad | Beschrijving |
|--------|------|-------------|
| `GET` | `/agent/` | Agent-dashboard |
| `GET` | `/agent/tickets` | Ticketwachtrij |
| `GET` | `/agent/tickets/{id}` | Ticketdetail |
| `POST` | `/agent/tickets/{id}/assign` | Ticket toewijzen |
| `POST` | `/agent/tickets/{id}/replies` | Antwoord / interne notitie |
| `POST` | `/agent/tickets/{id}/status` | Status wijzigen |

### Admin-UI (vereist `AdminCheck`)

| Methode | Pad | Beschrijving |
|--------|------|-------------|
| `GET/POST/PATCH/DELETE` | `/admin/departments` | Afdelingen beheren |
| `GET/POST/DELETE` | `/admin/tags` | Tags beheren |
| `GET/POST/DELETE` | `/admin/sla-policies` | SLA-beleid beheren |

## Aangepaste store

Implementeer de `store.Store`-interface om een andere database te gebruiken:

```go
esc, _ := escalated.New(cfg)
esc.Store = myCustomStore // satisfies store.Store interface
```

## Ticketstatussen

| Waarde | Naam |
|-------|------|
| 0 | open |
| 1 | in_progress |
| 2 | waiting_on_customer |
| 3 | waiting_on_agent |
| 4 | escalated |
| 5 | resolved |
| 6 | closed |
| 7 | reopened |

## Prioriteiten

| Waarde | Naam |
|-------|------|
| 0 | low |
| 1 | medium |
| 2 | high |
| 3 | urgent |
| 4 | critical |

## Licentie

MIT
