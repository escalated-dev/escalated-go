<p align="center">
  <b>العربية</b> •
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
  <a href="README.zh-CN.md">简体中文</a>
</p>

# Escalated Go

نظام تذاكر دعم قابل للتضمين لتطبيقات Go. يعمل مع `net/http` القياسي و Chi وأي موجه يقبل `http.HandlerFunc`.

## الميزات

- تذاكر مع حالات وأولويات وأنواع وتتبع SLA
- ردود (عامة، ملاحظات داخلية، رسائل نظام)
- أقسام وعلامات
- سياسات SLA مع أهداف استجابة/حل لكل أولوية
- لوحة تحكم الوكيل وإعدادات المسؤول
- واجهة مستخدم Inertia.js أو وضع JSON API بدون واجهة
- دعم PostgreSQL و SQLite
- معالجات HTTP مستقلة عن إطار العمل
- ترحيلات SQL مضمنة

### الميزات

- **Ticket splitting** — تقسيم رد إلى تذكرة مستقلة جديدة مع الحفاظ على السياق الأصلي
- **Ticket snooze** — تأجيل التذاكر مع إعدادات مسبقة (ساعة، 4 ساعات، غداً، الأسبوع القادم)؛ مجدول goroutine في الخلفية يوقظها تلقائياً حسب الجدول
- **Saved views / custom queues** — حفظ وتسمية ومشاركة إعدادات التصفية المسبقة كعروض تذاكر قابلة لإعادة الاستخدام
- **Embeddable support widget** — أداة `<script>` خفيفة مع بحث قاعدة المعرفة ونموذج التذكرة والتحقق من الحالة
- **Email threading** — تتضمن رسائل البريد الصادرة رؤوس `In-Reply-To` و `References` الصحيحة لسلسلة الرسائل في عملاء البريد
- **Branded email templates** — شعار قابل للتكوين ولون أساسي ونص تذييل لجميع رسائل البريد الصادرة
- **Real-time updates** — نقطة نهاية Server-Sent Events (SSE) لتحديثات التذاكر المباشرة مع استطلاع احتياطي تلقائي
- **Knowledge base toggle** — تمكين أو تعطيل قاعدة المعرفة العامة من إعدادات المسؤول

## التثبيت

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

## بدء سريع مع المكتبة القياسية

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

## الإعدادات

| الحقل | النوع | الافتراضي | الوصف |
|-------|------|---------|-------------|
| `RoutePrefix` | `string` | `/escalated` | بادئة URL لجميع المسارات |
| `UIEnabled` | `bool` | `true` | تركيب مسارات واجهة Inertia؛ `false` لـ JSON API فقط |
| `TablePrefix` | `string` | `escalated_` | بادئة اسم جدول قاعدة البيانات |
| `AdminCheck` | `func(*http.Request) bool` | `false` | يعيد true للمستخدمين المسؤولين |
| `AgentCheck` | `func(*http.Request) bool` | `false` | يعيد true لمستخدمي الوكلاء |
| `UserIDFunc` | `func(*http.Request) int64` | `0` | يستخرج معرف المستخدم الحالي من الطلب |
| `DB` | `*sql.DB` | required | اتصال قاعدة البيانات |

## مسارات API

جميع المسارات مسبوقة بـ `RoutePrefix` (الافتراضي `/escalated`).

### JSON API (مركب دائماً)

| الطريقة | المسار | الوصف |
|--------|------|-------------|
| `GET` | `/api/tickets` | عرض التذاكر (مع فلاتر) |
| `POST` | `/api/tickets` | إنشاء تذكرة |
| `GET` | `/api/tickets/{id}` | الحصول على تذكرة مع الردود والأنشطة |
| `PATCH` | `/api/tickets/{id}` | تحديث تذكرة |
| `POST` | `/api/tickets/{id}/replies` | إضافة رد |
| `GET` | `/api/departments` | عرض الأقسام |
| `GET` | `/api/tags` | عرض العلامات |

### واجهة العميل (عندما `UIEnabled: true`)

| الطريقة | المسار | الوصف |
|--------|------|-------------|
| `GET` | `/tickets` | تذاكري |
| `POST` | `/tickets` | إرسال تذكرة |
| `GET` | `/tickets/{id}` | عرض التذكرة |
| `POST` | `/tickets/{id}/replies` | الرد على التذكرة |

### واجهة الوكيل (تتطلب `AgentCheck`)

| الطريقة | المسار | الوصف |
|--------|------|-------------|
| `GET` | `/agent/` | لوحة تحكم الوكيل |
| `GET` | `/agent/tickets` | قائمة التذاكر |
| `GET` | `/agent/tickets/{id}` | تفاصيل التذكرة |
| `POST` | `/agent/tickets/{id}/assign` | تعيين تذكرة |
| `POST` | `/agent/tickets/{id}/replies` | رد / ملاحظة داخلية |
| `POST` | `/agent/tickets/{id}/status` | تغيير الحالة |

### واجهة المسؤول (تتطلب `AdminCheck`)

| الطريقة | المسار | الوصف |
|--------|------|-------------|
| `GET/POST/PATCH/DELETE` | `/admin/departments` | إدارة الأقسام |
| `GET/POST/DELETE` | `/admin/tags` | إدارة العلامات |
| `GET/POST/DELETE` | `/admin/sla-policies` | إدارة سياسات SLA |

## متجر مخصص

نفّذ واجهة `store.Store` لاستخدام قاعدة بيانات مختلفة:

```go
esc, _ := escalated.New(cfg)
esc.Store = myCustomStore // satisfies store.Store interface
```

## حالات التذكرة

| القيمة | الاسم |
|-------|------|
| 0 | open |
| 1 | in_progress |
| 2 | waiting_on_customer |
| 3 | waiting_on_agent |
| 4 | escalated |
| 5 | resolved |
| 6 | closed |
| 7 | reopened |

## الأولويات

| القيمة | الاسم |
|-------|------|
| 0 | low |
| 1 | medium |
| 2 | high |
| 3 | urgent |
| 4 | critical |

## الترخيص

MIT
