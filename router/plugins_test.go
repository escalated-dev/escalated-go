package router_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"

	escalated "github.com/escalated-dev/escalated-go"
	"github.com/escalated-dev/escalated-go/internal/pluginruntimetest"
	"github.com/escalated-dev/escalated-go/internal/sqldialect"
	"github.com/escalated-dev/escalated-go/internal/testdb"
	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/plugins"
	"github.com/escalated-dev/escalated-go/router"
	"github.com/escalated-dev/escalated-go/services"
)

// The port test checklist of the plugin contract
// (escalated-developer-context domain-model/plugin-contract.md), through both
// routers: a native stand-in plugin registered in config, and SDK plugins
// served by a stand-in runtime (internal/pluginruntimetest) over the real
// os/exec bridge. This test binary is that runtime when TestMain sees
// pluginruntimetest.EnvRun.

func TestMain(m *testing.M) {
	if os.Getenv(pluginruntimetest.EnvRun) != "" {
		pluginruntimetest.Serve(os.Stdin, os.Stdout)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

const (
	crmName   = "@acme/escalated-plugin-crm"
	crmSlug   = "acme--escalated-plugin-crm"
	slackName = "@escalated-dev/plugin-slack"
	slackSlug = "escalated-dev--plugin-slack"
	jiraName  = "@escalated-dev/plugin-jira"
	jiraSlug  = "escalated-dev--plugin-jira"
	capHeader = "X-Test-Capabilities"
)

// sdkManifests is what the stand-in runtime reports, in the SDK's
// toManifest() shape.
const sdkManifests = `{
  "@escalated-dev/plugin-slack": {
    "name": "@escalated-dev/plugin-slack", "version": "0.3.0", "description": "Slack alerts",
    "config": [], "components": [],
    "widgets": [{"component": "Inbox", "label": "Inbox", "hasBadge": true},
                {"component": "Count", "label": "Count", "order": 2, "hasBadge": true},
                {"component": "Broken", "label": "Broken", "order": 3, "hasBadge": true},
                {"component": "Plain", "label": "Plain", "order": 4, "hasBadge": false}],
    "pages": [{"route": "settings", "component": "Settings", "menu": {"label": "Slack"}}],
    "actionHooks": ["ticket.created", "plugin.activated", "custom.ping"],
    "filterHooks": [{"hook": "subject", "priority": 15}],
    "endpoints": [{"method": "GET", "path": "/echo/:id"}, {"method": "POST", "path": "/echo/:id"},
                  {"method": "GET", "path": "/boom"}, {"method": "GET", "path": "/envelope"},
                  {"method": "GET", "path": "/config"}, {"method": "GET", "path": "/Settings"},
                  {"method": "GET", "path": "/spy"}, {"method": "POST", "path": "/ctx"},
                  {"method": "POST", "path": "/ctx-together"}],
    "webhooks": [{"method": "POST", "path": "/echo/:id"}],
    "cronSchedules": ["* * * * *", "0 9 * * *", "not a schedule"]
  },
  "@escalated-dev/plugin-jira": {
    "name": "@escalated-dev/plugin-jira", "version": "0.1.0",
    "config": [], "pages": [], "components": [], "widgets": [],
    "actionHooks": ["ticket.created", "custom.ping"], "filterHooks": [],
    "endpoints": [{"method": "GET", "path": "/echo/:id"}], "webhooks": [], "cronSchedules": ["* * * * *"]
  }
}`

// recorder keeps what the native stand-in saw, in order.
type recorder struct {
	mu     sync.Mutex
	events []string
}

func (r *recorder) add(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, s)
}

func (r *recorder) take() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.events
	r.events = nil
	return out
}

// crmPlugin is the native stand-in: a plugin a host writes in Go.
type crmPlugin struct {
	rec            *recorder
	failActivate   bool
	panicOnCreated bool
	// badgeFails makes CrmWidget's badge "error", "panic" or "slow".
	badgeFails string
}

func (p *crmPlugin) Manifest() plugins.Manifest {
	return plugins.Manifest{
		Name:        crmName,
		Version:     "2.1.0",
		Description: "CRM sync",
		Author:      "Acme",
		Homepage:    "https://acme.example",
		Pages: []plugins.Page{
			{Route: "settings", Component: "CrmSettings", Menu: &plugins.Menu{Label: "CRM"}},
			{Route: "/secret", Component: "CrmSecret", Capability: "crm.secret"},
			{Route: "", Component: "Skipped"},
		},
		Components: []plugins.Component{{Page: "ticket.show", Slot: "sidebar", Component: "CrmCard"}},
		Widgets: []plugins.Widget{
			{Component: "CrmWidget", Label: "CRM"},
			{Component: "CrmForecast", Label: "Forecast", Size: "full", Order: 5, Capability: "crm.secret"},
		},
	}
}

func (p *crmPlugin) OnActivate(_ context.Context, _ *plugins.Context) error {
	p.rec.add("onActivate")
	if p.failActivate {
		return errors.New("activation callback failed")
	}
	return nil
}

func (p *crmPlugin) OnDeactivate(_ context.Context, _ *plugins.Context) error {
	p.rec.add("onDeactivate")
	return nil
}

