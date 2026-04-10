<p align="center">
  <a href="README.ar.md">العربية</a> •
  <a href="README.de.md">Deutsch</a> •
  <a href="../../README.md">English</a> •
  <a href="README.es.md">Español</a> •
  <a href="README.fr.md">Français</a> •
  <a href="README.it.md">Italiano</a> •
  <a href="README.ja.md">日本語</a> •
  <b>한국어</b> •
  <a href="README.nl.md">Nederlands</a> •
  <a href="README.pl.md">Polski</a> •
  <a href="README.pt-BR.md">Português (BR)</a> •
  <a href="README.ru.md">Русский</a> •
  <a href="README.tr.md">Türkçe</a> •
  <a href="README.zh-CN.md">简体中文</a>
</p>

# Escalated Go

Go 애플리케이션을 위한 내장 가능한 지원 티켓 시스템. 표준 `net/http`, Chi 및 `http.HandlerFunc`를 받는 모든 라우터와 함께 작동합니다.

## 기능

- 상태, 우선순위, 유형, SLA 추적이 포함된 티켓
- 답변 (공개, 내부 메모, 시스템 메시지)
- 부서 및 태그
- 우선순위별 응답/해결 목표를 가진 SLA 정책
- 에이전트 대시보드 및 관리자 구성
- Inertia.js UI 또는 헤드리스 JSON API 모드
- PostgreSQL 및 SQLite 지원
- 프레임워크에 독립적인 HTTP 핸들러
- 내장된 SQL 마이그레이션

### 기능

- **Ticket splitting** — 원본 컨텍스트를 보존하면서 답변을 새로운 독립 티켓으로 분할
- **Ticket snooze** — 프리셋(1시간, 4시간, 내일, 다음 주)으로 티켓 스누즈; 백그라운드 goroutine 스케줄러가 예정대로 자동 깨움
- **Saved views / custom queues** — 필터 프리셋을 재사용 가능한 티켓 뷰로 저장, 명명, 공유
- **Embeddable support widget** — KB 검색, 티켓 양식, 상태 확인이 포함된 경량 `<script>` 위젯
- **Email threading** — 발신 이메일에 메일 클라이언트에서 올바른 스레딩을 위한 적절한 `In-Reply-To` 및 `References` 헤더 포함
- **Branded email templates** — 모든 발신 이메일에 구성 가능한 로고, 기본 색상, 바닥글 텍스트
- **Real-time updates** — 자동 폴링 폴백이 포함된 실시간 티켓 업데이트를 위한 Server-Sent Events (SSE) 엔드포인트
- **Knowledge base toggle** — 관리자 설정에서 공개 지식 베이스 활성화 또는 비활성화

## 설치

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

## 표준 라이브러리로 빠른 시작

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

## 구성

| 필드 | 유형 | 기본값 | 설명 |
|-------|------|---------|-------------|
| `RoutePrefix` | `string` | `/escalated` | 모든 경로의 URL 접두사 |
| `UIEnabled` | `bool` | `true` | Inertia UI 경로 마운트; `false`이면 JSON API만 |
| `TablePrefix` | `string` | `escalated_` | 데이터베이스 테이블 이름 접두사 |
| `AdminCheck` | `func(*http.Request) bool` | `false` | 관리자 사용자일 때 true 반환 |
| `AgentCheck` | `func(*http.Request) bool` | `false` | 에이전트 사용자일 때 true 반환 |
| `UserIDFunc` | `func(*http.Request) int64` | `0` | 요청에서 현재 사용자 ID 추출 |
| `DB` | `*sql.DB` | required | 데이터베이스 연결 |

## API 경로

모든 경로에는 `RoutePrefix` (기본값 `/escalated`)가 접두사로 붙습니다.

### JSON API (항상 마운트)

| 메서드 | 경로 | 설명 |
|--------|------|-------------|
| `GET` | `/api/tickets` | 티켓 목록 (필터 포함) |
| `POST` | `/api/tickets` | 티켓 생성 |
| `GET` | `/api/tickets/{id}` | 답변 및 활동이 포함된 티켓 조회 |
| `PATCH` | `/api/tickets/{id}` | 티켓 수정 |
| `POST` | `/api/tickets/{id}/replies` | 답변 추가 |
| `GET` | `/api/departments` | 부서 목록 |
| `GET` | `/api/tags` | 태그 목록 |

### 고객 UI (`UIEnabled: true`일 때)

| 메서드 | 경로 | 설명 |
|--------|------|-------------|
| `GET` | `/tickets` | 내 티켓 |
| `POST` | `/tickets` | 티켓 제출 |
| `GET` | `/tickets/{id}` | 티켓 보기 |
| `POST` | `/tickets/{id}/replies` | 티켓에 답변 |

### 에이전트 UI (`AgentCheck` 필요)

| 메서드 | 경로 | 설명 |
|--------|------|-------------|
| `GET` | `/agent/` | 에이전트 대시보드 |
| `GET` | `/agent/tickets` | 티켓 대기열 |
| `GET` | `/agent/tickets/{id}` | 티켓 상세 |
| `POST` | `/agent/tickets/{id}/assign` | 티켓 할당 |
| `POST` | `/agent/tickets/{id}/replies` | 답변 / 내부 메모 |
| `POST` | `/agent/tickets/{id}/status` | 상태 변경 |

### 관리자 UI (`AdminCheck` 필요)

| 메서드 | 경로 | 설명 |
|--------|------|-------------|
| `GET/POST/PATCH/DELETE` | `/admin/departments` | 부서 관리 |
| `GET/POST/DELETE` | `/admin/tags` | 태그 관리 |
| `GET/POST/DELETE` | `/admin/sla-policies` | SLA 정책 관리 |

## 커스텀 스토어

다른 데이터베이스를 사용하려면 `store.Store` 인터페이스를 구현합니다:

```go
esc, _ := escalated.New(cfg)
esc.Store = myCustomStore // satisfies store.Store interface
```

## 티켓 상태

| 값 | 이름 |
|-------|------|
| 0 | open |
| 1 | in_progress |
| 2 | waiting_on_customer |
| 3 | waiting_on_agent |
| 4 | escalated |
| 5 | resolved |
| 6 | closed |
| 7 | reopened |

## 우선순위

| 값 | 이름 |
|-------|------|
| 0 | low |
| 1 | medium |
| 2 | high |
| 3 | urgent |
| 4 | critical |

## 라이선스

MIT
