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
  <a href="README.ru.md">Русский</a> •
  <a href="README.tr.md">Türkçe</a> •
  <b>简体中文</b>
</p>

# Escalated Go

适用于 Go 应用程序的可嵌入式支持工单系统。兼容标准 `net/http`、Chi 以及任何接受 `http.HandlerFunc` 的路由器。

## 功能

- 带有状态、优先级、类型和 SLA 跟踪的工单
- 回复（公开、内部备注、系统消息）
- 部门和标签
- 按优先级设置响应/解决目标的 SLA 策略
- 客服人员仪表板和管理员配置
- Inertia.js UI 或无头 JSON API 模式
- PostgreSQL 和 SQLite 支持
- 与框架无关的 HTTP 处理程序
- 嵌入式 SQL 迁移

### 功能

- **Ticket splitting** — 将回复拆分为新的独立工单，同时保留原始上下文
- **Ticket snooze** — 使用预设（1小时、4小时、明天、下周）休眠工单；后台 goroutine 调度器按计划自动唤醒
- **Saved views / custom queues** — 将过滤预设保存、命名和共享为可重用的工单视图
- **Embeddable support widget** — 轻量级 `<script>` 小部件，包含知识库搜索、工单表单和状态检查
- **Email threading** — 外发电子邮件包含正确的 `In-Reply-To` 和 `References` 标头，确保邮件客户端中的正确线程化
- **Branded email templates** — 所有外发电子邮件的可配置徽标、主色调和页脚文本
- **Real-time updates** — 用于实时工单更新的 Server-Sent Events (SSE) 端点，带自动轮询回退
- **Knowledge base toggle** — 从管理员设置中启用或禁用公共知识库

## 安装

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

## 配置

| 字段 | 类型 | 默认值 | 描述 |
|-------|------|---------|-------------|
| `RoutePrefix` | `string` | `/escalated` | 所有路由的 URL 前缀 |
| `UIEnabled` | `bool` | `true` | 挂载 Inertia UI 路由；`false` 则仅 JSON API |
| `TablePrefix` | `string` | `escalated_` | 数据库表名前缀 |
| `AdminCheck` | `func(*http.Request) bool` | `false` | 管理员用户返回 true |
| `AgentCheck` | `func(*http.Request) bool` | `false` | 客服人员用户返回 true |
| `UserIDFunc` | `func(*http.Request) int64` | `0` | 从请求中提取当前用户 ID |
| `DB` | `*sql.DB` | required | 数据库连接 |

## API 路由

所有路由都以 `RoutePrefix`（默认 `/escalated`）为前缀。

### JSON API（始终挂载）

| 方法 | 路径 | 描述 |
|--------|------|-------------|
| `GET` | `/api/tickets` | 列出工单（带过滤器） |
| `POST` | `/api/tickets` | 创建工单 |
| `GET` | `/api/tickets/{id}` | 获取工单及回复和活动 |
| `PATCH` | `/api/tickets/{id}` | 更新工单 |
| `POST` | `/api/tickets/{id}/replies` | 添加回复 |
| `GET` | `/api/departments` | 列出部门 |
| `GET` | `/api/tags` | 列出标签 |

### 客户 UI（当 `UIEnabled: true` 时）

| 方法 | 路径 | 描述 |
|--------|------|-------------|
| `GET` | `/tickets` | 我的工单 |
| `POST` | `/tickets` | 提交工单 |
| `GET` | `/tickets/{id}` | 查看工单 |
| `POST` | `/tickets/{id}/replies` | 回复工单 |

### 客服人员 UI（需要 `AgentCheck`）

| 方法 | 路径 | 描述 |
|--------|------|-------------|
| `GET` | `/agent/` | 客服人员仪表板 |
| `GET` | `/agent/tickets` | 工单队列 |
| `GET` | `/agent/tickets/{id}` | 工单详情 |
| `POST` | `/agent/tickets/{id}/assign` | 分配工单 |
| `POST` | `/agent/tickets/{id}/replies` | 回复 / 内部备注 |
| `POST` | `/agent/tickets/{id}/status` | 更改状态 |

### 管理员 UI（需要 `AdminCheck`）

| 方法 | 路径 | 描述 |
|--------|------|-------------|
| `GET/POST/PATCH/DELETE` | `/admin/departments` | 管理部门 |
| `GET/POST/DELETE` | `/admin/tags` | 管理标签 |
| `GET/POST/DELETE` | `/admin/sla-policies` | 管理 SLA 策略 |

## 自定义存储

实现 `store.Store` 接口以使用不同的数据库：

```go
esc, _ := escalated.New(cfg)
esc.Store = myCustomStore // satisfies store.Store interface
```

## 工单状态

| 值 | 名称 |
|-------|------|
| 0 | open |
| 1 | in_progress |
| 2 | waiting_on_customer |
| 3 | waiting_on_agent |
| 4 | escalated |
| 5 | resolved |
| 6 | closed |
| 7 | reopened |

## 优先级

| 值 | 名称 |
|-------|------|
| 0 | low |
| 1 | medium |
| 2 | high |
| 3 | urgent |
| 4 | critical |

## 许可证

MIT
