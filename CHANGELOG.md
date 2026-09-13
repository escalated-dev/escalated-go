# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed
- **A new ticket could fail because its reference was already taken.** The
  reference's random part was 3 bytes, 24 bits for each month, and the
  `reference` column is unique, so a busy site lost a ticket whenever a draw
  matched a stored one. The random part is now 8 characters of Crockford base32
  (40 bits), e.g. `ESC-2609-7KQ2M9XH`. When an insert still hits a taken
  reference, `CreateTicket` inserts again under a fresh one, up to three
  attempts, on SQLite and PostgreSQL. A reference the caller sets is never
  replaced, other unique violations fail at once, and older six-character
  references keep resolving.

### Security
- **A live-chat guest token could be guessed.** `ChatSessionService.StartSession`
  issued the token with `GenerateReference("GT")`: `GT-`, the year and month,
  and 3 random bytes, so 24 random bits. On a host that serves live chat, the
  token is the visitor's only credential: the public
  `/api/guest/tickets/{token}` route opens the ticket with it alone, and the
  widget's chat and lookup handlers accept it with the ticket reference. All of
  a month's chat tokens could be enumerated. New chats get their token
  from `GenerateGuestToken`, 32 bytes from `crypto/rand` (256 bits), as guest
  tickets already did. The widget now compares tokens in constant time. Tokens
  already stored keep working, and are no stronger than before.

## [0.1.2] - 2026-09-13