func (p *crmPlugin) Actions() map[string]plugins.ActionFunc {
	record := func(hook string) plugins.ActionFunc {
		return func(_ context.Context, event map[string]any, _ *plugins.Context) error {
			p.rec.add("action:" + hook + ":" + jsonString(event))
			return nil
		}
	}
	actions := map[string]plugins.ActionFunc{
		"plugin.activated":    record("plugin.activated"),
		"plugin.deactivated":  record("plugin.deactivated"),
		"plugin.uninstalling": record("plugin.uninstalling"),
		"custom.ping":         record("custom.ping"),
		"ticket.created": func(ctx context.Context, event map[string]any, pc *plugins.Context) error {
			p.rec.add("action:ticket.created:" + jsonString(event))
			if p.panicOnCreated {
				panic("a plugin bug in ticket.created")
			}
			return nil
		},
		"custom.explode": func(context.Context, map[string]any, *plugins.Context) error {
			panic("a plugin bug")
		},
	}
	for _, name := range append(append([]string{}, services.SupportedWebhookEvents...), legacyNames()...) {
		if _, ok := actions[name]; !ok {
			actions[name] = record(name)
		}
	}
	return actions
}

// Cron runs on the host scheduler while the plugin is active.
func (p *crmPlugin) Cron() map[string]plugins.CronFunc {
	record := func(name string) plugins.CronFunc {
		return func(ctx context.Context, pc *plugins.Context) error {
			user, _ := pc.CurrentUser(ctx)
			p.rec.add(fmt.Sprintf("cron:%s:user=%v", name, user != nil))
			return nil
		}
	}
	return map[string]plugins.CronFunc{
		"* * * * *": record("every-minute"),
		"0 9 * * *": record("nine"),
		"0 * * * *": func(context.Context, *plugins.Context) error {
			p.rec.add("cron:hourly-fails")
			return errors.New("the hourly sync failed")
		},
		"*/30 * * * *": func(context.Context, *plugins.Context) error {
			panic("a plugin bug in cron")
		},
		"not a schedule": record("unreadable"),
	}
}

// Badges answers CrmWidget's badge as the viewer; CrmForecast has none.
func (p *crmPlugin) Badges() map[string]plugins.BadgeFunc {
	return map[string]plugins.BadgeFunc{
		"CrmWidget": func(ctx context.Context, pc *plugins.Context) (any, error) {
			switch p.badgeFails {
			case "error":
				return nil, errors.New("badge failed")
			case "panic":
				panic("a plugin bug in a badge")
			case "slow":
				<-ctx.Done()
				return 1, nil
			}
			user, err := pc.CurrentUser(ctx)
			if err != nil || user == nil {
				return "for nobody", err
			}
			return fmt.Sprintf("for %v", user.ID), nil
		},
		"NotAWidget": func(context.Context, *plugins.Context) (any, error) { return 3, nil },
	}
}

func legacyNames() []string {
	var out []string
	for name := range models.LegacyEventNames {
		out = append(out, name)
	}
	return out
}

func (p *crmPlugin) Filters() []plugins.Filter {
	appendTo := func(suffix string) plugins.FilterFunc {
		return func(_ context.Context, v any, _ *plugins.Context) (any, error) {
			return v.(string) + suffix, nil
		}
	}
	return []plugins.Filter{
		{Hook: "subject", Priority: 20, Handler: appendTo("|crm20")},
		{Hook: "subject", Priority: 5, Handler: appendTo("|crm5")},
		{Hook: "subject", Priority: 7, Handler: func(context.Context, any, *plugins.Context) (any, error) {
			return nil, errors.New("this filter fails")
		}},
		{Hook: "subject", Handler: appendTo("|crm10")},
	}
}

func (p *crmPlugin) Endpoints() []plugins.Route {
	return []plugins.Route{
		{Method: "GET", Path: "/CrmSettings", Handler: func(ctx context.Context, pc *plugins.Context, _ *plugins.Request) (any, error) {
			key, err := pc.Config.Get(ctx, "api_key")
			return map[string]any{"api_key_set": key != nil}, err
		}},
		{Method: "GET", Path: "/items/:id", Handler: echo},
		{Method: "POST", Path: "/items/:id", Handler: echo},
		{Method: "POST", Path: "/config", Handler: func(ctx context.Context, pc *plugins.Context, req *plugins.Request) (any, error) {
			body, _ := req.Body.(map[string]any)
			if err := pc.Config.Set(ctx, body); err != nil {
				return nil, err
			}
			return pc.Config.All(ctx)
		}},
		{Method: "POST", Path: "/query", Handler: func(ctx context.Context, pc *plugins.Context, req *plugins.Request) (any, error) {
			body, _ := req.Body.(map[string]any)
			filter, _ := body["filter"].(map[string]any)
			var opts plugins.QueryOptions
			if o, ok := body["orderBy"].(string); ok {
				opts.OrderBy = o
			}
			if o, ok := body["order"].(string); ok {
				opts.Order = o
			}
			if l, ok := body["limit"].(float64); ok {
				n := int(l)
				opts.Limit = &n
			}
			return pc.Store.Query(ctx, "deals", filter, opts)
		}},
		{Method: "GET", Path: "/boom", Handler: func(context.Context, *plugins.Context, *plugins.Request) (any, error) {
			return nil, errors.New("secret failure detail")
		}},
		{Method: "GET", Path: "/teapot", Handler: func(context.Context, *plugins.Context, *plugins.Request) (any, error) {
			return plugins.Response{Status: 418, Headers: map[string]string{"X-Brew": "tea"}, Body: "short and stout", Text: true}, nil
		}},
		{Method: "GET", Path: "/bad-header", Handler: func(context.Context, *plugins.Context, *plugins.Request) (any, error) {
			return plugins.Response{Headers: map[string]string{"Set-Cookie": "x=1"}}, nil
		}},
		{Method: "GET", Path: "/secret", Capability: "crm.secret", Handler: func(context.Context, *plugins.Context, *plugins.Request) (any, error) {
			return map[string]any{"secret": true}, nil
		}},
		{Method: "POST", Path: "/ticket", Handler: func(ctx context.Context, pc *plugins.Context, req *plugins.Request) (any, error) {
			body, _ := req.Body.(map[string]any)
			t, err := pc.Tickets().Create(ctx, body)
			if err != nil {
				return nil, err
			}
			reply, err := pc.Replies().Create(ctx, map[string]any{"ticket_id": t["id"], "body": "Logged by the CRM", "is_internal": true})
			return map[string]any{"ticket": t, "note": reply}, err
		}},
		{Method: "POST", Path: "/emit", Handler: func(ctx context.Context, pc *plugins.Context, req *plugins.Request) (any, error) {
			body, _ := req.Body.(map[string]any)
			hook, _ := body["hook"].(string)
			return map[string]any{"ok": true}, pc.Emit(ctx, hook, map[string]any{"from": "crm"})
		}},
	}
}

