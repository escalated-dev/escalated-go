<p align="center">
  <a href="README.ar.md">العربية</a> •
  <a href="README.de.md">Deutsch</a> •
  <a href="../../README.md">English</a> •
  <a href="README.es.md">Español</a> •
  <a href="README.fr.md">Français</a> •
  <b>Italiano</b> •
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

Sistema di ticket di supporto integrabile per applicazioni Go. Funziona con `net/http` standard, Chi e qualsiasi router che accetta `http.HandlerFunc`.

## Funzionalità

- Ticket con stati, priorità, tipi e tracciamento SLA
- Risposte (pubbliche, note interne, messaggi di sistema)
- Dipartimenti e tag
- Politiche SLA con obiettivi di risposta/risoluzione per priorità
- Dashboard agente e configurazione admin
- Interfaccia Inertia.js o modalità API JSON headless
- Supporto PostgreSQL e SQLite
- Handler HTTP indipendenti dal framework
- Migrazioni SQL integrate

### Funzionalità

- **Ticket splitting** — Dividere una risposta in un nuovo ticket autonomo preservando il contesto originale
- **Ticket snooze** — Posticipare ticket con preset (1h, 4h, domani, prossima settimana); uno scheduler goroutine in background li risveglia automaticamente secondo programma
- **Saved views / custom queues** — Salvare, denominare e condividere preset di filtri come viste ticket riutilizzabili
- **Embeddable support widget** — Widget `<script>` leggero con ricerca KB, modulo ticket e controllo stato
- **Email threading** — Le email in uscita includono gli header `In-Reply-To` e `References` corretti per il threading appropriato nei client email
- **Branded email templates** — Logo configurabile, colore primario e testo footer per tutte le email in uscita
- **Real-time updates** — Endpoint Server-Sent Events (SSE) per aggiornamenti ticket in tempo reale con fallback automatico di polling
- **Knowledge base toggle** — Abilitare o disabilitare la base di conoscenza pubblica dalle impostazioni admin

## Installazione

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

## Configurazione

| Campo | Tipo | Predefinito | Descrizione |
|-------|------|---------|-------------|
| `RoutePrefix` | `string` | `/escalated` | Prefisso URL per tutte le rotte |
| `UIEnabled` | `bool` | `true` | Montare rotte UI Inertia; `false` per solo API JSON |
| `TablePrefix` | `string` | `escalated_` | Prefisso nome tabella del database |
| `AdminCheck` | `func(*http.Request) bool` | `false` | Restituisce true per utenti admin |
| `AgentCheck` | `func(*http.Request) bool` | `false` | Restituisce true per utenti agente |
| `UserIDFunc` | `func(*http.Request) int64` | `0` | Estrae l'ID dell'utente corrente dalla richiesta |
| `DB` | `*sql.DB` | required | Connessione al database |

## Rotte API

Tutte le rotte sono prefissate con `RoutePrefix` (predefinito `/escalated`).

### JSON API (sempre montata)

| Metodo | Percorso | Descrizione |
|--------|------|-------------|
| `GET` | `/api/tickets` | Elenco ticket (con filtri) |
| `POST` | `/api/tickets` | Creare un ticket |
| `GET` | `/api/tickets/{id}` | Ottenere ticket con risposte e attività |
| `PATCH` | `/api/tickets/{id}` | Aggiornare un ticket |
| `POST` | `/api/tickets/{id}/replies` | Aggiungere una risposta |
| `GET` | `/api/departments` | Elenco dipartimenti |
| `GET` | `/api/tags` | Elenco tag |

### UI Cliente (quando `UIEnabled: true`)

| Metodo | Percorso | Descrizione |
|--------|------|-------------|
| `GET` | `/tickets` | I miei ticket |
| `POST` | `/tickets` | Inviare un ticket |
| `GET` | `/tickets/{id}` | Visualizzare ticket |
| `POST` | `/tickets/{id}/replies` | Rispondere al ticket |

### UI Agente (richiede `AgentCheck`)

| Metodo | Percorso | Descrizione |
|--------|------|-------------|
| `GET` | `/agent/` | Dashboard agente |
| `GET` | `/agent/tickets` | Coda ticket |
| `GET` | `/agent/tickets/{id}` | Dettaglio ticket |
| `POST` | `/agent/tickets/{id}/assign` | Assegnare ticket |
| `POST` | `/agent/tickets/{id}/replies` | Risposta / nota interna |
| `POST` | `/agent/tickets/{id}/status` | Cambiare stato |

### UI Admin (richiede `AdminCheck`)

| Metodo | Percorso | Descrizione |
|--------|------|-------------|
| `GET/POST/PATCH/DELETE` | `/admin/departments` | Gestire dipartimenti |
| `GET/POST/DELETE` | `/admin/tags` | Gestire tag |
| `GET/POST/DELETE` | `/admin/sla-policies` | Gestire politiche SLA |

## Store personalizzato

Implementare l'interfaccia `store.Store` per usare un database diverso:

```go
esc, _ := escalated.New(cfg)
esc.Store = myCustomStore // satisfies store.Store interface
```

## Stati del ticket

| Valore | Nome |
|-------|------|
| 0 | open |
| 1 | in_progress |
| 2 | waiting_on_customer |
| 3 | waiting_on_agent |
| 4 | escalated |
| 5 | resolved |
| 6 | closed |
| 7 | reopened |

## Priorità

| Valore | Nome |
|-------|------|
| 0 | low |
| 1 | medium |
| 2 | high |
| 3 | urgent |
| 4 | critical |

## Licenza

MIT
