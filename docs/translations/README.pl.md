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
  <b>Polski</b> ·
  <a href="README.pt-BR.md">Português (BR)</a> ·
  <a href="README.ru.md">Русский</a> ·
  <a href="README.tr.md">Türkçe</a> ·
  <a href="README.zh-CN.md">简体中文</a>
</sub></p>

<p align="center">
  <a href="https://escalated.dev"><img src="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/logo.png" width="72" height="72" alt="Escalated"></a>
</p>

<h1 align="center">Escalated for Go</h1>

<p align="center">
  Dodaj kompletny helpdesk do swojej aplikacji w Go.<br>
  Zgłoszenia, SLA, reguły eskalacji, skrzynka dla agentów i portal klienta, działające na twojej własnej bazie danych.
</p>

<p align="center">
  <a href="https://escalated.dev"><b>Strona</b></a> ·
  <a href="https://escalated.dev/docs"><b>Dokumentacja</b></a> ·
  <a href="https://escalated.dev/cloud"><b>Cloud</b></a> ·
  <a href="../../CHANGELOG.md"><b>Lista zmian</b></a>
</p>

<br>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/inbox-dark.png">
  <img src="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/inbox-light.png" alt="Skrzynka agentów w Escalated: zapisane widoki z licznikami, odliczanie SLA i priorytety przy każdym zgłoszeniu" width="100%">
</picture>

## Co robi

<table>
  <tr>
    <td width="40%">
      <h3>Skrzynka, w której twoi agenci mogą pracować cały dzień</h3>
      Rozmowa, notatki wewnętrzne, gotowe odpowiedzi i liczniki SLA na jednym ekranie. Zgłoszenia przychodzą też e-mailem przez Mailgun, Postmark lub SES.
    </td>
    <td width="60%">
      <picture>
        <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/ticket-dark.png">
        <img src="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/ticket-light.png" alt="Zgłoszenie otwarte w przestrzeni roboczej agenta z edytorem odpowiedzi">
      </picture>
    </td>
  </tr>
  <tr>
    <td width="60%">
      <picture>
        <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/dashboard-dark.png">
        <img src="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/dashboard-light.png" alt="Strona raportów w panelu administracyjnym z czasami odpowiedzi, CSAT i naruszeniami SLA">
      </picture>
    </td>
    <td width="40%">
      <h3>SLA i reguły eskalacji, które widzisz</h3>
      Cele odpowiedzi i rozwiązania dla każdego priorytetu z godzinami pracy, reguły, które eskalują lub przypisują ponownie, gdy cel nie jest dotrzymany, oraz raporty naruszeń.
    </td>
  </tr>
</table>

## Funkcje

<table>
<tr>
<td width="33%" valign="top">

**Zgłoszenia**
- Statusy, priorytety i typy zgłoszeń
- Odpowiedzi publiczne, notatki wewnętrzne, komunikaty systemowe
- Wydzielenie odpowiedzi do nowego zgłoszenia
- Odkładanie z automatycznym wznowieniem
- Zgłoszenia powiązane z twoimi encjami

</td>
<td width="33%" valign="top">

**Przestrzeń agenta i SLA**
- Pulpit agenta i zapisane widoki
- Działy i tagi
- Aktualizacje na żywo przez Server-Sent Events
- Cele odpowiedzi i rozwiązania dla każdego priorytetu
- Śledzenie SLA przy każdym zgłoszeniu

</td>
<td width="33%" valign="top">

**Klienci**
- Portal klienta, gdy interfejs jest włączony
- Zweryfikowany dostęp gościa przez kod wysłany e-mailem
- Osadzany widget z wyszukiwaniem w bazie wiedzy i czatem
- Baza wiedzy, włączona lub wyłączona

</td>
</tr>
<tr>
<td width="33%" valign="top">

**E-mail**
- Powiadomienia łączone w wątki przez Message-ID
- Odpowiedzi przychodzące z webhooków dostawcy
- Poprawne wątki w klientach poczty
- Szablony z twoją marką

</td>
<td width="33%" valign="top">

