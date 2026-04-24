# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Attachment model, store, handler, and download endpoint (#20)
- Parity with Laravel reference across tickets, workflows, chat, KB, reports (#19)

### Fixed
- Include computed ticket fields in serialization (#21)
- Include chat, context panel, and activity fields for ticket detail serialization (#22)
- Include missing workflow and workflow log computed fields in serialization (#23)

### Internal
- One-command Docker dev/demo environment under `docker/` with click-to-login agent picker (#24)
- `gofmt` cleanup across ticket model and handler files (style commits)

## Initial release

Go port of `escalated` reaching feature parity with the Laravel reference: tickets, workflow engine, chat, KB, reports, SLA tracking, and Inertia-driven Vue frontend served through the shared `@escalated-dev/escalated` package. Versioning follows Go module semantics — consumers pin via `go.mod` against commit SHAs until the first tagged release.
