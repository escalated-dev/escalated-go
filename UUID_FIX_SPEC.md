# Task: support UUID/string host-app user keys (escalated-go)

The Go package assumes the host app's user id is `int64`/`BIGINT`. Hosts whose
user primary key is a UUID/string break: SQL migrations create `BIGINT` columns,
structs use `*int64`, handlers bind JSON to `int64`, and query/workflow code
uses `strconv.ParseInt`/`Itoa`. Make the package work with integer **and**
UUID/string host user keys while keeping integer hosts behaving exactly as
before.

## Step 1 — UserID type (the core of the fix)

Create `models/user_id.go`:

```go
package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strconv"
)

// UserID is a host-application user identifier. Host user primary keys may be
// integers or UUID/strings, so UserID stores the value as a string internally
// and accepts either form from JSON and the database. Integer-keyed hosts are
// unaffected: numeric ids round-trip as JSON numbers and store fine in BIGINT.
type UserID string

func (u *UserID) Scan(value any) error {
	switch v := value.(type) {
	case nil:
		*u = ""
	case int64:
		*u = UserID(strconv.FormatInt(v, 10))
	case string:
		*u = UserID(v)
	case []byte:
		*u = UserID(string(v))
	default:
		return fmt.Errorf("unsupported UserID scan type %T", value)
	}
	return nil
}

func (u UserID) Value() (driver.Value, error) {
	if u == "" {
		return nil, nil
	}
	return string(u), nil
}

// MarshalJSON emits a JSON number when the id is purely numeric (back-compat for
// integer-keyed hosts) and a JSON string otherwise (UUID/string ids).
func (u UserID) MarshalJSON() ([]byte, error) {
	if u == "" {
		return []byte("null"), nil
	}
	if _, err := strconv.ParseInt(string(u), 10, 64); err == nil {
		return []byte(string(u)), nil
	}
	return json.Marshal(string(u))
}

func (u *UserID) UnmarshalJSON(b []byte) error {
	s := string(b)
	if s == "null" {
		*u = ""
		return nil
	}
	if len(b) > 0 && b[0] == '"' {
		var str string
		if err := json.Unmarshal(b, &str); err != nil {
			return err
		}
		*u = UserID(str)
		return nil
	}
	*u = UserID(s) // bare number literal
	return nil
}

// Empty reports whether the id is unset.
func (u UserID) Empty() bool { return u == "" }
```

## Step 2 — Migration column type

In `migrations/migrations.go`, make the host-user-id columns use a type that
holds both. Add a helper near the top:

```go
// userIDColumnType returns the SQL column type for a host user id.
// Default BIGINT (existing behavior). Set ESCALATED_USER_KEY_TYPE=uuid|string
// (or varchar) to use VARCHAR(255) for UUID/string-keyed hosts.
func userIDColumnType() string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("ESCALATED_USER_KEY_TYPE"))) {
	case "uuid", "string", "varchar":
		return "VARCHAR(255)"
	default:
		return "BIGINT"
	}
}
```

Replace the hardcoded `BIGINT` in these host-user-id columns with
`" + userIDColumnType() + "` (string-concatenate into the DDL), keeping the
existing SQLite `BIGINT`→`INTEGER` adaptation working (a VARCHAR stays VARCHAR
under SQLite, which is fine). Columns to change (from audit; verify):
- tickets: `requester_id`, `assigned_to`, and `snoozed_by` if present
- replies: `author_id`
- ticket_activities: `causer_id`
- audit_logs: `performer_id`
- two_factors: `user_id`
- contacts: `user_id`
- agent_skills: `user_id`

Leave polymorphic `_type` columns and Escalated's own `BIGINT` PKs/internal FKs
unchanged. (If the SQLite-adaptation `strings.Replace(... "BIGINT", "INTEGER")`
would wrongly touch these, scope it so VARCHAR survives — VARCHAR has no BIGINT
substring, so it's safe.)

## Step 3 — Struct fields

Change host-user-id struct fields from `*int64`/`int64` to `*models.UserID` /
`models.UserID` (pointer where currently a pointer) in:
- `models/ticket.go` — `RequesterID`, `AssignedTo`, `SnoozedBy` (leave `ContactID` as `*int64`, it's an internal id)
- `models/reply.go` — `AuthorID`
- `models/activity.go` — `CauserID`
- `models/contact.go` — `UserID`
- `models/macro.go` — `CreatedBy`
- `models/skill.go` — `UserID` (AgentSkill + SkillFormAgentRow) — NOTE: if this is actually an internal agent-profile id, leave it; confirm against how it's used (host user id → change). Per audit it's the host agent user id, so change it.
- any audit-log struct `PerformerID`

Update any code that constructs/compares these fields accordingly (e.g.
`&uid` where `uid` was int64 — make `uid` a `UserID`).

## Step 4 — Handlers / services coercion

- `handlers/agent.go` (~225) — `AgentID int64` in the assign request struct → `models.UserID`; pass through to `Reassign`.
- `handlers/admin.go` (~272) — `GuestPolicyUserID *int64` → `*models.UserID`; the `> 0` check → `!= nil && !id.Empty()`; `strconv.FormatInt` → `string(*id)`.
- `handlers/api.go` (~63) — `assigned_to` filter: drop `strconv.ParseInt`; set the filter to a `models.UserID` from the raw query string.
- `handlers/snooze.go` (~52) — `snoozedBy *int64` → `*models.UserID` from the actor id.
- `services/workflow_engine.go` (~120) — `strconv.Itoa(*ticket.AssignedTo)` → `string(*ticket.AssignedTo)`; the assign action that writes `assigned_to` should set a `UserID` (no ParseInt).
- Reassign/assignment service signatures: `agentID int64` → `models.UserID`.
- `services/automation`/escalation/macro assign actions: pass the raw action value as `models.UserID` (no `strconv.ParseInt`).

`h.userID(r)` (the current actor) — if it returns `int64`, either change it to
return `models.UserID` or convert at call sites. Prefer changing it to
`models.UserID` for consistency. Keep genuinely-internal int64 ids (ticket id,
department id) as int64.

## Step 5 — Test

Add `models/user_id_test.go`:
- `Scan(int64(5))` → `"5"`; `Scan("uuid-x")` → `"uuid-x"`; `Scan(nil)` → empty.
- `Value()` of `"5"` → `"5"`; of `""` → `nil`.
- `MarshalJSON` of `"5"` → `5` (number); of `"abc-1"` → `"abc-1"` (quoted string); of `""` → `null`.
- `UnmarshalJSON` of `5`, `"abc"`, `null`.
- `userIDColumnType()` default `BIGINT`; with `ESCALATED_USER_KEY_TYPE=uuid` → `VARCHAR(255)` (save/restore env).

## Step 6 — Build, vet, test, commit

From repo root, make all green:

```
go build ./...
go vet ./...
go test ./...
gofmt -l .    # must print nothing; run gofmt -w on any listed files
```

If golangci-lint is configured, run it and fix new issues. Then commit (do NOT
push):

```
git add -A
git commit -m "fix(users): support UUID/string host user keys"
```

Do NOT delete UUID_FIX_SPEC.md. Report every file changed and the final
build/vet/test/gofmt status.