func (p *crmPlugin) Webhooks() []plugins.Route {
	return []plugins.Route{{Method: "POST", Path: "/inbound/:channel", Handler: func(_ context.Context, _ *plugins.Context, req *plugins.Request) (any, error) {
		return map[string]any{"channel": req.Params["channel"], "raw": string(req.RawBody), "body": req.Body}, nil
	}}}
}

func echo(_ context.Context, _ *plugins.Context, req *plugins.Request) (any, error) {
	return map[string]any{"params": req.Params, "query": req.Query, "body": req.Body, "headers": req.Headers, "path": req.Path}, nil
}

func jsonString(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

type pluginFixture struct {
	esc *escalated.Escalated
	rec *recorder
	crm *crmPlugin
}

func newPluginFixture(t *testing.T, configure ...func(*escalated.Config)) *pluginFixture {
	t.Helper()
	rec := &recorder{}
	crm := &crmPlugin{rec: rec}
	cfg := escalated.DefaultConfig()
	cfg.DB = testdb.Open(t)
	cfg.UIEnabled = true
	cfg.AdminCheck = func(r *http.Request) bool { return r.Header.Get(headerRole) == "admin" }
	cfg.AgentCheck = func(r *http.Request) bool {
		role := r.Header.Get(headerRole)
		return role == "agent" || role == "admin"
	}
	cfg.UserIDFunc = func(r *http.Request) models.UserID { return models.UserID(r.Header.Get(headerUser)) }
	cfg.Plugins.Native = []plugins.Plugin{crm}
	cfg.Plugins.Capability = func(r *http.Request, capability string) bool {
		for _, c := range strings.Split(r.Header.Get(capHeader), ",") {
			if c == capability {
				return true
			}
		}
		return false
	}
	for _, c := range configure {
		c(&cfg)
	}
	esc, err := escalated.New(cfg)
	if err != nil {
		t.Fatalf("new escalated: %v", err)
	}
	t.Cleanup(esc.Close)
	return &pluginFixture{esc: esc, rec: rec, crm: crm}
}

// withRuntime runs the stand-in runtime: this test binary.
func withRuntime(cfg *escalated.Config) {
	var compact bytes.Buffer
	_ = json.Compact(&compact, []byte(sdkManifests))
	cfg.Plugins.Runtime = plugins.RuntimeConfig{
		Enabled: true,
		Command: os.Args[0],
		Args:    []string{"-test.run=^$"},
		Env:     []string{pluginruntimetest.EnvRun + "=1", pluginruntimetest.EnvManifest + "=" + compact.String()},
	}
}

func (f *pluginFixture) mounts() map[string]http.Handler {
	r := chi.NewRouter()
	router.MountChi(r, f.esc)
	mux := http.NewServeMux()
	router.MountStdlib(mux, f.esc)
	return map[string]http.Handler{"chi": r, "stdlib": mux}
}

// preq sends a request as role ("" for a guest), with extra headers.
func preq(h http.Handler, method, path, role, body string, headers map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	if role != "" {
		r.Header.Set(headerRole, role)
		r.Header.Set(headerUser, uiAgent)
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

var inertiaHeaders = map[string]string{"X-Inertia": "true"}

func pageOf(t *testing.T, rec *httptest.ResponseRecorder) inertiaPage {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var page inertiaPage
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("not an Inertia page: %v: %s", err, rec.Body.String())
	}
	return page
}

func bodyOf(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("not JSON (%d): %v: %s", rec.Code, err, rec.Body.String())
	}
	return out
}

// lifecycleCall posts a lifecycle route as the page does and follows the 303
// with its cookie, returning the index page it lands on.
func lifecycleCall(t *testing.T, h http.Handler, method, path string) inertiaPage {
	t.Helper()
	rec := preq(h, method, path, "admin", "", inertiaHeaders)
	wantRedirect(t, rec, method+" "+path, "/escalated/admin/plugins")
	r := httptest.NewRequest(http.MethodGet, "/escalated/admin/plugins", nil)
	r.Header.Set("X-Inertia", "true")
	r.Header.Set(headerRole, "admin")
	for _, c := range rec.Result().Cookies() {
		r.AddCookie(c)
	}
	next := httptest.NewRecorder()
	h.ServeHTTP(next, r)
	return pageOf(t, next)
}

func listed(t *testing.T, page inertiaPage, slug string) map[string]any {
	t.Helper()
	for _, item := range list(t, page.Props, "plugins") {
		p := item.(map[string]any)
		if p["slug"] == slug {
			return p
		}
	}
	t.Fatalf("%s is not listed: %v", slug, page.Props["plugins"])
	return nil
}

type pluginRow struct {
	active      bool
	activated   bool
	deactivated bool
}

func (f *pluginFixture) row(t *testing.T, slug string) *pluginRow {
	t.Helper()
	db := f.esc.Config.DB
	var r pluginRow
	var activated, deactivated any
	err := db.QueryRow(sqldialect.Rebind(db, `SELECT is_active, activated_at, deactivated_at FROM escalated_plugins WHERE slug = ?`), slug).Scan(&r.active, &activated, &deactivated)
	if err != nil {
		return nil
	}
	r.activated, r.deactivated = activated != nil, deactivated != nil
	return &r
}

func (f *pluginFixture) storeCount(t *testing.T, plugin, collection string) int {
	t.Helper()
	db := f.esc.Config.DB
	var n int
	if err := db.QueryRow(sqldialect.Rebind(db, `SELECT COUNT(1) FROM escalated_plugin_store WHERE plugin = ? AND collection = ?`), plugin, collection).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestPluginsAdminIndex(t *testing.T) {
	f := newPluginFixture(t)
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			page := pageOf(t, preq(h, http.MethodGet, "/escalated/admin/plugins", "admin", "", inertiaHeaders))
			if page.Component != "Escalated/Admin/Plugins/Index" {
				t.Fatalf("component %q", page.Component)
			}
			if page.Props["can_upload"] != false {
				t.Errorf("can_upload %v", page.Props["can_upload"])
			}
			crm := listed(t, page, crmSlug)
			hasKeys(t, "plugin", crm, "slug", "name", "description", "version", "author", "author_url", "homepage", "is_active", "activated_at", "source", "provides")
			if crm["name"] != crmName || crm["version"] != "2.1.0" || crm["is_active"] != false || crm["source"] != "registered" ||
				crm["author_url"] != "https://acme.example" || crm["homepage"] != "https://acme.example" {
				t.Errorf("plugin object %v", crm)
			}
			provides := obj(t, crm, "provides")
			if provides["menu_items"] != float64(1) || provides["widgets"] != float64(2) || provides["page_components"] != float64(1) {
				t.Errorf("provides %v", provides)
			}
			if strings.Contains(jsonString(page.Props), "api_key") {
				t.Error("plugin config reached the index page")
			}

			// Agents, customers and guests are refused and see no page.
			for _, role := range []string{"agent", "customer", ""} {
				rec := preq(h, http.MethodGet, "/escalated/admin/plugins", role, "", inertiaHeaders)
				if rec.Code != http.StatusForbidden || strings.Contains(rec.Body.String(), "Plugins/Index") {
					t.Errorf("as %q: %d %s", role, rec.Code, rec.Body.String())
				}
				rec = preq(h, http.MethodPost, "/escalated/admin/plugins/"+crmSlug+"/activate", role, "", inertiaHeaders)
				if rec.Code != http.StatusForbidden {
					t.Errorf("activate as %q: %d", role, rec.Code)
				}
			}
			if f.row(t, crmSlug) != nil {
				t.Error("a refused activation wrote a row")
			}

			// No upload on this host.
			if rec := preq(h, http.MethodPost, "/escalated/admin/plugins/upload", "admin", "", inertiaHeaders); rec.Code != http.StatusNotFound {
				t.Errorf("upload: %d, want 404", rec.Code)
			}
		})
	}
}

