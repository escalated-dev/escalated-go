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
  <a href="README.tr.md">Türkçe</a> ·
  <b>简体中文</b>
</sub></p>

<p align="center">
  <a href="https://escalated.dev"><img src="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/logo.png" width="72" height="72" alt="Escalated"></a>
</p>

<h1 align="center">Escalated for Go</h1>

<p align="center">
  为你的 Go 应用加上一套完整的客服工单系统。<br>
  工单、SLA、升级规则、客服收件箱和客户门户，直接运行在你自己的数据库上。
</p>

<p align="center">
  <a href="https://escalated.dev"><b>网站</b></a> ·
  <a href="https://escalated.dev/docs"><b>文档</b></a> ·
  <a href="https://escalated.dev/cloud"><b>Cloud</b></a> ·
  <a href="../../CHANGELOG.md"><b>更新日志</b></a>
</p>

<br>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/inbox-dark.png">
  <img src="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/inbox-light.png" alt="Escalated 客服收件箱：带计数的已保存视图、SLA 倒计时以及每个工单的优先级" width="100%">
</picture>

## 它能做什么

<table>
  <tr>
    <td width="40%">
      <h3>客服可以整天在里面工作的收件箱</h3>
      对话、内部备注、预设回复和 SLA 计时器都在一个界面上。工单也可以通过 Mailgun、Postmark 或 SES 以邮件方式进入。
    </td>
    <td width="60%">
      <picture>
        <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/ticket-dark.png">
        <img src="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/ticket-light.png" alt="在客服工作区中打开的工单和回复编辑器">
      </picture>
    </td>
  </tr>
  <tr>
    <td width="60%">
      <picture>
        <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/dashboard-dark.png">
        <img src="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/dashboard-light.png" alt="管理后台报表页面，显示响应时间、CSAT 和 SLA 违约">
      </picture>
    </td>
    <td width="40%">
      <h3>看得见的 SLA 和升级规则</h3>
      按优先级设定响应和解决目标并考虑工作时间，目标落空时自动升级或重新分配的规则，以及违约报表。
    </td>
  </tr>
</table>

## 功能

<table>
<tr>
<td width="33%" valign="top">

**工单**
- 状态、优先级和工单类型
- 公开回复、内部备注、系统消息
- 把一条回复拆分为新工单
- 暂缓处理并自动唤醒
- 工单可关联到你自己的实体

</td>
<td width="33%" valign="top">

**客服工作区和 SLA**
- 客服仪表盘和已保存视图
- 部门和标签
- 通过 Server-Sent Events 实时更新
- 按优先级设定响应和解决目标
- 每个工单都有 SLA 跟踪

</td>
<td width="33%" valign="top">

**客户**
- 启用 UI 时提供客户门户
- 通过邮件验证码验证的访客访问
- 可嵌入的小组件，支持知识库搜索和聊天
- 知识库，可开可关

</td>
</tr>
<tr>
<td width="33%" valign="top">

**邮件**
- 按 Message-ID 串联的通知
- 通过服务商 Webhook 接收回复
- 在邮件客户端中正确归入会话
- 带品牌的邮件模板

</td>
<td width="33%" valign="top">

**管理和访问**
- 后台配置
- 角色、权限、双因素认证、SSO
- 原生 Go 插件和 SDK 插件

</td>
<td width="33%" valign="top">

**为 Go 打造**
- 支持 net/http、Chi 等
- Inertia UI 或无头 JSON API
- PostgreSQL 和 SQLite
- 内嵌 SQL 迁移

</td>
</tr>
</table>

<p align="center"><sub><a href="../features.md">查看全部功能详情</a></sub></p>

<p align="center">
  <a href="https://go.dev"><img src="https://img.shields.io/badge/Go_1.26+-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go 1.26+"></a>
  <a href="https://www.postgresql.org"><img src="https://img.shields.io/badge/PostgreSQL-4169E1?style=for-the-badge&logo=postgresql&logoColor=white" alt="PostgreSQL"></a>
  <a href="https://www.sqlite.org"><img src="https://img.shields.io/badge/SQLite-003B57?style=for-the-badge&logo=sqlite&logoColor=white" alt="SQLite"></a>
  <a href="https://inertiajs.com"><img src="https://img.shields.io/badge/Inertia-9553E9?style=for-the-badge&logo=inertia&logoColor=white" alt="Inertia"></a>
  <a href="https://vuejs.org"><img src="https://img.shields.io/badge/Vue_3-4FC08D?style=for-the-badge&logo=vuedotjs&logoColor=white" alt="Vue 3"></a>
</p>

## 快速开始

用 `go get github.com/escalated-dev/escalated-go` 安装，然后：

```go
migrations.Migrate(db, "escalated_")
cfg := escalated.DefaultConfig()
cfg.DB = db
esc, err := escalated.New(cfg)
router.MountChi(r, esc)
```

需要 Go 1.26.9 或更高版本。先把 `AdminCheck`、`AgentCheck` 和 `UserIDFunc` 设置为你自己的会话检查；在你设置之前，它们会拒绝访问。[入门指南](../getting-started.md)里有 Chi 和标准库的完整示例。

## 了解更多

- [全部功能](../features.md) · [入门](../getting-started.md) · [配置](../configuration.md) · [API 路由](../api-routes.md)
- [自定义操作、存储、状态和翻译](../customization.md) · [UI 页面](../ui-pages.md)
- [访问控制](../access-control.md) · [API 令牌](../api-tokens.md) · [工单访问和路由](../ticket-access-and-routing.md)
- [通知邮件](../notification-emails.md) · [嵌入小组件](../embedding-the-widget.md) · [访客访问](../guest-access.md)
- [客服面板](../agent-panel.md) · [嵌入管理后台](../embedding-admin.md)
- [插件](../plugins.md) · [邮件简报](../newsletters.md)

Escalated 也提供 [Laravel](https://github.com/escalated-dev/escalated-laravel)、[Rails](https://github.com/escalated-dev/escalated-rails)、[Django](https://github.com/escalated-dev/escalated-django)、[NestJS](https://github.com/escalated-dev/escalated-nestjs)、[AdonisJS](https://github.com/escalated-dev/escalated-adonis)、[Symfony](https://github.com/escalated-dev/escalated-symfony)、[Phoenix](https://github.com/escalated-dev/escalated-phoenix)、[.NET](https://github.com/escalated-dev/escalated-dotnet)、[Spring](https://github.com/escalated-dev/escalated-spring)、[WordPress](https://github.com/escalated-dev/escalated-wordpress) 和 [Filament](https://github.com/escalated-dev/escalated-filament) 版本。UI 位于 [escalated](https://github.com/escalated-dev/escalated)。

## 许可证

MIT。Copyright (c) Escalated.dev。参见 [LICENSE](../../LICENSE)。
