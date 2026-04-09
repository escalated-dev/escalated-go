<p align="center">
  <a href="README.ar.md">العربية</a> •
  <a href="README.de.md">Deutsch</a> •
  <a href="../../README.md">English</a> •
  <a href="README.es.md">Español</a> •
  <b>Français</b> •
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

Système de tickets de support intégrable pour les applications Go. Fonctionne avec `net/http` standard, Chi et tout routeur acceptant `http.HandlerFunc`.

## Fonctionnalités

- Tickets avec statuts, priorités, types et suivi SLA
- Réponses (publiques, notes internes, messages système)
- Départements et tags
- Politiques SLA avec objectifs de réponse/résolution par priorité
- Tableau de bord agent et configuration d'administration
- Interface Inertia.js ou mode API JSON sans interface
- Support PostgreSQL et SQLite
- Gestionnaires HTTP indépendants du framework
- Migrations SQL embarquées

### Fonctionnalités

- **Ticket splitting** — Diviser une réponse en un nouveau ticket autonome tout en préservant le contexte original
- **Ticket snooze** — Mettre en veille les tickets avec des préréglages (1h, 4h, demain, semaine prochaine) ; un planificateur goroutine en arrière-plan les réveille automatiquement
- **Saved views / custom queues** — Sauvegarder, nommer et partager des préréglages de filtres comme vues de tickets réutilisables
- **Embeddable support widget** — Widget `<script>` léger avec recherche KB, formulaire de ticket et vérification de statut
- **Email threading** — Les e-mails sortants incluent les en-têtes `In-Reply-To` et `References` corrects pour un chaînage approprié
- **Branded email templates** — Logo configurable, couleur principale et texte de pied de page pour tous les e-mails sortants
- **Real-time updates** — Endpoint Server-Sent Events (SSE) pour les mises à jour de tickets en direct avec repli automatique par sondage
- **Knowledge base toggle** — Activer ou désactiver la base de connaissances publique depuis les paramètres administrateur

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

| Champ | Type | Défaut | Description |
|-------|------|---------|-------------|
| `RoutePrefix` | `string` | `/escalated` | Préfixe URL pour toutes les routes |
| `UIEnabled` | `bool` | `true` | Monter les routes d'interface Inertia ; `false` pour API JSON uniquement |
| `TablePrefix` | `string` | `escalated_` | Préfixe de nom de table de la base de données |
| `AdminCheck` | `func(*http.Request) bool` | `false` | Renvoie true pour les utilisateurs administrateurs |
| `AgentCheck` | `func(*http.Request) bool` | `false` | Renvoie true pour les utilisateurs agents |
| `UserIDFunc` | `func(*http.Request) int64` | `0` | Extrait l'ID de l'utilisateur actuel de la requête |
| `DB` | `*sql.DB` | required | Connexion à la base de données |

## Routes API

Toutes les routes sont préfixées avec `RoutePrefix` (défaut `/escalated`).

### JSON API (toujours montée)

| Méthode | Chemin | Description |
|--------|------|-------------|
| `GET` | `/api/tickets` | Lister les tickets (avec filtres) |
| `POST` | `/api/tickets` | Créer un ticket |
| `GET` | `/api/tickets/{id}` | Obtenir le ticket avec réponses et activités |
| `PATCH` | `/api/tickets/{id}` | Mettre à jour un ticket |
| `POST` | `/api/tickets/{id}/replies` | Ajouter une réponse |
| `GET` | `/api/departments` | Lister les départements |
| `GET` | `/api/tags` | Lister les tags |

### Interface client (quand `UIEnabled: true`)

| Méthode | Chemin | Description |
|--------|------|-------------|
| `GET` | `/tickets` | Mes tickets |
| `POST` | `/tickets` | Soumettre un ticket |
| `GET` | `/tickets/{id}` | Voir le ticket |
| `POST` | `/tickets/{id}/replies` | Répondre au ticket |

### Interface agent (nécessite `AgentCheck`)

| Méthode | Chemin | Description |
|--------|------|-------------|
| `GET` | `/agent/` | Tableau de bord de l'agent |
| `GET` | `/agent/tickets` | File de tickets |
| `GET` | `/agent/tickets/{id}` | Détail du ticket |
| `POST` | `/agent/tickets/{id}/assign` | Affecter le ticket |
| `POST` | `/agent/tickets/{id}/replies` | Réponse / note interne |
| `POST` | `/agent/tickets/{id}/status` | Changer le statut |

### Interface admin (nécessite `AdminCheck`)

| Méthode | Chemin | Description |
|--------|------|-------------|
| `GET/POST/PATCH/DELETE` | `/admin/departments` | Gérer les départements |
| `GET/POST/DELETE` | `/admin/tags` | Gérer les tags |
| `GET/POST/DELETE` | `/admin/sla-policies` | Gérer les politiques SLA |

## Store personnalisé

Implémentez l'interface `store.Store` pour utiliser une base de données différente :

```go
esc, _ := escalated.New(cfg)
esc.Store = myCustomStore // satisfies store.Store interface
```

## Statuts des tickets

| Valeur | Nom |
|-------|------|
| 0 | open |
| 1 | in_progress |
| 2 | waiting_on_customer |
| 3 | waiting_on_agent |
| 4 | escalated |
| 5 | resolved |
| 6 | closed |
| 7 | reopened |

## Priorités

| Valeur | Nom |
|-------|------|
| 0 | low |
| 1 | medium |
| 2 | high |
| 3 | urgent |
| 4 | critical |

## Licence

MIT