func TestPluginsLifecycle(t *testing.T) {
	for _, name := range []string{"chi", "stdlib"} {
		t.Run(name, func(t *testing.T) {
			f := newPluginFixture(t)
			h := f.mounts()[name]
			base := "/escalated/admin/plugins/" + crmSlug

			page := lifecycleCall(t, h, http.MethodPost, base+"/activate")
			if obj(t, page.Props, "flash")["success"] != "Plugin activated." {
				t.Errorf("flash %v", page.Props["flash"])
			}
			if p := listed(t, page, crmSlug); p["is_active"] != true || p["activated_at"] == nil {
				t.Errorf("after activate %v", p)
			}
			if row := f.row(t, crmSlug); row == nil || !row.active || !row.activated || row.deactivated {
				t.Errorf("row after activate %+v", row)
			}
			slugEvent := `{"plugin":"` + crmName + `","slug":"` + crmSlug + `"}`
			if got := f.rec.take(); strings.Join(got, ",") != "onActivate,action:plugin.activated:"+slugEvent {
				t.Errorf("activation order %v", got)
			}

			// Idempotent: a success that does nothing.
			lifecycleCall(t, h, http.MethodPost, base+"/activate")
			if got := f.rec.take(); len(got) != 0 {
				t.Errorf("activating an active plugin ran %v", got)
			}

			page = lifecycleCall(t, h, http.MethodPost, base+"/deactivate")
			if obj(t, page.Props, "flash")["success"] != "Plugin deactivated." {
				t.Errorf("flash %v", page.Props["flash"])
			}
			if row := f.row(t, crmSlug); row == nil || row.active || !row.deactivated {
				t.Errorf("row after deactivate %+v", row)
			}
			if got := f.rec.take(); strings.Join(got, ",") != "action:plugin.deactivated:"+slugEvent+",onDeactivate" {
				t.Errorf("deactivation order %v", got)
			}
			lifecycleCall(t, h, http.MethodPost, base+"/deactivate")
			if got := f.rec.take(); len(got) != 0 {
				t.Errorf("deactivating an inactive plugin ran %v", got)
			}

			// The flash is read once.
			if again := pageOf(t, preq(h, http.MethodGet, "/escalated/admin/plugins", "admin", "", inertiaHeaders)); again.Props["flash"] != nil {
				t.Errorf("flash shown again: %v", again.Props["flash"])
			}

			// Uninstall: plugin.uninstalling, deactivation, the row goes, the
			// plugin stays listed and its stored data stays.
			lifecycleCall(t, h, http.MethodPost, base+"/activate")
			f.rec.take()
			if rec := preq(h, http.MethodPost, "/escalated/api/plugins/"+crmSlug+"/config", "admin", `{"api_key":"k"}`, map[string]string{"X-Requested-With": "XMLHttpRequest"}); rec.Code != http.StatusOK {
				t.Fatalf("config: %d %s", rec.Code, rec.Body.String())
			}
			page = lifecycleCall(t, h, http.MethodDelete, base)
			if obj(t, page.Props, "flash")["success"] != "Plugin uninstalled." {
				t.Errorf("flash %v", page.Props["flash"])
			}
			if f.row(t, crmSlug) != nil {
				t.Error("uninstall kept the row")
			}
			if p := listed(t, page, crmSlug); p["is_active"] != false {
				t.Errorf("after uninstall %v", p)
			}
			if f.storeCount(t, crmName, "__config__") != 1 {
				t.Error("uninstall deleted the plugin's stored data")
			}
			if got := f.rec.take(); strings.Join(got, ",") != "action:plugin.uninstalling:"+slugEvent+",action:plugin.deactivated:"+slugEvent+",onDeactivate" {
				t.Errorf("uninstall order %v", got)
			}
		})
	}
}

