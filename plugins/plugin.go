// Package plugins runs Escalated plugins: native plugins written in Go and
// registered in config, and SDK plugins (@escalated-dev/plugin-sdk) run by the
// Node plugin runtime over a JSON-RPC bridge on its stdio. Both kinds share one
// registry, one activation table (escalated_plugins) and the admin screen
// Escalated/Admin/Plugins/Index.
//
// The behaviour every Escalated backend shares is the plugin contract in
// escalated-developer-context (domain-model/plugin-contract.md). docs/plugins.md
// covers what is specific to this package.
package plugins

import (
	"context"
	"net/http"
)

// Plugin is a native plugin: Go code compiled into the host and listed in
// Config.Plugins.Native. Manifest is the only required method; the optional
// interfaces below (Activator, Deactivator, ActionPlugin, FilterPlugin,
// EndpointPlugin, WebhookPlugin, CronPlugin, BadgePlugin) add behaviour. A plugin runs with the host's
// privileges, so only register code you would merge into your own.
type Plugin interface {
	Manifest() Manifest
}

// Activator is called after an admin activates the plugin.
type Activator interface {
	OnActivate(ctx context.Context, pc *Context) error
}

// Deactivator is called while an admin deactivates the plugin, before its row
// is switched off.
type Deactivator interface {
	OnDeactivate(ctx context.Context, pc *Context) error
}

// ActionPlugin handles actions: the canonical domain events
// (ticket.created, reply.created, ...), the lifecycle actions
// (plugin.activated, plugin.deactivated, plugin.uninstalling) and whatever
// active plugins emit. The event is the JSON-shaped payload an SDK plugin
// receives.
type ActionPlugin interface {
	Actions() map[string]ActionFunc
}

// FilterPlugin declares filters, run by Host.ApplyFilter.
type FilterPlugin interface {
	Filters() []Filter
}

// EndpointPlugin declares endpoints, served at
// {RoutePrefix}/api/plugins/{slug}{path} behind the admin guard.
type EndpointPlugin interface {
	Endpoints() []Route
}

// WebhookPlugin declares webhooks, served at
// {RoutePrefix}/webhooks/plugins/{slug}{path} with no guard: the plugin
// authenticates the sender.
type WebhookPlugin interface {
	Webhooks() []Route
}

// CronPlugin declares cron schedules, the native form of an SDK plugin's
// cron keys: five-field cron expressions (minute hour day-of-month month
// day-of-week, or @hourly, @daily, ...), evaluated in UTC, each with the
// function to run. Host.RunDueCron runs the due ones of active plugins once a
// minute, as no user, within 30 s each.
type CronPlugin interface {
	Cron() map[string]CronFunc
}

// BadgePlugin gives dashboard widgets a live badge, the native form of an SDK
// widget's badge function: a map of a manifest widget's Component to the
// function that answers its badge.
type BadgePlugin interface {
	Badges() map[string]BadgeFunc
}

// CronFunc runs one cron schedule. An error or a panic is logged and never
// stops the other schedules.
type CronFunc func(ctx context.Context, pc *Context) error

// BadgeFunc answers a widget's badge for the viewer (pc.CurrentUser and
// pc.Request are the viewer's): a number or a string, or nil for none. It
// has 5 s; an error, a panic, a slow answer or any other type is null.
type BadgeFunc func(ctx context.Context, pc *Context) (any, error)

// ActionFunc handles one action. An error is logged; it never reaches the
// request that caused the event.
type ActionFunc func(ctx context.Context, event map[string]any, pc *Context) error

// FilterFunc returns the filtered value. An error skips this filter and the
// value carries on to the next one.
type FilterFunc func(ctx context.Context, value any, pc *Context) (any, error)

// Filter is one filter. Filters run in ascending Priority; zero means the
// default, 10.
type Filter struct {
	Hook     string
	Priority int
	Handler  FilterFunc
}

// RouteFunc answers an endpoint or webhook. Return a value for a JSON 200, or
// a Response (or *Response) to choose the status, headers and format. An error
// is a 500 whose body says nothing about it; the error goes to the log.
type RouteFunc func(ctx context.Context, pc *Context, req *Request) (any, error)

