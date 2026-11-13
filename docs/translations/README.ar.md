<p align="center"><sub>
  <b>العربية</b> ·
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
  <a href="README.zh-CN.md">简体中文</a>
</sub></p>

<p align="center">
  <a href="https://escalated.dev"><img src="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/logo.png" width="72" height="72" alt="Escalated"></a>
</p>

<h1 align="center">Escalated for Go</h1>

<p align="center">
  أضف نظام دعم فني متكاملًا إلى تطبيق Go الخاص بك.<br>
  تذاكر، واتفاقيات مستوى الخدمة (SLA)، وقواعد تصعيد، وصندوق وارد للوكلاء، وبوابة للعملاء، تعمل كلها على قاعدة بياناتك الخاصة.
</p>

<p align="center">
  <a href="https://escalated.dev"><b>الموقع</b></a> ·
  <a href="https://escalated.dev/docs"><b>التوثيق</b></a> ·
  <a href="https://escalated.dev/cloud"><b>Cloud</b></a> ·
  <a href="../../CHANGELOG.md"><b>سجل التغييرات</b></a>
</p>

<br>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/inbox-dark.png">
  <img src="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/inbox-light.png" alt="صندوق وارد الوكلاء في Escalated: طرق عرض محفوظة مع أعداد، وعدّادات تنازلية لـ SLA، وأولويات على كل تذكرة" width="100%">
</picture>

## ماذا يفعل

<table>
  <tr>
    <td width="40%">
      <h3>صندوق وارد يمكن لوكلائك العمل فيه طوال اليوم</h3>
      المحادثة والملاحظات الداخلية والردود الجاهزة ومؤقتات SLA في شاشة واحدة. تصل التذاكر أيضًا بالبريد الإلكتروني عبر Mailgun أو Postmark أو SES.
    </td>
    <td width="60%">
      <picture>
        <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/ticket-dark.png">
        <img src="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/ticket-light.png" alt="تذكرة مفتوحة في مساحة عمل الوكيل مع محرر الرد">
      </picture>
    </td>
  </tr>
  <tr>
    <td width="60%">
      <picture>
        <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/dashboard-dark.png">
        <img src="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/dashboard-light.png" alt="صفحة التقارير في لوحة الإدارة مع أوقات الاستجابة وCSAT وخروقات SLA">
      </picture>
    </td>
    <td width="40%">
      <h3>اتفاقيات SLA وقواعد تصعيد تراها بوضوح</h3>
      أهداف للاستجابة والحل حسب الأولوية مع ساعات العمل، وقواعد تصعّد التذكرة أو تعيد إسنادها عندما يفوت الهدف، وتقارير عن الخروقات.
    </td>
  </tr>
</table>

## الميزات

<table>
<tr>
<td width="33%" valign="top">

**التذاكر**
- الحالات والأولويات وأنواع التذاكر
- ردود عامة وملاحظات داخلية ورسائل نظام
- فصل رد إلى تذكرة جديدة
- تأجيل مع استئناف تلقائي
- ربط التذاكر بالكيانات الخاصة بك

</td>
<td width="33%" valign="top">

**مساحة عمل الوكيل وSLA**
- لوحة الوكيل وطرق العرض المحفوظة
- الأقسام والوسوم
- تحديثات مباشرة عبر Server-Sent Events
- أهداف الاستجابة والحل حسب الأولوية
- تتبع SLA على كل تذكرة

</td>
<td width="33%" valign="top">

**العملاء**
- بوابة العملاء عند تفعيل الواجهة
- وصول ضيف موثّق برمز يُرسل بالبريد
- أداة قابلة للتضمين مع بحث في قاعدة المعرفة ودردشة
- قاعدة معرفة يمكن تشغيلها أو إيقافها

</td>
</tr>
<tr>
<td width="33%" valign="top">

**البريد الإلكتروني**
- إشعارات مرتبطة في سلاسل عبر Message-ID
- ردود واردة من webhooks مزود الخدمة
- سلاسل صحيحة في برامج البريد
- قوالب تحمل علامتك التجارية

</td>
<td width="33%" valign="top">