func TestPluginsUnknownSlugAndFailingCallback(t *testing.T) {
	f := newPluginFixture(t)
	f.crm.failActivate = true
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			for _, call := range []struct{ method, path string }{
				{http.MethodPost, "/escalated/admin/plugins/nope/activate"},
				{http.MethodPost, "/escalated/admin/plugins/nope/deactivate"},
				{http.MethodDelete, "/escalated/admin/plugins/nope"},
			} {
				page := lifecycleCall(t, h, call.method, call.path)
				if obj(t, page.Props, "flash")["error"] != "Plugin not found." {
					t.Errorf("%s %s flash %v", call.method, call.path, page.Props["flash"])
				}
				rec := preq(h, call.method, call.path, "admin", "", map[string]string{"Accept": "application/json"})
				if rec.Code != http.StatusNotFound {
					t.Errorf("%s %s as JSON: %d", call.method, call.path, rec.Code)
				}
			}
			if f.row(t, "nope") != nil {
				t.Error("an unknown slug got a row")
			}

			// A throwing callback is logged and does not undo the transition.
			lifecycleCall(t, h, http.MethodPost, "/escalated/admin/plugins/"+crmSlug+"/activate")
			if row := f.row(t, crmSlug); row == nil || !row.active {
				t.Errorf("a failing onActivate undid the activation: %+v", row)
			}
			lifecycleCall(t, h, http.MethodPost, "/escalated/admin/plugins/"+crmSlug+"/deactivate")
			f.rec.take()
		})
	}
}

