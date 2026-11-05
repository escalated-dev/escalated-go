package escalated

import (
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// NavigationItem is one entry of the admin menu (escalated.navigation, ADR
// 2026-11-05-embeddable-admin): what a host needs to list Escalated's admin
// screens in its own sidebar.
type NavigationItem struct {
	// Key is a stable id: the core item's key, or "plugin:<slug>:<n>".
	Key   string `json:"key"`
	Label string `json:"label"`
	// Route is the route name (the ziggy name), nil for a plugin page or a
	// screen the frontend has no name for.
	Route *string `json:"route"`
	// URL is the absolute path of the screen.
	URL string `json:"url"`
	// Icon is the icon key (the frontend's escalatedNavIcons), or the
	// plugin's own icon value.
	Icon any `json:"icon"`
	// Position orders the menu, ascending, as the standalone sidebar does.
	Position float64 `json:"position"`
	// Active is true when the request path is URL or below it.
	Active bool `json:"active"`
	// Badge is a plugin item's badge, else nil.
	Badge any `json:"badge"`
	// Plugin is the plugin slug of a plugin item, else nil.
	Plugin *string `json:"plugin"`
	// Children are a plugin item's submenu items; always empty here (plugin
	// menus have no submenus in this package).
	Children []NavigationChild `json:"children"`
}

// NavigationChild is a submenu item.
type NavigationChild struct {
	Label string `json:"label"`
	URL   string `json:"url"`
	Icon  any    `json:"icon"`
}

// navItem is a core admin menu entry: key (also the icon key), label, route
// name and position, as the standalone sidebar in the shared frontend.
type navItem struct {
	key      string
	label    string
	route    string
	position float64
}

var navItems = []navItem{
	{"reports", "Reports", "escalated.admin.reports", 10},
	{"tickets", "Tickets", "escalated.admin.tickets.index", 20},
	{"departments", "Departments", "escalated.admin.departments.index", 30},
	{"sla-policies", "SLA Policies", "escalated.admin.sla-policies.index", 40},
	{"knowledge-base", "Knowledge Base", "escalated.admin.kb-articles.index", 45},
	{"escalation-rules", "Escalation Rules", "escalated.admin.escalation-rules.index", 50},
	{"workflows", "Workflows", "escalated.admin.workflows.index", 53},
	{"automations", "Automations", "escalated.admin.automations.index", 53.5},
	{"webhooks", "Webhooks", "escalated.admin.webhooks.index", 54},
	{"tags", "Tags", "escalated.admin.tags.index", 60},
	{"custom-fields", "Custom Fields", "escalated.admin.custom-fields.index", 62},
	{"statuses", "Statuses", "escalated.admin.statuses.index", 64},
	{"canned-responses", "Canned Responses", "escalated.admin.canned-responses.index", 70},
	{"macros", "Macros", "escalated.admin.macros.index", 80},
	{"newsletters", "Newsletters", "escalated.admin.newsletters.index", 81},
	{"skills", "Skills", "escalated.admin.skills.index", 82},
	{"users", "Users", "escalated.admin.users.index", 83},
	{"roles", "Roles", "escalated.admin.roles.index", 84},
	{"capacity", "Capacity", "escalated.admin.capacity.index", 85},
	{"business-hours", "Business Hours", "escalated.admin.business-hours.index", 86},
	{"audit-log", "Audit Log", "escalated.admin.audit-log", 88},
	{"api-tokens", "API Tokens", "escalated.admin.api-tokens.index", 90},
	{"custom-objects", "Custom Objects", "escalated.admin.custom-objects.index", 92},
	{"plugins", "Plugins", "escalated.admin.plugins.index", 95},
	{"import", "Import", "escalated.admin.import.index", 97},
	{"settings", "Settings", "escalated.admin.settings", 100},
}

// AdminNavigation is the admin menu for the request's user, as shared in
// escalated.navigation while embedding is on: for a server-rendered host
// layout (the root view gets it as escalatedNavigation) or any page of the
// host's own. The user is the request's (Config.UserIDFunc, AdminCheck).
// It works whether or not embedding is on: URLs follow wherever the admin
// routes are.
//
// Filtered so a host sidebar never links to a screen that would answer 403
// or 404: empty for a user who fails AdminCheck (for any signed-in user when
// ReplaceAdminGate is on), screens that are not mounted (newsletters while
// they are off, roles while roles & permissions is off, names a
// RouteOverride removed), and while roles & permissions is on, each screen
// needs the permissions its route requires. Active plugins' admin menu
// items are merged in and the list is sorted by position.
func (e *Escalated) AdminNavigation(r *http.Request) []NavigationItem {
	items := []NavigationItem{}
	if r == nil {
		return items
	}
	if e.Embedding() && e.Config.UI.Embed.ReplaceAdminGate {
		if e.Config.UserIDFunc(r).Empty() {
			return items
		}
	} else if !e.Config.AdminCheck(r) {
		return items
	}

	path := requestPath(r)
	have, gated := e.permissionsFor(r)
	routes := e.RouteList()
	standalone := e.standaloneAdmin()
	for _, item := range navItems {
		var url string
		var route *string
		switch item.key {
		case "newsletters":
			// Not a named route here, and not moved while embedded.
			if !e.Config.EnableNewsletters {
				continue
			}
			url = standalone + "/newsletters"
		case "roles":
			if !e.Features.RolesPermissions || e.Roles() == nil {
				continue
			}
			fallthrough
		default:
			named, ok := routes[item.route]
			if !ok {
				continue
			}
			url = "/" + strings.TrimLeft(named.URI, "/")
			name := item.route
			route = &name
		}
		if gated {
			internal := url
			if mapped, ok := e.StandaloneAdminPath(url); ok {
				internal = mapped
			}
			rest, under := strings.CutPrefix(internal, strings.TrimRight(e.Config.RoutePrefix, "/"))
			if !under || !routeAllowed(have, rest) {
				continue
			}
		}
		items = append(items, NavigationItem{
			Key:      item.key,
			Label:    item.label,
			Route:    route,
			URL:      url,
			Icon:     item.key,
			Position: item.position,
			Active:   activePath(path, url),
			Children: []NavigationChild{},
		})
	}
	items = append(items, e.pluginNavigation(r, path)...)
	sort.SliceStable(items, func(i, j int) bool { return items[i].Position < items[j].Position })
	return items
}

// pluginNavigation are the active plugins' admin menu items, filtered as
// escalated.plugins filters them (the capability, admins only).
func (e *Escalated) pluginNavigation(r *http.Request, path string) []NavigationItem {
	out := []NavigationItem{}
	if e.Plugins == nil {
		return out
	}
	menu, _ := e.Plugins.UIExtensions(r)["menuItems"].([]map[string]any)
	for i, m := range menu {
		if section, _ := m["section"].(string); section != "" && section != "admin" {
			continue
		}
		url, _ := m["route"].(string)
		if url == "" {
			continue
		}
		slug, _ := m["plugin"].(string)
		label, _ := m["label"].(string)
		position := 100.0
		if p, ok := m["position"].(int); ok {
			position = float64(p)
		}
		key := "plugin:" + slug + ":" + strconv.Itoa(i)
		plugin := slug
		out = append(out, NavigationItem{
			Key:      key,
			Label:    label,
			URL:      url,
			Icon:     m["icon"],
			Position: position,
			Active:   activePath(path, url),
			Badge:    m["badge"],
			Plugin:   &plugin,
			Children: []NavigationChild{},
		})
	}
	return out
}

// activePath reports whether path is target or below it.
func activePath(path, target string) bool {
	target = strings.TrimRight(target, "/")
	return path == target || strings.HasPrefix(path, target+"/")
}

type namedPattern struct {
	name    string
	re      *regexp.Regexp
	params  int
	methods []string
}

var routeParamPattern = regexp.MustCompile(`\\\{\w+\\\?\\\}|\\\{\w+\\\}`)

// routeName is the route list's name for the method and path, preferring
// the route with the fewest parameters (a literal segment over {id}).
func (e *Escalated) routeName(method, path string) (string, bool) {
	e.routeNamesOnce.Do(func() {
		for name, route := range e.RouteList() {
			quoted := regexp.QuoteMeta("/" + strings.TrimLeft(route.URI, "/"))
			params := len(routeParamPattern.FindAllString(quoted, -1))
			re := regexp.MustCompile("^" + routeParamPattern.ReplaceAllString(quoted, `[^/]+`) + "/?$")
			e.routeNames = append(e.routeNames, namedPattern{name: name, re: re, params: params, methods: route.Methods})
		}
	})
	best := namedPattern{params: -1}
	for _, p := range e.routeNames {
		if !containsString(p.methods, method) || !p.re.MatchString(path) {
			continue
		}
		if best.params < 0 || p.params < best.params || (p.params == best.params && p.name < best.name) {
			best = p
		}
	}
	return best.name, best.params >= 0
}

// routeNamesState is the lazily compiled route list for routeName.
type routeNamesState struct {
	routeNamesOnce sync.Once
	routeNames     []namedPattern
}