**Administracja i dostęp**
- Konfiguracja w panelu administracyjnym
- Role, uprawnienia, uwierzytelnianie dwuskładnikowe, SSO
- Natywne wtyczki Go i wtyczki SDK

</td>
<td width="33%" valign="top">

**Stworzony dla Go**
- Działa z net/http, Chi i innymi
- Interfejs Inertia albo headless JSON API
- PostgreSQL i SQLite
- Wbudowane migracje SQL

</td>
</tr>
</table>

<p align="center"><sub><a href="../features.md">Wszystkie funkcje w szczegółach</a></sub></p>

<p align="center">
  <a href="https://go.dev"><img src="https://img.shields.io/badge/Go_1.26+-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go 1.26+"></a>
  <a href="https://www.postgresql.org"><img src="https://img.shields.io/badge/PostgreSQL-4169E1?style=for-the-badge&logo=postgresql&logoColor=white" alt="PostgreSQL"></a>
  <a href="https://www.sqlite.org"><img src="https://img.shields.io/badge/SQLite-003B57?style=for-the-badge&logo=sqlite&logoColor=white" alt="SQLite"></a>
  <a href="https://inertiajs.com"><img src="https://img.shields.io/badge/Inertia-9553E9?style=for-the-badge&logo=inertia&logoColor=white" alt="Inertia"></a>
  <a href="https://vuejs.org"><img src="https://img.shields.io/badge/Vue_3-4FC08D?style=for-the-badge&logo=vuedotjs&logoColor=white" alt="Vue 3"></a>
</p>

## Szybki start

Zainstaluj przez `go get github.com/escalated-dev/escalated-go`, a potem:

```go
migrations.Migrate(db, "escalated_")
cfg := escalated.DefaultConfig()
cfg.DB = db
esc, err := escalated.New(cfg)
router.MountChi(r, esc)
```

Wymaga Go 1.26.9 lub nowszego. Najpierw ustaw `AdminCheck`, `AgentCheck` i `UserIDFunc` na własne sprawdzanie sesji; dopóki tego nie zrobisz, odmawiają dostępu. [Przewodnik na start](../getting-started.md) zawiera pełne przykłady dla Chi i biblioteki standardowej.

## Dowiedz się więcej

- [Wszystkie funkcje](../features.md) · [Pierwsze kroki](../getting-started.md) · [Konfiguracja](../configuration.md) · [Trasy API](../api-routes.md)
- [Własne akcje, magazyny, statusy i tłumaczenia](../customization.md) · [Strony interfejsu](../ui-pages.md)
- [Kontrola dostępu](../access-control.md) · [Tokeny API](../api-tokens.md) · [Dostęp do zgłoszeń i routing](../ticket-access-and-routing.md)
- [E-maile z powiadomieniami](../notification-emails.md) · [Osadzanie widgetu](../embedding-the-widget.md) · [Dostęp gościa](../guest-access.md)
- [Panel agenta](../agent-panel.md) · [Osadzanie panelu administracyjnego](../embedding-admin.md)
- [Wtyczki](../plugins.md) · [Newslettery](../newsletters.md)

Escalated jest dostępny także dla [Laravel](https://github.com/escalated-dev/escalated-laravel), [Rails](https://github.com/escalated-dev/escalated-rails), [Django](https://github.com/escalated-dev/escalated-django), [NestJS](https://github.com/escalated-dev/escalated-nestjs), [AdonisJS](https://github.com/escalated-dev/escalated-adonis), [Symfony](https://github.com/escalated-dev/escalated-symfony), [Phoenix](https://github.com/escalated-dev/escalated-phoenix), [.NET](https://github.com/escalated-dev/escalated-dotnet), [Spring](https://github.com/escalated-dev/escalated-spring), [WordPress](https://github.com/escalated-dev/escalated-wordpress) i [Filament](https://github.com/escalated-dev/escalated-filament). Interfejs znajduje się w [escalated](https://github.com/escalated-dev/escalated).

## Licencja

MIT. Copyright (c) Escalated.dev. Zobacz [LICENSE](../../LICENSE).
