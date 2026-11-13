<p align="center"><sub>
  <a href="README.ar.md">العربية</a> ·
  <a href="README.de.md">Deutsch</a> ·
  <a href="../../README.md">English</a> ·
  <b>Español</b> ·
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
  Añade un helpdesk completo a tu app de Go.<br>
  Tickets, SLAs, reglas de escalado, una bandeja de entrada para agentes y un portal de clientes, funcionando sobre tu propia base de datos.
</p>

<p align="center">
  <a href="https://escalated.dev"><b>Sitio web</b></a> ·
  <a href="https://escalated.dev/docs"><b>Documentación</b></a> ·
  <a href="https://escalated.dev/cloud"><b>Cloud</b></a> ·
  <a href="../../CHANGELOG.md"><b>Registro de cambios</b></a>
</p>

<br>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/inbox-dark.png">
  <img src="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/inbox-light.png" alt="La bandeja de entrada de agentes de Escalated: vistas guardadas con contadores, cuentas atrás de SLA y prioridades en cada ticket" width="100%">
</picture>

## Qué hace

<table>
  <tr>
    <td width="40%">
      <h3>Una bandeja de entrada en la que tus agentes pueden trabajar todo el día</h3>
      Conversación, notas internas, respuestas predefinidas y temporizadores de SLA en una sola pantalla. Los tickets también llegan por correo a través de Mailgun, Postmark o SES.
    </td>
    <td width="60%">
      <picture>
        <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/ticket-dark.png">
        <img src="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/ticket-light.png" alt="Un ticket abierto en el espacio de trabajo del agente con el editor de respuestas">
      </picture>
    </td>
  </tr>
  <tr>
    <td width="60%">
      <picture>
        <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/dashboard-dark.png">
        <img src="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/dashboard-light.png" alt="La página de informes de administración con tiempos de respuesta, CSAT e incumplimientos de SLA">
      </picture>
    </td>
    <td width="40%">
      <h3>SLAs y reglas de escalado que puedes ver</h3>
      Objetivos de respuesta y resolución por prioridad con horario laboral, reglas que escalan o reasignan cuando no se cumplen e informes de incumplimientos.
    </td>
  </tr>
</table>

## Funciones

<table>
<tr>
<td width="33%" valign="top">

**Tickets**
- Estados, prioridades y tipos de ticket
- Respuestas públicas, notas internas, mensajes del sistema
- Separar una respuesta en un ticket nuevo
- Posponer con reactivación automática
- Asuntos de ticket vinculados a tus entidades

</td>
<td width="33%" valign="top">

**Espacio de agentes y SLAs**
- Panel del agente y vistas guardadas
- Departamentos y etiquetas
- Actualizaciones en vivo mediante Server-Sent Events
- Objetivos de respuesta y resolución por prioridad
- Seguimiento de SLA en cada ticket

</td>
<td width="33%" valign="top">

**Clientes**
- Portal de clientes cuando la interfaz está activada
- Acceso de invitados verificado con código por correo
- Widget integrable con búsqueda en la base de conocimiento y chat
- Base de conocimiento, activada o desactivada

</td>
</tr>
<tr>
<td width="33%" valign="top">

**Correo**
- Notificaciones agrupadas en hilos por Message-ID
- Respuestas entrantes desde webhooks del proveedor
- Hilos correctos en los clientes de correo
- Plantillas con tu marca

</td>
<td width="33%" valign="top">

**Administración y acceso**
- Configuración desde la administración
- Roles, permisos, doble factor, SSO
- Plugins nativos de Go y del SDK

</td>
<td width="33%" valign="top">

**Hecho para Go**
- Funciona con net/http, Chi y más
- Interfaz Inertia o API JSON headless
- PostgreSQL y SQLite
- Migraciones SQL integradas

</td>
</tr>
</table>

<p align="center"><sub><a href="../features.md">Todas las funciones en detalle</a></sub></p>

<p align="center">
  <a href="https://go.dev"><img src="https://img.shields.io/badge/Go_1.26+-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go 1.26+"></a>
  <a href="https://www.postgresql.org"><img src="https://img.shields.io/badge/PostgreSQL-4169E1?style=for-the-badge&logo=postgresql&logoColor=white" alt="PostgreSQL"></a>
  <a href="https://www.sqlite.org"><img src="https://img.shields.io/badge/SQLite-003B57?style=for-the-badge&logo=sqlite&logoColor=white" alt="SQLite"></a>
  <a href="https://inertiajs.com"><img src="https://img.shields.io/badge/Inertia-9553E9?style=for-the-badge&logo=inertia&logoColor=white" alt="Inertia"></a>
  <a href="https://vuejs.org"><img src="https://img.shields.io/badge/Vue_3-4FC08D?style=for-the-badge&logo=vuedotjs&logoColor=white" alt="Vue 3"></a>
</p>

## Inicio rápido

Instala con `go get github.com/escalated-dev/escalated-go` y luego:

```go
migrations.Migrate(db, "escalated_")
cfg := escalated.DefaultConfig()
cfg.DB = db
esc, err := escalated.New(cfg)
router.MountChi(r, esc)
```

Requiere Go 1.26.9 o posterior. Primero configura `AdminCheck`, `AgentCheck` y `UserIDFunc` con tus propias comprobaciones de sesión; hasta que lo hagas, deniegan el acceso. La [guía de inicio](../getting-started.md) tiene los ejemplos completos para Chi y la biblioteca estándar.

## Más información

- [Todas las funciones](../features.md) · [Primeros pasos](../getting-started.md) · [Configuración](../configuration.md) · [Rutas de la API](../api-routes.md)
- [Acciones, stores, estados y traducciones personalizados](../customization.md) · [Páginas de la interfaz](../ui-pages.md)
- [Control de acceso](../access-control.md) · [Tokens de API](../api-tokens.md) · [Acceso a tickets y enrutamiento](../ticket-access-and-routing.md)
- [Correos de notificación](../notification-emails.md) · [Integrar el widget](../embedding-the-widget.md) · [Acceso de invitados](../guest-access.md)
- [Panel del agente](../agent-panel.md) · [Integrar la administración](../embedding-admin.md)
- [Plugins](../plugins.md) · [Boletines](../newsletters.md)

Escalated también está disponible para [Laravel](https://github.com/escalated-dev/escalated-laravel), [Rails](https://github.com/escalated-dev/escalated-rails), [Django](https://github.com/escalated-dev/escalated-django), [NestJS](https://github.com/escalated-dev/escalated-nestjs), [AdonisJS](https://github.com/escalated-dev/escalated-adonis), [Symfony](https://github.com/escalated-dev/escalated-symfony), [Phoenix](https://github.com/escalated-dev/escalated-phoenix), [.NET](https://github.com/escalated-dev/escalated-dotnet), [Spring](https://github.com/escalated-dev/escalated-spring), [WordPress](https://github.com/escalated-dev/escalated-wordpress) y [Filament](https://github.com/escalated-dev/escalated-filament). La interfaz vive en [escalated](https://github.com/escalated-dev/escalated).

## Licencia

MIT. Copyright (c) Escalated.dev. Consulta [LICENSE](../../LICENSE).
