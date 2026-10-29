package plugins

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/services"
)

// Timeouts per call kind, as the plugin runtime has them.
const (
	actionTimeout   = 30 * time.Second
	filterTimeout   = 5 * time.Second
	endpointTimeout = 30 * time.Second
	webhookTimeout  = 60 * time.Second
)

// Source values of the admin screen's plugin objects.
const (
	// SourceRegistered is a native plugin listed in Config.Plugins.Native.
	SourceRegistered = "registered"
	// SourceComposer is an SDK plugin installed with npm. The name is the
	// contract's, historical: any package manager. The admin screen refuses to
	// delete it; the package manager removes it.
	SourceComposer = "composer"
)

// LifecycleActions are fired to every active plugin on activation,
// deactivation and uninstall, with {plugin, slug}.
var LifecycleActions = []string{"plugin.activated", "plugin.deactivated", "plugin.uninstalling"}

// Config configures plugins (escalated.Config.Plugins).
type Config struct {
	// Native are the native plugins compiled into the host. Each is listed on
	// Admin > Plugins, inactive until an admin activates it.
	Native []Plugin

	// Runtime runs SDK plugins through @escalated-dev/plugin-runtime. Off by
	// default.
	Runtime RuntimeConfig

	// Capability reports whether the request's user holds a capability a
	// page, endpoint or UI item declares. Nil grants every capability to
	// admins (AdminCheck) and none to anyone else.
	Capability CapabilityFunc

	// Logger receives plugin errors and log lines. Nil uses the standard
	// logger.
	Logger *log.Logger

	// Broadcaster carries ctx.broadcast to the real-time channels. Nil makes
	// ctx.broadcast a no-op.
	Broadcaster *services.Broadcaster
}

// Options are what the host passes New besides Config.
type Options struct {
	DB          *sql.DB
	TablePrefix string
	RoutePrefix string
	AdminCheck  func(r *http.Request) bool
	UserID      func(r *http.Request) models.UserID
	// UserName and UserEmail name the host's users for ctx.currentUser.
	UserName  func(ctx context.Context, id models.UserID) string
	UserEmail func(ctx context.Context, id models.UserID) (string, error)
	// ClientIP resolves a request's client address through the host's
	// trusted-proxy rules. Nil uses the connection's address.
	ClientIP func(r *http.Request) string
	// Reserved are names ctx.emit may not fire: the canonical domain events
	// and their legacy names. The lifecycle actions are always reserved.
	Reserved []string
	// Version is the host's version, sent in the runtime handshake.
	Version string
}

// Host is the plugin registry: the registered plugins, their activation in
// escalated_plugins, their lifecycle, and the dispatch of hooks, pages,
// endpoints and webhooks to the active ones. Inactive plugins are never
// called.
type Host struct {
	rows        *rows
	native      []*entry
	bridge      *bridge
	capability  CapabilityFunc
	adminCheck  func(r *http.Request) bool
	userID      func(r *http.Request) models.UserID
	userName    func(ctx context.Context, id models.UserID) string
	userEmail   func(ctx context.Context, id models.UserID) (string, error)
	clientIP    func(r *http.Request) string
	logger      *log.Logger
	routePrefix string
	reserved    map[string]bool
	broadcaster *services.Broadcaster
	domain      *Domain
}

// entry is one registered plugin, normalised once.
type entry struct {
	name     string
	slug     string
	source   string
	manifest Manifest
	native   Plugin

	endpoints []route
	webhooks  []route
	filters   []nativeFilter
	actions   map[string]ActionFunc

	// SDK plugins: what the runtime's manifest declares.
	sdk         bool
	actionHooks map[string]bool
	sdkFilters  map[string]int
}

type nativeFilter struct {
	hook     string
	priority int
	handler  FilterFunc
}

