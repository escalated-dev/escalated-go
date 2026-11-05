package router_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	escalated "github.com/escalated-dev/escalated-go"
	"github.com/escalated-dev/escalated-go/renderer"
	"github.com/escalated-dev/escalated-go/router"
)

// The embeddable admin (ADR 2026-11-05-embeddable-admin): optional, off by
// default. With it on, the admin moves under the host's prefix and
// middleware, pages carry escalated.embed and escalated.navigation, and
// iframe mode allows the configured origins to frame it.

const (
	hostHeader = "X-Host-Admin"
	hostDeny   = "X-Host-Deny"
	hostAllow  = "X-Host-Allow"
)

// hostAdmin is the host's own admin middleware: it marks what it served and
// refuses a request carrying X-Host-Deny.
func hostAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(hostDeny) != "" {
			http.Error(w, "host refused", http.StatusUnauthorized)
			return
		}
		w.Header().Set(hostHeader, "1")
		next.ServeHTTP(w, r)
	})
}

// hostCan is a host admin gate that alone decides who reaches the admin.
func hostCan(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(hostAllow) == "" {
			http.Error(w, "host gate", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func embedded(opts escalated.EmbedConfig) func(*escalated.Config) {
	return func(cfg *escalated.Config) { cfg.EmbedAdmin(opts) }
}

func layoutEmbed() escalated.EmbedConfig {
	return escalated.EmbedConfig{
		Prefix:     "admin/support",
		Middleware: []func(http.Handler) http.Handler{hostAdmin},
		Layout:     "Layouts/AdminLayout",
	}
}

func embedReq(h http.Handler, method, path, user, role string, headers map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, nil)
	r.Header.Set("X-Inertia", "true")
	if user != "" {
		r.Header.Set(headerUser, user)
	}
	if role != "" {
		r.Header.Set(headerRole, role)
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

type fullPage struct {
	Component string         `json:"component"`
	URL       string         `json:"url"`
	Props     map[string]any `json:"props"`
}

func pageFrom(t *testing.T, rec *httptest.ResponseRecorder) fullPage {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var page fullPage
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("not an Inertia page: %v: %s", err, rec.Body.String())
	}
	return page
}

func escalatedProp(t *testing.T, page fullPage) map[string]any {
	t.Helper()
	return obj(t, page.Props, "escalated")
}

func navKeys(t *testing.T, page fullPage) []string {
	t.Helper()
	var keys []string
	for _, item := range list(t, escalatedProp(t, page), "navigation") {
		keys = append(keys, item.(map[string]any)["key"].(string))
	}
	return keys
}

func navItem(t *testing.T, page fullPage, key string) map[string]any {
	t.Helper()
	for _, item := range list(t, escalatedProp(t, page), "navigation") {
		if m := item.(map[string]any); m["key"] == key {
			return m
		}
	}
	t.Fatalf("no navigation item %q in %v", key, navKeys(t, page))
	return nil
}

// Every core item, in order, for an admin with every optional screen on
// except roles and newsletters.
var defaultNav = []string{
	"reports", "tickets", "departments", "sla-policies", "knowledge-base", "escalation-rules",
	"workflows", "automations", "webhooks", "tags", "custom-fields", "statuses", "canned-responses",
	"macros", "skills", "users", "capacity", "business-hours", "audit-log", "api-tokens",
	"custom-objects", "plugins", "import", "settings",
}

func TestEmbedOffChangesNoPropOrHeader(t *testing.T) {
	f := newUIFixture(t)
	if f.esc.Embedding() || f.esc.Config.UI.Embed.Enabled {
		t.Fatal("embedding is on by default")
	}
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			for _, path := range []string{"/escalated/admin/departments", "/escalated/agent/tickets"} {
				rec := embedReq(h, http.MethodGet, path, uiAgent, "admin", nil)
				page := pageFrom(t, rec)
				esc := escalatedProp(t, page)
				for _, key := range []string{"embed", "navigation"} {
					if _, ok := esc[key]; ok {
						t.Errorf("%s: escalated.%s shared while embedding is off", path, key)
					}
				}
				if v := rec.Header().Values("Content-Security-Policy"); len(v) != 0 {
					t.Errorf("%s: CSP %v", path, v)
				}
				if page.URL != path {
					t.Errorf("%s: page url %q", path, page.URL)
				}
			}
			// The standalone admin keeps its gate.
			if rec := embedReq(h, http.MethodGet, "/escalated/admin/departments", uiAgent, "agent", nil); rec.Code != http.StatusForbidden {
				t.Errorf("non-admin: %d", rec.Code)
			}
			if rec := embedReq(h, http.MethodGet, "/admin/support/departments", uiAgent, "admin", nil); rec.Code != http.StatusNotFound {
				t.Errorf("embed prefix answered %d while embedding is off", rec.Code)
			}
		})
	}
}

