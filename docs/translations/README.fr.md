<p align="center"><sub>
  <a href="README.ar.md">العربية</a> ·
  <a href="README.de.md">Deutsch</a> ·
  <a href="../../README.md">English</a> ·
  <a href="README.es.md">Español</a> ·
  <b>Français</b> ·
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
  Ajoute un helpdesk complet à ton app Go.<br>
  Tickets, SLA, règles d'escalade, une boîte de réception pour les agents et un portail client, sur ta propre base de données.
</p>

<p align="center">
  <a href="https://escalated.dev"><b>Site web</b></a> ·
  <a href="https://escalated.dev/docs"><b>Documentation</b></a> ·
  <a href="https://escalated.dev/cloud"><b>Cloud</b></a> ·
  <a href="../../CHANGELOG.md"><b>Journal des modifications</b></a>
</p>

<br>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/inbox-dark.png">
  <img src="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/inbox-light.png" alt="La boîte de réception des agents d'Escalated : vues enregistrées avec compteurs, comptes à rebours SLA et priorités sur chaque ticket" width="100%">
</picture>

## Ce qu'il fait

<table>
  <tr>
    <td width="40%">
      <h3>Une boîte de réception où tes agents peuvent travailler toute la journée</h3>
      Conversation, notes internes, réponses types et minuteurs SLA sur un seul écran. Les tickets arrivent aussi par e-mail via Mailgun, Postmark ou SES.
    </td>
    <td width="60%">
      <picture>
        <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/ticket-dark.png">
        <img src="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/ticket-light.png" alt="Un ticket ouvert dans l'espace de travail de l'agent avec l'éditeur de réponse">
      </picture>
    </td>
  </tr>
  <tr>
    <td width="60%">
      <picture>
        <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/dashboard-dark.png">
        <img src="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/dashboard-light.png" alt="La page des rapports d'administration avec les temps de réponse, le CSAT et les dépassements de SLA">
      </picture>
    </td>
    <td width="40%">
      <h3>Des SLA et des règles d'escalade que tu peux voir</h3>
      Objectifs de réponse et de résolution par priorité avec heures ouvrées, règles qui escaladent ou réassignent quand un objectif glisse, et rapports sur les dépassements.
    </td>
  </tr>
</table>

## Fonctionnalités

<table>
<tr>
<td width="33%" valign="top">

**Tickets**
- Statuts, priorités et types de ticket
- Réponses publiques, notes internes, messages système
- Séparer une réponse dans un nouveau ticket
- Mise en pause avec réveil automatique
- Sujets de ticket liés à tes propres entités

</td>
<td width="33%" valign="top">

**Espace agent et SLA**
- Tableau de bord agent et vues enregistrées
- Départements et tags
- Mises à jour en direct via Server-Sent Events
- Objectifs de réponse et de résolution par priorité
- Suivi des SLA sur chaque ticket

</td>
<td width="33%" valign="top">

**Clients**
- Portail client quand l'interface est activée
- Accès invité vérifié par code envoyé par e-mail
- Widget intégrable avec recherche dans la base de connaissances et chat
- Base de connaissances, activée ou non

</td>
</tr>
<tr>
<td width="33%" valign="top">

**E-mail**
- Notifications regroupées en fils par Message-ID
- Réponses entrantes via les webhooks du fournisseur
- Fils corrects dans les clients mail
- Modèles à ta marque

</td>
<td width="33%" valign="top">

**Administration et accès**
- Configuration dans l'administration
- Rôles, permissions, double authentification, SSO
- Plugins Go natifs et plugins SDK

</td>
<td width="33%" valign="top">

**Conçu pour Go**
- Fonctionne avec net/http, Chi et d'autres
- Interface Inertia ou API JSON headless
- PostgreSQL et SQLite
- Migrations SQL embarquées

</td>
</tr>
</table>

<p align="center"><sub><a href="../features.md">Toutes les fonctionnalités en détail</a></sub></p>

<p align="center">
  <a href="https://go.dev"><img src="https://img.shields.io/badge/Go_1.26+-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go 1.26+"></a>
  <a href="https://www.postgresql.org"><img src="https://img.shields.io/badge/PostgreSQL-4169E1?style=for-the-badge&logo=postgresql&logoColor=white" alt="PostgreSQL"></a>
  <a href="https://www.sqlite.org"><img src="https://img.shields.io/badge/SQLite-003B57?style=for-the-badge&logo=sqlite&logoColor=white" alt="SQLite"></a>
  <a href="https://inertiajs.com"><img src="https://img.shields.io/badge/Inertia-9553E9?style=for-the-badge&logo=inertia&logoColor=white" alt="Inertia"></a>
  <a href="https://vuejs.org"><img src="https://img.shields.io/badge/Vue_3-4FC08D?style=for-the-badge&logo=vuedotjs&logoColor=white" alt="Vue 3"></a>
</p>

## Démarrage rapide

Installe avec `go get github.com/escalated-dev/escalated-go`, puis :

```go
migrations.Migrate(db, "escalated_")
cfg := escalated.DefaultConfig()
cfg.DB = db
esc, err := escalated.New(cfg)
router.MountChi(r, esc)
```

Nécessite Go 1.26.9 ou plus récent. Configure d'abord `AdminCheck`, `AgentCheck` et `UserIDFunc` avec tes propres vérifications de session ; tant que ce n'est pas fait, ils refusent l'accès. Le [guide de démarrage](../getting-started.md) contient les exemples complets pour Chi et la bibliothèque standard.

## En savoir plus

- [Toutes les fonctionnalités](../features.md) · [Premiers pas](../getting-started.md) · [Configuration](../configuration.md) · [Routes de l'API](../api-routes.md)
- [Actions, stores, statuts et traductions personnalisés](../customization.md) · [Pages de l'interface](../ui-pages.md)
- [Contrôle d'accès](../access-control.md) · [Jetons d'API](../api-tokens.md) · [Accès aux tickets et routage](../ticket-access-and-routing.md)
- [E-mails de notification](../notification-emails.md) · [Intégrer le widget](../embedding-the-widget.md) · [Accès invité](../guest-access.md)
- [Panneau agent](../agent-panel.md) · [Intégrer l'administration](../embedding-admin.md)
- [Plugins](../plugins.md) · [Newsletters](../newsletters.md)

Escalated existe aussi pour [Laravel](https://github.com/escalated-dev/escalated-laravel), [Rails](https://github.com/escalated-dev/escalated-rails), [Django](https://github.com/escalated-dev/escalated-django), [NestJS](https://github.com/escalated-dev/escalated-nestjs), [AdonisJS](https://github.com/escalated-dev/escalated-adonis), [Symfony](https://github.com/escalated-dev/escalated-symfony), [Phoenix](https://github.com/escalated-dev/escalated-phoenix), [.NET](https://github.com/escalated-dev/escalated-dotnet), [Spring](https://github.com/escalated-dev/escalated-spring), [WordPress](https://github.com/escalated-dev/escalated-wordpress) et [Filament](https://github.com/escalated-dev/escalated-filament). L'interface se trouve dans [escalated](https://github.com/escalated-dev/escalated).

## Licence

MIT. Copyright (c) Escalated.dev. Voir [LICENSE](../../LICENSE).
