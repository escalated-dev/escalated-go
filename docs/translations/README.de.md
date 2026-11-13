<p align="center"><sub>
  <a href="README.ar.md">العربية</a> ·
  <b>Deutsch</b> ·
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
  <a href="README.tr.md">Türkçe</a> ·
  <a href="README.zh-CN.md">简体中文</a>
</sub></p>

<p align="center">
  <a href="https://escalated.dev"><img src="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/logo.png" width="72" height="72" alt="Escalated"></a>
</p>

<h1 align="center">Escalated for Go</h1>

<p align="center">
  Füge deiner Go-App einen vollständigen Helpdesk hinzu.<br>
  Tickets, SLAs, Eskalationsregeln, ein Agenten-Posteingang und ein Kundenportal, direkt auf deiner eigenen Datenbank.
</p>

<p align="center">
  <a href="https://escalated.dev"><b>Website</b></a> ·
  <a href="https://escalated.dev/docs"><b>Dokumentation</b></a> ·
  <a href="https://escalated.dev/cloud"><b>Cloud</b></a> ·
  <a href="../../CHANGELOG.md"><b>Änderungsprotokoll</b></a>
</p>

<br>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/inbox-dark.png">
  <img src="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/inbox-light.png" alt="Der Agenten-Posteingang von Escalated: gespeicherte Ansichten mit Zählern, SLA-Countdowns und Prioritäten an jedem Ticket" width="100%">
</picture>

## Was es kann

<table>
  <tr>
    <td width="40%">
      <h3>Ein Posteingang, in dem deine Agenten den ganzen Tag arbeiten können</h3>
      Konversation, interne Notizen, Textbausteine und SLA-Timer auf einem Bildschirm. Tickets kommen auch per E-Mail über Mailgun, Postmark oder SES an.
    </td>
    <td width="60%">
      <picture>
        <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/ticket-dark.png">
        <img src="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/ticket-light.png" alt="Ein geöffnetes Ticket im Agenten-Arbeitsbereich mit dem Antwort-Editor">
      </picture>
    </td>
  </tr>
  <tr>
    <td width="60%">
      <picture>
        <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/dashboard-dark.png">
        <img src="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/dashboard-light.png" alt="Die Berichtsseite im Adminbereich mit Antwortzeiten, CSAT und SLA-Verstößen">
      </picture>
    </td>
    <td width="40%">
      <h3>SLAs und Eskalationsregeln, die du im Blick hast</h3>
      Reaktions- und Lösungsziele pro Priorität mit Geschäftszeiten, Regeln, die eskalieren oder neu zuweisen, wenn ein Ziel verfehlt wird, und Berichte über Verstöße.
    </td>
  </tr>
</table>

## Funktionen

<table>
<tr>
<td width="33%" valign="top">

**Tickets**
- Status, Prioritäten und Tickettypen
- Öffentliche Antworten, interne Notizen, Systemnachrichten
- Eine Antwort in ein neues Ticket auslagern
- Zurückstellen mit automatischem Wiederaufnehmen
- Ticket-Bezüge zu deinen eigenen Entitäten

</td>
<td width="33%" valign="top">

**Agenten-Arbeitsbereich und SLAs**
- Agenten-Dashboard und gespeicherte Ansichten
- Abteilungen und Tags
- Live-Updates über Server-Sent Events
- Reaktions- und Lösungsziele pro Priorität
- SLA-Tracking an jedem Ticket

</td>
<td width="33%" valign="top">

**Kunden**
- Kundenportal, wenn die Oberfläche aktiv ist
- Verifizierter Gastzugang per E-Mail-Code
- Einbettbares Widget mit Wissensdatenbank-Suche und Chat
- Wissensdatenbank, ein- oder ausschaltbar

</td>
</tr>
<tr>
<td width="33%" valign="top">

**E-Mail**
- Benachrichtigungen, per Message-ID zu Threads verknüpft
- Eingehende Antworten über Provider-Webhooks
- Korrekte Threads in Mail-Clients
- Vorlagen in deinem Branding

