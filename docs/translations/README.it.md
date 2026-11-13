<p align="center"><sub>
  <a href="README.ar.md">العربية</a> ·
  <a href="README.de.md">Deutsch</a> ·
  <a href="../../README.md">English</a> ·
  <a href="README.es.md">Español</a> ·
  <a href="README.fr.md">Français</a> ·
  <b>Italiano</b> ·
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
  Aggiungi un helpdesk completo alla tua app Go.<br>
  Ticket, SLA, regole di escalation, una casella per gli agenti e un portale clienti, sul tuo database.
</p>

<p align="center">
  <a href="https://escalated.dev"><b>Sito web</b></a> ·
  <a href="https://escalated.dev/docs"><b>Documentazione</b></a> ·
  <a href="https://escalated.dev/cloud"><b>Cloud</b></a> ·
  <a href="../../CHANGELOG.md"><b>Registro delle modifiche</b></a>
</p>

<br>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/inbox-dark.png">
  <img src="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/inbox-light.png" alt="La casella degli agenti di Escalated: viste salvate con contatori, conti alla rovescia SLA e priorità su ogni ticket" width="100%">
</picture>

## Cosa fa

<table>
  <tr>
    <td width="40%">
      <h3>Una casella in cui i tuoi agenti possono lavorare tutto il giorno</h3>
      Conversazione, note interne, risposte predefinite e timer SLA in un'unica schermata. I ticket arrivano anche via email tramite Mailgun, Postmark o SES.
    </td>
    <td width="60%">
      <picture>
        <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/ticket-dark.png">
        <img src="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/ticket-light.png" alt="Un ticket aperto nell'area di lavoro dell'agente con l'editor di risposta">
      </picture>
    </td>
  </tr>
  <tr>
    <td width="60%">
      <picture>
        <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/dashboard-dark.png">
        <img src="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/dashboard-light.png" alt="La pagina dei report di amministrazione con tempi di risposta, CSAT e violazioni degli SLA">
      </picture>
    </td>
    <td width="40%">
      <h3>SLA e regole di escalation che puoi vedere</h3>
      Obiettivi di risposta e risoluzione per priorità con orari lavorativi, regole che fanno escalation o riassegnano quando un obiettivo slitta, e report sulle violazioni.
    </td>
  </tr>
</table>

## Funzionalità

<table>
<tr>
<td width="33%" valign="top">

**Ticket**
- Stati, priorità e tipi di ticket
- Risposte pubbliche, note interne, messaggi di sistema
- Separare una risposta in un nuovo ticket
- Posticipa con riattivazione automatica
- Oggetti dei ticket collegati alle tue entità

</td>
<td width="33%" valign="top">

**Area agenti e SLA**
- Dashboard dell'agente e viste salvate
- Reparti e tag
- Aggiornamenti in tempo reale con Server-Sent Events
- Obiettivi di risposta e risoluzione per priorità
- Monitoraggio SLA su ogni ticket

</td>
<td width="33%" valign="top">

**Clienti**
- Portale clienti quando l'interfaccia è attiva
- Accesso ospite verificato con codice via email
- Widget incorporabile con ricerca nella knowledge base e chat
- Knowledge base, attiva o disattiva

</td>
</tr>
<tr>
<td width="33%" valign="top">

**Email**
- Notifiche raggruppate in thread tramite Message-ID
- Risposte in arrivo dai webhook del provider
- Thread corretti nei client di posta
- Modelli con il tuo brand

</td>
<td width="33%" valign="top">

**Amministrazione e accesso**
- Configurazione dall'amministrazione
- Ruoli, permessi, autenticazione a due fattori, SSO
- Plugin Go nativi e plugin SDK

</td>
<td width="33%" valign="top">

**Fatto per Go**
- Funziona con net/http, Chi e altri
- Interfaccia Inertia o API JSON headless
- PostgreSQL e SQLite
- Migrazioni SQL incorporate

</td>
</tr>
</table>

<p align="center"><sub><a href="../features.md">Tutte le funzionalità nel dettaglio</a></sub></p>

<p align="center">
  <a href="https://go.dev"><img src="https://img.shields.io/badge/Go_1.26+-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go 1.26+"></a>
  <a href="https://www.postgresql.org"><img src="https://img.shields.io/badge/PostgreSQL-4169E1?style=for-the-badge&logo=postgresql&logoColor=white" alt="PostgreSQL"></a>
  <a href="https://www.sqlite.org"><img src="https://img.shields.io/badge/SQLite-003B57?style=for-the-badge&logo=sqlite&logoColor=white" alt="SQLite"></a>
  <a href="https://inertiajs.com"><img src="https://img.shields.io/badge/Inertia-9553E9?style=for-the-badge&logo=inertia&logoColor=white" alt="Inertia"></a>
  <a href="https://vuejs.org"><img src="https://img.shields.io/badge/Vue_3-4FC08D?style=for-the-badge&logo=vuedotjs&logoColor=white" alt="Vue 3"></a>
</p>

## Avvio rapido

Installa con `go get github.com/escalated-dev/escalated-go`, poi:

```go
migrations.Migrate(db, "escalated_")
cfg := escalated.DefaultConfig()
cfg.DB = db
esc, err := escalated.New(cfg)
router.MountChi(r, esc)
```

Richiede Go 1.26.9 o successivo. Prima imposta `AdminCheck`, `AgentCheck` e `UserIDFunc` con i tuoi controlli di sessione; finché non lo fai, negano l'accesso. La [guida introduttiva](../getting-started.md) contiene gli esempi completi per Chi e la libreria standard.

## Per saperne di più

- [Tutte le funzionalità](../features.md) · [Primi passi](../getting-started.md) · [Configurazione](../configuration.md) · [Route dell'API](../api-routes.md)
- [Azioni, store, stati e traduzioni personalizzati](../customization.md) · [Pagine dell'interfaccia](../ui-pages.md)
- [Controllo degli accessi](../access-control.md) · [Token API](../api-tokens.md) · [Accesso ai ticket e instradamento](../ticket-access-and-routing.md)
- [Email di notifica](../notification-emails.md) · [Incorporare il widget](../embedding-the-widget.md) · [Accesso ospite](../guest-access.md)
- [Pannello agente](../agent-panel.md) · [Incorporare l'amministrazione](../embedding-admin.md)
- [Plugin](../plugins.md) · [Newsletter](../newsletters.md)

Escalated è disponibile anche per [Laravel](https://github.com/escalated-dev/escalated-laravel), [Rails](https://github.com/escalated-dev/escalated-rails), [Django](https://github.com/escalated-dev/escalated-django), [NestJS](https://github.com/escalated-dev/escalated-nestjs), [AdonisJS](https://github.com/escalated-dev/escalated-adonis), [Symfony](https://github.com/escalated-dev/escalated-symfony), [Phoenix](https://github.com/escalated-dev/escalated-phoenix), [.NET](https://github.com/escalated-dev/escalated-dotnet), [Spring](https://github.com/escalated-dev/escalated-spring), [WordPress](https://github.com/escalated-dev/escalated-wordpress) e [Filament](https://github.com/escalated-dev/escalated-filament). L'interfaccia si trova in [escalated](https://github.com/escalated-dev/escalated).

## Licenza

MIT. Copyright (c) Escalated.dev. Vedi [LICENSE](../../LICENSE).
