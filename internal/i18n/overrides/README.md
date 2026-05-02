# Local locale overrides

Drop a `{locale}.json` file in this directory to override individual translation
keys on top of the central data shipped by
[`github.com/escalated-dev/escalated-locale`](https://github.com/escalated-dev/escalated-locale).

Files are deep-merged with the upstream data — only the keys you list here are
overridden, everything else falls through to the central package.

Example `en.json`:

```json
{
  "ticket": {
    "actions": {
      "reply": "Send reply"
    }
  }
}
```

The merge happens in `internal/i18n/loader.go` (`Load` / `T`). No code change is
needed when adding or removing override files.