func TestEmbedConfigDefaultsAndEnvironment(t *testing.T) {
	f := newUIFixture(t, embedded(escalated.EmbedConfig{}))
	got := f.esc.Config.UI.Embed
	if !got.Enabled || got.Mode != escalated.EmbedModeLayout || got.Prefix != "admin/support" || got.ReplaceAdminGate || len(got.FrameAncestors) != 0 {
		t.Errorf("defaults: %+v", got)
	}

	t.Setenv("ESCALATED_EMBED", "true")
	t.Setenv("ESCALATED_EMBED_MODE", "iframe")
	t.Setenv("ESCALATED_EMBED_PREFIX", "/backoffice/help/")
	t.Setenv("ESCALATED_EMBED_FRAME_ANCESTORS", "https://admin.example.com, *, https://*.example.org, https://Admin.Example.com, ftp://x.example, https://b.example/path, https://c.example:8443")
	f = newUIFixture(t)
	got = f.esc.Config.UI.Embed
	want := []string{"https://admin.example.com", "https://c.example:8443"}
	if !got.Enabled || got.Mode != escalated.EmbedModeIframe || got.Prefix != "backoffice/help" || !reflect.DeepEqual(got.FrameAncestors, want) {
		t.Errorf("from the environment: %+v", got)
	}
	if f.esc.AdminBase() != "/backoffice/help" {
		t.Errorf("admin base %q", f.esc.AdminBase())
	}
}

