package escalated

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/escalated-dev/escalated-go/access"
	"github.com/escalated-dev/escalated-go/renderer"
)

// Embedding the admin in the host's own admin panel (ADR
// 2026-11-05-embeddable-admin). Optional and off by default: while
// Config.UI.Embed.Enabled is false nothing in this file changes a route, a
// prop, a header or a gate. See docs/embedding-admin.md.

// Embed modes.
const (
	// EmbedModeLayout renders the embedded admin inside the host's own layout:
	// a Vue + Inertia host layout, or a server-rendered root view.
	EmbedModeLayout = "layout"
	// EmbedModeIframe renders it chrome-free for an iframe on an allow-listed
	// origin.
	EmbedModeIframe = "iframe"
)

// DefaultEmbedPrefix is where the admin moves to when EmbedConfig.Prefix is
// empty.
const DefaultEmbedPrefix = "admin/support"

// UIConfig groups the UI options (escalated.ui in escalated-laravel).
type UIConfig struct {
	// Embed moves the admin into the host's own admin panel. Off by default.
	Embed EmbedConfig
}

// EmbedConfig is escalated.ui.embed. Every field is optional; with Enabled
// false (the default) none of them has any effect.
//
// Zero values read their environment variables when New runs:
// ESCALATED_EMBED (Enabled), ESCALATED_EMBED_MODE (Mode),
// ESCALATED_EMBED_PREFIX (Prefix) and ESCALATED_EMBED_FRAME_ANCESTORS
// (FrameAncestors, comma-separated).
type EmbedConfig struct {
	// Enabled is the master switch.
	Enabled bool

	// Mode is EmbedModeLayout (the default) or EmbedModeIframe.
	Mode string

	// Prefix is the URL path the admin routes move under, instead of
	// {RoutePrefix}/admin. Default DefaultEmbedPrefix ("admin/support").
	Prefix string

	// Middleware wraps the embedded admin routes, outermost first: the
	// host's own authentication and authorization for its admin panel. Nil
	// adds none, so the embedded admin runs what the standalone admin runs
	// (whatever wraps the router, plus Escalated's own gates).
	Middleware []func(http.Handler) http.Handler

	// ReplaceAdminGate drops Escalated's admin gate (AdminCheck) from the
	// embedded admin routes, so Middleware alone decides who reaches them.
	// Two-factor and route permissions still apply. Only set it when
	// Middleware restricts the routes to admins.
	ReplaceAdminGate bool

	// Layout names the host layout the frontend wraps admin pages in (the
	// EscalatedPlugin `layouts` map key), shared as escalated.embed.layout.
	Layout string

	// RootView is the path of an html/template root view used for embedded
	// admin responses only: the host's admin chrome around the Inertia mount
	// point. Empty keeps the renderer's root template. It is given the view
	// data escalatedNavigation (Escalated.AdminNavigation) beside page.
	RootView string

	// FrameAncestors are the origins allowed to frame the admin in iframe
	// mode: exact http(s) origins (https://admin.example.com). `*`,
	// wildcards, paths and anything else are dropped.
	FrameAncestors []string
}

// EmbedAdmin turns embedding on with opts: config and the call are
// equivalent. Call it on the Config before New.
func (c *Config) EmbedAdmin(opts EmbedConfig) {
	opts.Enabled = true
	c.UI.Embed = opts
}

// resolveEmbed applies the environment and the defaults.
func resolveEmbed(e EmbedConfig) EmbedConfig {
	if !e.Enabled {
		switch strings.ToLower(strings.TrimSpace(os.Getenv("ESCALATED_EMBED"))) {
		case "1", "true", "on", "yes":
			e.Enabled = true
		}
	}
	if e.Mode == "" {
		e.Mode = strings.ToLower(strings.TrimSpace(os.Getenv("ESCALATED_EMBED_MODE")))
	}
	if e.Mode != EmbedModeIframe {
		e.Mode = EmbedModeLayout
	}
	if e.Prefix == "" {
		e.Prefix = os.Getenv("ESCALATED_EMBED_PREFIX")
	}
	e.Prefix = strings.Trim(strings.TrimSpace(e.Prefix), "/")
	if e.Prefix == "" {
		e.Prefix = DefaultEmbedPrefix
	}
	if e.FrameAncestors == nil {
		if env := os.Getenv("ESCALATED_EMBED_FRAME_ANCESTORS"); env != "" {
			e.FrameAncestors = strings.Split(env, ",")
		}
	}
	origins := []string{}
	for _, entry := range e.FrameAncestors {
		if origin, ok := exactOrigin(entry); ok && !containsString(origins, origin) {
			origins = append(origins, origin)
		}
	}
	e.FrameAncestors = origins
	return e
}

// exactOrigin is value as scheme://host[:port] when it is exactly an http(s)
// origin: no wildcard, path, query, fragment or credentials.
func exactOrigin(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" || strings.Contains(value, "*") {
		return "", false
	}
	u, err := url.Parse(value)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return "", false
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", false
	}
	if u.Path != "" && u.Path != "/" {
		return "", false
	}
	return scheme + "://" + strings.ToLower(u.Host), true
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// Embedding reports whether the admin is embedded in the host's admin panel.
func (e *Escalated) Embedding() bool { return e.Config.UI.Embed.Enabled }

