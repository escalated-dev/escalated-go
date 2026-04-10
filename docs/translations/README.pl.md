<p align="center">
  <a href="README.ar.md">العربية</a> •
  <a href="README.de.md">Deutsch</a> •
  <a href="../../README.md">English</a> •
  <a href="README.es.md">Español</a> •
  <a href="README.fr.md">Français</a> •
  <a href="README.it.md">Italiano</a> •
  <a href="README.ja.md">日本語</a> •
  <a href="README.ko.md">한국어</a> •
  <a href="README.nl.md">Nederlands</a> •
  <b>Polski</b> •
  <a href="README.pt-BR.md">Português (BR)</a> •
  <a href="README.ru.md">Русский</a> •
  <a href="README.tr.md">Türkçe</a> •
  <a href="README.zh-CN.md">简体中文</a>
</p>

# Escalated Go

Wbudowywalny system zgłoszeń wsparcia dla aplikacji Go. Działa ze standardowym `net/http`, Chi i dowolnym routerem akceptującym `http.HandlerFunc`.

## Funkcje

- Zgłoszenia ze statusami, priorytetami, typami i śledzeniem SLA
- Odpowiedzi (publiczne, notatki wewnętrzne, wiadomości systemowe)
- Działy i tagi
- Polityki SLA z celami odpowiedzi/rozwiązania na priorytet
- Panel agenta i konfiguracja administratora
- Interfejs Inertia.js lub tryb bezinterfejsowy JSON API
- Obsługa PostgreSQL i SQLite
- Handlery HTTP niezależne od frameworka
- Wbudowane migracje SQL

### Funkcje

- **Ticket splitting** — Podziel odpowiedź na nowe samodzielne zgłoszenie zachowując oryginalny kontekst
- **Ticket snooze** — Odłóż zgłoszenia z presetami (1h, 4h, jutro, następny tydzień); scheduler goroutine w tle budzi je automatycznie zgodnie z harmonogramem
- **Saved views / custom queues** — Zapisuj, nazywaj i udostępniaj presety filtrów jako widoki zgłoszeń wielokrotnego użytku
- **Embeddable support widget** — Lekki widget `<script>` z wyszukiwaniem KB, formularzem zgłoszenia i sprawdzaniem statusu
- **Email threading** — E-maile wychodzące zawierają prawidłowe nagłówki `In-Reply-To` i `References` dla poprawnego wątkowania w klientach poczty
- **Branded email templates** — Konfigurowalny logo, kolor główny i tekst stopki dla wszystkich e-maili wychodzących
- **Real-time updates** — Endpoint Server-Sent Events (SSE) dla aktualizacji zgłoszeń na żywo z automatycznym pollingiem zapasowym
- **Knowledge base toggle** — Włączanie lub wyłączanie publicznej bazy wiedzy z ustawień administratora

## Instalacja

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

## Szybki Start z Biblioteką Standardową

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

## Konfiguracja

| Pole | Typ | Domyślny | Opis |
|-------|------|---------|-------------|
| `RoutePrefix` | `string` | `/escalated` | Prefiks URL dla wszystkich tras |
| `UIEnabled` | `bool` | `true` | Montowanie tras interfejsu Inertia; `false` dla samego JSON API |
| `TablePrefix` | `string` | `escalated_` | Prefiks nazwy tabeli bazy danych |
| `AdminCheck` | `func(*http.Request) bool` | `false` | Zwraca true dla użytkowników administratorów |
| `AgentCheck` | `func(*http.Request) bool` | `false` | Zwraca true dla użytkowników agentów |
| `UserIDFunc` | `func(*http.Request) int64` | `0` | Wyodrębnia ID bieżącego użytkownika z żądania |
| `DB` | `*sql.DB` | required | Połączenie z bazą danych |

## Trasy API

Wszystkie trasy mają prefiks `RoutePrefix` (domyślnie `/escalated`).

### JSON API (zawsze zamontowane)

| Metoda | Ścieżka | Opis |
|--------|------|-------------|
| `GET` | `/api/tickets` | Lista zgłoszeń (z filtrami) |
| `POST` | `/api/tickets` | Utwórz zgłoszenie |
| `GET` | `/api/tickets/{id}` | Pobierz zgłoszenie z odpowiedziami i aktywnościami |
| `PATCH` | `/api/tickets/{id}` | Zaktualizuj zgłoszenie |
| `POST` | `/api/tickets/{id}/replies` | Dodaj odpowiedź |
| `GET` | `/api/departments` | Lista działów |
| `GET` | `/api/tags` | Lista tagów |

### Interfejs klienta (gdy `UIEnabled: true`)

| Metoda | Ścieżka | Opis |
|--------|------|-------------|
| `GET` | `/tickets` | Moje zgłoszenia |
| `POST` | `/tickets` | Wyślij zgłoszenie |
| `GET` | `/tickets/{id}` | Wyświetl zgłoszenie |
| `POST` | `/tickets/{id}/replies` | Odpowiedz na zgłoszenie |

### Interfejs agenta (wymaga `AgentCheck`)

| Metoda | Ścieżka | Opis |
|--------|------|-------------|
| `GET` | `/agent/` | Panel agenta |
| `GET` | `/agent/tickets` | Kolejka zgłoszeń |
| `GET` | `/agent/tickets/{id}` | Szczegóły zgłoszenia |
| `POST` | `/agent/tickets/{id}/assign` | Przypisz zgłoszenie |
| `POST` | `/agent/tickets/{id}/replies` | Odpowiedź / notatka wewnętrzna |
| `POST` | `/agent/tickets/{id}/status` | Zmień status |

### Interfejs administratora (wymaga `AdminCheck`)

| Metoda | Ścieżka | Opis |
|--------|------|-------------|
| `GET/POST/PATCH/DELETE` | `/admin/departments` | Zarządzaj działami |
| `GET/POST/DELETE` | `/admin/tags` | Zarządzaj tagami |
| `GET/POST/DELETE` | `/admin/sla-policies` | Zarządzaj politykami SLA |

## Niestandardowy magazyn

Zaimplementuj interfejs `store.Store`, aby użyć innej bazy danych:

```go
esc, _ := escalated.New(cfg)
esc.Store = myCustomStore // satisfies store.Store interface
```

## Statusy zgłoszeń

| Wartość | Nazwa |
|-------|------|
| 0 | open |
| 1 | in_progress |
| 2 | waiting_on_customer |
| 3 | waiting_on_agent |
| 4 | escalated |
| 5 | resolved |
| 6 | closed |
| 7 | reopened |

## Priorytety

| Wartość | Nazwa |
|-------|------|
| 0 | low |
| 1 | medium |
| 2 | high |
| 3 | urgent |
| 4 | critical |

## Licencja

MIT