func TestPluginsPagesEndpointsAndWebhooks(t *testing.T) {
	f := newPluginFixture(t)
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			page := "/escalated/admin/plugins/" + crmSlug + "/settings"
			api := "/escalated/api/plugins/" + crmSlug
			hook := "/escalated/webhooks/plugins/" + crmSlug + "/inbound/sales"

			// Inactive: nothing answers.
			for _, rec := range []*httptest.ResponseRecorder{
				preq(h, http.MethodGet, page, "admin", "", inertiaHeaders),
				preq(h, http.MethodGet, api+"/items/1", "admin", "", nil),
				preq(h, http.MethodPost, hook, "", `{}`, nil),
			} {
				if rec.Code != http.StatusNotFound {
					t.Errorf("inactive plugin answered %d: %s", rec.Code, rec.Body.String())
				}
			}

			lifecycleCall(t, h, http.MethodPost, "/escalated/admin/plugins/"+crmSlug+"/activate")
			defer lifecycleCall(t, h, http.MethodPost, "/escalated/admin/plugins/"+crmSlug+"/deactivate")

			got := pageOf(t, preq(h, http.MethodGet, page, "admin", "", inertiaHeaders))
			if got.Component != "Escalated/Plugin/Page" || got.Props["plugin"] != crmName || got.Props["component"] != "CrmSettings" || got.Props["layout"] != "admin" {
				t.Errorf("page %+v", got)
			}
			if props := obj(t, got.Props, "props"); props["api_key_set"] != false {
				t.Errorf("page props %v", props)
			}
			if rec := preq(h, http.MethodGet, page, "agent", "", inertiaHeaders); rec.Code != http.StatusForbidden {
				t.Errorf("page as agent: %d", rec.Code)
			}
			if rec := preq(h, http.MethodGet, "/escalated/admin/plugins/"+crmSlug+"/nowhere", "admin", "", inertiaHeaders); rec.Code != http.StatusNotFound {
				t.Errorf("undeclared page: %d", rec.Code)
			}

			// A capability the user lacks: 403 for the page and the endpoint.
			secret := "/escalated/admin/plugins/" + crmSlug + "/secret"
			if rec := preq(h, http.MethodGet, secret, "admin", "", inertiaHeaders); rec.Code != http.StatusForbidden {
				t.Errorf("page without the capability: %d", rec.Code)
			}
			withCap := map[string]string{"X-Inertia": "true", capHeader: "crm.secret"}
			if p := pageOf(t, preq(h, http.MethodGet, secret, "admin", "", withCap)); len(obj(t, p.Props, "props")) != 0 || p.Props["component"] != "CrmSecret" {
				t.Errorf("secret page %+v", p)
			}
			if rec := preq(h, http.MethodGet, api+"/secret", "admin", "", nil); rec.Code != http.StatusForbidden {
				t.Errorf("endpoint without the capability: %d", rec.Code)
			}
			if rec := preq(h, http.MethodGet, api+"/secret", "admin", "", map[string]string{capHeader: "crm.secret"}); rec.Code != http.StatusOK {
				t.Errorf("endpoint with the capability: %d", rec.Code)
			}

			// An endpoint gets its params, query and body, and never the
			// caller's credentials.
			rec := preq(h, http.MethodPost, api+"/items/42?expand=owner", "admin", `{"note":"hi"}`, map[string]string{
				"X-Requested-With": "XMLHttpRequest", "Cookie": "session=abc", "Authorization": "Bearer t", "X-CSRF-Token": "c", "Proxy-Authorization": "p", "X-Custom": "yes",
			})
			if rec.Code != http.StatusOK {
				t.Fatalf("endpoint: %d %s", rec.Code, rec.Body.String())
			}
			echoed := bodyOf(t, rec)
			if obj(t, echoed, "params")["id"] != "42" || obj(t, echoed, "query")["expand"] != "owner" || obj(t, echoed, "body")["note"] != "hi" || echoed["path"] != "/items/:id" {
				t.Errorf("echo %v", echoed)
			}
			headers := obj(t, echoed, "headers")
			for _, h := range []string{"cookie", "authorization", "x-csrf-token", "proxy-authorization"} {
				if _, ok := headers[h]; ok {
					t.Errorf("%s reached the plugin", h)
				}
			}
			if headers["x-custom"] != "yes" {
				t.Errorf("headers %v", headers)
			}

			// A state-changing call must come from a page script.
			if rec := preq(h, http.MethodPost, api+"/items/42", "admin", `{}`, nil); rec.Code != http.StatusForbidden {
				t.Errorf("unscripted POST: %d", rec.Code)
			}
			if rec := preq(h, http.MethodGet, api+"/items/42", "agent", "", nil); rec.Code != http.StatusForbidden {
				t.Errorf("endpoint as agent: %d", rec.Code)
			}

			// A throwing endpoint is a 500 that says nothing about the error.
			rec = preq(h, http.MethodGet, api+"/boom", "admin", "", nil)
			if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "secret failure detail") {
				t.Errorf("boom: %d %s", rec.Code, rec.Body.String())
			}
			rec = preq(h, http.MethodGet, api+"/teapot", "admin", "", nil)
			if rec.Code != 418 || rec.Header().Get("X-Brew") != "tea" || rec.Body.String() != "short and stout" || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/plain") {
				t.Errorf("teapot: %d %v %q", rec.Code, rec.Header(), rec.Body.String())
			}
			if rec := preq(h, http.MethodGet, api+"/bad-header", "admin", "", nil); rec.Code != http.StatusBadGateway || rec.Header().Get("Set-Cookie") != "" {
				t.Errorf("bad header: %d %v", rec.Code, rec.Header())
			}

			// Webhooks need no auth and get the exact bytes.
			rec = preq(h, http.MethodPost, hook, "", `{"a": 1}`, nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("webhook: %d %s", rec.Code, rec.Body.String())
			}
			if b := bodyOf(t, rec); b["raw"] != `{"a": 1}` || b["channel"] != "sales" || obj(t, b, "body")["a"] != float64(1) {
				t.Errorf("webhook %v", b)
			}

		})
	}
}

func TestPluginsFiltersEmitAndStore(t *testing.T) {
	f := newPluginFixture(t)
	h := f.mounts()["chi"]
	ctx := context.Background()
	scripted := map[string]string{"X-Requested-With": "XMLHttpRequest"}

	// Inactive: no filter runs.
	if got := f.esc.Plugins.ApplyFilter(ctx, "subject", "s"); got != "s" {
		t.Errorf("inactive filter ran: %v", got)
	}
	lifecycleCall(t, h, http.MethodPost, "/escalated/admin/plugins/"+crmSlug+"/activate")
	f.rec.take()

	// Ascending priority, and the failing filter is skipped.
	if got := f.esc.Plugins.ApplyFilter(ctx, "subject", "s"); got != "s|crm5|crm10|crm20" {
		t.Errorf("filters: %v", got)
	}

	// ctx.emit reaches active plugins, but never under a host-owned name.
	api := "/escalated/api/plugins/" + crmSlug
	for _, hook := range []string{"ticket.created", "ticket.replied", "plugin.activated", "custom.ping"} {
		if rec := preq(h, http.MethodPost, api+"/emit", "admin", `{"hook":"`+hook+`"}`, scripted); rec.Code != http.StatusOK {
			t.Fatalf("emit %s: %d %s", hook, rec.Code, rec.Body.String())
		}
	}
	if got := f.rec.take(); strings.Join(got, ",") != `action:custom.ping:{"from":"crm"}` {
		t.Errorf("emitted %v", got)
	}

	// A panicking action is logged, not fatal.
	f.esc.Plugins.DispatchAction(ctx, "custom.explode", map[string]any{})

	// ctx.config.set merges.
	preq(h, http.MethodPost, api+"/config", "admin", `{"api_key":"k","region":"eu"}`, scripted)
	rec := preq(h, http.MethodPost, api+"/config", "admin", `{"region":"us"}`, scripted)
	if cfg := bodyOf(t, rec); cfg["api_key"] != "k" || cfg["region"] != "us" {
		t.Errorf("config after merge %v", cfg)
	}

	// ctx.store.query operators, and no SQL through a field name.
	pc := pluginContext(t, f, crmSlug)
	for i, deal := range []map[string]any{
		{"key": "a", "amount": 10, "stage": "won", "tags": []any{"vip"}, "owner": map[string]any{"team": "red"}},
		{"key": "b", "amount": 25, "stage": "lost", "tags": []any{"new"}, "owner": map[string]any{"team": "blue"}},
		{"key": "c", "amount": 40, "stage": "won", "tags": []any{"vip", "new"}, "owner": map[string]any{"team": "red"}},
	} {
		if _, err := pc.Store.Insert(ctx, "deals", deal); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}
	keys := func(body string) string {
		t.Helper()
		rec := preq(h, http.MethodPost, api+"/query", "admin", body, scripted)
		if rec.Code != http.StatusOK {
			return "error"
		}
		var rows []map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &rows)
		var out []string
		for _, r := range rows {
			if r["_id"] == nil {
				t.Errorf("row without _id: %v", r)
			}
			out = append(out, r["key"].(string))
		}
		return strings.Join(out, "")
	}
	cases := map[string]string{
		`{"filter":{"stage":"won"}}`:                            "ac",
		`{"filter":{"amount":{"$gt":10}}}`:                      "bc",
		`{"filter":{"amount":{"$gte":10,"$lt":40}}}`:            "ab",
		`{"filter":{"amount":{"$lte":25}}}`:                     "ab",
		`{"filter":{"stage":{"$ne":"won"}}}`:                    "b",
		`{"filter":{"stage":{"$in":["lost","open"]}}}`:          "b",
		`{"filter":{"stage":{"$nin":["lost"]}}}`:                "ac",
		`{"filter":{"tags":"vip"}}`:                             "ac",
		`{"filter":{"owner.team":"red"}}`:                       "ac",
		`{"filter":{},"orderBy":"amount","order":"desc"}`:       "cba",
		`{"filter":{},"orderBy":"amount","limit":2}`:            "ab",
		`{"filter":{"amount; DROP TABLE escalated_tickets":1}}`: "error",
		`{"filter":{"amount":{"$where":"1"}}}`:                  "error",
		`{"filter":{},"orderBy":"amount) OR 1=1 --"}`:           "error",
	}
	for body, want := range cases {
		if got := keys(body); got != want {
			t.Errorf("query %s: got %q, want %q", body, got, want)
		}
	}
}

