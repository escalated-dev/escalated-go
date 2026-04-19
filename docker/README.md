# Escalated Go — Docker demo (scaffold, not end-to-end)

Draft scaffold. Go 1.22 + Postgres 16 + Mailpit. Host app at `docker/host-app/` imports the library via a `replace` directive.

**Not end-to-end.** Current scaffold only exposes a placeholder `/demo` — the actual library router, models, migrations, and the click-to-login wiring are next steps. See the PR body.
