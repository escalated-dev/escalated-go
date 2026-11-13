<p align="center"><sub>
  <a href="README.ar.md">العربية</a> ·
  <a href="README.de.md">Deutsch</a> ·
  <a href="../../README.md">English</a> ·
  <a href="README.es.md">Español</a> ·
  <a href="README.fr.md">Français</a> ·
  <a href="README.it.md">Italiano</a> ·
  <a href="README.ja.md">日本語</a> ·
  <b>한국어</b> ·
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
  Go 앱에 완전한 헬프데스크를 추가하세요.<br>
  티켓, SLA, 에스컬레이션 규칙, 상담원 받은편지함, 고객 포털이 여러분의 데이터베이스에서 그대로 동작합니다.
</p>

<p align="center">
  <a href="https://escalated.dev"><b>웹사이트</b></a> ·
  <a href="https://escalated.dev/docs"><b>문서</b></a> ·
  <a href="https://escalated.dev/cloud"><b>Cloud</b></a> ·
  <a href="../../CHANGELOG.md"><b>변경 로그</b></a>
</p>

<br>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/inbox-dark.png">
  <img src="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/inbox-light.png" alt="Escalated 상담원 받은편지함: 개수가 표시된 저장된 보기, SLA 카운트다운, 모든 티켓의 우선순위" width="100%">
</picture>

## 하는 일

<table>
  <tr>
    <td width="40%">
      <h3>상담원이 하루 종일 일할 수 있는 받은편지함</h3>
      대화, 내부 메모, 저장된 답변, SLA 타이머를 한 화면에서 다룹니다. 티켓은 Mailgun, Postmark, SES를 통해 이메일로도 들어옵니다.
    </td>
    <td width="60%">
      <picture>
        <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/ticket-dark.png">
        <img src="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/ticket-light.png" alt="답장 작성기와 함께 상담원 작업 공간에 열린 티켓">
      </picture>
    </td>
  </tr>
  <tr>
    <td width="60%">
      <picture>
        <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/dashboard-dark.png">
        <img src="https://raw.githubusercontent.com/escalated-dev/escalated/main/docs/assets/readme/dashboard-light.png" alt="응답 시간, CSAT, SLA 위반을 보여 주는 관리자 보고서 페이지">
      </picture>
    </td>
    <td width="40%">
      <h3>눈으로 확인할 수 있는 SLA와 에스컬레이션 규칙</h3>
      업무 시간을 반영한 우선순위별 응답 및 해결 목표, 목표를 놓치면 에스컬레이션하거나 재배정하는 규칙, 위반 보고서를 제공합니다.
    </td>
  </tr>
</table>

## 기능

<table>
<tr>
<td width="33%" valign="top">

**티켓**
- 상태, 우선순위, 티켓 유형
- 공개 답장, 내부 메모, 시스템 메시지
- 답장을 새 티켓으로 분리
- 자동으로 다시 열리는 일시 중지
- 여러분의 엔터티에 연결된 티켓

</td>
<td width="33%" valign="top">

**상담원 작업 공간과 SLA**
- 상담원 대시보드와 저장된 보기
- 부서와 태그
- Server-Sent Events를 통한 실시간 업데이트
- 우선순위별 응답 및 해결 목표
- 모든 티켓의 SLA 추적

</td>
<td width="33%" valign="top">

**고객**
- UI를 켰을 때 제공되는 고객 포털
- 이메일 코드로 인증하는 게스트 접근
- 지식 베이스 검색과 채팅이 있는 임베드 위젯
- 지식 베이스, 켜기 또는 끄기

</td>
</tr>
<tr>
<td width="33%" valign="top">

**이메일**
- Message-ID로 스레드를 묶는 알림
- 제공업체 웹훅을 통한 수신 답장
- 메일 클라이언트에서 올바른 스레드
- 브랜드에 맞춘 템플릿

</td>
<td width="33%" valign="top">