// pluginContext is the ctx a plugin's handlers receive, captured through a
// lifecycle callback-free route: the store is reached through the registry.
func pluginContext(t *testing.T, f *pluginFixture, slug string) *plugins.Context {
	t.Helper()
	pc, err := f.esc.Plugins.ContextFor(slug)
	if err != nil {
		t.Fatal(err)
	}
	return pc
}

func TestPluginsDuplicateSlugsFailAtStartup(t *testing.T) {
	cfg := escalated.DefaultConfig()
	cfg.DB = testdb.Open(t)
	cfg.Plugins.Native = []plugins.Plugin{&crmPlugin{rec: &recorder{}}, &crmPlugin{rec: &recorder{}}}
	_, err := escalated.New(cfg)
	if err == nil || !strings.Contains(err.Error(), crmName) || !strings.Contains(err.Error(), crmSlug) {
		t.Fatalf("duplicate slugs: %v", err)
	}
}

func TestSDKPluginsThroughTheRuntimeBridge(t *testing.T) {
	f := newPluginFixture(t, withRuntime)
	h := f.mounts()["chi"]
	mux := f.mounts()["stdlib"]
	ctx := context.Background()

	page := pageOf(t, preq(h, http.MethodGet, "/escalated/admin/plugins", "admin", "", inertiaHeaders))
	slack := listed(t, page, slackSlug)
	if slack["name"] != slackName || slack["source"] != "composer" || slack["version"] != "0.3.0" || slack["is_active"] != false {
		t.Errorf("slack listing %v", slack)
	}
	listed(t, page, jiraSlug)
	listed(t, page, crmSlug)

	// Activate: the runtime's activate (which writes through ctx.store), then
	// plugin.activated, which the runtime delivers to the slack plugin.
	lifecycleCall(t, h, http.MethodPost, "/escalated/admin/plugins/"+slackSlug+"/activate")
	if f.storeCount(t, slackName, "lifecycle") != 1 {
		t.Error("the runtime was not sent activate")
	}
	if got := f.received(t, slackName, "plugin.activated"); len(got) != 1 {
		t.Errorf("plugin.activated reached slack %d times", len(got))
	}

	// Inactive jira: its endpoint is a 404, and the runtime's attempt to act
	// for it during a dispatch is refused.
	if rec := preq(h, http.MethodGet, "/escalated/api/plugins/"+jiraSlug+"/echo/1", "admin", "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("inactive SDK endpoint: %d", rec.Code)
	}
	f.esc.Plugins.DispatchAction(ctx, "custom.ping", map[string]any{"n": 1})
	if got := f.received(t, slackName, "custom.ping"); len(got) != 1 || jsonString(got[0]["plugins"]) != `["`+slackName+`"]` {
		t.Errorf("slack received %v", got)
	}
	if got := f.received(t, jiraName, "custom.ping"); len(got) != 0 {
		t.Errorf("inactive jira received %v", got)
	}

	// Endpoints over the HTTP contract.
	api := "/escalated/api/plugins/" + slackSlug
	for name, handler := range map[string]http.Handler{"chi": h, "stdlib": mux} {
		rec := preq(handler, http.MethodPost, api+"/echo/7?q=1", "admin", `{"x": true}`, map[string]string{"X-Requested-With": "XMLHttpRequest", "Cookie": "s=1", "Authorization": "Bearer z"})
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: SDK endpoint %d %s", name, rec.Code, rec.Body.String())
		}
		e := bodyOf(t, rec)
		raw, _ := base64.StdEncoding.DecodeString(e["rawBodyBase64"].(string))
		if e["kind"] != "endpoint" || e["path"] != "/echo/:id" || obj(t, e, "params")["id"] != "7" || obj(t, e, "query")["q"] != "1" ||
			obj(t, e, "body")["x"] != true || e["httpContract"] != float64(1) || string(raw) != `{"x": true}` {
			t.Errorf("%s: echo %v", name, e)
		}
		if hd := obj(t, e, "headers"); hd["cookie"] != nil || hd["authorization"] != nil {
			t.Errorf("%s: credentials reached the runtime: %v", name, hd)
		}
	}
	rec := preq(h, http.MethodGet, api+"/boom", "admin", "", nil)
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "secret failure detail") {
		t.Errorf("SDK boom: %d %s", rec.Code, rec.Body.String())
	}
	rec = preq(h, http.MethodGet, api+"/envelope", "admin", "", nil)
	if rec.Code != http.StatusCreated || rec.Header().Get("X-Plugin") != "yes" || bodyOf(t, rec)["ok"] != true {
		t.Errorf("envelope: %d %v %s", rec.Code, rec.Header(), rec.Body.String())
	}

	// ctx.config through the bridge, and no reading another plugin's rows.
	slackCtx := pluginContext(t, f, slackSlug)
	if err := slackCtx.Config.Set(ctx, map[string]any{"api_key": "xoxb"}); err != nil {
		t.Fatal(err)
	}
	if rec := preq(h, http.MethodGet, api+"/config", "admin", "", nil); strings.TrimSpace(rec.Body.String()) != `"xoxb"` {
		t.Errorf("ctx.config.get over the bridge: %d %s", rec.Code, rec.Body.String())
	}
	crmCtx := pluginContext(t, f, crmSlug)
	if err := crmCtx.Store.Set(ctx, "secrets", "token", "crm-secret"); err != nil {
		t.Fatal(err)
	}
	if rec := preq(h, http.MethodGet, api+"/spy?as="+crmName, "admin", "", nil); strings.Contains(rec.Body.String(), "crm-secret") {
		t.Errorf("the runtime read another plugin's store: %s", rec.Body.String())
	}

	// The page's props come from the plugin's GET /{component}.
	p := pageOf(t, preq(h, http.MethodGet, "/escalated/admin/plugins/"+slackSlug+"/settings", "admin", "", inertiaHeaders))
	if p.Props["plugin"] != slackName || obj(t, p.Props, "props")["greeting"] != "hello from "+slackName {
		t.Errorf("SDK page %+v", p)
	}

	// Webhooks, unauthenticated.
	rec = preq(h, http.MethodPost, "/escalated/webhooks/plugins/"+slackSlug+"/echo/9", "", `{"y":2}`, nil)
	if rec.Code != http.StatusOK || bodyOf(t, rec)["kind"] != "webhook" {
		t.Errorf("SDK webhook: %d %s", rec.Code, rec.Body.String())
	}

	// SDK filters run in the runtime, ordered by their priority among the
	// native ones (slack 15 sits between crm 10 and crm 20).
	lifecycleCall(t, h, http.MethodPost, "/escalated/admin/plugins/"+crmSlug+"/activate")
	if got := f.esc.Plugins.ApplyFilter(ctx, "subject", "s"); got != "s|crm5|crm10|sdk|crm20" {
		t.Errorf("mixed filters: %v", got)
	}
	if got := f.esc.Plugins.ApplyFilter(ctx, "fail", "s"); got != "s" {
		t.Errorf("a failing runtime filter: %v", got)
	}

	// A package-managed plugin is not uninstalled from the screen.
	page = lifecycleCall(t, h, http.MethodDelete, "/escalated/admin/plugins/"+slackSlug)
	if obj(t, page.Props, "flash")["error"] == nil {
		t.Errorf("uninstalling an npm plugin: flash %v", page.Props["flash"])
	}
	if row := f.row(t, slackSlug); row == nil || !row.active {
		t.Error("a refused uninstall changed the row")
	}

	// Deactivate: plugin.deactivated, then the runtime's deactivate.
	lifecycleCall(t, h, http.MethodPost, "/escalated/admin/plugins/"+slackSlug+"/deactivate")
	if f.storeCount(t, slackName, "lifecycle") != 2 {
		t.Error("the runtime was not sent deactivate")
	}
}

func TestPluginRuntimeUnavailable(t *testing.T) {
	f := newPluginFixture(t, func(cfg *escalated.Config) {
		cfg.Plugins.Runtime = plugins.RuntimeConfig{Enabled: true, Command: "escalated-no-such-node-binary"}
	})
	h := f.mounts()["chi"]
	// The host still serves the screen, with its native plugins.
	page := pageOf(t, preq(h, http.MethodGet, "/escalated/admin/plugins", "admin", "", inertiaHeaders))
	listed(t, page, crmSlug)
	if got := f.esc.Plugins.ApplyFilter(context.Background(), "subject", "s"); got != "s" {
		t.Errorf("filter with no runtime: %v", got)
	}
}

// received are the actions the stand-in runtime recorded for a plugin.
func (f *pluginFixture) received(t *testing.T, plugin, hook string) []map[string]any {
	t.Helper()
	db := f.esc.Config.DB
	rows, err := db.Query(sqldialect.Rebind(db, `SELECT data FROM escalated_plugin_store WHERE plugin = ? AND collection = 'received' ORDER BY id`), plugin)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []map[string]any
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		_ = json.Unmarshal([]byte(raw), &m)
		if m["hook"] == hook {
			out = append(out, m)
		}
	}
	return out
}
