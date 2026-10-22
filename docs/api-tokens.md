# API tokens

Admins issue bearer tokens for the REST API on the **API Tokens** page
(`Escalated/Admin/ApiTokens/Index`, `GET {RoutePrefix}/admin/api-tokens`), as
in escalated-laravel. A token lets a script, a desktop app or another service
call the ticket API as the agent it was issued to, without the host's session.

Tokens are **opt-in**. Without `Config.APITokensEnabled` the page still issues
and lists tokens (and says the API is disabled), but they authenticate nothing:
the API is left to `AgentCheck` and `AdminCheck`, exactly as before.

```go
cfg.APITokensEnabled = true
// Who is staff now. Defaults to Config.UserDirectory.GetUser (integer ids).
cfg.APITokenOwner = func(ctx context.Context, id models.UserID) (*handlers.APITokenOwner, error) {
	u, err := users.Find(ctx, string(id))
	if err != nil || u == nil {
		return nil, err
	}
	return &handlers.APITokenOwner{IsAgent: u.IsAgent, IsAdmin: u.IsAdmin}, nil
}
```

Run the migrations (`migrations.Migrate` / `MigrateSQLite`): they add
`{prefix}api_tokens`.

## Using a token

```
curl -H "Authorization: Bearer $TOKEN" https://example.com/escalated/api/tickets
```

Tokens authenticate the agent API only: `/api/tickets` (and everything under
it), `/api/departments` and `/api/tags`. They never reach the admin, agent or
customer pages.

A request with a token acts as the token's owner: `UserIDFunc` returns the
owner, and `AgentCheck` / `AdminCheck` answer for the owner narrowed by the
token's abilities. Ticket visibility is the owner's.

| Answer | When |
|---|---|
| 401 `Invalid token.` | The token is unknown and the host's own checks refuse the request. |
| 401 `Token has expired.` | Past its expiry. |
| 401 `Token owner not found.` | `APITokenOwner` returns nil (or neither it nor `UserDirectory` is set). |
| 403 `Insufficient permissions.` | The token lacks the `agent` ability (an `admin`-only token). |
| 403 `User no longer has agent access.` | The owner is no longer an agent or admin. |

A bearer value the package did not issue is left to `AgentCheck` and
`AdminCheck`, so a host that authenticates the API with tokens of its own keeps
working with tokens enabled. `/api/auth/*` (the host's `APIAuth` callbacks) is
never looked at.

## Storage

Each token is 32 random bytes as 64 hex characters. Only its SHA-256 digest is
stored, so it is shown once, when it is created, and never again. Each token has:

- **abilities**: `agent`, `admin` and/or `*`. They narrow the owner, never widen
  them: `admin` counts only when the owner is an admin.
- **expiry**: optional, 1 to 365 days.
- **last used**: when and from which address, updated at most every five
  minutes.

## Admin routes

| Method | Path | |
|---|---|---|
| `GET` | `/admin/api-tokens` | The page: `tokens`, `users` (from `SkillAgentDirectory`, else the staff in `UserDirectory`), `api_enabled`. JSON `{tokens}` for API clients. |
| `POST` | `/admin/api-tokens` | `name`, `user_id` (must be staff), `abilities`, `expires_in_days`. A page visit gets the page again with `flash.plain_text_token`; an API client gets `201 {token, plain_text_token}`. Both `Cache-Control: no-store`. |
| `PATCH`/`PUT` | `/admin/api-tokens/{id}` | `name` and/or `abilities`. |
| `DELETE` | `/admin/api-tokens/{id}` | Revokes the token. |

There is no session to flash through, so creating a token answers with the
page itself rather than a redirect; the page reads the token from its `flash`
prop either way.
