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
  <a href="README.pt-BR.md">Português (BR)</a> •
  <b>Русский</b> •
  <a href="README.tr.md">Türkçe</a> •
  <a href="README.zh-CN.md">简体中文</a>
</p>

# Escalated Go

Встраиваемая система тикетов поддержки для приложений Go. Работает со стандартным `net/http`, Chi и любым маршрутизатором, принимающим `http.HandlerFunc`.

## Возможности

- Заявки со статусами, приоритетами, типами и отслеживанием SLA
- Ответы (публичные, внутренние заметки, системные сообщения)
- Отделы и теги
- Политики SLA с целевыми показателями ответа/решения для каждого приоритета
- Панель агента и настройка администратора
- Интерфейс Inertia.js или безголовый режим JSON API
- Поддержка PostgreSQL и SQLite
- Фреймворк-независимые HTTP-обработчики
- Встроенные SQL-миграции

### Возможности

- **Ticket splitting** — Разделение ответа в новую самостоятельную заявку с сохранением исходного контекста
- **Ticket snooze** — Откладывание заявок с пресетами (1ч, 4ч, завтра, следующая неделя); фоновый планировщик goroutine автоматически пробуждает их по расписанию
- **Saved views / custom queues** — Сохранение, именование и распространение пресетов фильтров как многоразовых представлений заявок
- **Embeddable support widget** — Лёгкий `<script>` виджет с поиском по базе знаний, формой заявки и проверкой статуса
- **Email threading** — Исходящие письма включают правильные заголовки `In-Reply-To` и `References` для корректной цепочки в почтовых клиентах
- **Branded email templates** — Настраиваемый логотип, основной цвет и текст подвала для всех исходящих писем
- **Real-time updates** — Эндпоинт Server-Sent Events (SSE) для обновлений заявок в реальном времени с автоматическим откатом на опрос
- **Knowledge base toggle** — Включение или отключение публичной базы знаний из настроек администратора

## Установка

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

## Конфигурация

| Поле | Тип | По умолчанию | Описание |
|-------|------|---------|-------------|
| `RoutePrefix` | `string` | `/escalated` | URL-префикс для всех маршрутов |
| `UIEnabled` | `bool` | `true` | Монтировать маршруты Inertia UI; `false` для только JSON API |
| `TablePrefix` | `string` | `escalated_` | Префикс имени таблицы базы данных |
| `AdminCheck` | `func(*http.Request) bool` | `false` | Возвращает true для администраторов |
| `AgentCheck` | `func(*http.Request) bool` | `false` | Возвращает true для агентов |
| `UserIDFunc` | `func(*http.Request) int64` | `0` | Извлекает ID текущего пользователя из запроса |
| `DB` | `*sql.DB` | required | Подключение к базе данных |

## Маршруты API

Все маршруты имеют префикс `RoutePrefix` (по умолчанию `/escalated`).

### JSON API (всегда смонтирован)

| Метод | Путь | Описание |
|--------|------|-------------|
| `GET` | `/api/tickets` | Список заявок (с фильтрами) |
| `POST` | `/api/tickets` | Создать заявку |
| `GET` | `/api/tickets/{id}` | Получить заявку с ответами и активностями |
| `PATCH` | `/api/tickets/{id}` | Обновить заявку |
| `POST` | `/api/tickets/{id}/replies` | Добавить ответ |
| `GET` | `/api/departments` | Список отделов |
| `GET` | `/api/tags` | Список тегов |

### UI клиента (при `UIEnabled: true`)

| Метод | Путь | Описание |
|--------|------|-------------|
| `GET` | `/tickets` | Мои заявки |
| `POST` | `/tickets` | Отправить заявку |
| `GET` | `/tickets/{id}` | Просмотр заявки |
| `POST` | `/tickets/{id}/replies` | Ответить на заявку |

### UI агента (требует `AgentCheck`)

| Метод | Путь | Описание |
|--------|------|-------------|
| `GET` | `/agent/` | Панель агента |
| `GET` | `/agent/tickets` | Очередь заявок |
| `GET` | `/agent/tickets/{id}` | Детали заявки |
| `POST` | `/agent/tickets/{id}/assign` | Назначить заявку |
| `POST` | `/agent/tickets/{id}/replies` | Ответ / внутренняя заметка |
| `POST` | `/agent/tickets/{id}/status` | Изменить статус |

### UI администратора (требует `AdminCheck`)

| Метод | Путь | Описание |
|--------|------|-------------|
| `GET/POST/PATCH/DELETE` | `/admin/departments` | Управление отделами |
| `GET/POST/DELETE` | `/admin/tags` | Управление тегами |
| `GET/POST/DELETE` | `/admin/sla-policies` | Управление политиками SLA |

## Пользовательское хранилище

Реализуйте интерфейс `store.Store` для использования другой базы данных:

```go
esc, _ := escalated.New(cfg)
esc.Store = myCustomStore // satisfies store.Store interface
```

## Статусы заявок

| Значение | Имя |
|-------|------|
| 0 | open |
| 1 | in_progress |
| 2 | waiting_on_customer |
| 3 | waiting_on_agent |
| 4 | escalated |
| 5 | resolved |
| 6 | closed |
| 7 | reopened |

## Приоритеты

| Значение | Имя |
|-------|------|
| 0 | low |
| 1 | medium |
| 2 | high |
| 3 | urgent |
| 4 | critical |

## Лицензия

MIT