### Security
- **The JSON API, customer ticket pages and attachment downloads had no access
  check.** Only `/agent` and `/admin` were guarded, on both routers. Anyone could
  list every ticket through `/api/tickets`, read internal notes, change status
  and priority, open or reply to another customer's ticket, and download any
  attachment by id. Now:
  - The API's ticket, department and tag routes require `AgentCheck` or
    `AdminCheck`. The auth, guest and knowledge-base routes stay public.
  - Customer routes require a signed-in user (`UserIDFunc` returning an id). Show
    and Reply are limited to the ticket's requester.
  - Attachments download for agents and admins, or for the requester of the
    owning ticket, and never from an internal note.

  **Hosts must wire `AgentCheck`/`AdminCheck` for API clients and `UserIDFunc`
  for customer pages.** Requests without them now get 401 or 403 (#113).
- **Webhook URLs could point at internal addresses.** Creating or updating a
  webhook now refuses loopback, private, link-local, carrier-grade NAT and
  reserved destinations (422). The dispatcher's default client checks the
  address it actually connects to, doesn't follow redirects or use a proxy, and
  doesn't retry a refused destination. A host that sets its own
  `WebhookDispatcher.Client` owns that policy (#114).

### Fixed
- **Tagging and following failed on PostgreSQL.** Workflow, automation and macro
  `add_tag`, and `AddFollower`, used SQLite's `INSERT OR IGNORE`, which
  PostgreSQL rejects as a syntax error. They now use `ON CONFLICT DO NOTHING`
  (#111).
- **Escalation rules couldn't be created or toggled on PostgreSQL.**
  `escalation_rules.is_active` shipped as `INTEGER` while the handlers bind a
  bool. A PostgreSQL upgrade step converts the column to `BOOLEAN DEFAULT TRUE`,
  keeping stored values, and is a no-op once applied (#112).

## [0.1.1] - 2026-09-13

### Fixed
- **Automation and macro actions that write a reply always failed.** The
  `add_note` automation action, and the `add_reply`, `add_note` and
  `insert_canned_reply` macro actions, inserted into `is_internal_note` (and the
  automation into `metadata` as well). `escalated_replies` has `is_internal` and
  `is_system` and never had either column, so every one of them failed on
  insert, on SQLite and PostgreSQL alike. Automation notes are now written
  internal and system-authored, the way workflow notes already were; macro
  notes and replies keep the agent who applied the macro as their author.

## [0.1.0] - 2026-09-12

### Fixed
- **The PostgreSQL migrations could not create the schema.** `escalated_replies`
  and `escalated_ticket_activities` passed their `Sprintf` arguments in the
  wrong order, so the statements asked PostgreSQL for `REFERENCES BIGINT(id)`
  and a column typed `escalated_tickets`. Both are refused outright, which means
  `migrations.Migrate` has never completed against a real PostgreSQL server.
  Every test in the package opened SQLite, so nothing had ever said so.

- **Everything outside the store only worked on SQLite.** Handlers, services, the
  newsletter store and the workflow runners build SQL inline with `?`
  placeholders, which PostgreSQL rejects as a syntax error. 196 statements now
  go through `sqldialect.Rebind`, which rewrites them as `$1, $2, …` for a
  PostgreSQL driver and leaves them alone for everything else — including `?`
  inside a string literal, where it is data rather than a placeholder.

- **Inserts could not read back the new row's id.** PostgreSQL's driver does not
  implement `LastInsertId`; the value comes back from a `RETURNING` clause.
  `sqldialect.ExecInsert` adds one and returns a result of the same shape, so
  every call site reads the id exactly as before.

- **Every JSON column failed to read on PostgreSQL.** `json.RawMessage` is a
  `[]byte`, and `database/sql` will only scan a `[]byte` into it. SQLite's driver
  hands back `[]byte`; lib/pq hands back a `string`, and the read failed with
  "unsupported Scan". The 23 JSON columns are now `models.JSONText`, which scans
  from either and marshals identically.

- **Timestamps came back shifted by the server's time zone.** The PostgreSQL
  migrations declared `TIMESTAMP`, which has no time zone, so the driver wrote
  the local wall clock and read it back labelled UTC. A newsletter retry
  scheduled a minute ahead came back hours in the past. They are `TIMESTAMPTZ`
  now, which is what every one of those columns means; SQLite keeps `TIMESTAMP`,
  having no time zones to get wrong.

### Added
- **The test suite can run against PostgreSQL.** `internal/testdb` opens the
  database the suite was told to use — SQLite by default, so running it locally
  still needs nothing installed. PostgreSQL gets a schema per fixture, pinned
  through `search_path`, so tests stay isolated on a shared server without a
  database each.

  An unrecognised `ESCALATED_TEST_DRIVER` fails the test rather than falling
  back: a CI leg that quietly ran SQLite would report green having tested
  nothing the matrix exists for.

  `migrations/postgres_test.go` asserts the schema PostgreSQL actually ends up
  with — the tables, the foreign keys that the swapped arguments turned into
  references to a type, and the column types they mistyped. The whole suite now
  runs on both: **PostgreSQL went from 215 failures to none.**
- **`escalated.New` detects the database it was given.** It picks the PostgreSQL
  or SQLite store from the connection in `Config.DB`, so a host that opened a
  SQLite connection no longer has to know `NewSQLite` exists. `DetectDialect` is
  exported for hosts that want the answer for their own code.

  Detection reads the driver's import path first — lib/pq, pgx, modernc.org/sqlite,
  mattn/go-sqlite3 and the rest are recognised without touching the database — and
  falls back to a single statement for a driver it does not recognise, which is
  what a tracing or proxy wrapper looks like.

### Fixed
- **`New` assumed PostgreSQL.** Handing it a SQLite connection was not an error
  anyone saw at startup; the wrong SQL reached the database on the first query
  that happened to differ. `Config.DatabaseDialect` now defaults to empty rather
  than `"postgres"`, and a dialect Escalated has no store for is named in an
  error instead of being silently treated as PostgreSQL.
- **Configurable database connection, documented and guarded.** `Config.DB` has
  always been a `*sql.DB` the host opens and hands over, so Escalated's tables go
  wherever that connection points — including a database the host application
  otherwise never touches. That was never written down, and nothing stopped it
  from quietly rotting.

  The README now covers it, and a test fails the build if any statement in the
  package references a table the host owns. On a separate database such a query
  does not error; it returns no rows, which reads as users who do not exist.

  Host user data continues to arrive through the `UserDirectory` and
  `SkillAgentDirectory` interfaces, which run on the host's own connection.
  `escalated_tickets.requester_id` is a plain unconstrained column with no
  foreign key, so no query joins the two — no database can join across two
  connections.
- Admin users-management page (`GET/PATCH /admin/users`) backed by a host-supplied `handlers.UserDirectory` hook on `Config.UserDirectory`, mirroring escalated-laravel#94 (admin/agent role toggles, self-demote guard, name+email search)
- Central translation loader at `internal/i18n` consuming `github.com/escalated-dev/escalated-locale/packages/go`, with deep-merge override support at `internal/i18n/overrides/{locale}.json` and a `T(key, locale, params)` helper
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
