package router_test

import (
	"bufio"
	"net/http"
	"os"
	"sort"
	"strings"
	"testing"

	escalated "github.com/escalated-dev/escalated-go"
	"github.com/escalated-dev/escalated-go/renderer"
)

// notServed are the names the shared frontend calls that this package has no
// route for. route() returns "#" for them (and warns), so the control they
// back does nothing until the feature lands here or the host maps the name
// (Config.RouteOverrides).
var notServed = map[string]string{
	// The host owns sign-in: Escalated has no login screen or configurable
	// login path, so the host maps it (Config.RouteOverrides or its own Ziggy).
	"login": "host sign-in",

	// Saved views: the store has methods, but no migration creates the table.
	"escalated.admin.saved-views.destroy": "saved views not served",
	"escalated.admin.saved-views.store":   "saved views not served",
	"escalated.admin.saved-views.update":  "saved views not served",

	// Plugin uploads: plugins are registered in code (Config.Plugins).
	"escalated.admin.plugins.upload": "no plugin uploads",

	// Presence and snooze: no presence channel, no snooze columns.
	"escalated.admin.tickets.presence": "presence not served",
	"escalated.admin.tickets.snooze":   "snooze not served",
	"escalated.admin.tickets.unsnooze": "snooze not served",
	"escalated.agent.tickets.presence": "presence not served",
	"escalated.agent.tickets.snooze":   "snooze not served",
	"escalated.agent.tickets.unsnooze": "snooze not served",
}

func frontendRouteNames(t *testing.T) []string {
	t.Helper()
	f, err := os.Open("../testdata/frontend-route-names.txt")
	if err != nil {
		t.Fatalf("open names: %v", err)
	}
	defer f.Close()
	var names []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		names = append(names, line)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("read names: %v", err)
	}
	if len(names) < 150 {
		t.Fatalf("read %d names, want the whole list", len(names))
	}
	return names
}

func TestRouteListCoversEveryFrontendRouteName(t *testing.T) {
	routes := escalated.BuiltInRoutes(escalated.DefaultConfig().RoutePrefix)
	var missing, stale []string
	for _, name := range frontendRouteNames(t) {
		_, served := routes[name]
		_, allowed := notServed[name]
		switch {
		case !served && !allowed:
			missing = append(missing, name)
		case served && allowed:
			stale = append(stale, name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("names the frontend calls with no route and not listed as not served:\n  %s", strings.Join(missing, "\n  "))
	}
	if len(stale) > 0 {
		t.Errorf("names served but still listed as not served: %v", stale)
	}
}

func TestRouteListURIsAreZiggyShaped(t *testing.T) {
	for name, route := range escalated.BuiltInRoutes("/support") {
		if strings.HasPrefix(route.URI, "/") || !strings.HasPrefix(route.URI, "support/") && route.URI != "support" {
			t.Errorf("%s: uri %q, want it under support/ without a leading slash", name, route.URI)
		}
		if strings.Contains(route.URI, ":") {
			t.Errorf("%s: uri %q uses :param, want {param}", name, route.URI)
		}
		if len(route.Methods) == 0 {
			t.Errorf("%s: no methods", name)
		}
	}
	if got := escalated.BuiltInRoutes("")["escalated.agent.tickets.index"].URI; got != "agent/tickets" {
		t.Errorf("empty prefix: uri %q, want agent/tickets", got)
	}
}

func ziggyOf(t *testing.T, page inertiaPage) (map[string]any, map[string]any) {
	t.Helper()
	ziggy := obj(t, page.Props, "ziggy")
	return ziggy, obj(t, ziggy, "routes")
}

func TestZiggyPropOnEveryInertiaPageHonoursThePrefix(t *testing.T) {
	f := newUIFixture(t, func(cfg *escalated.Config) { cfg.RoutePrefix = "/support" })
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			for _, path := range []string{"/support/agent/tickets", "/support/admin/departments", "/support/tickets"} {
				role := "admin"
				page := visit(t, h, path, customerB, role)
				ziggy, routes := ziggyOf(t, page)
				if ziggy["url"] != "http://example.com" {
					t.Errorf("%s: url %v", path, ziggy["url"])
				}
				if ziggy["location"] != "http://example.com"+path {
					t.Errorf("%s: location %v", path, ziggy["location"])
				}
				if _, ok := ziggy["port"]; !ok || ziggy["port"] != nil {
					t.Errorf("%s: port %v, want null", path, ziggy["port"])
				}
				if d, ok := ziggy["defaults"].(map[string]any); !ok || len(d) != 0 {
					t.Errorf("%s: defaults %v, want {}", path, ziggy["defaults"])
				}
				show := obj(t, routes, "escalated.agent.tickets.show")
				if show["uri"] != "support/agent/tickets/{ticket}" {
					t.Errorf("%s: show uri %v", path, show["uri"])
				}
				methods := list(t, show, "methods")
				if len(methods) == 0 || methods[0] != "GET" {
					t.Errorf("%s: show methods %v", path, methods)
				}
				if obj(t, routes, "escalated.admin.chat.end")["uri"] != "support/agent/chat/{session}/end" {
					t.Errorf("%s: chat.end uri %v", path, obj(t, routes, "escalated.admin.chat.end")["uri"])
				}
			}
		})
	}
}

