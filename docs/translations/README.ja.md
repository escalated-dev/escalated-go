<p align="center"><sub>
  <a href="README.ar.md">العربية</a> ·
  <a href="README.de.md">Deutsch</a> ·
  <a href="../../README.md">English</a> ·
  <a href="README.es.md">Español</a> ·
  <a href="README.fr.md">Français</a> ·
  <a href="README.it.md">Italiano</a> ·
  <b>日本語</b> ·
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
  Go アプリに本格的なヘルプデスクを追加できます。<br>
  チケット、SLA、エスカレーションルール、エージェント用受信箱、カスタマーポータルが、あなた自身のデータベース上で動きます。
</p>

<p align="center">
  <a href="https://escalated.dev"><b>ウェブサイト</b></a> ·
  <a href="https://escalated.dev/docs"><b>ドキュメント</b></a> ·
  <a href="https://escalated.dev/cloud"><b>Cloud</b></a> ·
  <a href="../../CHANGELOG.md"><b>変更履歴</b></a>
</p>

<br>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/inbox-dark.png">
  <img src="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/inbox-light.png" alt="Escalated のエージェント受信箱: 件数付きの保存ビュー、SLA のカウントダウン、各チケットの優先度" width="100%">
</picture>

## できること

<table>
  <tr>
    <td width="40%">
      <h3>エージェントが一日中作業できる受信箱</h3>
      会話、社内メモ、定型返信、SLA タイマーをひとつの画面に。チケットは Mailgun、Postmark、SES 経由のメールでも届きます。
    </td>
    <td width="60%">
      <picture>
        <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/ticket-dark.png">
        <img src="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/ticket-light.png" alt="返信エディタ付きでエージェントのワークスペースに開いたチケット">
      </picture>
    </td>
  </tr>
  <tr>
    <td width="60%">
      <picture>
        <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/dashboard-dark.png">
        <img src="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/dashboard-light.png" alt="応答時間、CSAT、SLA 違反を表示する管理画面のレポートページ">
      </picture>
    </td>
    <td width="40%">
      <h3>見える SLA とエスカレーションルール</h3>
      営業時間を考慮した優先度ごとの応答・解決目標、目標を外れたときにエスカレーションや再割り当てを行うルール、そして違反レポート。
    </td>
  </tr>
</table>

## 機能

<table>
<tr>
<td width="33%" valign="top">

**チケット**
- ステータス、優先度、チケット種別
- 公開返信、社内メモ、システムメッセージ
- 返信を新しいチケットに分割
- 自動で再開するスヌーズ
- あなたのエンティティに紐づくチケット

</td>
<td width="33%" valign="top">

**エージェントワークスペースと SLA**
- エージェントダッシュボードと保存ビュー
- 部署とタグ
- Server-Sent Events によるリアルタイム更新
- 優先度ごとの応答・解決目標
- すべてのチケットで SLA を追跡

</td>
<td width="33%" valign="top">

**顧客**
- UI を有効にしたときのカスタマーポータル
- メールで届くコードによる認証済みゲストアクセス
- ナレッジベース検索とチャット付きの埋め込みウィジェット
- ナレッジベース(オン・オフ切り替え可)

</td>
</tr>
<tr>
<td width="33%" valign="top">

**メール**
- Message-ID でスレッド化される通知
- プロバイダの Webhook からの受信返信
- メールクライアントで正しく表示されるスレッド
- ブランドに合わせたテンプレート

</td>
<td width="33%" valign="top">

**管理とアクセス**
- 管理画面での設定
- ロール、権限、二要素認証、SSO
- ネイティブ Go プラグインと SDK プラグイン

</td>
<td width="33%" valign="top">

**Go のために設計**
- net/http、Chi などで動作
- Inertia UI またはヘッドレス JSON API
- PostgreSQL と SQLite
- 埋め込み SQL マイグレーション

</td>
</tr>
</table>

<p align="center"><sub><a href="../features.md">すべての機能の詳細</a></sub></p>

<p align="center">
  <a href="https://go.dev"><img src="https://img.shields.io/badge/Go_1.26+-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go 1.26+"></a>
  <a href="https://www.postgresql.org"><img src="https://img.shields.io/badge/PostgreSQL-4169E1?style=for-the-badge&logo=postgresql&logoColor=white" alt="PostgreSQL"></a>
  <a href="https://www.sqlite.org"><img src="https://img.shields.io/badge/SQLite-003B57?style=for-the-badge&logo=sqlite&logoColor=white" alt="SQLite"></a>
  <a href="https://inertiajs.com"><img src="https://img.shields.io/badge/Inertia-9553E9?style=for-the-badge&logo=inertia&logoColor=white" alt="Inertia"></a>
  <a href="https://vuejs.org"><img src="https://img.shields.io/badge/Vue_3-4FC08D?style=for-the-badge&logo=vuedotjs&logoColor=white" alt="Vue 3"></a>
</p>

## クイックスタート

`go get github.com/escalated-dev/escalated-go` でインストールしてから、次のようにします。

```go
migrations.Migrate(db, "escalated_")
cfg := escalated.DefaultConfig()
cfg.DB = db
esc, err := escalated.New(cfg)
router.MountChi(r, esc)
```

Go 1.26.9 以降が必要です。まず `AdminCheck`、`AgentCheck`、`UserIDFunc` に自分のセッションチェックを設定してください。設定するまでは、これらはアクセスを拒否します。[入門ガイド](../getting-started.md)に Chi と標準ライブラリの完全な例があります。

## さらに詳しく

- [すべての機能](../features.md) · [はじめに](../getting-started.md) · [設定](../configuration.md) · [API ルート](../api-routes.md)
- [カスタムアクション、ストア、ステータス、翻訳](../customization.md) · [UI ページ](../ui-pages.md)
- [アクセス制御](../access-control.md) · [API トークン](../api-tokens.md) · [チケットのアクセスとルーティング](../ticket-access-and-routing.md)
- [通知メール](../notification-emails.md) · [ウィジェットの埋め込み](../embedding-the-widget.md) · [ゲストアクセス](../guest-access.md)
- [エージェントパネル](../agent-panel.md) · [管理画面の埋め込み](../embedding-admin.md)
- [プラグイン](../plugins.md) · [ニュースレター](../newsletters.md)

Escalated は [Laravel](https://github.com/escalated-dev/escalated-laravel)、[Rails](https://github.com/escalated-dev/escalated-rails)、[Django](https://github.com/escalated-dev/escalated-django)、[NestJS](https://github.com/escalated-dev/escalated-nestjs)、[AdonisJS](https://github.com/escalated-dev/escalated-adonis)、[Symfony](https://github.com/escalated-dev/escalated-symfony)、[Phoenix](https://github.com/escalated-dev/escalated-phoenix)、[.NET](https://github.com/escalated-dev/escalated-dotnet)、[Spring](https://github.com/escalated-dev/escalated-spring)、[WordPress](https://github.com/escalated-dev/escalated-wordpress)、[Filament](https://github.com/escalated-dev/escalated-filament) 向けにも提供されています。UI は [escalated](https://github.com/escalated-dev/escalated) にあります。

## ライセンス

MIT. Copyright (c) Escalated.dev. 詳しくは [LICENSE](../../LICENSE) を参照してください。
