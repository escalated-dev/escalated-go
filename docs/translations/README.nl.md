<p align="center"><sub>
  <a href="README.ar.md">العربية</a> ·
  <a href="README.de.md">Deutsch</a> ·
  <a href="../../README.md">English</a> ·
  <a href="README.es.md">Español</a> ·
  <a href="README.fr.md">Français</a> ·
  <a href="README.it.md">Italiano</a> ·
  <a href="README.ja.md">日本語</a> ·
  <a href="README.ko.md">한국어</a> ·
  <b>Nederlands</b> ·
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
  Voeg een complete helpdesk toe aan je Go-app.<br>
  Tickets, SLA's, escalatieregels, een inbox voor agents en een klantportaal, op je eigen database.
</p>

<p align="center">
  <a href="https://escalated.dev"><b>Website</b></a> ·
  <a href="https://escalated.dev/docs"><b>Documentatie</b></a> ·
  <a href="https://escalated.dev/cloud"><b>Cloud</b></a> ·
  <a href="../../CHANGELOG.md"><b>Wijzigingslog</b></a>
</p>

<br>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/inbox-dark.png">
  <img src="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/inbox-light.png" alt="De agent-inbox van Escalated: opgeslagen weergaven met tellers, SLA-afteltimers en prioriteiten op elk ticket" width="100%">
</picture>

## Wat het doet

<table>
  <tr>
    <td width="40%">
      <h3>Een inbox waarin je agents de hele dag kunnen werken</h3>
      Gesprek, interne notities, standaardantwoorden en SLA-timers op één scherm. Tickets komen ook binnen per e-mail via Mailgun, Postmark of SES.
    </td>
    <td width="60%">
      <picture>
        <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/ticket-dark.png">
        <img src="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/ticket-light.png" alt="Een geopend ticket in de werkruimte van de agent met de antwoordeditor">
      </picture>
    </td>
  </tr>
  <tr>
    <td width="60%">
      <picture>
        <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/dashboard-dark.png">
        <img src="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/dashboard-light.png" alt="De rapportpagina in het beheer met reactietijden, CSAT en SLA-overschrijdingen">
      </picture>
    </td>
    <td width="40%">
      <h3>SLA's en escalatieregels die je kunt zien</h3>
      Reactie- en oplosdoelen per prioriteit met kantooruren, regels die escaleren of opnieuw toewijzen wanneer een doel niet wordt gehaald, en rapporten over overschrijdingen.
    </td>
  </tr>
</table>

## Functies

<table>
<tr>
<td width="33%" valign="top">

**Tickets**
- Statussen, prioriteiten en tickettypen
- Openbare antwoorden, interne notities, systeemberichten
- Een antwoord afsplitsen naar een nieuw ticket
- Uitstellen met automatisch heropenen
- Ticketonderwerpen gekoppeld aan je eigen entiteiten

</td>
<td width="33%" valign="top">

**Agentwerkruimte en SLA's**
- Agentdashboard en opgeslagen weergaven
- Afdelingen en tags
- Live updates via Server-Sent Events
- Reactie- en oplosdoelen per prioriteit
- SLA-bewaking op elk ticket

</td>
<td width="33%" valign="top">

**Klanten**
- Klantportaal wanneer de UI aan staat
- Geverifieerde gasttoegang met een code per e-mail
- Insluitbare widget met zoeken in de kennisbank en chat
- Kennisbank, aan of uit

</td>
</tr>
<tr>
<td width="33%" valign="top">

**E-mail**
- Meldingen in threads via Message-ID
- Inkomende antwoorden via webhooks van de provider
- Correcte threads in mailclients
- Sjablonen in je eigen huisstijl

</td>
<td width="33%" valign="top">

**Beheer en toegang**
- Configuratie in het beheer
- Rollen, rechten, tweestapsverificatie, SSO
- Native Go- en SDK-plugins

</td>
<td width="33%" valign="top">

**Gemaakt voor Go**
- Werkt met net/http, Chi en meer
- Inertia-UI of headless JSON-API
- PostgreSQL en SQLite
- Ingebouwde SQL-migraties

</td>
</tr>
</table>

<p align="center"><sub><a href="../features.md">Alle functies in detail</a></sub></p>

<p align="center">
  <a href="https://go.dev"><img src="https://img.shields.io/badge/Go_1.26+-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go 1.26+"></a>
  <a href="https://www.postgresql.org"><img src="https://img.shields.io/badge/PostgreSQL-4169E1?style=for-the-badge&logo=postgresql&logoColor=white" alt="PostgreSQL"></a>
  <a href="https://www.sqlite.org"><img src="https://img.shields.io/badge/SQLite-003B57?style=for-the-badge&logo=sqlite&logoColor=white" alt="SQLite"></a>
  <a href="https://inertiajs.com"><img src="https://img.shields.io/badge/Inertia-9553E9?style=for-the-badge&logo=inertia&logoColor=white" alt="Inertia"></a>
  <a href="https://vuejs.org"><img src="https://img.shields.io/badge/Vue_3-4FC08D?style=for-the-badge&logo=vuedotjs&logoColor=white" alt="Vue 3"></a>
</p>

## Snel aan de slag

Installeer met `go get github.com/escalated-dev/escalated-go` en daarna:

```go
migrations.Migrate(db, "escalated_")
cfg := escalated.DefaultConfig()
cfg.DB = db
esc, err := escalated.New(cfg)
router.MountChi(r, esc)
```

Vereist Go 1.26.9 of nieuwer. Stel eerst `AdminCheck`, `AgentCheck` en `UserIDFunc` in op je eigen sessiecontroles; tot je dat doet, weigeren ze toegang. De [handleiding om te beginnen](../getting-started.md) bevat de volledige voorbeelden voor Chi en de standaardbibliotheek.

## Meer weten

- [Alle functies](../features.md) · [Aan de slag](../getting-started.md) · [Configuratie](../configuration.md) · [API-routes](../api-routes.md)
- [Eigen acties, stores, statussen en vertalingen](../customization.md) · [UI-pagina's](../ui-pages.md)
- [Toegangsbeheer](../access-control.md) · [API-tokens](../api-tokens.md) · [Tickettoegang en routering](../ticket-access-and-routing.md)
- [Meldingsmails](../notification-emails.md) · [De widget insluiten](../embedding-the-widget.md) · [Gasttoegang](../guest-access.md)
- [Agentpaneel](../agent-panel.md) · [Het beheer insluiten](../embedding-admin.md)
- [Plugins](../plugins.md) · [Nieuwsbrieven](../newsletters.md)

Escalated is ook beschikbaar voor [Laravel](https://github.com/escalated-dev/escalated-laravel), [Rails](https://github.com/escalated-dev/escalated-rails), [Django](https://github.com/escalated-dev/escalated-django), [NestJS](https://github.com/escalated-dev/escalated-nestjs), [AdonisJS](https://github.com/escalated-dev/escalated-adonis), [Symfony](https://github.com/escalated-dev/escalated-symfony), [Phoenix](https://github.com/escalated-dev/escalated-phoenix), [.NET](https://github.com/escalated-dev/escalated-dotnet), [Spring](https://github.com/escalated-dev/escalated-spring), [WordPress](https://github.com/escalated-dev/escalated-wordpress) en [Filament](https://github.com/escalated-dev/escalated-filament). De UI staat in [escalated](https://github.com/escalated-dev/escalated).

## Licentie

MIT. Copyright (c) Escalated.dev. Zie [LICENSE](../../LICENSE).