// New validates the configured native plugins and returns the registry. Two
// plugins with one slug, or a malformed plugin, are an error naming them.
func New(cfg Config, opts Options) (*Host, error) {
	logger := cfg.Logger
	if logger == nil {
		logger = log.New(os.Stderr, "", log.LstdFlags)
	}
	h := &Host{
		rows:        &rows{db: opts.DB, prefix: opts.TablePrefix},
		capability:  cfg.Capability,
		adminCheck:  opts.AdminCheck,
		userID:      opts.UserID,
		userName:    opts.UserName,
		userEmail:   opts.UserEmail,
		clientIP:    opts.ClientIP,
		logger:      logger,
		routePrefix: strings.TrimRight(opts.RoutePrefix, "/"),
		reserved:    map[string]bool{},
		broadcaster: cfg.Broadcaster,
	}
	if h.adminCheck == nil {
		h.adminCheck = func(*http.Request) bool { return false }
	}
	if h.userID == nil {
		h.userID = func(*http.Request) models.UserID { return "" }
	}
	for _, name := range append(append([]string{}, opts.Reserved...), LifecycleActions...) {
		h.reserved[name] = true
	}

	seen := map[string]string{}
	for _, p := range cfg.Native {
		e, err := nativeEntry(p)
		if err != nil {
			return nil, err
		}
		if other, ok := seen[e.slug]; ok {
			return nil, fmt.Errorf("escalated: plugins %q and %q share the slug %q; rename one", other, e.name, e.slug)
		}
		seen[e.slug] = e.name
		h.native = append(h.native, e)
	}

	if cfg.Runtime.Enabled {
		h.bridge = newBridge(cfg.Runtime, opts.Version, logger, h.serveContext)
	}
	return h, nil
}

func nativeEntry(p Plugin) (*entry, error) {
	if p == nil {
		return nil, errors.New("escalated: a nil plugin is registered")
	}
	m := p.Manifest()
	if strings.TrimSpace(m.Name) == "" {
		return nil, fmt.Errorf("escalated: a plugin (%T) has no manifest name", p)
	}
	e := &entry{name: m.Name, slug: Slug(m.Name), source: SourceRegistered, manifest: m, native: p, actions: map[string]ActionFunc{}}
	if ep, ok := p.(EndpointPlugin); ok {
		for _, r := range ep.Endpoints() {
			if r.Handler == nil {
				return nil, fmt.Errorf("plugin %s: route %q has no handler", m.Name, r.Method+" "+r.Path)
			}
			parsed, err := parseRoute(m.Name, r.Method, r.Path, r.Capability, r.Handler)
			if err != nil {
				return nil, err
			}
			e.endpoints = append(e.endpoints, parsed)
		}
	}
	if wp, ok := p.(WebhookPlugin); ok {
		for _, r := range wp.Webhooks() {
			if r.Handler == nil {
				return nil, fmt.Errorf("plugin %s: webhook %q has no handler", m.Name, r.Method+" "+r.Path)
			}
			parsed, err := parseRoute(m.Name, r.Method, r.Path, "", r.Handler)
			if err != nil {
				return nil, err
			}
			e.webhooks = append(e.webhooks, parsed)
		}
	}
	if fp, ok := p.(FilterPlugin); ok {
		for _, f := range fp.Filters() {
			if f.Handler == nil {
				return nil, fmt.Errorf("plugin %s: filter %q has no handler", m.Name, f.Hook)
			}
			priority := f.Priority
			if priority == 0 {
				priority = 10
			}
			e.filters = append(e.filters, nativeFilter{hook: f.Hook, priority: priority, handler: f.Handler})
		}
	}
	if ap, ok := p.(ActionPlugin); ok {
		for hook, fn := range ap.Actions() {
			if fn != nil {
				e.actions[hook] = fn
			}
		}
	}
	return e, nil
}

// Close stops the plugin runtime, if one is running.
func (h *Host) Close() {
	if h != nil && h.bridge != nil {
		h.bridge.close()
	}
}

func (h *Host) logf(format string, args ...any) {
	h.logger.Printf("escalated plugins: "+format, args...)
}

// entries are every registered plugin: native ones, then the SDK plugins the
// runtime reports (starting it on first use). An SDK plugin whose slug a
// native plugin already has is left out, and logged.
func (h *Host) entries(ctx context.Context) []*entry {
	out := append([]*entry{}, h.native...)
	if h.bridge == nil {
		return out
	}
	seen := map[string]string{}
	for _, e := range out {
		seen[e.slug] = e.name
	}
	for _, e := range h.bridge.entries(ctx) {
		if other, ok := seen[e.slug]; ok {
			h.logf("plugins %q and %q share the slug %q; %q is not loaded", other, e.name, e.slug, e.name)
			continue
		}
		seen[e.slug] = e.name
		out = append(out, e)
	}
	return out
}

