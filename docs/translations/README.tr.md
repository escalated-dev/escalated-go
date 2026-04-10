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
  <b>Türkçe</b> •
  <a href="README.zh-CN.md">简体中文</a>
</p>

# Escalated Go

Go uygulamaları için gömülebilir destek talep sistemi. Standart `net/http`, Chi ve `http.HandlerFunc` kabul eden herhangi bir yönlendirici ile çalışır.

## Özellikler

- Durumlar, öncelikler, türler ve SLA takibi ile talepler
- Yanıtlar (genel, dahili notlar, sistem mesajları)
- Departmanlar ve etiketler
- Öncelik başına yanıt/çözüm hedefleri ile SLA politikaları
- Temsilci panosu ve yönetici yapılandırması
- Inertia.js arayüzü veya başsız JSON API modu
- PostgreSQL ve SQLite desteği
- Framework-bağımsız HTTP işleyicileri
- Gömülü SQL göçleri

### Özellikler

- **Ticket splitting** — Orijinal bağlamı koruyarak bir yanıtı yeni bağımsız bir talebe bölme
- **Ticket snooze** — Ön ayarlarla (1s, 4s, yarın, gelecek hafta) talepleri erteleme; arka plan goroutine zamanlayıcısı onları programa göre otomatik uyandırır
- **Saved views / custom queues** — Filtre ön ayarlarını yeniden kullanılabilir talep görünümleri olarak kaydetme, adlandırma ve paylaşma
- **Embeddable support widget** — KB arama, talep formu ve durum kontrolü ile hafif `<script>` widget'ı
- **Email threading** — Giden e-postalar, posta istemcilerinde doğru zincirleme için uygun `In-Reply-To` ve `References` başlıklarını içerir
- **Branded email templates** — Tüm giden e-postalar için yapılandırılabilir logo, birincil renk ve alt bilgi metni
- **Real-time updates** — Otomatik yoklama yedeklemeli canlı talep güncellemeleri için Server-Sent Events (SSE) uç noktası
- **Knowledge base toggle** — Yönetici ayarlarından genel bilgi tabanını etkinleştirme veya devre dışı bırakma

## Kurulum

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

## Standart Kütüphane ile Hızlı Başlangıç

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

## Yapılandırma

| Alan | Tür | Varsayılan | Açıklama |
|-------|------|---------|-------------|
| `RoutePrefix` | `string` | `/escalated` | Tüm rotalar için URL öneki |
| `UIEnabled` | `bool` | `true` | Inertia UI rotalarını bağla; `false` yalnızca JSON API için |
| `TablePrefix` | `string` | `escalated_` | Veritabanı tablo adı öneki |
| `AdminCheck` | `func(*http.Request) bool` | `false` | Yönetici kullanıcılar için true döndürür |
| `AgentCheck` | `func(*http.Request) bool` | `false` | Temsilci kullanıcılar için true döndürür |
| `UserIDFunc` | `func(*http.Request) int64` | `0` | İstekten mevcut kullanıcı kimliğini çıkarır |
| `DB` | `*sql.DB` | required | Veritabanı bağlantısı |

## API Rotaları

Tüm rotalar `RoutePrefix` (varsayılan `/escalated`) ile öneklenir.

### JSON API (her zaman bağlı)

| Yöntem | Yol | Açıklama |
|--------|------|-------------|
| `GET` | `/api/tickets` | Talepleri listele (filtrelerle) |
| `POST` | `/api/tickets` | Talep oluştur |
| `GET` | `/api/tickets/{id}` | Yanıtlar ve etkinliklerle talep getir |
| `PATCH` | `/api/tickets/{id}` | Talebi güncelle |
| `POST` | `/api/tickets/{id}/replies` | Yanıt ekle |
| `GET` | `/api/departments` | Departmanları listele |
| `GET` | `/api/tags` | Etiketleri listele |

### Müşteri Arayüzü (`UIEnabled: true` olduğunda)

| Yöntem | Yol | Açıklama |
|--------|------|-------------|
| `GET` | `/tickets` | Taleplerim |
| `POST` | `/tickets` | Talep gönder |
| `GET` | `/tickets/{id}` | Talebi görüntüle |
| `POST` | `/tickets/{id}/replies` | Talebe yanıt ver |

### Temsilci Arayüzü (`AgentCheck` gerektirir)

| Yöntem | Yol | Açıklama |
|--------|------|-------------|
| `GET` | `/agent/` | Temsilci panosu |
| `GET` | `/agent/tickets` | Talep kuyruğu |
| `GET` | `/agent/tickets/{id}` | Talep detayı |
| `POST` | `/agent/tickets/{id}/assign` | Talep ata |
| `POST` | `/agent/tickets/{id}/replies` | Yanıt / dahili not |
| `POST` | `/agent/tickets/{id}/status` | Durum değiştir |

### Yönetici Arayüzü (`AdminCheck` gerektirir)

| Yöntem | Yol | Açıklama |
|--------|------|-------------|
| `GET/POST/PATCH/DELETE` | `/admin/departments` | Departmanları yönet |
| `GET/POST/DELETE` | `/admin/tags` | Etiketleri yönet |
| `GET/POST/DELETE` | `/admin/sla-policies` | SLA politikalarını yönet |

## Özel Depo

Farklı bir veritabanı kullanmak için `store.Store` arayüzünü uygulayın:

```go
esc, _ := escalated.New(cfg)
esc.Store = myCustomStore // satisfies store.Store interface
```

## Talep Durumları

| Değer | Ad |
|-------|------|
| 0 | open |
| 1 | in_progress |
| 2 | waiting_on_customer |
| 3 | waiting_on_agent |
| 4 | escalated |
| 5 | resolved |
| 6 | closed |
| 7 | reopened |

## Öncelikler

| Değer | Ad |
|-------|------|
| 0 | low |
| 1 | medium |
| 2 | high |
| 3 | urgent |
| 4 | critical |

## Lisans

MIT
