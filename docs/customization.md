# Customization

## Custom Ticket Actions

Add host-defined action buttons to the agent ticket screen via
`Config.TicketActions`. Each visible action is exposed on the ticket responses
(`custom_actions`, plus a top-level `customActions` prop on the agent screen),
and triggering it records an internal note and invokes `Config.OnCustomAction`:

```go
import "github.com/escalated-dev/escalated-go/actions"

cfg := escalated.DefaultConfig()
cfg.TicketActions = []actions.TicketAction{
    {
        Key:          "sync-crm",
        Label:        "Sync CRM",
        Variant:      "primary", // primary | secondary | danger
        Confirmation: "Sync this ticket to the CRM?",
        Metadata:     map[string]any{"icon": "refresh-cw"},
        // Visible/Enabled are optional; nil means always visible/enabled.
        Enabled: func(t *models.Ticket, userID int64) bool { return t.ResolvedAt == nil },
    },
}
cfg.OnCustomAction = func(ctx context.Context, e actions.CustomActionEvent) error {
    if e.Action == "sync-crm" {
        // e.Ticket, e.UserID, e.Payload, e.Metadata
    }
    return nil
}
```

Triggering an action (`POST {prefix}/agent/tickets/{id}/actions/{key}` or the
`/api` equivalent) returns 404 if the action is unknown or not visible, 403 if
it is disabled, otherwise records the audit note and calls `OnCustomAction`.

## Custom Store

Implement the `store.Store` interface to use a different database:

```go
esc, _ := escalated.New(cfg)
esc.Store = myCustomStore // satisfies store.Store interface
```

## Ticket Statuses

| Value | Name |
|-------|------|
| 0 | open |
| 1 | in_progress |
| 2 | waiting_on_customer |
| 3 | waiting_on_agent |
| 4 | escalated |
| 5 | resolved |
| 6 | closed |
| 7 | reopened |

## Priorities

| Value | Name |
|-------|------|
| 0 | low |
| 1 | medium |
| 2 | high |
| 3 | urgent |
| 4 | critical |

## Translations

Strings come from the central [`escalated-locale`](https://github.com/escalated-dev/escalated-locale)
Go module so they stay in sync with every other Escalated plugin. The thin
loader at `internal/i18n` deep-merges optional local overrides on top of the
upstream data:

```go
import "github.com/escalated-dev/escalated-go/internal/i18n"

label := i18n.T("ticket.status.open", "fr", nil)
msg   := i18n.T("validation.required", "en", map[string]any{"field": "Email"})
```

To override a single key without forking the locale file, drop a JSON file at
`internal/i18n/overrides/{locale}.json` — only the keys you list there are
overridden, everything else falls through to the central package. See
`internal/i18n/overrides/README.md` for the full pattern.

## Configurable ticket access and routing

See [ticket access and routing](ticket-access-and-routing.md) for opt-in department/account visibility, account resolver hooks, rule conditions and targeted SLA policies. All restrictions default off.