func (h *Host) find(ctx context.Context, slug string) *entry {
	for _, e := range h.entries(ctx) {
		if e.slug == slug {
			return e
		}
	}
	return nil
}

// active returns the plugin with this slug only when it is active.
func (h *Host) active(ctx context.Context, slug string) (*entry, error) {
	e := h.find(ctx, slug)
	if e == nil {
		return nil, nil
	}
	a, err := h.rows.activation(ctx, slug)
	if err != nil || a == nil || !a.IsActive {
		return nil, err
	}
	return e, nil
}

func (h *Host) activeEntries(ctx context.Context) ([]*entry, error) {
	all := h.entries(ctx)
	if len(all) == 0 {
		return nil, nil
	}
	acts, err := h.rows.activations(ctx)
	if err != nil {
		return nil, err
	}
	var out []*entry
	for _, e := range all {
		if acts[e.slug].IsActive {
			out = append(out, e)
		}
	}
	return out, nil
}

// Listing is a plugin as Escalated/Admin/Plugins/Index lists it.
type Listing struct {
	Slug        string         `json:"slug"`
	Name        string         `json:"name"`
	Description *string        `json:"description"`
	Version     string         `json:"version"`
	Author      *string        `json:"author"`
	AuthorURL   *string        `json:"author_url"`
	Homepage    *string        `json:"homepage"`
	IsActive    bool           `json:"is_active"`
	ActivatedAt *string        `json:"activated_at"`
	Source      string         `json:"source"`
	Provides    map[string]int `json:"provides"`
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func iso(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}

// List returns every registered plugin, active or not, in registration order.
func (h *Host) List(ctx context.Context) ([]Listing, error) {
	all := h.entries(ctx)
	acts := map[string]activation{}
	if len(all) > 0 {
		var err error
		if acts, err = h.rows.activations(ctx); err != nil {
			return nil, err
		}
	}
	out := make([]Listing, 0, len(all))
	for _, e := range all {
		a := acts[e.slug]
		version := e.manifest.Version
		if version == "" {
			version = "1.0.0"
		}
		menus := 0
		for _, p := range e.manifest.Pages {
			if p.Menu != nil && pageRoute(p) != "" {
				menus++
			}
		}
		l := Listing{
			Slug:        e.slug,
			Name:        e.name,
			Description: optional(e.manifest.Description),
			Version:     version,
			Author:      optional(e.manifest.Author),
			AuthorURL:   optional(e.manifest.Homepage),
			Homepage:    optional(e.manifest.Homepage),
			IsActive:    a.IsActive,
			Source:      e.source,
			Provides: map[string]int{
				"menu_items":      menus,
				"widgets":         len(e.manifest.Widgets),
				"page_components": len(e.manifest.Components),
			},
		}
		if a.IsActive {
			l.ActivatedAt = iso(a.ActivatedAt)
		}
		out = append(out, l)
	}
	return out, nil
}

// ErrUnknownPlugin is returned for a slug no registered plugin has.
var ErrUnknownPlugin = errors.New("plugin not found")

// ErrPackageManaged is returned when uninstalling a plugin the package
// manager installed.
var ErrPackageManaged = errors.New("plugin is managed by the package manager")

// Activate switches a plugin on: the row, then OnActivate (the runtime's
// activate), then plugin.activated. Activating an active plugin does nothing.
func (h *Host) Activate(ctx context.Context, slug string) error {
	e := h.find(ctx, slug)
	if e == nil {
		return ErrUnknownPlugin
	}
	a, err := h.rows.activation(ctx, slug)
	if err != nil {
		return err
	}
	if a != nil && a.IsActive {
		return nil
	}
	if err := h.rows.activate(ctx, slug, time.Now().UTC()); err != nil {
		return err
	}
	h.lifecycle(ctx, e, true)
	h.DispatchAction(ctx, "plugin.activated", map[string]any{"plugin": e.name, "slug": e.slug})
	return nil
}

// Deactivate switches a plugin off: plugin.deactivated while it is still
// active, OnDeactivate (the runtime's deactivate), then the row. Deactivating
// an inactive plugin does nothing.
func (h *Host) Deactivate(ctx context.Context, slug string) error {
	e := h.find(ctx, slug)
	if e == nil {
		return ErrUnknownPlugin
	}
	a, err := h.rows.activation(ctx, slug)
	if err != nil {
		return err
	}
	if a == nil || !a.IsActive {
		return nil
	}
	h.DispatchAction(ctx, "plugin.deactivated", map[string]any{"plugin": e.name, "slug": e.slug})
	h.lifecycle(ctx, e, false)
	return h.rows.deactivate(ctx, slug, time.Now().UTC())
}

// Uninstall fires plugin.uninstalling, deactivates the plugin and deletes its
// row. A registered plugin stays listed, inactive, because its code is in the
// build; its stored data is kept for a reinstall. A package-managed plugin is
// refused (ErrPackageManaged).
func (h *Host) Uninstall(ctx context.Context, slug string) error {
	e := h.find(ctx, slug)
	if e == nil {
		return ErrUnknownPlugin
	}
	if e.source == SourceComposer {
		return ErrPackageManaged
	}
	a, err := h.rows.activation(ctx, slug)
	if err != nil {
		return err
	}
	if a != nil && a.IsActive {
		h.DispatchAction(ctx, "plugin.uninstalling", map[string]any{"plugin": e.name, "slug": e.slug})
	}
	if err := h.Deactivate(ctx, slug); err != nil {
		return err
	}
	return h.rows.forget(ctx, slug)
}

// lifecycle calls OnActivate or OnDeactivate. A failure is logged with the
// plugin's name and undoes nothing.
func (h *Host) lifecycle(ctx context.Context, e *entry, activate bool) {
	hook := "OnDeactivate"
	if activate {
		hook = "OnActivate"
	}
	if e.sdk {
		method := "deactivate"
		if activate {
			method = "activate"
		}
		if _, err := h.bridge.call(ctx, scope{plugins: []string{e.name}, request: requestFrom(ctx)}, method, map[string]any{"plugin": e.name}, actionTimeout); err != nil {
			h.logf("plugin %s %s failed: %v", e.name, method, err)
		}
		return
	}
	var fn func(context.Context, *Context) error
	if activate {
		if a, ok := e.native.(Activator); ok {
			fn = a.OnActivate
		}
	} else if d, ok := e.native.(Deactivator); ok {
		fn = d.OnDeactivate
	}
	if fn == nil {
		return
	}
	if err := h.runNative(ctx, actionTimeout, func(c context.Context) error { return fn(c, h.context(e, requestFrom(ctx))) }); err != nil {
		h.logf("plugin %s %s failed: %v", e.name, hook, err)
	}
}

// runNative runs plugin code with a timeout, turning a panic into an error.
// A handler that overruns is abandoned: its goroutine finishes on its own.
func (h *Host) runNative(ctx context.Context, timeout time.Duration, fn func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	done := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- fmt.Errorf("panic: %v", r)
			}
		}()
		done <- fn(ctx)
	}()
	select {
	case err := <-done:
		cancel()
		return err
	case <-ctx.Done():
		cancel()
		return fmt.Errorf("timed out after %s", timeout)
	}
}

