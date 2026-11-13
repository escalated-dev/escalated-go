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
  <b>Русский</b> ·
  <a href="README.tr.md">Türkçe</a> ·
  <a href="README.zh-CN.md">简体中文</a>
</sub></p>

<p align="center">
  <a href="https://escalated.dev"><img src="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/logo.png" width="72" height="72" alt="Escalated"></a>
</p>

<h1 align="center">Escalated for Go</h1>

<p align="center">
  Добавь полноценный helpdesk в своё Go-приложение.<br>
  Тикеты, SLA, правила эскалации, входящие для агентов и клиентский портал, прямо на твоей собственной базе данных.
</p>

<p align="center">
  <a href="https://escalated.dev"><b>Сайт</b></a> ·
  <a href="https://escalated.dev/docs"><b>Документация</b></a> ·
  <a href="https://escalated.dev/cloud"><b>Cloud</b></a> ·
  <a href="../../CHANGELOG.md"><b>Список изменений</b></a>
</p>

<br>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/inbox-dark.png">
  <img src="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/inbox-light.png" alt="Входящие агентов в Escalated: сохранённые представления со счётчиками, обратный отсчёт SLA и приоритеты у каждого тикета" width="100%">
</picture>

## Что он умеет

<table>
  <tr>
    <td width="40%">
      <h3>Входящие, в которых твои агенты могут работать весь день</h3>
      Переписка, внутренние заметки, шаблоны ответов и таймеры SLA на одном экране. Тикеты также приходят по почте через Mailgun, Postmark или SES.
    </td>
    <td width="60%">
      <picture>
        <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/ticket-dark.png">
        <img src="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/ticket-light.png" alt="Тикет, открытый в рабочем пространстве агента, с редактором ответа">
      </picture>
    </td>
  </tr>
  <tr>
    <td width="60%">
      <picture>
        <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/dashboard-dark.png">
        <img src="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/dashboard-light.png" alt="Страница отчётов в админке со временем ответа, CSAT и нарушениями SLA">
      </picture>
    </td>
    <td width="40%">
      <h3>SLA и правила эскалации у тебя на виду</h3>
      Цели по ответу и решению для каждого приоритета с учётом рабочих часов, правила, которые эскалируют или переназначают тикет, когда цель срывается, и отчёты о нарушениях.
    </td>
  </tr>
</table>

## Возможности

<table>
<tr>
<td width="33%" valign="top">

**Тикеты**
- Статусы, приоритеты и типы тикетов
- Публичные ответы, внутренние заметки, системные сообщения
- Вынос ответа в новый тикет
- Откладывание с автоматическим возвратом
- Привязка тикетов к твоим сущностям

</td>
<td width="33%" valign="top">

**Рабочее место агента и SLA**
- Панель агента и сохранённые представления
- Отделы и теги
- Обновления в реальном времени через Server-Sent Events
- Цели по ответу и решению для каждого приоритета
- Отслеживание SLA у каждого тикета

</td>
<td width="33%" valign="top">

**Клиенты**
- Клиентский портал, когда интерфейс включён
- Проверенный гостевой доступ по коду из письма
- Встраиваемый виджет с поиском по базе знаний и чатом
- База знаний, включается и выключается

</td>
</tr>
<tr>
<td width="33%" valign="top">

**Почта**
- Уведомления, связанные в цепочки по Message-ID
- Входящие ответы через вебхуки провайдера
- Правильные цепочки в почтовых клиентах
- Шаблоны в твоём фирменном стиле

</td>
<td width="33%" valign="top">

**Администрирование и доступ**
- Настройка через админку
- Роли, права, двухфакторная аутентификация, SSO
- Нативные Go-плагины и плагины SDK

</td>
<td width="33%" valign="top">

**Сделан для Go**
- Работает с net/http, Chi и другими
- Интерфейс на Inertia или headless JSON API
- PostgreSQL и SQLite
- Встроенные SQL-миграции

</td>
</tr>
</table>

<p align="center"><sub><a href="../features.md">Все возможности подробно</a></sub></p>

<p align="center">
  <a href="https://go.dev"><img src="https://img.shields.io/badge/Go_1.26+-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go 1.26+"></a>
  <a href="https://www.postgresql.org"><img src="https://img.shields.io/badge/PostgreSQL-4169E1?style=for-the-badge&logo=postgresql&logoColor=white" alt="PostgreSQL"></a>
  <a href="https://www.sqlite.org"><img src="https://img.shields.io/badge/SQLite-003B57?style=for-the-badge&logo=sqlite&logoColor=white" alt="SQLite"></a>
  <a href="https://inertiajs.com"><img src="https://img.shields.io/badge/Inertia-9553E9?style=for-the-badge&logo=inertia&logoColor=white" alt="Inertia"></a>
  <a href="https://vuejs.org"><img src="https://img.shields.io/badge/Vue_3-4FC08D?style=for-the-badge&logo=vuedotjs&logoColor=white" alt="Vue 3"></a>
</p>

## Быстрый старт

Установи через `go get github.com/escalated-dev/escalated-go`, затем:

```go
migrations.Migrate(db, "escalated_")
cfg := escalated.DefaultConfig()
cfg.DB = db
esc, err := escalated.New(cfg)
router.MountChi(r, esc)
```

Нужен Go 1.26.9 или новее. Сначала задай для `AdminCheck`, `AgentCheck` и `UserIDFunc` свои проверки сессии; пока ты этого не сделаешь, они запрещают доступ. В [руководстве по началу работы](../getting-started.md) есть полные примеры для Chi и стандартной библиотеки.

## Подробнее

- [Все возможности](../features.md) · [Начало работы](../getting-started.md) · [Настройка](../configuration.md) · [Маршруты API](../api-routes.md)
- [Свои действия, хранилища, статусы и переводы](../customization.md) · [Страницы интерфейса](../ui-pages.md)
- [Управление доступом](../access-control.md) · [API-токены](../api-tokens.md) · [Доступ к тикетам и маршрутизация](../ticket-access-and-routing.md)
- [Письма-уведомления](../notification-emails.md) · [Встраивание виджета](../embedding-the-widget.md) · [Гостевой доступ](../guest-access.md)
- [Панель агента](../agent-panel.md) · [Встраивание админки](../embedding-admin.md)
- [Плагины](../plugins.md) · [Рассылки](../newsletters.md)

Escalated также доступен для [Laravel](https://github.com/escalated-dev/escalated-laravel), [Rails](https://github.com/escalated-dev/escalated-rails), [Django](https://github.com/escalated-dev/escalated-django), [NestJS](https://github.com/escalated-dev/escalated-nestjs), [AdonisJS](https://github.com/escalated-dev/escalated-adonis), [Symfony](https://github.com/escalated-dev/escalated-symfony), [Phoenix](https://github.com/escalated-dev/escalated-phoenix), [.NET](https://github.com/escalated-dev/escalated-dotnet), [Spring](https://github.com/escalated-dev/escalated-spring), [WordPress](https://github.com/escalated-dev/escalated-wordpress) и [Filament](https://github.com/escalated-dev/escalated-filament). Интерфейс находится в [escalated](https://github.com/escalated-dev/escalated).

## Лицензия

MIT. Copyright (c) Escalated.dev. См. [LICENSE](../../LICENSE).
