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
  <b>Português (BR)</b> ·
  <a href="README.ru.md">Русский</a> ·
  <a href="README.tr.md">Türkçe</a> ·
  <a href="README.zh-CN.md">简体中文</a>
</sub></p>

<p align="center">
  <a href="https://escalated.dev"><img src="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/logo.png" width="72" height="72" alt="Escalated"></a>
</p>

<h1 align="center">Escalated for Go</h1>

<p align="center">
  Adicione um helpdesk completo ao seu app Go.<br>
  Tickets, SLAs, regras de escalonamento, uma caixa de entrada para agentes e um portal do cliente, rodando no seu próprio banco de dados.
</p>

<p align="center">
  <a href="https://escalated.dev"><b>Site</b></a> ·
  <a href="https://escalated.dev/docs"><b>Documentação</b></a> ·
  <a href="https://escalated.dev/cloud"><b>Cloud</b></a> ·
  <a href="../../CHANGELOG.md"><b>Registro de alterações</b></a>
</p>

<br>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/inbox-dark.png">
  <img src="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/inbox-light.png" alt="A caixa de entrada dos agentes no Escalated: visualizações salvas com contadores, contagens regressivas de SLA e prioridades em cada ticket" width="100%">
</picture>

## O que ele faz

<table>
  <tr>
    <td width="40%">
      <h3>Uma caixa de entrada onde seus agentes podem trabalhar o dia todo</h3>
      Conversa, notas internas, respostas prontas e timers de SLA em uma só tela. Os tickets também chegam por e-mail via Mailgun, Postmark ou SES.
    </td>
    <td width="60%">
      <picture>
        <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/ticket-dark.png">
        <img src="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/ticket-light.png" alt="Um ticket aberto no espaço de trabalho do agente com o editor de resposta">
      </picture>
    </td>
  </tr>
  <tr>
    <td width="60%">
      <picture>
        <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/dashboard-dark.png">
        <img src="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/dashboard-light.png" alt="A página de relatórios do admin com tempos de resposta, CSAT e violações de SLA">
      </picture>
    </td>
    <td width="40%">
      <h3>SLAs e regras de escalonamento que você consegue ver</h3>
      Metas de resposta e resolução por prioridade com horário comercial, regras que escalonam ou reatribuem quando uma meta escapa, e relatórios de violações.
    </td>
  </tr>
</table>

## Recursos

<table>
<tr>
<td width="33%" valign="top">

**Tickets**
- Status, prioridades e tipos de ticket
- Respostas públicas, notas internas, mensagens do sistema
- Separar uma resposta em um novo ticket
- Adiar com retomada automática
- Assuntos de ticket ligados às suas entidades

</td>
<td width="33%" valign="top">

**Espaço do agente e SLAs**
- Painel do agente e visualizações salvas
- Departamentos e tags
- Atualizações ao vivo via Server-Sent Events
- Metas de resposta e resolução por prioridade
- Acompanhamento de SLA em cada ticket

</td>
<td width="33%" valign="top">

**Clientes**
- Portal do cliente quando a interface está ativa
- Acesso de convidado verificado por código enviado por e-mail
- Widget incorporável com busca na base de conhecimento e chat
- Base de conhecimento, ligada ou desligada

</td>
</tr>
<tr>
<td width="33%" valign="top">

**E-mail**
- Notificações agrupadas em threads por Message-ID
- Respostas recebidas pelos webhooks do provedor
- Threads corretas nos clientes de e-mail
- Templates com a sua marca

</td>
<td width="33%" valign="top">

**Admin e acesso**
- Configuração pelo admin
- Papéis, permissões, autenticação em dois fatores, SSO
- Plugins Go nativos e plugins do SDK

</td>
<td width="33%" valign="top">

**Feito para Go**
- Funciona com net/http, Chi e outros
- Interface Inertia ou API JSON headless
- PostgreSQL e SQLite
- Migrações SQL embutidas

</td>
</tr>
</table>

<p align="center"><sub><a href="../features.md">Todos os recursos em detalhes</a></sub></p>

<p align="center">
  <a href="https://go.dev"><img src="https://img.shields.io/badge/Go_1.26+-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go 1.26+"></a>
  <a href="https://www.postgresql.org"><img src="https://img.shields.io/badge/PostgreSQL-4169E1?style=for-the-badge&logo=postgresql&logoColor=white" alt="PostgreSQL"></a>
  <a href="https://www.sqlite.org"><img src="https://img.shields.io/badge/SQLite-003B57?style=for-the-badge&logo=sqlite&logoColor=white" alt="SQLite"></a>
  <a href="https://inertiajs.com"><img src="https://img.shields.io/badge/Inertia-9553E9?style=for-the-badge&logo=inertia&logoColor=white" alt="Inertia"></a>
  <a href="https://vuejs.org"><img src="https://img.shields.io/badge/Vue_3-4FC08D?style=for-the-badge&logo=vuedotjs&logoColor=white" alt="Vue 3"></a>
</p>

## Início rápido

Instale com `go get github.com/escalated-dev/escalated-go` e depois:

```go
migrations.Migrate(db, "escalated_")
cfg := escalated.DefaultConfig()
cfg.DB = db
esc, err := escalated.New(cfg)
router.MountChi(r, esc)
```

Requer Go 1.26.9 ou mais recente. Primeiro defina `AdminCheck`, `AgentCheck` e `UserIDFunc` com as suas próprias verificações de sessão; enquanto você não fizer isso, eles negam o acesso. O [guia de primeiros passos](../getting-started.md) traz os exemplos completos para Chi e a biblioteca padrão.

## Saiba mais

- [Todos os recursos](../features.md) · [Primeiros passos](../getting-started.md) · [Configuração](../configuration.md) · [Rotas da API](../api-routes.md)
- [Ações, stores, status e traduções personalizados](../customization.md) · [Páginas da interface](../ui-pages.md)
- [Controle de acesso](../access-control.md) · [Tokens de API](../api-tokens.md) · [Acesso a tickets e roteamento](../ticket-access-and-routing.md)
- [E-mails de notificação](../notification-emails.md) · [Incorporar o widget](../embedding-the-widget.md) · [Acesso de convidado](../guest-access.md)
- [Painel do agente](../agent-panel.md) · [Incorporar o admin](../embedding-admin.md)
- [Plugins](../plugins.md) · [Newsletters](../newsletters.md)

O Escalated também está disponível para [Laravel](https://github.com/escalated-dev/escalated-laravel), [Rails](https://github.com/escalated-dev/escalated-rails), [Django](https://github.com/escalated-dev/escalated-django), [NestJS](https://github.com/escalated-dev/escalated-nestjs), [AdonisJS](https://github.com/escalated-dev/escalated-adonis), [Symfony](https://github.com/escalated-dev/escalated-symfony), [Phoenix](https://github.com/escalated-dev/escalated-phoenix), [.NET](https://github.com/escalated-dev/escalated-dotnet), [Spring](https://github.com/escalated-dev/escalated-spring), [WordPress](https://github.com/escalated-dev/escalated-wordpress) e [Filament](https://github.com/escalated-dev/escalated-filament). A interface fica em [escalated](https://github.com/escalated-dev/escalated).

## Licença

MIT. Copyright (c) Escalated.dev. Veja [LICENSE](../../LICENSE).