// DispatchAction runs hook on every active plugin that handles it, side by
// side, and returns when they finish or time out (30 s). A failing or slow
// handler is logged and never reaches the caller. Domain events call it in
// the background.
func (h *Host) DispatchAction(ctx context.Context, hook string, event map[string]any) {
	if h == nil {
		return
	}
	active, err := h.activeEntries(ctx)
	if err != nil {
		h.logf("action %s: %v", hook, err)
		return
	}
	payload, err := roundTrip(event)
	if err != nil {
		h.logf("action %s: payload: %v", hook, err)
		return
	}
	var wg sync.WaitGroup
	var sdkNames []string
	sdkHandles := false
	for _, e := range active {
		if e.sdk {
			sdkNames = append(sdkNames, e.name)
			if e.actionHooks[hook] {
				sdkHandles = true
			}
			continue
		}
		fn := e.actions[hook]
		if fn == nil {
			continue
		}
		wg.Add(1)
		go func(e *entry, fn ActionFunc) {
			defer wg.Done()
			ev, _ := roundTrip(payload)
			m := asRecord(ev)
			if m == nil {
				m = map[string]any{}
			}
			if err := h.runNative(ctx, actionTimeout, func(c context.Context) error { return fn(c, m, h.context(e, requestFrom(ctx))) }); err != nil {
				h.logf("plugin %s action %s failed: %v", e.name, hook, err)
			}
		}(e, fn)
	}
	if sdkHandles {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := h.bridge.call(ctx, scope{plugins: sdkNames, request: requestFrom(ctx)}, "action", map[string]any{"hook": hook, "event": payload, "plugins": sdkNames}, actionTimeout); err != nil {
				h.logf("action %s: runtime: %v", hook, err)
			}
		}()
	}
	wg.Wait()
}