func TestEmbeddedAdminMovesUnderTheHostPrefix(t *testing.T) {
	f := newUIFixture(t, embedded(layoutEmbed()))
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			rec := embedReq(h, http.MethodGet, "/admin/support/departments", uiAgent, "admin", nil)
			page := pageFrom(t, rec)
			if page.Component != "Escalated/Admin/Departments/Index" {
				t.Errorf("component %q", page.Component)
			}
			if page.URL != "/admin/support/departments" {
				t.Errorf("page url %q", page.URL)
			}
			if rec.Header().Get(hostHeader) != "1" {
				t.Error("the host middleware did not run")
			}
			// Layout mode adds no framing header.
			if v := rec.Header().Values("Content-Security-Policy"); len(v) != 0 {
				t.Errorf("layout mode CSP %v", v)
			}

			// The host middleware can refuse.
			if rec := embedReq(h, http.MethodGet, "/admin/support/departments", uiAgent, "admin", map[string]string{hostDeny: "1"}); rec.Code != http.StatusUnauthorized {
				t.Errorf("host refusal: %d", rec.Code)
			}
			// Escalated's admin gate is kept.
			if rec := embedReq(h, http.MethodGet, "/admin/support/departments", uiAgent, "agent", nil); rec.Code != http.StatusForbidden {
				t.Errorf("non-admin reached the embedded admin: %d", rec.Code)
			}

			// The standalone admin URLs are gone; what does not move stays.
			for _, path := range []string{"/escalated/admin/departments", "/escalated/admin/tickets", "/escalated/admin/plugins", "/escalated/admin/api-tokens", "/escalated/admin/import"} {
				if rec := embedReq(h, http.MethodGet, path, uiAgent, "admin", nil); rec.Code != http.StatusNotFound {
					t.Errorf("%s: %d, want 404", path, rec.Code)
				}
			}
			if rec := embedReq(h, http.MethodGet, "/admin/support/settings/two-factor", uiAgent, "admin", nil); rec.Code != http.StatusNotFound {
				t.Errorf("two-factor enrolment moved: %d", rec.Code)
			}
			agent := pageFrom(t, embedReq(h, http.MethodGet, "/escalated/agent/tickets", uiAgent, "admin", nil))
			if agent.Component != "Escalated/Agent/TicketIndex" {
				t.Errorf("agent component %q", agent.Component)
			}
			if obj(t, escalatedProp(t, agent), "embed")["enabled"] != false {
				t.Error("an agent page is marked embedded")
			}

			// A write answers with a redirect to the embedded URL.
			req := httptest.NewRequest(http.MethodPost, "/admin/support/departments", strings.NewReader(`{"name":"Escalations `+name+`"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Inertia", "true")
			req.Header.Set(headerUser, uiAgent)
			req.Header.Set(headerRole, "admin")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/admin/support/departments" {
				t.Errorf("write: %d to %q %s", w.Code, w.Header().Get("Location"), w.Body.String())
			}

			// The route() names follow the move; what does not move keeps its URI.
			routes := obj(t, obj(t, page.Props, "ziggy"), "routes")
			for routeName, uri := range map[string]string{
				"escalated.admin.departments.index": "admin/support/departments",
				"escalated.admin.tickets.show":      "admin/support/tickets/{ticket}",
				"escalated.admin.plugins.index":     "admin/support/plugins",
				"escalated.admin.api-tokens.index":  "admin/support/api-tokens",
				"escalated.admin.two-factor.index":  "escalated/admin/settings/two-factor",
				"escalated.agent.tickets.index":     "escalated/agent/tickets",
				"escalated.admin.chat.end":          "escalated/agent/chat/{session}/end",
			} {
				if got := obj(t, routes, routeName)["uri"]; got != uri {
					t.Errorf("ziggy %s: %v, want %s", routeName, got, uri)
				}
			}
			if loc := obj(t, page.Props, "ziggy")["location"]; !strings.HasSuffix(loc.(string), "/admin/support/departments") {
				t.Errorf("ziggy location %v", loc)
			}
		})
	}
}

// Every admin name the route list moves resolves, through the embed
// mapping, to a route the routers mount.
func TestEveryMovedRouteNameResolves(t *testing.T) {
	f := newUIFixture(t, embedded(layoutEmbed()))
	r := chi.NewRouter()
	router.MountChi(r, f.esc)
	param := regexp.MustCompile(`\{\w+\??\}`)
	moved := 0
	for name, route := range f.esc.RouteList() {
		if !strings.HasPrefix(route.URI, "admin/support/") && route.URI != "admin/support" {
			if strings.HasPrefix(name, "escalated.admin.") && strings.Contains(route.URI, "/admin/") && !strings.Contains(route.URI, "/two-factor") {
				t.Errorf("%s stayed at %s", name, route.URI)
			}
			continue
		}
		moved++
		internal, ok := f.esc.StandaloneAdminPath("/" + param.ReplaceAllString(route.URI, "1"))
		if !ok {
			t.Errorf("%s: %s does not map back", name, route.URI)
			continue
		}
		for _, method := range route.Methods {
			if method != http.MethodHead && !r.Match(chi.NewRouteContext(), method, internal) {
				t.Errorf("%s: %s %s is not mounted", name, method, internal)
			}
		}
	}
	if moved < 150 {
		t.Errorf("%d names moved, want every admin route", moved)
	}
}

func TestEmbedPropShape(t *testing.T) {
	f := newUIFixture(t, embedded(layoutEmbed()))
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			page := pageFrom(t, embedReq(h, http.MethodGet, "/admin/support/tickets/"+itoa(f.ticket.ID), uiAgent, "admin", nil))
			got := obj(t, escalatedProp(t, page), "embed")
			want := map[string]any{
				"enabled":         true,
				"mode":            "layout",
				"layout":          "Layouts/AdminLayout",
				"route":           "escalated.admin.tickets.show",
				"base_url":        "/admin/support",
				"allowed_origins": []any{},
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("embed %v, want %v", got, want)
			}
			// The rest of the shared prop is untouched.
			hasKeys(t, "escalated", escalatedProp(t, page), "features", "permissions", "plugins", "navigation")

			customer := pageFrom(t, embedReq(h, http.MethodGet, "/escalated/agent/tickets", uiAgent, "admin", nil))
			off := obj(t, escalatedProp(t, customer), "embed")
			if off["enabled"] != false || off["route"] != nil || off["base_url"] != "/admin/support" {
				t.Errorf("non-embedded page embed %v", off)
			}
		})
	}
}

func TestNavigationShapeOrderAndActive(t *testing.T) {
	f := newUIFixture(t, embedded(layoutEmbed()))
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			page := pageFrom(t, embedReq(h, http.MethodGet, "/admin/support/departments/1/edit", uiAgent, "admin", nil))
			if got := navKeys(t, page); !reflect.DeepEqual(got, defaultNav) {
				t.Errorf("navigation %v\nwant %v", got, defaultNav)
			}
			dept := navItem(t, page, "departments")
			want := map[string]any{
				"key": "departments", "label": "Departments", "route": "escalated.admin.departments.index",
				"url": "/admin/support/departments", "icon": "departments", "position": float64(30),
				"active": true, "badge": nil, "plugin": nil, "children": []any{},
			}
			if !reflect.DeepEqual(dept, want) {
				t.Errorf("item %v\nwant %v", dept, want)
			}
			if navItem(t, page, "automations")["position"] != 53.5 {
				t.Error("automations position")
			}
			for _, key := range defaultNav {
				if key != "departments" && navItem(t, page, key)["active"] != false {
					t.Errorf("%s is active", key)
				}
			}

			// Shared on every page, the agent panel's included; empty for a
			// user who fails the admin gate.
			agentPage := pageFrom(t, embedReq(h, http.MethodGet, "/escalated/agent/tickets", uiAgent, "admin", nil))
			if len(navKeys(t, agentPage)) != len(defaultNav) {
				t.Error("navigation missing on the agent panel")
			}
			agentOnly := pageFrom(t, embedReq(h, http.MethodGet, "/escalated/agent/tickets", uiAgent, "agent", nil))
			if got := list(t, escalatedProp(t, agentOnly), "navigation"); len(got) != 0 {
				t.Errorf("non-admin navigation %v", got)
			}
		})
	}
}

func TestNavigationFeatureAndPermissionFiltering(t *testing.T) {
	f := newRolesFixture(t, rolesOn(true), embedded(layoutEmbed()))
	f.customRole(t, staffNoRole, "department.view", "ticket.view", "report.view")
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			// The admin role holds every permission: Roles is listed while
			// roles & permissions is on.
			admin := pageFrom(t, embedReq(h, http.MethodGet, "/admin/support/departments", uiAgent, "admin", nil))
			keys := navKeys(t, admin)
			if !containsKey(keys, "roles") || !containsKey(keys, "settings") {
				t.Errorf("admin navigation %v", keys)
			}

			// A user lacking settings.view loses Settings, and everything
			// else they cannot open.
			limited := pageFrom(t, embedReq(h, http.MethodGet, "/admin/support/departments", staffNoRole, "admin", nil))
			if got, want := navKeys(t, limited), []string{"reports", "tickets", "departments"}; !reflect.DeepEqual(got, want) {
				t.Errorf("limited navigation %v, want %v", got, want)
			}
			if rec := embedReq(h, http.MethodGet, "/admin/support/settings", staffNoRole, "admin", nil); rec.Code != http.StatusForbidden {
				t.Errorf("route permissions not enforced on the embedded admin: %d", rec.Code)
			}
		})
	}

	// Newsletters are listed while they are on, at their unmoved URL.
	news := newUIFixture(t, embedded(layoutEmbed()), func(cfg *escalated.Config) { cfg.EnableNewsletters = true })
	r := chi.NewRouter()
	router.MountChi(r, news.esc)
	page := pageFrom(t, embedReq(r, http.MethodGet, "/admin/support/departments", uiAgent, "admin", nil))
	item := navItem(t, page, "newsletters")
	if item["url"] != "/escalated/admin/newsletters" || item["route"] != nil || item["position"] != float64(81) {
		t.Errorf("newsletters item %v", item)
	}
	if rec := embedReq(r, http.MethodGet, "/escalated/admin/newsletters", uiAgent, "admin", nil); rec.Code != http.StatusOK {
		t.Errorf("newsletter admin moved: %d", rec.Code)
	}
}

func containsKey(keys []string, key string) bool {
	for _, k := range keys {
		if k == key {
			return true
		}
	}
	return false
}

func TestReplaceAdminGateLetsTheHostDecide(t *testing.T) {
	opts := layoutEmbed()
	opts.Middleware = []func(http.Handler) http.Handler{hostCan}
	opts.ReplaceAdminGate = true
	f := newUIFixture(t, embedded(opts))
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			// Not an Escalated admin, allowed by the host: in.
			page := pageFrom(t, embedReq(h, http.MethodGet, "/admin/support/departments", uiAgent, "agent", map[string]string{hostAllow: "1"}))
			if got := navKeys(t, page); !reflect.DeepEqual(got, defaultNav) {
				t.Errorf("navigation for the host's admin %v", got)
			}
			// An Escalated admin the host refuses: out.
			if rec := embedReq(h, http.MethodGet, "/admin/support/departments", uiAgent, "admin", nil); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "host gate") {
				t.Errorf("host gate: %d %s", rec.Code, rec.Body.String())
			}
			// The agent panel keeps Escalated's gates.
			if rec := embedReq(h, http.MethodGet, "/escalated/agent/tickets", "", "", map[string]string{hostAllow: "1"}); rec.Code != http.StatusForbidden && rec.Code != http.StatusUnauthorized {
				t.Errorf("agent panel opened: %d", rec.Code)
			}
		})
	}
}

func TestEmbeddedPluginPageAndMenuItem(t *testing.T) {
	f := newPluginFixture(t, embedded(layoutEmbed()))
	if err := f.esc.Plugins.Activate(context.Background(), crmSlug); err != nil {
		t.Fatalf("activate: %v", err)
	}
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			page := pageFrom(t, preq(h, http.MethodGet, "/admin/support/plugins/"+crmSlug+"/settings", "admin", "", inertiaHeaders))
			item := navItem(t, fullPage{Props: page.Props}, "plugin:"+crmSlug+":0")
			want := map[string]any{
				"key": "plugin:" + crmSlug + ":0", "label": "CRM", "route": nil,
				"url": "/admin/support/plugins/" + crmSlug + "/settings", "icon": nil, "position": float64(100),
				"active": true, "badge": nil, "plugin": crmSlug, "children": []any{},
			}
			if !reflect.DeepEqual(item, want) {
				t.Errorf("plugin item %v\nwant %v", item, want)
			}
			// Sorted by position: the plugin item sits after settings (100,
			// stable) and before nothing else.
			keys := navKeys(t, fullPage{Props: page.Props})
			if keys[len(keys)-1] != "plugin:"+crmSlug+":0" || keys[len(keys)-2] != "settings" {
				t.Errorf("order %v", keys)
			}
			menu := list(t, obj(t, escalatedProp(t, fullPage{Props: page.Props}), "plugins"), "menuItems")
			if first(t, menu, "menuItems")["route"] != "/admin/support/plugins/"+crmSlug+"/settings" {
				t.Errorf("plugin menu item %v", menu)
			}
			if rec := preq(h, http.MethodGet, "/escalated/admin/plugins/"+crmSlug+"/settings", "admin", "", inertiaHeaders); rec.Code != http.StatusNotFound {
				t.Errorf("standalone plugin page: %d", rec.Code)
			}
			// The lifecycle redirect goes back to the embedded Plugins page.
			rec := preq(h, http.MethodPost, "/admin/support/plugins/"+crmSlug+"/deactivate", "admin", "", inertiaHeaders)
			if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin/support/plugins" {
				t.Errorf("lifecycle: %d to %q", rec.Code, rec.Header().Get("Location"))
			}
			if err := f.esc.Plugins.Activate(context.Background(), crmSlug); err != nil {
				t.Fatalf("activate: %v", err)
			}
		})
	}
}

func TestRootViewOnlyOnEmbeddedResponses(t *testing.T) {
	opts := layoutEmbed()
	opts.RootView = "../testdata/embed/host-admin.html"
	f := newUIFixture(t, embedded(opts))
	f.esc.Renderer = renderer.NewInertiaRenderer("../testdata/embed/app.html")
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			full := func(path, role string) *httptest.ResponseRecorder {
				r := httptest.NewRequest(http.MethodGet, path, nil)
				r.Header.Set("Accept", "text/html")
				r.Header.Set(headerUser, uiAgent)
				r.Header.Set(headerRole, role)
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, r)
				return rec
			}
			rec := full("/admin/support/departments", "admin")
			body := rec.Body.String()
			if rec.Code != http.StatusOK || !strings.Contains(body, `class="host-header"`) {
				t.Fatalf("embedded page did not use the root view: %d %s", rec.Code, body)
			}
			if !strings.Contains(body, `<a href="/admin/support/departments" class="active" aria-current="page">Departments</a>`) {
				t.Errorf("root view menu missing the active Departments link:\n%s", body)
			}
			if !strings.Contains(body, `<a href="/admin/support/tickets">Tickets</a>`) {
				t.Errorf("root view menu missing Tickets:\n%s", body)
			}
			if !strings.Contains(body, "Escalated/Admin/Departments/Index") {
				t.Error("root view lacks the page")
			}

			rec = full("/escalated/agent/tickets", "admin")
			if body := rec.Body.String(); rec.Code != http.StatusOK || strings.Contains(body, "host-header") || !strings.Contains(body, "escalated-default-root") {
				t.Errorf("agent page root: %d %s", rec.Code, body)
			}
		})
	}
}

// outerFrameDeny is host middleware around the whole router that sends
// X-Frame-Options on every response, as many hosts do.
func outerFrameDeny(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'")
		next.ServeHTTP(w, r)
	})
}

func TestIframeModeFramingHeaders(t *testing.T) {
	opts := layoutEmbed()
	opts.Mode = escalated.EmbedModeIframe
	opts.FrameAncestors = []string{"https://allowed.example", "*", "https://*.example.org"}
	opts.Middleware = []func(http.Handler) http.Handler{hostAdmin, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Frame-Options", "SAMEORIGIN")
			next.ServeHTTP(w, r)
		})
	}}
	f := newUIFixture(t, embedded(opts))
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			h = outerFrameDeny(h)
			rec := embedReq(h, http.MethodGet, "/admin/support/departments", uiAgent, "admin", nil)
			page := pageFrom(t, rec)
			if v := rec.Header().Values("X-Frame-Options"); len(v) != 0 {
				t.Errorf("X-Frame-Options kept on the embedded response: %v", v)
			}
			csp := rec.Header().Values("Content-Security-Policy")
			if !reflect.DeepEqual(csp, []string{"default-src 'self'", "frame-ancestors 'self' https://allowed.example"}) {
				t.Errorf("CSP %v", csp)
			}
			embed := obj(t, escalatedProp(t, page), "embed")
			if embed["mode"] != "iframe" || !reflect.DeepEqual(embed["allowed_origins"], []any{"https://allowed.example"}) {
				t.Errorf("embed %v", embed)
			}

			// Other routes are untouched.
			rec = embedReq(h, http.MethodGet, "/escalated/agent/tickets", uiAgent, "admin", nil)
			if rec.Header().Get("X-Frame-Options") != "DENY" || len(rec.Header().Values("Content-Security-Policy")) != 1 {
				t.Errorf("agent page headers %v", rec.Header())
			}
		})
	}

	// No allow-list: 'self' only.
	opts.FrameAncestors = nil
	f = newUIFixture(t, embedded(opts))
	rec := embedReq(f.mounts()["chi"], http.MethodGet, "/admin/support/departments", uiAgent, "admin", nil)
	if got := rec.Header().Get("Content-Security-Policy"); got != "frame-ancestors 'self'" {
		t.Errorf("CSP %q", got)
	}
}

func TestAdminNavigationHelperWithoutEmbedding(t *testing.T) {
	f := newUIFixture(t)
	r := httptest.NewRequest(http.MethodGet, "/escalated/admin/tags", nil)
	r.Header.Set(headerUser, uiAgent)
	r.Header.Set(headerRole, "admin")
	items := f.esc.AdminNavigation(r)
	if len(items) != len(defaultNav) {
		t.Fatalf("%d items", len(items))
	}
	for _, item := range items {
		if item.Key == "tags" && (!item.Active || item.URL != "/escalated/admin/tags") {
			t.Errorf("tags %+v", item)
		}
		if item.Key == "reports" && item.URL != "/escalated/admin/reports" {
			t.Errorf("reports %+v", item)
		}
	}
	r.Header.Set(headerRole, "agent")
	if got := f.esc.AdminNavigation(r); len(got) != 0 {
		t.Errorf("non-admin %v", got)
	}
}

// Two-factor still challenges the embedded admin, returns to it after the
// challenge, and its cookies reach the embedded URLs.
func TestEmbeddedAdminKeepsTwoFactor(t *testing.T) {
	for _, name := range []string{"chi", "stdlib"} {
		t.Run(name, func(t *testing.T) {
			f := newUIFixture(t, twoFactorOn(true), embedded(layoutEmbed()))
			h := f.mounts()[name]
			secret, _ := enrol(t, f.esc, uiAgent)
			admin := newBrowser(h, uiAgent, "admin", "s1")
			wantLocation(t, admin.get("/admin/support/departments"), "embedded admin", "/escalated/two-factor/challenge")
			rec := admin.post("/escalated/two-factor/challenge", `{"code":"`+nowCode(secret)+`"}`)
			wantLocation(t, rec, "verify", "/admin/support/departments")
			for _, c := range rec.Result().Cookies() {
				if c.MaxAge >= 0 && !strings.HasPrefix("/admin/support/departments", c.Path) {
					t.Errorf("cookie %s has path %q, which the embedded admin does not send", c.Name, c.Path)
				}
			}
			if rec := admin.get("/admin/support/departments"); rec.Code != http.StatusOK {
				t.Errorf("after passing: %d", rec.Code)
			}
		})
	}
}

// The import wizard keeps its credentials between steps in a cookie, which
// must reach the embedded wizard.
func TestEmbeddedImportWizardCookie(t *testing.T) {
	for _, name := range []string{"chi", "stdlib"} {
		t.Run(name, func(t *testing.T) {
			f, _ := newImportFixture(t, embedded(layoutEmbed()))
			c := &importClient{t: t, h: f.mounts()[name], role: "admin"}
			body, _ := json.Marshal(map[string]any{"credentials": map[string]string{"subdomain": "acme", "api_token": fakeToken}})
			rec := c.do(http.MethodPost, "/admin/support/import/fakedesk/test-connection", string(body))
			if rec.Code != http.StatusOK {
				t.Fatalf("test-connection: %d %s", rec.Code, rec.Body.String())
			}
			cookies := rec.Result().Cookies()
			if len(cookies) == 0 {
				t.Fatal("no credentials cookie")
			}
			for _, ck := range cookies {
				if ck.Path != "/admin/support/import" {
					t.Errorf("cookie %s path %q", ck.Name, ck.Path)
				}
			}
		})
	}
}

// docs/embedding-admin.md shows the root view the tests render.
func TestEmbeddingDocsShowTheTestedRootView(t *testing.T) {
	read := func(path string) string {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return strings.ReplaceAll(string(b), "\r\n", "\n")
	}
	if !strings.Contains(read("../docs/embedding-admin.md"), read("../testdata/embed/host-admin.html")) {
		t.Error("docs/embedding-admin.md does not show testdata/embed/host-admin.html verbatim")
	}
}