**الإدارة والوصول**
- إعدادات من لوحة الإدارة
- الأدوار والصلاحيات والتحقق بخطوتين وSSO
- إضافات Go أصلية وإضافات SDK

</td>
<td width="33%" valign="top">

**مصمم لـ Go**
- يعمل مع net/http وChi وغيرها
- واجهة Inertia أو JSON API بدون واجهة
- PostgreSQL وSQLite
- ترحيلات SQL مضمّنة

</td>
</tr>
</table>

<p align="center"><sub><a href="../features.md">كل الميزات بالتفصيل</a></sub></p>

<p align="center">
  <a href="https://go.dev"><img src="https://img.shields.io/badge/Go_1.26+-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go 1.26+"></a>
  <a href="https://www.postgresql.org"><img src="https://img.shields.io/badge/PostgreSQL-4169E1?style=for-the-badge&logo=postgresql&logoColor=white" alt="PostgreSQL"></a>
  <a href="https://www.sqlite.org"><img src="https://img.shields.io/badge/SQLite-003B57?style=for-the-badge&logo=sqlite&logoColor=white" alt="SQLite"></a>
  <a href="https://inertiajs.com"><img src="https://img.shields.io/badge/Inertia-9553E9?style=for-the-badge&logo=inertia&logoColor=white" alt="Inertia"></a>
  <a href="https://vuejs.org"><img src="https://img.shields.io/badge/Vue_3-4FC08D?style=for-the-badge&logo=vuedotjs&logoColor=white" alt="Vue 3"></a>
</p>

## البدء السريع

ثبّته باستخدام `go get github.com/escalated-dev/escalated-go`، ثم:

```go
migrations.Migrate(db, "escalated_")
cfg := escalated.DefaultConfig()
cfg.DB = db
esc, err := escalated.New(cfg)
router.MountChi(r, esc)
```

يتطلب Go 1.26.9 أو أحدث. اضبط أولًا `AdminCheck` و`AgentCheck` و`UserIDFunc` على فحوصات الجلسة الخاصة بك؛ وإلى أن تفعل ذلك، فإنها ترفض الوصول. يحتوي [دليل البدء](../getting-started.md) على الأمثلة الكاملة لـ Chi والمكتبة القياسية.

## اعرف المزيد

- [كل الميزات](../features.md) · [البدء](../getting-started.md) · [الإعدادات](../configuration.md) · [مسارات API](../api-routes.md)
- [إجراءات ومخازن وحالات وترجمات مخصصة](../customization.md) · [صفحات الواجهة](../ui-pages.md)
- [التحكم في الوصول](../access-control.md) · [رموز API](../api-tokens.md) · [الوصول إلى التذاكر وتوجيهها](../ticket-access-and-routing.md)
- [رسائل الإشعارات](../notification-emails.md) · [تضمين الأداة](../embedding-the-widget.md) · [وصول الضيف](../guest-access.md)
- [لوحة الوكيل](../agent-panel.md) · [تضمين لوحة الإدارة](../embedding-admin.md)
- [الإضافات](../plugins.md) · [النشرات البريدية](../newsletters.md)

يتوفر Escalated أيضًا لـ [Laravel](https://github.com/escalated-dev/escalated-laravel)، [Rails](https://github.com/escalated-dev/escalated-rails)، [Django](https://github.com/escalated-dev/escalated-django)، [NestJS](https://github.com/escalated-dev/escalated-nestjs)، [AdonisJS](https://github.com/escalated-dev/escalated-adonis)، [Symfony](https://github.com/escalated-dev/escalated-symfony)، [Phoenix](https://github.com/escalated-dev/escalated-phoenix)، [.NET](https://github.com/escalated-dev/escalated-dotnet)، [Spring](https://github.com/escalated-dev/escalated-spring)، [WordPress](https://github.com/escalated-dev/escalated-wordpress) و[Filament](https://github.com/escalated-dev/escalated-filament). توجد الواجهة في [escalated](https://github.com/escalated-dev/escalated).

## الترخيص

MIT. Copyright (c) Escalated.dev. راجع [LICENSE](../../LICENSE).
