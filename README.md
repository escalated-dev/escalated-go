<p align="center"><sub>
  <a href="docs/translations/README.ar.md">العربية</a> ·
  <a href="docs/translations/README.de.md">Deutsch</a> ·
  <b>English</b> ·
  <a href="docs/translations/README.es.md">Español</a> ·
  <a href="docs/translations/README.fr.md">Français</a> ·
  <a href="docs/translations/README.it.md">Italiano</a> ·
  <a href="docs/translations/README.ja.md">日本語</a> ·
  <a href="docs/translations/README.ko.md">한국어</a> ·
  <a href="docs/translations/README.nl.md">Nederlands</a> ·
  <a href="docs/translations/README.pl.md">Polski</a> ·
  <a href="docs/translations/README.pt-BR.md">Português (BR)</a> ·
  <a href="docs/translations/README.ru.md">Русский</a> ·
  <a href="docs/translations/README.tr.md">Türkçe</a> ·
  <a href="docs/translations/README.zh-CN.md">简体中文</a>
</sub></p>

<p align="center">
  <a href="https://escalated.dev"><img src="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/logo.png" width="72" height="72" alt="Escalated"></a>
</p>

<h1 align="center">Escalated for Go</h1>

<p align="center">
  Add a complete helpdesk to your Go app.<br>
  Tickets, SLAs, escalation rules, an agent inbox and a customer portal, running on your own database.
</p>

<p align="center">
  <a href="https://escalated.dev"><b>Website</b></a> ·
  <a href="https://escalated.dev/docs"><b>Docs</b></a> ·
  <a href="https://escalated.dev/cloud"><b>Cloud</b></a> ·
  <a href="CHANGELOG.md"><b>Changelog</b></a>
</p>

<br>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/inbox-dark.png">
  <img src="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/inbox-light.png" alt="The Escalated agent inbox: saved views with counts, SLA countdowns and priorities on every ticket" width="100%">
</picture>

## What it does

<table>
  <tr>
    <td width="40%">
      <h3>An inbox your agents can work in all day</h3>
      Conversation, internal notes, canned replies and SLA timers on one screen. Tickets also arrive by email through Mailgun, Postmark or SES.
    </td>
    <td width="60%">
      <picture>
        <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/ticket-dark.png">
        <img src="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/ticket-light.png" alt="A ticket open in the agent workspace with the reply composer">
      </picture>
    </td>
  </tr>
  <tr>
    <td width="60%">
      <picture>
        <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/dashboard-dark.png">
        <img src="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/dashboard-light.png" alt="The admin reports page with response times, CSAT and SLA breaches">
      </picture>
    </td>
    <td width="40%">
      <h3>SLAs and escalation rules you can see</h3>
      Response and resolution targets per priority with business hours, rules that escalate or reassign when they slip, and reports on breaches.
    </td>
  </tr>
</table>

## Features

<table>
<tr>
<td width="33%" valign="top">

**Tickets**
- Statuses, priorities and ticket types
- Public replies, internal notes, system messages
- Split a reply into a new ticket
- Snooze with automatic wake
- Ticket subjects linked to your entities

</td>
<td width="33%" valign="top">

**Agent workspace and SLAs**
- Agent dashboard and saved views
- Departments and tags
- Live updates over Server-Sent Events
- Response and resolution targets per priority
- SLA tracking on every ticket

</td>
<td width="33%" valign="top">

**Customers**
- Customer portal when the UI is on
- Verified guest access by emailed code
- Embeddable widget with KB search and chat
- Knowledge base, on or off

</td>
</tr>
<tr>
<td width="33%" valign="top">

**Email**
- Notifications threaded by Message-ID
- Inbound replies from provider webhooks
- Correct threading in mail clients
- Branded templates

</td>
<td width="33%" valign="top">

**Admin and access**
- Admin configuration
- Roles, permissions, two-factor, SSO
- Native Go and SDK plugins

</td>
<td width="33%" valign="top">

**Built for Go**
- Works with net/http, Chi and more
- Inertia UI or headless JSON API
- PostgreSQL and SQLite
- Embedded SQL migrations

</td>
</tr>
</table>

<p align="center"><sub><a href="docs/features.md">Every feature in detail</a></sub></p>

<p align="center">
  <a href="https://go.dev"><img src="https://img.shields.io/badge/Go_1.26+-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go 1.26+"></a>
  <a href="https://www.postgresql.org"><img src="https://img.shields.io/badge/PostgreSQL-4169E1?style=for-the-badge&logo=postgresql&logoColor=white" alt="PostgreSQL"></a>
  <a href="https://www.sqlite.org"><img src="https://img.shields.io/badge/SQLite-003B57?style=for-the-badge&logo=sqlite&logoColor=white" alt="SQLite"></a>
  <a href="https://inertiajs.com"><img src="https://img.shields.io/badge/Inertia-9553E9?style=for-the-badge&logo=inertia&logoColor=white" alt="Inertia"></a>
  <a href="https://vuejs.org"><img src="https://img.shields.io/badge/Vue_3-4FC08D?style=for-the-badge&logo=vuedotjs&logoColor=white" alt="Vue 3"></a>
</p>

## Quick start

Install with `go get github.com/escalated-dev/escalated-go`, then:

```go
migrations.Migrate(db, "escalated_")
cfg := escalated.DefaultConfig()
cfg.DB = db
esc, err := escalated.New(cfg)
router.MountChi(r, esc)
```

Needs Go 1.26.9 or later. Set `AdminCheck`, `AgentCheck` and `UserIDFunc` to your own session checks first; until you do, they deny access. The [getting started guide](docs/getting-started.md) has the full Chi and standard library examples.

## Learn more

- [All features](docs/features.md) · [Getting started](docs/getting-started.md) · [Configuration](docs/configuration.md) · [API routes](docs/api-routes.md)
- [Custom actions, stores, statuses and translations](docs/customization.md) · [UI pages](docs/ui-pages.md)
- [Access control](docs/access-control.md) · [API tokens](docs/api-tokens.md) · [Ticket access and routing](docs/ticket-access-and-routing.md)
- [Notification emails](docs/notification-emails.md) · [Embedding the widget](docs/embedding-the-widget.md) · [Guest access](docs/guest-access.md)
- [Agent panel](docs/agent-panel.md) · [Embedding the admin](docs/embedding-admin.md)
- [Plugins](docs/plugins.md) · [Newsletters](docs/newsletters.md)

Escalated is also available for [Laravel](https://github.com/escalated-dev/escalated-laravel), [Rails](https://github.com/escalated-dev/escalated-rails), [Django](https://github.com/escalated-dev/escalated-django), [NestJS](https://github.com/escalated-dev/escalated-nestjs), [AdonisJS](https://github.com/escalated-dev/escalated-adonis), [Symfony](https://github.com/escalated-dev/escalated-symfony), [Phoenix](https://github.com/escalated-dev/escalated-phoenix), [.NET](https://github.com/escalated-dev/escalated-dotnet), [Spring](https://github.com/escalated-dev/escalated-spring), [WordPress](https://github.com/escalated-dev/escalated-wordpress) and [Filament](https://github.com/escalated-dev/escalated-filament). The UI lives in [escalated](https://github.com/escalated-dev/escalated).

## License

MIT. Copyright (c) Escalated.dev. See [LICENSE](LICENSE).
