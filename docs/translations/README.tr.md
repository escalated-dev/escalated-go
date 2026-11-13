<p align="center"><sub>
  <a href="README.ar.md">العربية</a> ·
  <a href="README.de.md">Deutsch</a> ·
  <a href="../../README.md">English</a> ·
  <a href="README.es.md">Español</a> ·
  <a href="README.fr.md">Français</a> ·
  <a href="README.it.md">Italiano</a> ·
  <a href="README.ja.md">日本語</a> ·
  <a href="README.ko.md">한국어</a> ·
  <a href="README.nl.md">Nederlands</a> ·
  <a href="README.pl.md">Polski</a> ·
  <a href="README.pt-BR.md">Português (BR)</a> ·
  <a href="README.ru.md">Русский</a> ·
  <b>Türkçe</b> ·
  <a href="README.zh-CN.md">简体中文</a>
</sub></p>

<p align="center">
  <a href="https://escalated.dev"><img src="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/logo.png" width="72" height="72" alt="Escalated"></a>
</p>

<h1 align="center">Escalated for Go</h1>

<p align="center">
  Go uygulamana eksiksiz bir yardım masası ekle.<br>
  Talepler, SLA'lar, eskalasyon kuralları, temsilciler için bir gelen kutusu ve bir müşteri portalı, kendi veritabanında çalışır.
</p>

<p align="center">
  <a href="https://escalated.dev"><b>Web sitesi</b></a> ·
  <a href="https://escalated.dev/docs"><b>Belgeler</b></a> ·
  <a href="https://escalated.dev/cloud"><b>Cloud</b></a> ·
  <a href="../../CHANGELOG.md"><b>Değişiklik günlüğü</b></a>
</p>

<br>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/inbox-dark.png">
  <img src="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/inbox-light.png" alt="Escalated temsilci gelen kutusu: sayaçlı kayıtlı görünümler, SLA geri sayımları ve her talepte öncelikler" width="100%">
</picture>

## Ne yapar

<table>
  <tr>
    <td width="40%">
      <h3>Temsilcilerinin bütün gün çalışabileceği bir gelen kutusu</h3>
      Konuşma, iç notlar, hazır yanıtlar ve SLA sayaçları tek ekranda. Talepler Mailgun, Postmark veya SES üzerinden e-postayla da gelir.
    </td>
    <td width="60%">
      <picture>
        <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/ticket-dark.png">
        <img src="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/ticket-light.png" alt="Temsilci çalışma alanında yanıt düzenleyicisiyle açık bir talep">
      </picture>
    </td>
  </tr>
  <tr>
    <td width="60%">
      <picture>
        <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/dashboard-dark.png">
        <img src="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/dashboard-light.png" alt="Yanıt süreleri, CSAT ve SLA ihlalleriyle yönetici raporları sayfası">
      </picture>
    </td>
    <td width="40%">
      <h3>Görebildiğin SLA'lar ve eskalasyon kuralları</h3>
      Mesai saatleriyle öncelik başına yanıt ve çözüm hedefleri, hedef kaçtığında eskale eden veya yeniden atayan kurallar ve ihlal raporları.
    </td>
  </tr>
</table>

## Özellikler

<table>
<tr>
<td width="33%" valign="top">

**Talepler**
- Durumlar, öncelikler ve talep türleri
- Herkese açık yanıtlar, iç notlar, sistem mesajları
- Bir yanıtı yeni bir talebe ayırma
- Otomatik uyanmalı erteleme
- Kendi varlıklarına bağlı talep konuları

</td>
<td width="33%" valign="top">

**Temsilci çalışma alanı ve SLA'lar**
- Temsilci paneli ve kayıtlı görünümler
- Departmanlar ve etiketler
- Server-Sent Events ile canlı güncellemeler
- Öncelik başına yanıt ve çözüm hedefleri
- Her talepte SLA takibi

</td>
<td width="33%" valign="top">

**Müşteriler**
- Arayüz açıkken müşteri portalı
- E-postayla gönderilen kodla doğrulanmış misafir erişimi
- Bilgi bankası aramalı ve sohbetli gömülebilir widget
- Bilgi bankası, açık veya kapalı

</td>
</tr>
<tr>
<td width="33%" valign="top">

