<p align="center">
  <a href="README.ar.md">العربية</a> •
  <a href="README.de.md">Deutsch</a> •
  <a href="../../README.md">English</a> •
  <a href="README.es.md">Español</a> •
  <a href="README.fr.md">Français</a> •
  <a href="README.it.md">Italiano</a> •
  <b>日本語</b> •
  <a href="README.ko.md">한국어</a> •
  <a href="README.nl.md">Nederlands</a> •
  <a href="README.pl.md">Polski</a> •
  <a href="README.pt-BR.md">Português (BR)</a> •
  <a href="README.ru.md">Русский</a> •
  <a href="README.tr.md">Türkçe</a> •
  <a href="README.zh-CN.md">简体中文</a>
</p>

# Escalated Go

Goアプリケーション向けの組み込み可能なサポートチケットシステム。標準の`net/http`、Chi、および`http.HandlerFunc`を受け付ける任意のルーターで動作します。

## 機能

- ステータス、優先度、タイプ、SLAトラッキング付きチケット
- 返信（公開、内部メモ、システムメッセージ）
- 部門とタグ
- 優先度ごとの応答/解決目標を持つSLAポリシー
- エージェントダッシュボードと管理設定
- Inertia.js UIまたはヘッドレスJSON APIモード
- PostgreSQLとSQLiteサポート
- フレームワーク非依存のHTTPハンドラー
- 組み込みSQLマイグレーション

### 機能

- **Ticket splitting** — 返信を元のコンテキストを保持しながら新しい独立チケットに分割
- **Ticket snooze** — プリセット（1時間、4時間、明日、来週）でチケットをスヌーズ；バックグラウンドのgoroutineスケジューラがスケジュール通りに自動的にウェイクアップ
- **Saved views / custom queues** — フィルタープリセットを再利用可能なチケットビューとして保存、命名、共有
- **Embeddable support widget** — KB検索、チケットフォーム、ステータス確認を備えた軽量`<script>`ウィジェット
- **Email threading** — 送信メールにはメールクライアントでの正しいスレッディングのための適切な`In-Reply-To`と`References`ヘッダーが含まれる
- **Branded email templates** — すべての送信メールに設定可能なロゴ、プライマリカラー、フッターテキスト
- **Real-time updates** — 自動ポーリングフォールバック付きのライブチケット更新用Server-Sent Events (SSE)エンドポイント
- **Knowledge base toggle** — 管理設定から公開ナレッジベースの有効/無効を切り替え

## インストール

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

## 標準ライブラリでのクイックスタート

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

## 設定

| フィールド | 型 | デフォルト | 説明 |
|-------|------|---------|-------------|
| `RoutePrefix` | `string` | `/escalated` | すべてのルートのURLプレフィックス |
| `UIEnabled` | `bool` | `true` | Inertia UIルートをマウント；`false`でJSON APIのみ |
| `TablePrefix` | `string` | `escalated_` | データベーステーブル名のプレフィックス |
| `AdminCheck` | `func(*http.Request) bool` | `false` | 管理者ユーザーの場合trueを返す |
| `AgentCheck` | `func(*http.Request) bool` | `false` | エージェントユーザーの場合trueを返す |
| `UserIDFunc` | `func(*http.Request) int64` | `0` | リクエストから現在のユーザーIDを抽出 |
| `DB` | `*sql.DB` | required | データベース接続 |

## APIルート

すべてのルートは`RoutePrefix`（デフォルト`/escalated`）がプレフィックスされます。

### JSON API（常にマウント）

| メソッド | パス | 説明 |
|--------|------|-------------|
| `GET` | `/api/tickets` | チケット一覧（フィルター付き） |
| `POST` | `/api/tickets` | チケット作成 |
| `GET` | `/api/tickets/{id}` | 返信とアクティビティ付きチケット取得 |
| `PATCH` | `/api/tickets/{id}` | チケット更新 |
| `POST` | `/api/tickets/{id}/replies` | 返信追加 |
| `GET` | `/api/departments` | 部門一覧 |
| `GET` | `/api/tags` | タグ一覧 |

### 顧客UI（`UIEnabled: true`の場合）

| メソッド | パス | 説明 |
|--------|------|-------------|
| `GET` | `/tickets` | マイチケット |
| `POST` | `/tickets` | チケット送信 |
| `GET` | `/tickets/{id}` | チケット表示 |
| `POST` | `/tickets/{id}/replies` | チケットに返信 |

### エージェントUI（`AgentCheck`が必要）

| メソッド | パス | 説明 |
|--------|------|-------------|
| `GET` | `/agent/` | エージェントダッシュボード |
| `GET` | `/agent/tickets` | チケットキュー |
| `GET` | `/agent/tickets/{id}` | チケット詳細 |
| `POST` | `/agent/tickets/{id}/assign` | チケット割り当て |
| `POST` | `/agent/tickets/{id}/replies` | 返信 / 内部メモ |
| `POST` | `/agent/tickets/{id}/status` | ステータス変更 |

### 管理UI（`AdminCheck`が必要）

| メソッド | パス | 説明 |
|--------|------|-------------|
| `GET/POST/PATCH/DELETE` | `/admin/departments` | 部門管理 |
| `GET/POST/DELETE` | `/admin/tags` | タグ管理 |
| `GET/POST/DELETE` | `/admin/sla-policies` | SLAポリシー管理 |

## カスタムストア

異なるデータベースを使用するには`store.Store`インターフェースを実装します：

```go
esc, _ := escalated.New(cfg)
esc.Store = myCustomStore // satisfies store.Store interface
```

## チケットステータス

| 値 | 名前 |
|-------|------|
| 0 | open |
| 1 | in_progress |
| 2 | waiting_on_customer |
| 3 | waiting_on_agent |
| 4 | escalated |
| 5 | resolved |
| 6 | closed |
| 7 | reopened |

## 優先度

| 値 | 名前 |
|-------|------|
| 0 | low |
| 1 | medium |
| 2 | high |
| 3 | urgent |
| 4 | critical |

## ライセンス

MIT