// Route is an endpoint or webhook: Method is GET, POST, PUT, PATCH, DELETE,
// OPTIONS or HEAD, and Path starts with a slash and may hold :name segments,
// which arrive as Request.Params. Capability, on an endpoint, is a permission
// the user must hold (403 without it).
type Route struct {
	Method     string
	Path       string
	Capability string
	Handler    RouteFunc
}

// Request is what an endpoint or webhook handler receives, as the SDK's HTTP
// contract version 1 has it: the parsed body, the exact bytes, the route params
// of the declared path, the query, and lower-case headers without Cookie,
// Authorization, X-XSRF-TOKEN, X-CSRF-TOKEN or Proxy-*.
type Request struct {
	Method   string
	Path     string
	Body     any
	RawBody  []byte
	Params   map[string]string
	Query    map[string]string
	Headers  map[string]string
	ClientIP string
}

// Response chooses an endpoint's status, headers and format: JSON by default,
// or plain text when Text is set (Body must then be a string). Transport
// headers and Set-Cookie are refused (502).
type Response struct {
	Status  int
	Headers map[string]string
	Body    any
	Text    bool
}

// Manifest describes a plugin: its identity, the screens it adds and its
// settings fields. The JSON names are the SDK manifest's.
type Manifest struct {
	// Name identifies the plugin; ctx.store and ctx.config are keyed by it.
	// Its slug (Slug) is what goes in URLs and the escalated_plugins row.
	Name        string        `json:"name"`
	Version     string        `json:"version"`
	Description string        `json:"description,omitempty"`
	Author      string        `json:"author,omitempty"`
	Homepage    string        `json:"homepage,omitempty"`
	Config      []ConfigField `json:"config,omitempty"`
	Pages       []Page        `json:"pages,omitempty"`
	Components  []Component   `json:"components,omitempty"`
	Widgets     []Widget      `json:"widgets,omitempty"`
}

// ConfigField is one settings field the plugin asks for.
type ConfigField struct {
	Name     string         `json:"name"`
	Label    string         `json:"label"`
	Type     string         `json:"type"`
	Required bool           `json:"required,omitempty"`
	Help     string         `json:"help,omitempty"`
	Options  []ConfigOption `json:"options,omitempty"`
	Default  any            `json:"default,omitempty"`
}

// ConfigOption is one choice of a select field.
type ConfigOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// Page is an admin page, served at {RoutePrefix}/admin/plugins/{slug}/{route}
// and rendered as Escalated/Plugin/Page. Its props are the result of the
// plugin's own GET /{Component} endpoint.
type Page struct {
	Route      string `json:"route"`
	Component  string `json:"component"`
	Layout     string `json:"layout,omitempty"`
	Capability string `json:"capability,omitempty"`
	Menu       *Menu  `json:"menu,omitempty"`
}

// Menu puts a page in the navigation. Section is admin (the default) or
// agent; Position zero means the default, 100.
type Menu struct {
	Label    string `json:"label"`
	Section  string `json:"section,omitempty"`
	Position int    `json:"position,omitempty"`
	Icon     string `json:"icon,omitempty"`
}

// Component fills a slot on a shared page ("{Page}.{Slot}"). Order zero means
// the default, 100.
type Component struct {
	Page       string         `json:"page"`
	Slot       string         `json:"slot"`
	Component  string         `json:"component"`
	Props      map[string]any `json:"props,omitempty"`
	Order      int            `json:"order,omitempty"`
	Capability string         `json:"capability,omitempty"`
}

// Widget is a dashboard widget. Size is full, half (the default) or quarter;
// Order zero means the default, 100.
type Widget struct {
	Component  string `json:"component"`
	Label      string `json:"label"`
	Size       string `json:"size,omitempty"`
	Order      int    `json:"order,omitempty"`
	Capability string `json:"capability,omitempty"`
}

// CapabilityFunc reports whether the request's user holds a capability (a
// permission slug a page, endpoint or UI item declares).
type CapabilityFunc func(r *http.Request, capability string) bool