**관리와 접근**
- 관리자 설정
- 역할, 권한, 2단계 인증, SSO
- 네이티브 Go 플러그인과 SDK 플러그인

</td>
<td width="33%" valign="top">

**Go를 위해 만듦**
- net/http, Chi 등과 함께 동작
- Inertia UI 또는 헤드리스 JSON API
- PostgreSQL과 SQLite
- 내장 SQL 마이그레이션

</td>
</tr>
</table>

<p align="center"><sub><a href="../features.md">모든 기능 자세히 보기</a></sub></p>

<p align="center">
  <a href="https://go.dev"><img src="https://img.shields.io/badge/Go_1.26+-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go 1.26+"></a>
  <a href="https://www.postgresql.org"><img src="https://img.shields.io/badge/PostgreSQL-4169E1?style=for-the-badge&logo=postgresql&logoColor=white" alt="PostgreSQL"></a>
  <a href="https://www.sqlite.org"><img src="https://img.shields.io/badge/SQLite-003B57?style=for-the-badge&logo=sqlite&logoColor=white" alt="SQLite"></a>
  <a href="https://inertiajs.com"><img src="https://img.shields.io/badge/Inertia-9553E9?style=for-the-badge&logo=inertia&logoColor=white" alt="Inertia"></a>
  <a href="https://vuejs.org"><img src="https://img.shields.io/badge/Vue_3-4FC08D?style=for-the-badge&logo=vuedotjs&logoColor=white" alt="Vue 3"></a>
</p>

## 빠른 시작

`go get github.com/escalated-dev/escalated-go`로 설치한 다음:

```go
migrations.Migrate(db, "escalated_")
cfg := escalated.DefaultConfig()
cfg.DB = db
esc, err := escalated.New(cfg)
router.MountChi(r, esc)
```

Go 1.26.9 이상이 필요합니다. 먼저 `AdminCheck`, `AgentCheck`, `UserIDFunc`를 여러분의 세션 확인 로직으로 설정하세요. 설정하기 전까지는 접근을 거부합니다. [시작 가이드](../getting-started.md)에 Chi와 표준 라이브러리용 전체 예제가 있습니다.

## 더 알아보기

- [모든 기능](../features.md) · [시작하기](../getting-started.md) · [설정](../configuration.md) · [API 라우트](../api-routes.md)
- [사용자 정의 액션, 스토어, 상태, 번역](../customization.md) · [UI 페이지](../ui-pages.md)
- [접근 제어](../access-control.md) · [API 토큰](../api-tokens.md) · [티켓 접근과 라우팅](../ticket-access-and-routing.md)
- [알림 이메일](../notification-emails.md) · [위젯 임베드](../embedding-the-widget.md) · [게스트 접근](../guest-access.md)
- [상담원 패널](../agent-panel.md) · [관리자 임베드](../embedding-admin.md)
- [플러그인](../plugins.md) · [뉴스레터](../newsletters.md)

Escalated는 [Laravel](https://github.com/escalated-dev/escalated-laravel), [Rails](https://github.com/escalated-dev/escalated-rails), [Django](https://github.com/escalated-dev/escalated-django), [NestJS](https://github.com/escalated-dev/escalated-nestjs), [AdonisJS](https://github.com/escalated-dev/escalated-adonis), [Symfony](https://github.com/escalated-dev/escalated-symfony), [Phoenix](https://github.com/escalated-dev/escalated-phoenix), [.NET](https://github.com/escalated-dev/escalated-dotnet), [Spring](https://github.com/escalated-dev/escalated-spring), [WordPress](https://github.com/escalated-dev/escalated-wordpress) 및 [Filament](https://github.com/escalated-dev/escalated-filament)용으로도 제공됩니다. UI는 [escalated](https://github.com/escalated-dev/escalated)에 있습니다.

## 라이선스

MIT. Copyright (c) Escalated.dev. [LICENSE](../../LICENSE)를 참고하세요.
