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
  <a href="README.pl.md">Polski</a> •
  <b>Português (BR)</b> •
  <a href="README.ru.md">Русский</a> •
  <a href="README.tr.md">Türkçe</a> •
  <a href="README.zh-CN.md">简体中文</a>
</p>

# Escalated Go

Sistema de tickets de suporte incorporável para aplicações Go. Funciona com `net/http` padrão, Chi e qualquer roteador que aceite `http.HandlerFunc`.

## Funcionalidades

- Tickets com status, prioridades, tipos e rastreamento de SLA
- Respostas (públicas, notas internas, mensagens do sistema)
- Departamentos e tags
- Políticas de SLA com metas de resposta/resolução por prioridade
- Painel do agente e configuração do administrador
- Interface Inertia.js ou modo API JSON sem interface
- Suporte a PostgreSQL e SQLite
- Handlers HTTP independentes de framework
- Migrações SQL incorporadas

### Funcionalidades

- **Ticket splitting** — Dividir uma resposta em um novo ticket independente preservando o contexto original
- **Ticket snooze** — Adiar tickets com predefinições (1h, 4h, amanhã, próxima semana); um agendador goroutine em segundo plano os desperta automaticamente conforme programado
- **Saved views / custom queues** — Salvar, nomear e compartilhar predefinições de filtro como visualizações de tickets reutilizáveis
- **Embeddable support widget** — Widget `<script>` leve com busca na KB, formulário de ticket e verificação de status
- **Email threading** — E-mails enviados incluem cabeçalhos `In-Reply-To` e `References` corretos para encadeamento adequado em clientes de e-mail
- **Branded email templates** — Logo configurável, cor primária e texto de rodapé para todos os e-mails enviados
- **Real-time updates** — Endpoint Server-Sent Events (SSE) para atualizações de tickets ao vivo com fallback automático de polling
- **Knowledge base toggle** — Ativar ou desativar a base de conhecimento pública nas configurações do administrador

## Instalação

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

## Configuração

| Campo | Tipo | Padrão | Descrição |
|-------|------|---------|-------------|
| `RoutePrefix` | `string` | `/escalated` | Prefixo de URL para todas as rotas |
| `UIEnabled` | `bool` | `true` | Montar rotas de interface Inertia; `false` para apenas API JSON |
| `TablePrefix` | `string` | `escalated_` | Prefixo do nome da tabela do banco de dados |
| `AdminCheck` | `func(*http.Request) bool` | `false` | Retorna true para usuários administradores |
| `AgentCheck` | `func(*http.Request) bool` | `false` | Retorna true para usuários agentes |
| `UserIDFunc` | `func(*http.Request) int64` | `0` | Extrai o ID do usuário atual da requisição |
| `DB` | `*sql.DB` | required | Conexão com o banco de dados |

## Rotas da API

Todas as rotas são prefixadas com `RoutePrefix` (padrão `/escalated`).

### JSON API (sempre montada)

| Método | Caminho | Descrição |
|--------|------|-------------|
| `GET` | `/api/tickets` | Listar tickets (com filtros) |
| `POST` | `/api/tickets` | Criar um ticket |
| `GET` | `/api/tickets/{id}` | Obter ticket com respostas e atividades |
| `PATCH` | `/api/tickets/{id}` | Atualizar um ticket |
| `POST` | `/api/tickets/{id}/replies` | Adicionar uma resposta |
| `GET` | `/api/departments` | Listar departamentos |
| `GET` | `/api/tags` | Listar tags |

### UI do Cliente (quando `UIEnabled: true`)

| Método | Caminho | Descrição |
|--------|------|-------------|
| `GET` | `/tickets` | Meus tickets |
| `POST` | `/tickets` | Enviar um ticket |
| `GET` | `/tickets/{id}` | Visualizar ticket |
| `POST` | `/tickets/{id}/replies` | Responder ao ticket |

### UI do Agente (requer `AgentCheck`)

| Método | Caminho | Descrição |
|--------|------|-------------|
| `GET` | `/agent/` | Painel do agente |
| `GET` | `/agent/tickets` | Fila de tickets |
| `GET` | `/agent/tickets/{id}` | Detalhe do ticket |
| `POST` | `/agent/tickets/{id}/assign` | Atribuir ticket |
| `POST` | `/agent/tickets/{id}/replies` | Resposta / nota interna |
| `POST` | `/agent/tickets/{id}/status` | Alterar status |

### UI do Admin (requer `AdminCheck`)

| Método | Caminho | Descrição |
|--------|------|-------------|
| `GET/POST/PATCH/DELETE` | `/admin/departments` | Gerenciar departamentos |
| `GET/POST/DELETE` | `/admin/tags` | Gerenciar tags |
| `GET/POST/DELETE` | `/admin/sla-policies` | Gerenciar políticas de SLA |

## Store personalizado

Implemente a interface `store.Store` para usar um banco de dados diferente:

```go
esc, _ := escalated.New(cfg)
esc.Store = myCustomStore // satisfies store.Store interface
```

## Status do ticket

| Valor | Nome |
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

| Valor | Nome |
|-------|------|
| 0 | low |
| 1 | medium |
| 2 | high |
| 3 | urgent |
| 4 | critical |

## Licença

MIT