func TestRouteOverridesWinOverTheBuiltInList(t *testing.T) {
	f := newUIFixture(t, func(cfg *escalated.Config) {
		cfg.RouteOverrides = map[string]escalated.NamedRoute{
			"login":                         {URI: "/sign-in", Methods: []string{"GET", "HEAD"}},
			"escalated.agent.tickets.index": {URI: "desk/queue", Methods: []string{"GET"}},
			"escalated.admin.tickets.show":  {}, // removes the name
		}
	})
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			_, routes := ziggyOf(t, visit(t, h, "/escalated/agent/tickets", uiAgent, "agent"))
			if obj(t, routes, "login")["uri"] != "sign-in" {
				t.Errorf("login uri %v", obj(t, routes, "login")["uri"])
			}
			if obj(t, routes, "escalated.agent.tickets.index")["uri"] != "desk/queue" {
				t.Errorf("override lost: %v", routes["escalated.agent.tickets.index"])
			}
			if _, ok := routes["escalated.admin.tickets.show"]; ok {
				t.Errorf("an empty override should remove the name")
			}
			if obj(t, routes, "escalated.agent.tickets.show")["uri"] != "escalated/agent/tickets/{ticket}" {
				t.Errorf("built-ins dropped: %v", routes["escalated.agent.tickets.show"])
			}
		})
	}
}

// A host that shares its own ziggy prop (its Ziggy route list) keeps it: its
// names win, and the built-in names it does not define are still there.
func TestHostSharedZiggyWinsOverTheBuiltInList(t *testing.T) {
	f := newUIFixture(t)
	hostZiggy := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := renderer.WithShared(r.Context(), "ziggy", func() any {
				return map[string]any{
					"url": "https://host.test",
					"routes": map[string]any{
						"escalated.agent.tickets.index": map[string]any{"uri": "host/queue", "methods": []any{"GET"}},
						"login":                         map[string]any{"uri": "login", "methods": []any{"GET"}},
					},
				}
			})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			ziggy, routes := ziggyOf(t, visit(t, hostZiggy(h), "/escalated/agent/tickets", uiAgent, "agent"))
			if ziggy["url"] != "https://host.test" {
				t.Errorf("url %v, want the host's", ziggy["url"])
			}
			if obj(t, routes, "escalated.agent.tickets.index")["uri"] != "host/queue" {
				t.Errorf("host route lost: %v", routes["escalated.agent.tickets.index"])
			}
			if obj(t, routes, "login")["uri"] != "login" {
				t.Errorf("host login lost")
			}
			if obj(t, routes, "escalated.agent.tickets.show")["uri"] != "escalated/agent/tickets/{ticket}" {
				t.Errorf("built-ins dropped: %v", routes["escalated.agent.tickets.show"])
			}
		})
	}
}