// AdminBase is the URL path of the admin: "/admin/support" (the embed
// prefix) while embedding is on, else {RoutePrefix}/admin.
func (e *Escalated) AdminBase() string {
	if e.Embedding() {
		return "/" + e.Config.UI.Embed.Prefix
	}
	return e.standaloneAdmin()
}

func (e *Escalated) standaloneAdmin() string {
	return strings.TrimRight(e.Config.RoutePrefix, "/") + "/admin"
}

// unmovedAdminPaths stay at {RoutePrefix}/admin while embedding is on: the
// two-factor enrolment page (personal, staff-wide) and the newsletter admin.
var unmovedAdminPaths = []string{"/settings/two-factor", "/newsletters"}

func unmovedAdminPath(rest string) bool {
	for _, p := range unmovedAdminPaths {
		if rest == p || strings.HasPrefix(rest, p+"/") {
			return true
		}
	}
	return false
}

// StandaloneAdminPath maps a path under the embed prefix to the standalone
// admin path the routers serve it from ({RoutePrefix}/admin/...). False for
// a path outside the embed prefix, for the routes that do not move, and
// while embedding is off.
func (e *Escalated) StandaloneAdminPath(embedded string) (string, bool) {
	if !e.Embedding() {
		return "", false
	}
	base := e.AdminBase()
	rest, ok := strings.CutPrefix(embedded, base)
	if !ok || (rest != "" && !strings.HasPrefix(rest, "/")) || unmovedAdminPath(rest) {
		return "", false
	}
	return e.standaloneAdmin() + rest, true
}

// embeddedAdminURL maps a standalone admin path to its embedded one.
func (e *Escalated) embeddedAdminURL(path string) (string, bool) {
	if !e.Embedding() {
		return "", false
	}
	rest, ok := strings.CutPrefix(path, e.standaloneAdmin())
	if !ok || (rest != "" && !strings.HasPrefix(rest, "/")) || unmovedAdminPath(rest) {
		return "", false
	}
	return e.AdminBase() + rest, true
}

type embedKey struct{}

// embedState marks a request served by an embedded admin route.
type embedState struct {
	// internal is set on the request the routers see (the standalone path).
	internal bool
	// original is the request's URL under the embed prefix.
	original *url.URL
}

func embedFrom(ctx context.Context) *embedState {
	s, _ := ctx.Value(embedKey{}).(*embedState)
	return s
}

// EmbeddedRequest reports whether r is served by an embedded admin route.
func EmbeddedRequest(r *http.Request) bool {
	return embedFrom(r.Context()) != nil
}

// requestPath is the path the browser asked for: the embedded one on an
// embedded admin request.
func requestPath(r *http.Request) string {
	if s := embedFrom(r.Context()); s != nil {
		return s.original.Path
	}
	return r.URL.Path
}

// EmbeddedAdminHandler serves the admin under the embed prefix: the host's
// Middleware runs on the request as the browser sent it, then the request is
// handed to dispatch (the routers' Escalated mount) at its standalone path,
// marked as embedded. In iframe mode the framing headers are set on the
// response after everything else, host middleware included. The routers
// mount it at the embed prefix; hosts do not need to call it.
func (e *Escalated) EmbeddedAdminHandler(dispatch http.Handler) http.Handler {
	cfg := e.Config.UI.Embed
	var h http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		internal, ok := e.StandaloneAdminPath(r.URL.Path)
		if !ok {
			http.NotFound(w, r)
			return
		}
		original := *r.URL
		ctx := context.WithValue(r.Context(), embedKey{}, &embedState{internal: true, original: &original})
		if cfg.RootView != "" {
			ctx = renderer.WithRootView(ctx, cfg.RootView)
		}
		in := r.WithContext(ctx)
		u := *r.URL
		u.Path = internal
		u.RawPath = ""
		in.URL = &u
		// The renderer's root view lists the admin menu.
		in = in.WithContext(renderer.WithViewData(in.Context(), "escalatedNavigation", func() any { return e.AdminNavigation(in) }))
		dispatch.ServeHTTP(w, in)
	})
	for i := len(cfg.Middleware) - 1; i >= 0; i-- {
		h = cfg.Middleware[i](h)
	}
	if cfg.Mode == EmbedModeIframe {
		h = e.frameHeaders(h)
	}
	return h
}

// frameHeaders allows the configured origins to frame the response and
// drops X-Frame-Options, as the response's headers are written: after the
// host middleware it wraps. A CSP already on the response is kept and
// frame-ancestors is added as a second policy.
func (e *Escalated) frameHeaders(next http.Handler) http.Handler {
	directive := strings.TrimSpace("frame-ancestors 'self' " + strings.Join(e.Config.UI.Embed.FrameAncestors, " "))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(&headerHook{ResponseWriter: w, hook: func(h http.Header) {
			h.Del("X-Frame-Options")
			h.Add("Content-Security-Policy", directive)
		}}, r)
	})
}