**E-posta**
- Message-ID ile ileti dizilerine bağlanan bildirimler
- Sağlayıcı webhook'larından gelen yanıtlar
- E-posta istemcilerinde doğru ileti dizileri
- Markana uygun şablonlar

</td>
<td width="33%" valign="top">

**Yönetim ve erişim**
- Yönetici yapılandırması
- Roller, izinler, iki faktörlü doğrulama, SSO
- Yerel Go ve SDK eklentileri

</td>
<td width="33%" valign="top">

**Go için yapıldı**
- net/http, Chi ve diğerleriyle çalışır
- Inertia arayüzü veya headless JSON API
- PostgreSQL ve SQLite
- Gömülü SQL migration'ları

</td>
</tr>
</table>

<p align="center"><sub><a href="../features.md">Tüm özellikler ayrıntılı olarak</a></sub></p>

<p align="center">
  <a href="https://go.dev"><img src="https://img.shields.io/badge/Go_1.26+-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go 1.26+"></a>
  <a href="https://www.postgresql.org"><img src="https://img.shields.io/badge/PostgreSQL-4169E1?style=for-the-badge&logo=postgresql&logoColor=white" alt="PostgreSQL"></a>
  <a href="https://www.sqlite.org"><img src="https://img.shields.io/badge/SQLite-003B57?style=for-the-badge&logo=sqlite&logoColor=white" alt="SQLite"></a>
  <a href="https://inertiajs.com"><img src="https://img.shields.io/badge/Inertia-9553E9?style=for-the-badge&logo=inertia&logoColor=white" alt="Inertia"></a>
  <a href="https://vuejs.org"><img src="https://img.shields.io/badge/Vue_3-4FC08D?style=for-the-badge&logo=vuedotjs&logoColor=white" alt="Vue 3"></a>
</p>

## Hızlı başlangıç

`go get github.com/escalated-dev/escalated-go` ile kur, ardından:

```go
migrations.Migrate(db, "escalated_")
cfg := escalated.DefaultConfig()
cfg.DB = db
esc, err := escalated.New(cfg)
router.MountChi(r, esc)
```

Go 1.26.9 veya daha yenisi gerekir. Önce `AdminCheck`, `AgentCheck` ve `UserIDFunc` değerlerini kendi oturum kontrollerine ayarla; bunu yapana kadar erişimi reddederler. [Başlangıç kılavuzu](../getting-started.md) Chi ve standart kütüphane için tam örnekleri içerir.

## Daha fazla bilgi

- [Tüm özellikler](../features.md) · [Başlarken](../getting-started.md) · [Yapılandırma](../configuration.md) · [API rotaları](../api-routes.md)
- [Özel eylemler, depolar, durumlar ve çeviriler](../customization.md) · [Arayüz sayfaları](../ui-pages.md)
- [Erişim denetimi](../access-control.md) · [API token'ları](../api-tokens.md) · [Talep erişimi ve yönlendirme](../ticket-access-and-routing.md)
- [Bildirim e-postaları](../notification-emails.md) · [Widget'ı gömme](../embedding-the-widget.md) · [Misafir erişimi](../guest-access.md)
- [Temsilci paneli](../agent-panel.md) · [Yönetim panelini gömme](../embedding-admin.md)
- [Eklentiler](../plugins.md) · [Bültenler](../newsletters.md)

Escalated ayrıca [Laravel](https://github.com/escalated-dev/escalated-laravel), [Rails](https://github.com/escalated-dev/escalated-rails), [Django](https://github.com/escalated-dev/escalated-django), [NestJS](https://github.com/escalated-dev/escalated-nestjs), [AdonisJS](https://github.com/escalated-dev/escalated-adonis), [Symfony](https://github.com/escalated-dev/escalated-symfony), [Phoenix](https://github.com/escalated-dev/escalated-phoenix), [.NET](https://github.com/escalated-dev/escalated-dotnet), [Spring](https://github.com/escalated-dev/escalated-spring), [WordPress](https://github.com/escalated-dev/escalated-wordpress) ve [Filament](https://github.com/escalated-dev/escalated-filament) için de mevcut. Arayüz [escalated](https://github.com/escalated-dev/escalated) içinde yer alır.

## Lisans

MIT. Copyright (c) Escalated.dev. Bkz. [LICENSE](../../LICENSE).
