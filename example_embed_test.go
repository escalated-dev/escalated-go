package escalated_test

import (
	"fmt"
	"net/http"

	escalated "github.com/escalated-dev/escalated-go"
)

// The examples in docs/embedding-admin.md, compiled here so they stay
// correct.

// requireAdminSession stands in for the host's own admin middleware.
func requireAdminSession(next http.Handler) http.Handler { return next }

// Layout embed (a Vue + Inertia or server-rendered host).
func ExampleConfig_EmbedAdmin() {
	cfg := escalated.DefaultConfig()
	cfg.RoutePrefix = "/support"
	cfg.EmbedAdmin(escalated.EmbedConfig{
		Prefix:     "admin/support",
		Middleware: []func(http.Handler) http.Handler{requireAdminSession},
		Layout:     "Layouts/AdminLayout",
	})
	fmt.Println(cfg.UI.Embed.Enabled, cfg.UI.Embed.Prefix)
	// Output: true admin/support
}

// Server-rendered host: the host's admin template is the root view of the
// embedded admin pages.
func ExampleConfig_EmbedAdmin_rootView() {
	cfg := escalated.DefaultConfig()
	cfg.EmbedAdmin(escalated.EmbedConfig{
		Prefix:     "admin/support",
		Middleware: []func(http.Handler) http.Handler{requireAdminSession},
		RootView:   "templates/admin/escalated.html",
	})
	fmt.Println(cfg.UI.Embed.RootView)
	// Output: templates/admin/escalated.html
}

// iframe embed.
func ExampleConfig_EmbedAdmin_iframe() {
	cfg := escalated.DefaultConfig()
	cfg.EmbedAdmin(escalated.EmbedConfig{
		Prefix:         "admin/support",
		Middleware:     []func(http.Handler) http.Handler{requireAdminSession},
		Mode:           escalated.EmbedModeIframe,
		FrameAncestors: []string{"https://admin.example.com"},
	})
	fmt.Println(cfg.UI.Embed.Mode, cfg.UI.Embed.FrameAncestors)
	// Output: iframe [https://admin.example.com]
}