</td>
<td width="33%" valign="top">

**Verwaltung und Zugriff**
- Konfiguration im Adminbereich
- Rollen, Berechtigungen, Zwei-Faktor, SSO
- Native Go- und SDK-Plugins

</td>
<td width="33%" valign="top">

**Gebaut für Go**
- Funktioniert mit net/http, Chi und mehr
- Inertia-Oberfläche oder Headless-JSON-API
- PostgreSQL und SQLite
- Eingebettete SQL-Migrationen

</td>
</tr>
</table>

<p align="center"><sub><a href="../features.md">Alle Funktionen im Detail</a></sub></p>

<p align="center">
  <a href="https://go.dev"><img src="https://img.shields.io/badge/Go_1.26+-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go 1.26+"></a>
  <a href="https://www.postgresql.org"><img src="https://img.shields.io/badge/PostgreSQL-4169E1?style=for-the-badge&logo=postgresql&logoColor=white" alt="PostgreSQL"></a>
  <a href="https://www.sqlite.org"><img src="https://img.shields.io/badge/SQLite-003B57?style=for-the-badge&logo=sqlite&logoColor=white" alt="SQLite"></a>
  <a href="https://inertiajs.com"><img src="https://img.shields.io/badge/Inertia-9553E9?style=for-the-badge&logo=inertia&logoColor=white" alt="Inertia"></a>
  <a href="https://vuejs.org"><img src="https://img.shields.io/badge/Vue_3-4FC08D?style=for-the-badge&logo=vuedotjs&logoColor=white" alt="Vue 3"></a>
</p>

## Schnellstart

Installiere mit `go get github.com/escalated-dev/escalated-go`, dann:

```go
migrations.Migrate(db, "escalated_")
cfg := escalated.DefaultConfig()
cfg.DB = db
esc, err := escalated.New(cfg)
router.MountChi(r, esc)
```

Voraussetzung: Go 1.26.9 oder neuer. Setze zuerst `AdminCheck`, `AgentCheck` und `UserIDFunc` auf deine eigenen Session-Prüfungen; bis dahin verweigern sie den Zugriff. Die [Einstiegsanleitung](../getting-started.md) enthält die vollständigen Beispiele für Chi und die Standardbibliothek.

## Mehr erfahren

- [Alle Funktionen](../features.md) · [Erste Schritte](../getting-started.md) · [Konfiguration](../configuration.md) · [API-Routen](../api-routes.md)
- [Eigene Aktionen, Stores, Status und Übersetzungen](../customization.md) · [UI-Seiten](../ui-pages.md)
- [Zugriffskontrolle](../access-control.md) · [API-Tokens](../api-tokens.md) · [Ticketzugriff und Routing](../ticket-access-and-routing.md)
- [Benachrichtigungs-E-Mails](../notification-emails.md) · [Widget einbetten](../embedding-the-widget.md) · [Gastzugang](../guest-access.md)
- [Agenten-Panel](../agent-panel.md) · [Adminbereich einbetten](../embedding-admin.md)
- [Plugins](../plugins.md) · [Newsletter](../newsletters.md)

Escalated gibt es auch für [Laravel](https://github.com/escalated-dev/escalated-laravel), [Rails](https://github.com/escalated-dev/escalated-rails), [Django](https://github.com/escalated-dev/escalated-django), [NestJS](https://github.com/escalated-dev/escalated-nestjs), [AdonisJS](https://github.com/escalated-dev/escalated-adonis), [Symfony](https://github.com/escalated-dev/escalated-symfony), [Phoenix](https://github.com/escalated-dev/escalated-phoenix), [.NET](https://github.com/escalated-dev/escalated-dotnet), [Spring](https://github.com/escalated-dev/escalated-spring), [WordPress](https://github.com/escalated-dev/escalated-wordpress) und [Filament](https://github.com/escalated-dev/escalated-filament). Die Oberfläche liegt in [escalated](https://github.com/escalated-dev/escalated).

## Lizenz

MIT. Copyright (c) Escalated.dev. Siehe [LICENSE](../../LICENSE).
