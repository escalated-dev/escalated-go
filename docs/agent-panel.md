# Agent panel data

The shared frontend's agent panel (`@escalated-dev/escalated`, `docs/agent-panel.md`) reads five things this package provides, as escalated-dev/escalated-developer-context ADR `2026-11-05-agent-panel-data` specifies. `Migrate` / `MigrateSQLite` add what they need on the next start: `tickets.last_customer_activity_at` and the `ticket_reads`, `customer_presence`, `notifications` and `agent_statuses` tables (with your table prefix).

## Agent status

- `PUT` or `POST {prefix}/agent/status` (`escalated.agent.status`) with `{"status": "online" | "away" | "offline"}` stores the agent's status (`agent_statuses`, one row per agent). A JSON client gets `{"agent_status": "away"}`; an Inertia visit goes back. Anything else is a 422.
- Staff pages share `escalated.agent_status` (`offline` until set) and `escalated.agent_statuses` (`["online", "away", "offline"]`).
- No permission is needed beyond the agent gate.
- Live chat reads it, as escalated-laravel's chat routing: only `online` agents in a routing rule get new chats and count towards the widget's chat availability (`Escalated.ChatAgentOnline`). Away and offline agents, and agents who never set a status, are skipped. `Config.ChatAgentOnline`, when set, can only veto an online agent.

## Knowledge base search

- `GET {prefix}/agent/kb/search?q=<2..200 chars>&limit=<1..20, default 10>` (`escalated.agent.kb.search`, `kb.view` under roles & permissions) answers `{"data": [{id, title, excerpt, url, category: {id, name} | null, updated_at}]}`.
- Published articles only; title or body contains the term, case-insensitive, with `%` and `_` taken literally (the knowledge base API and pages now search the same way). Title matches come first, then the most recently updated. `excerpt` is the body as plain text, cut at 160 characters; `url` is the article's absolute knowledge-base page URL.
- 404 while the knowledge base is off.

## Unread tickets

- A ticket is unread for an agent when the customer was active on it after the agent last opened it, or when the agent marked it unread. Customer activity is a ticket's creation through the ticket service (not an import), a chat starting, a visitor's chat message, and every public reply from the customer side: the requesting user, or no author (a guest, an inbound email). Agents' replies, internal notes and system replies are not. Existing tickets have no activity, so none turn unread on upgrade.
- Opening a ticket in the agent panel marks it read. `POST {prefix}/agent/tickets/{ticket}/read` and `/unread` (`escalated.agent.tickets.read` / `.unread`) answer `{"is_unread": bool}`; the bulk endpoint takes `mark_read` and `mark_unread` (no value).
- `is_unread` is on every `Agent/TicketIndex` row (one query for the page) and on `Agent/TicketShow`'s ticket (`false`). `inbox.unread` is `{mine, unassigned, all}`, counted with `EXISTS` through the list's own filters and the ticket visibility scope.

## Customer presence

- The customer portal (signed-in users who are not staff), the guest ticket page, the guest ticket API and the widget's ticket and chat endpoints record when the customer was last seen, at most once every 30 seconds. The subject is the requesting user, else the ticket's contact, else the ticket itself (a guest chat).
- Agent pages only: each `Agent/TicketIndex` row and `Agent/TicketShow`'s ticket carry `requester_online` (seen within `Config.AgentPanel.PresenceOnlineWindow`, default 120 seconds) and `requester_last_seen_at`. Customer, guest and widget responses never include them.

## Notifications

| Type | Who | When |
|---|---|---|
| `ticket_assigned` | the assignee | someone else assigns them a ticket (assigning yourself sends nothing, email included) |
| `ticket_reply` | the assignee and followers | a public reply from the customer side |
| `sla_warning` | the assignee | `sla.warning` |
| `sla_breached` | the assignee | `sla.breached` |
| `chat_transferred` | the receiving agent | a chat is transferred to them |

Nobody is notified of their own action.

- `GET {prefix}/agent/notifications?page=&per_page=<1..50, default 20>&unread=1` (`escalated.agent.notifications.index`) answers `{data: [{id, type, title, body, ticket: {id, reference, subject} | null, url, created_at, read_at}], meta: {current_page, last_page, per_page, total}}`, newest first. A notification about a ticket the agent can no longer view is redacted: title "A ticket you can no longer view", `body`, `ticket` and `url` null.
- `POST {prefix}/agent/notifications/{id}/read` answers `{"unread_count": n}` (404 for another agent's notification); `POST {prefix}/agent/notifications/read-all` answers `{"unread_count": 0}`.
- Staff pages share `escalated.notifications.unread_count`.
- With a broadcaster (`Config.AgentPanel.Broadcaster`, else `Config.Plugins.Broadcaster`), each new notification is published as `notification.created` (the feed item) on the agent's private channel `services.UserChannel(id)` (`user.<id>`). Under restricted ticket visibility the broadcaster checks the item's `ticket.id` before delivering it.
- `Config.AgentPanel.DisableNotifications` switches the feed off: nothing is stored, the routes answer 404 and the shared count is left out.

## Configuration

```go
cfg.AgentPanel = escalated.AgentPanelConfig{
	PresenceOnlineWindow: 2 * time.Minute,
	DisableNotifications: false,
	Broadcaster:          broadcaster, // optional
}
```
