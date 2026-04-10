<p align="center">
  <a href="README.ar.md">العربية</a> •
  <a href="README.de.md">Deutsch</a> •
  <a href="../../README.md">English</a> •
  <b>Español</b> •
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

Sistema de tickets de soporte integrable para aplicaciones Go. Funciona con `net/http` estándar, Chi y cualquier enrutador que acepte `http.HandlerFunc`.

## Características

- Tickets con estados, prioridades, tipos y seguimiento de SLA
- Respuestas (públicas, notas internas, mensajes del sistema)
- Departamentos y etiquetas
- Políticas de SLA con objetivos de respuesta/resolución por prioridad
- Panel de agente y configuración de administrador
- Interfaz Inertia.js o modo API JSON sin interfaz
- Soporte para PostgreSQL y SQLite
- Controladores HTTP independientes del framework
- Migraciones SQL integradas

### Características

- **Ticket splitting** — Dividir una respuesta en un nuevo ticket independiente conservando el contexto original
- **Ticket snooze** — Posponer tickets con preajustes (1h, 4h, mañana, la próxima semana); un planificador goroutine en segundo plano los despierta automáticamente según lo programado
- **Saved views / custom queues** — Guardar, nombrar y compartir preajustes de filtros como vistas de tickets reutilizables
- **Embeddable support widget** — Widget `<script>` ligero con búsqueda en KB, formulario de ticket y verificación de estado
- **Email threading** — Los correos salientes incluyen encabezados `In-Reply-To` y `References` correctos para el encadenamiento adecuado en clientes de correo
- **Branded email templates** — Logo configurable, color principal y texto de pie de página para todos los correos salientes
- **Real-time updates** — Endpoint de Server-Sent Events (SSE) para actualizaciones de tickets en vivo con respaldo de sondeo automático
- **Knowledge base toggle** — Habilitar o deshabilitar la base de conocimientos pública desde la configuración del administrador

## Instalación

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

## Inicio Rápido con la Biblioteca Estándar

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

## Configuración

| Campo | Tipo | Predeterminado | Descripción |
|-------|------|---------|-------------|
| `RoutePrefix` | `string` | `/escalated` | Prefijo de URL para todas las rutas |
| `UIEnabled` | `bool` | `true` | Montar rutas de interfaz Inertia; `false` para solo API JSON |
| `TablePrefix` | `string` | `escalated_` | Prefijo de nombre de tabla de la base de datos |
| `AdminCheck` | `func(*http.Request) bool` | `false` | Devuelve true para usuarios administradores |
| `AgentCheck` | `func(*http.Request) bool` | `false` | Devuelve true para usuarios agentes |
| `UserIDFunc` | `func(*http.Request) int64` | `0` | Extrae el ID del usuario actual de la solicitud |
| `DB` | `*sql.DB` | required | Conexión a la base de datos |

## Rutas de API

Todas las rutas están prefijadas con `RoutePrefix` (predeterminado `/escalated`).

### JSON API (siempre montada)

| Método | Ruta | Descripción |
|--------|------|-------------|
| `GET` | `/api/tickets` | Listar tickets (con filtros) |
| `POST` | `/api/tickets` | Crear un ticket |
| `GET` | `/api/tickets/{id}` | Obtener ticket con respuestas y actividades |
| `PATCH` | `/api/tickets/{id}` | Actualizar un ticket |
| `POST` | `/api/tickets/{id}/replies` | Agregar una respuesta |
| `GET` | `/api/departments` | Listar departamentos |
| `GET` | `/api/tags` | Listar etiquetas |

### Interfaz del cliente (cuando `UIEnabled: true`)

| Método | Ruta | Descripción |
|--------|------|-------------|
| `GET` | `/tickets` | Mis tickets |
| `POST` | `/tickets` | Enviar un ticket |
| `GET` | `/tickets/{id}` | Ver ticket |
| `POST` | `/tickets/{id}/replies` | Responder al ticket |

### Interfaz del agente (requiere `AgentCheck`)

| Método | Ruta | Descripción |
|--------|------|-------------|
| `GET` | `/agent/` | Panel del agente |
| `GET` | `/agent/tickets` | Cola de tickets |
| `GET` | `/agent/tickets/{id}` | Detalle del ticket |
| `POST` | `/agent/tickets/{id}/assign` | Asignar ticket |
| `POST` | `/agent/tickets/{id}/replies` | Respuesta / nota interna |
| `POST` | `/agent/tickets/{id}/status` | Cambiar estado |

### Interfaz del administrador (requiere `AdminCheck`)

| Método | Ruta | Descripción |
|--------|------|-------------|
| `GET/POST/PATCH/DELETE` | `/admin/departments` | Gestionar departamentos |
| `GET/POST/DELETE` | `/admin/tags` | Gestionar etiquetas |
| `GET/POST/DELETE` | `/admin/sla-policies` | Gestionar políticas de SLA |

## Almacén personalizado

Implemente la interfaz `store.Store` para usar una base de datos diferente:

```go
esc, _ := escalated.New(cfg)
esc.Store = myCustomStore // satisfies store.Store interface
```

## Estados del ticket

| Valor | Nombre |
|-------|------|
| 0 | open |
| 1 | in_progress |
| 2 | waiting_on_customer |
| 3 | waiting_on_agent |
| 4 | escalated |
| 5 | resolved |
| 6 | closed |
| 7 | reopened |

## Prioridades

| Valor | Nombre |
|-------|------|
| 0 | low |
| 1 | medium |
| 2 | high |
| 3 | urgent |
| 4 | critical |

## Licencia

MIT
