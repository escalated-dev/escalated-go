# Ticket access and routing

All new options are opt-in. `Config.Visibility` defaults to `"all"`; accounts, account scoping and targeted SLA policies default off. Apply the normal package migrations before deploying code that reads the added fields. Existing tickets retain null account identity. The migration adds nullable indexed account columns, nullable SLA targets and a department-agent pivot without joining host user tables.

## Configure the host hooks

```go
cfg := escalated.DefaultConfig()
cfg.Visibility = "department"
cfg.Accounts = ticketaccess.AccountConfig{
    Enabled: true,
    ScopeAgents: true,
    Resolver: func(ctx context.Context, ref ticketaccess.Reference) (*ticketaccess.Account, error) {
        // ref.Kind: requester, subject, or agent. Type/ID identify the host record.
        return directory.ResolveAccount(ctx, ref.Kind, ref.Type, ref.ID)
    },
}
cfg.TargetedSLAs = true
cfg.TicketAccessUserResolver = func(ctx context.Context, id models.UserID) (*ticketaccess.User, error) {
    // Reload current roles, including revoked access, from the host directory.
    return directory.ResolveTicketUser(ctx, id)
}
esc, err := escalated.New(cfg)
```

Imports for the hooks are `github.com/escalated-dev/escalated-go/ticketaccess` and `/models`. The example `directory` belongs to the host app. Invalid visibility values or account scoping without enabled accounts and a resolver fail startup.

Maintain `<prefix>department_agent(department_id, agent_id)` from the host access system. Agent IDs are strings and need no host database foreign key. Department and account restrictions intersect; admins retain full access. In department mode, no memberships or no ticket department means no visible tickets. Account scoping compares one complete, case-sensitive type/ID pair; a missing account matches nothing. Agents covering several accounts should use department mode with account scoping disabled.

`AdminCheck`, `AgentCheck` and `UserIDFunc` identify the current HTTP actor. Authentication, including bearer token validation and token-owner identity, remains host-owned. Run it before mounting package routes, and make these callbacks refer to the authenticated token owner. `TicketAccessUserResolver` is used for assignment and delivery; without it, restricted assignment/delivery fails closed. It must return the same ID requested, current agent/admin roles and optional host model data.

## Scope coverage

Both Chi and standard-library mounts install the access middleware. The shared SQL helpers scope built-in stores and direct handler queries before counts, pagination and aggregation. This covers list/search, detail, mutations, reports, API, attachments, related records and macros. Ticket links require both endpoints. Writes check existing visibility; inserts and moves of children check their parents. Nested reads in write statements use the same scope.

For a separately mounted widget or other host endpoint, wrap it in `esc.TicketAccessMiddleware(handler)`. To run work for a user, obtain a context with `esc.Access.ForUser(ctx, ticketaccess.User{...})`, and pass that context through every store/service call. A context without an actor is trusted background work. `ticketaccess.WithoutVisibility(ctx)` is the explicit maintenance bypass.

Use the built-in stores, context-aware service methods and `ticketaccess.QueryContext`, `QueryRowContext`, `ExecContext` or `ExecInsertContext` for scoped host SQL. The helpers support the statement shapes used by this package, including nested reads and ordinary inserts; unsupported scoped writes fail closed. Raw `*sql.DB` calls, older model helpers without contexts and custom stores are host-owned access paths. They do not infer a current request; custom stores must honor the context or apply equivalent checks. Use `MacroService.ApplyContext` for request-driven macros.

Account identity is filled from the requester on creation, or from subject attachment if both fields are empty. Existing identity is retained; splits copy it. List filters accept `account_type` plus `account_id`; partial identity matches nothing. Backfill old tickets through trusted code before enabling account scope. A host resolver error is returned, never converted to unrestricted access.

## Rules and SLA policies

Escalations and automations accept conditions such as:

```json
[
  {"field":"ticket_type","operator":"equals","value":"incident"},
  {"field":"account","operator":"equals","value":{"type":"Company","id":"42"}}
]
```

Workflows use their existing `{"all":[...]}` or `{"any":[...]}` envelope. Account equality compares both components; account inequality also requires a complete expected pair. Invalid account conditions and malformed workflow conditions never match.

With targeted SLAs enabled, active policies rank by department plus type, department, type, then untargeted default. Lowest policy ID breaks ties. Creation selects the policy. Updating routing preserves its current policy and clock. Explicit `SLAService.ApplyDefaultPolicy` reselects using the original creation time. With targeted SLAs off, automatic selection excludes targeted policies and retains the existing untargeted department/global default behavior.

Package routers pass access hooks to workflow, escalation, automation and macro services. When constructing those services yourself, pass `esc.Access` as the optional last constructor argument. `NewSkillRoutingService` accepts the same hook to filter candidates. Chat routing uses `FindAvailableAgentForTicket` to check the entire ticket identity before assignment.

## Notifications and live events

The host owns notification delivery. Pass follower/assignee/requester IDs through `esc.Access.Recipients(ctx, ticketID, ids)` immediately before delivery, including inside queued jobs. The existing `models.FollowerRecipients` function only deduplicates IDs and excludes the actor.

Set `BroadcastConfig.TicketAccess = esc.Access`, or call `broadcaster.ConfigureTicketAccess(esc.Access)` before serving connections. Chat services configure their broadcaster from the store automatically. Restricted subscriptions check current host identities, and ticket events are checked again during publication and immediately before an SSE transport writes buffered events. Revoked memberships therefore stop queued payloads from being delivered. Broad agent/private-user events must carry `ticket_id` or `ticket.id`; events without a ticket identity are suppressed in restricted mode. Ticket-specific channels encode it in their channel name. Default unrestricted broadcasting retains its existing behavior.

## Migration and verification

`Migrate` and `MigrateSQLite` upgrade existing tables idempotently. Account columns use PostgreSQL `C` or SQLite `BINARY` collation. Custom prefixes, including mixed-case prefixes, are covered without host-user joins. Schema rollback is host-managed; do not remove account columns while this package version is running.

Regression tests cover independent list/search/detail/update/reply/report/widget/API paths, bulk and preloaded writes, nested SQL reads, related records, concurrent requests, account/type rules, resolver hooks, SLA clocks, assignment, buffered event revocation and upgrades preserving old rows. The full suite runs on SQLite and PostgreSQL.