// EmbedRouting is the routers' middleware on the Escalated mount while
// embedding is on: the standalone admin URLs answer 404 (except the
// routes that do not move), and a redirect to a standalone admin URL is
// sent to its embedded one. It does nothing while embedding is off; the
// routers only install it while it is on.
func (e *Escalated) EmbedRouting(next http.Handler) http.Handler {
	if !e.Embedding() {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s := embedFrom(r.Context()); s == nil || !s.internal {
			if _, moved := e.embeddedAdminURL(r.URL.Path); moved {
				http.NotFound(w, r)
				return
			}
		}
		next.ServeHTTP(&headerHook{ResponseWriter: w, hook: func(h http.Header) {
			for _, key := range []string{"Location", "X-Inertia-Location"} {
				if v := h.Get(key); v != "" {
					h.Set(key, e.rewriteLocation(r, v))
				}
			}
		}}, r)
	})
}

// rewriteLocation sends a redirect to a standalone admin URL to its
// embedded one.
func (e *Escalated) rewriteLocation(r *http.Request, location string) string {
	u, err := url.Parse(location)
	if err != nil || (u.Host != "" && u.Host != r.Host) {
		return location
	}
	embedded, ok := e.embeddedAdminURL(u.Path)
	if !ok {
		return location
	}
	u.Path = embedded
	u.RawPath = ""
	return u.String()
}

// AdminGate is the admin gate for the admin routes: gate, unless the host
// replaced it (EmbedConfig.ReplaceAdminGate) and the request is an embedded
// admin request. The routers use it only while embedding is on.
func (e *Escalated) AdminGate(gate func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	if !e.Embedding() || !e.Config.UI.Embed.ReplaceAdminGate {
		return gate
	}
	return func(next http.Handler) http.Handler {
		gated := gate(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if EmbeddedRequest(r) {
				next.ServeHTTP(w, r)
				return
			}
			gated.ServeHTTP(w, r)
		})
	}
}

// EmbedShareMiddleware shares escalated.embed and escalated.navigation with
// every page rendered for the request while embedding is on, and does
// nothing while it is off. The routers install it on Escalated's pages; wrap
// your own Inertia pages with it to list the admin menu in your layout.
func (e *Escalated) EmbedShareMiddleware(next http.Handler) http.Handler {
	if !e.Embedding() {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := renderer.WithShared(r.Context(), "escalated", func() any {
			return map[string]any{
				"embed":      e.embedProp(r),
				"navigation": e.AdminNavigation(r),
			}
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// embedProp is escalated.embed for the request.
func (e *Escalated) embedProp(r *http.Request) map[string]any {
	cfg := e.Config.UI.Embed
	embedded := EmbeddedRequest(r)
	var layout, route any
	if cfg.Layout != "" {
		layout = cfg.Layout
	}
	if embedded {
		if name, ok := e.routeName(r.Method, requestPath(r)); ok {
			route = name
		}
	}
	origins := []string{}
	if cfg.Mode == EmbedModeIframe {
		origins = append(origins, cfg.FrameAncestors...)
	}
	return map[string]any{
		"enabled":         embedded,
		"mode":            cfg.Mode,
		"layout":          layout,
		"route":           route,
		"base_url":        e.AdminBase(),
		"allowed_origins": origins,
	}
}

// headerHook runs hook on the headers just before they are written.
type headerHook struct {
	http.ResponseWriter
	hook  func(http.Header)
	wrote bool
}

func (h *headerHook) WriteHeader(code int) {
	if !h.wrote {
		h.wrote = true
		h.hook(h.Header())
	}
	h.ResponseWriter.WriteHeader(code)
}

func (h *headerHook) Write(b []byte) (int, error) {
	if !h.wrote {
		h.WriteHeader(http.StatusOK)
	}
	return h.ResponseWriter.Write(b)
}

func (h *headerHook) Flush() {
	if !h.wrote {
		h.WriteHeader(http.StatusOK)
	}
	if f, ok := h.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (h *headerHook) Unwrap() http.ResponseWriter { return h.ResponseWriter }

// permissionsFor are the signed-in user's permission slugs while roles &
// permissions is on; nil while it is off.
func (e *Escalated) permissionsFor(r *http.Request) (map[string]bool, bool) {
	if !e.Features.RolesPermissions {
		return nil, false
	}
	have := map[string]bool{}
	uid := e.Config.UserIDFunc(r)
	if uid.Empty() || e.Roles() == nil {
		return have, true
	}
	slugs, err := e.Roles().PermissionsOf(r.Context(), uid)
	if err != nil {
		return have, true
	}
	for _, s := range slugs {
		have[s] = true
	}
	return have, true
}

// routeAllowed reports whether the user holds every permission the route
// at standalone path needs (access.RoutePermissions).
func routeAllowed(have map[string]bool, path string) bool {
	required, ok := access.Mapped(http.MethodGet, path)
	if !ok {
		return false
	}
	for _, slug := range required {
		if !have[slug] {
			return false
		}
	}
	return true
}
