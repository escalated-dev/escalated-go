# Newsletters

## Newsletters (optional, partial port)

This PR establishes the schema, models, and renderer for the newsletter system. The DB-bound planner / dispatcher / tracker services need integration with the host's `store/` layer, which is a follow-up.

```go
import (
    "github.com/escalated-dev/escalated-go/services/newsletter"
    "github.com/yuin/goldmark"
    "bytes"
)

cfg := newsletter.Config{
    BaseURL:             "https://support.example.com",
    DefaultTheme:        "default",
    TrackingEnabled:     true,
    ThemesDir:           "/path/to/templates/newsletter_themes",
    MarkdownToHTML:      func(md string) string {
        var buf bytes.Buffer
        _ = goldmark.Convert([]byte(md), &buf)
        return buf.String()
    },
    Brand: newsletter.Brand{
        Name:            "Acme",
        Accent:          "#2563eb",
        PhysicalAddress: "Acme Inc. · 123 Main St",
    },
    EnableNewsletters:   true,
}

r := newsletter.NewRenderer(cfg)
html, _ := r.Render(&delivery, &n, &contact, nil)
```

The package ships:
- `models/newsletter.go` — 5 entity structs + Contact gains `MarketingOptOutAt`
- `services/newsletter/renderer.go` — Markdown → theme → click rewrite → pixel injection
- `migrations/20260522000001_create_escalated_newsletter_system.sql` — goose SQL migration

Follow-up PR will add the Store interfaces for newsletters (postgres + sqlite) and the planner/dispatcher/tracker services that consume them.