// ApplyFilter passes value through active plugins' filters for hook, lowest
// priority first, and returns the result. A filter that fails or takes longer
// than 5 s is skipped and the value carries on. The runtime applies its SDK
// plugins' filters in one step, at the priority of the lowest of them.
func (h *Host) ApplyFilter(ctx context.Context, hook string, value any) any {
	if h == nil {
		return value
	}
	active, err := h.activeEntries(ctx)
	if err != nil {
		h.logf("filter %s: %v", hook, err)
		return value
	}
	type step struct {
		priority int
		e        *entry
		fn       FilterFunc
		sdk      bool
	}
	var steps []step
	var sdkNames []string
	sdkPriority, sdkHandles := 0, false
	for _, e := range active {
		if e.sdk {
			sdkNames = append(sdkNames, e.name)
			if p, ok := e.sdkFilters[hook]; ok {
				if !sdkHandles || p < sdkPriority {
					sdkPriority = p
				}
				sdkHandles = true
			}
			continue
		}
		for _, f := range e.filters {
			if f.hook == hook {
				steps = append(steps, step{priority: f.priority, e: e, fn: f.handler})
			}
		}
	}
	if sdkHandles {
		steps = append(steps, step{priority: sdkPriority, sdk: true})
	}
	sort.SliceStable(steps, func(i, j int) bool { return steps[i].priority < steps[j].priority })

	current := value
	for _, s := range steps {
		if s.sdk {
			res, err := h.bridge.call(ctx, scope{plugins: sdkNames, request: requestFrom(ctx)}, "filter", map[string]any{"hook": hook, "value": current, "plugins": sdkNames}, filterTimeout)
			if err != nil {
				h.logf("filter %s: runtime: %v", hook, err)
				continue
			}
			current = decodeJSON(res)
			continue
		}
		input := current
		var out any
		err := h.runNative(ctx, filterTimeout, func(c context.Context) error {
			v, err := s.fn(c, input, h.context(s.e, requestFrom(ctx)))
			out = v
			return err
		})
		if err != nil {
			h.logf("plugin %s filter %s failed: %v", s.e.name, hook, err)
			continue
		}
		current = out
	}
	return current
}

// allows reports whether the request's user holds capability. No capability
// means anyone the route already lets in.
func (h *Host) allows(r *http.Request, capability string) bool {
	if capability == "" {
		return true
	}
	if r == nil {
		return false
	}
	if h.capability != nil {
		return h.capability(r, capability)
	}
	return h.adminCheck(r)
}

type requestKey struct{}

// withRequest remembers the request a dispatch serves, for the acting user
// and ctx.currentUser.
func withRequest(ctx context.Context, r *http.Request) context.Context {
	if r == nil {
		return ctx
	}
	return context.WithValue(ctx, requestKey{}, r)
}

func requestFrom(ctx context.Context) *http.Request {
	r, _ := ctx.Value(requestKey{}).(*http.Request)
	return r
}

// ContextFor returns the ctx of the plugin with this slug, active or not, for
// host code that seeds or reads a plugin's config and store. It acts for no
// request.
func (h *Host) ContextFor(slug string) (*Context, error) {
	e := h.find(context.Background(), slug)
	if e == nil {
		return nil, ErrUnknownPlugin
	}
	return h.context(e, nil), nil
}
