package access

import (
	"regexp"
	"strings"
	"sync"
)

// RouteEntry is one line of the route-permission map: an HTTP method ("*"
// for any, or several joined by "|"), a path pattern under the route prefix
// and the permission slugs a request needs. In a pattern `*` is one path
// segment, `**` is one or more and `{a,b}` is either word. An empty slug
// list means "staff, no particular permission": personal screens and the
// plugin pages and endpoints, which the plugin guards with its own
// capability.
type RouteEntry struct {
	Method  string
	Pattern string
	Slugs   []string
}

// ClosingStatuses are the base statuses (models.StatusResolved, StatusClosed,
// StatusReopened) whose change needs ticket.close as well as ticket.edit.
var ClosingStatuses = map[int]bool{5: true, 6: true, 7: true}

// BulkActions are the bulk actions and the permission each needs.
var BulkActions = map[string][]string{
	"status":     {"ticket.edit"},
	"priority":   {"ticket.edit"},
	"assign":     {"ticket.assign"},
	"tags":       {"ticket.edit"},
	"department": {"ticket.edit"},
	"delete":     {"ticket.delete"},
}

const (
	adm   = "/admin"
	agt   = "/agent"
	staff = "/{admin,agent}"
)

var (
	view = []string{"ticket.view"}
	edit = []string{"ticket.edit"}
)

// resource maps an admin resource the way escalated-laravel's resource()
// helper does: the list and other reads => {group}.view; the create and edit
// forms and every write => {group}.manage.
func resource(path, group string, extraReads ...string) []RouteEntry {
	v := []string{group + ".view"}
	m := []string{group + ".manage"}
	out := []RouteEntry{
		{"GET", adm + "/" + path + "/create", m},
		{"GET", adm + "/" + path + "/new", m},
		{"GET", adm + "/" + path + "/*/edit", m},
	}
	for _, read := range extraReads {
		out = append(out, RouteEntry{"GET", adm + "/" + path + "/" + read, v})
	}
	return append(out,
		RouteEntry{"GET", adm + "/" + path, v},
		RouteEntry{"*", adm + "/" + path, m},
		RouteEntry{"*", adm + "/" + path + "/**", m},
	)
}

// RoutePermissions is escalated-laravel's src/Support/RoutePermissions.php,
// keyed by method and path (under Config.RoutePrefix) instead of route name.
// Entries are tried in order and the first match wins, so exact paths come
// before wider patterns. A governed route that matches nothing is refused,
// and a test enumerates the router so a new route cannot ship without an
// entry here. Every slug is one Seed creates.
var RoutePermissions = func() []RouteEntry {
	entries := []RouteEntry{
		// ---- Tickets (admin and agent panels, staff API) ------------------
		{"GET", agt, view},
		{"GET", agt + "/", view},
		{"POST", staff + "/tickets/bulk", view}, // plus each action's, see Required
		{"GET", adm + "/tickets/merge-search", view},
		{"GET", staff + "/tickets", view},
		{"GET", staff + "/tickets/*", view},
		{"GET", staff + "/tickets/*/{links,side-conversations}", view},
		{"POST", staff + "/tickets/*/follow", view},
		{"POST", adm + "/tickets/*/split", []string{"ticket.create"}},
		{"PUT", staff + "/tickets/*", edit},
		{"POST", staff + "/tickets/*/{status,priority,tags,department}", edit},
		{"POST", staff + "/tickets/*/replies/*/pin", edit},
		{"POST", staff + "/tickets/*/actions/*", edit},
		{"*", staff + "/tickets/*/subjects", edit},
		{"*", staff + "/tickets/*/subjects/*", edit},
		{"*", staff + "/tickets/*/links", edit},
		{"*", staff + "/tickets/*/links/*", edit},
		{"POST", staff + "/tickets/*/assign", []string{"ticket.assign"}},
		{"POST", adm + "/tickets/*/merge", []string{"ticket.merge"}},
		{"POST", staff + "/tickets/*/macro", []string{"macro.view", "ticket.edit"}},
		{"POST", agt + "/tickets/*/macros/*/apply", []string{"macro.view", "ticket.edit"}},
		{"POST", staff + "/tickets/*/reply", []string{"reply.create"}},
		{"POST", staff + "/tickets/*/replies", []string{"reply.create"}}, // internal: reply.create_internal
		{"POST", staff + "/tickets/*/note", []string{"reply.create_internal"}},
		{"POST", staff + "/tickets/*/side-conversations", []string{"reply.create"}},
		{"POST", staff + "/tickets/*/side-conversations/**", []string{"reply.create"}},
		{"GET", agt + "/macros", []string{"macro.view"}},
		{"GET", agt + "/canned-responses", view},

		// The staff JSON API (token owners included).
		{"GET", "/api/{tickets,departments,tags}", view},
		{"GET", "/api/tickets/*", view},
		{"POST", "/api/tickets", []string{"ticket.create"}},
		{"PATCH", "/api/tickets/*", edit},
		{"POST", "/api/tickets/*/replies", []string{"reply.create"}}, // internal: reply.create_internal
		{"POST", "/api/tickets/*/actions/*", edit},
		{"*", "/api/tickets/*/subjects", edit},
		{"*", "/api/tickets/*/subjects/*", edit},
		// Active plugins' endpoints: the endpoint's capability decides.
		{"*", "/api/plugins/**", nil},

		// ---- Live chat ------------------------------------------------------
		{"GET", agt + "/chat/**", view},
		{"POST", agt + "/chat/**", []string{"reply.create"}},

		// ---- Reports ----------------------------------------------------------
		{"GET", adm + "/reports", []string{"report.view"}},
		{"GET", adm + "/reports/**", []string{"report.view"}},

		// ---- Knowledge base (pages and JSON) -----------------------------------
		{"GET", adm + "/kb-articles/create", []string{"kb.create"}},
		{"GET", adm + "/kb-articles/*/edit", []string{"kb.edit"}},
		{"GET", adm + "/{kb-articles,kb-categories,kb/articles,kb/categories}", []string{"kb.view"}},
		{"GET", adm + "/kb/articles/*", []string{"kb.view"}},
		{"POST", adm + "/{kb-articles,kb/articles}", []string{"kb.create"}},
		{"PUT|PATCH", adm + "/{kb-articles,kb/articles}/*", []string{"kb.edit"}},
		{"POST", adm + "/{kb-categories,kb/categories}", []string{"kb.edit"}},
		{"PUT|PATCH", adm + "/{kb-categories,kb/categories}/*", []string{"kb.edit"}},
		{"DELETE", adm + "/{kb-articles,kb-categories,kb/articles,kb/categories}/*", []string{"kb.delete"}},

		// ---- Departments --------------------------------------------------------
		{"GET", adm + "/departments/create", []string{"department.create"}},
		{"GET", adm + "/departments/*/edit", []string{"department.edit"}},
		{"GET", adm + "/departments", []string{"department.view"}},
		{"POST", adm + "/departments", []string{"department.create"}},
		{"PUT|PATCH", adm + "/departments/*", []string{"department.edit"}},
		{"DELETE", adm + "/departments/*", []string{"department.delete"}},

		// ---- Macros: anyone who may create one may save it; edits manage ------
		{"GET", adm + "/macros", []string{"macro.view"}},
		{"POST", adm + "/macros", []string{"macro.create"}},
		{"*", adm + "/macros/**", []string{"macro.manage"}},

		// ---- Custom objects: schemas manage, records data ----------------------
		{"GET", adm + "/custom-objects/*/records", []string{"custom_object.view"}},
		{"*", adm + "/custom-objects/*/records", []string{"custom_object.data"}},
		{"*", adm + "/custom-objects/*/records/**", []string{"custom_object.data"}},
	}
	entries = append(entries, resource("custom-objects", "custom_object")...)

	entries = append(entries,
		// ---- Users and roles ------------------------------------------------
		RouteEntry{"GET", adm + "/users", []string{"user.view"}},
		RouteEntry{"PATCH", adm + "/users/*/role", []string{"user.manage"}},
		RouteEntry{"PUT", adm + "/users/*/roles", []string{"role.manage"}},
		RouteEntry{"GET", adm + "/audit-log", []string{"audit.view"}},

		// ---- Plugins: the screens; plugin pages are the plugin's ---------------
		RouteEntry{"GET", adm + "/plugins", []string{"plugin.view"}},
		RouteEntry{"GET", adm + "/plugins/*/**", nil},
		RouteEntry{"*", adm + "/plugins", []string{"plugin.manage"}},
		RouteEntry{"*", adm + "/plugins/**", []string{"plugin.manage"}},
	)
	entries = append(entries, resource("roles", "role")...)
	for _, r := range []struct{ path, group string }{
		{"sla-policies", "sla"},
		{"business-hours", "sla"},
		{"escalation-rules", "escalation"},
		{"automations", "automation"},
		{"tags", "tag"},
		{"custom-fields", "custom_field"},
		{"statuses", "settings"},
		{"skills", "user"},
		{"capacity", "user"},
		{"api-tokens", "api_token"},
		{"canned-responses", "macro"},
	} {
		entries = append(entries, resource(r.path, r.group)...)
	}
	entries = append(entries, resource("workflows", "automation", "logs", "*/logs")...)
	entries = append(entries, resource("webhooks", "webhook", "*/deliveries")...)

	return append(entries,
		// ---- Settings ---------------------------------------------------------
		RouteEntry{"*", adm + "/import", []string{"settings.manage"}},
		RouteEntry{"*", adm + "/import/**", []string{"settings.manage"}},
		RouteEntry{"POST", adm + "/data-retention/purge", []string{"settings.manage"}},
		RouteEntry{"GET", adm + "/settings", []string{"settings.view"}},
		RouteEntry{"GET", adm + "/settings/**", []string{"settings.view"}},
		RouteEntry{"*", adm + "/settings", []string{"settings.manage"}},
		RouteEntry{"*", adm + "/settings/**", []string{"settings.manage"}},

		// ---- Newsletters (ADR 2026-05-19-newsletter-system) --------------------
		RouteEntry{"*", adm + "/newsletters", []string{"newsletters.manage"}},
		RouteEntry{"*", adm + "/newsletters/**", []string{"newsletters.manage"}},
	)
}()

// Governed reports whether a path under the route prefix is an admin, agent
// or staff API route, which roles & permissions guards. The auth, guest,
// mobile guest and public knowledge base API routes are not staff routes.
func Governed(path string) bool {
	path = normalize(path)
	if path == adm || strings.HasPrefix(path, adm+"/") || path == agt || strings.HasPrefix(path, agt+"/") {
		return true
	}
	if !strings.HasPrefix(path, "/api/") {
		return false
	}
	for _, public := range []string{"/api/auth/", "/api/guest/", "/api/mobile/", "/api/kb/"} {
		if strings.HasPrefix(path, public) {
			return false
		}
	}
	return true
}

// Mapped returns the slugs a route needs before looking at the request body,
// or nil and false when no entry matches (the caller refuses the request).
func Mapped(method, path string) ([]string, bool) {
	verb := strings.ToUpper(method)
	if verb == "HEAD" {
		verb = "GET"
	}
	route := normalize(path)
	for _, e := range RoutePermissions {
		if e.Method != "*" && !hasMethod(e.Method, verb) {
			continue
		}
		if compile(e.Pattern).MatchString(route) {
			return append([]string{}, e.Slugs...), true
		}
	}
	return nil, false
}

func hasMethod(list, verb string) bool {
	for _, m := range strings.Split(list, "|") {
		if m == verb {
			return true
		}
	}
	return false
}

// normalize collapses repeated slashes and drops a trailing one, except for
// the agent dashboard's "/agent/".
func normalize(path string) string {
	for strings.Contains(path, "//") {
		path = strings.ReplaceAll(path, "//", "/")
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if len(path) > 1 && strings.HasSuffix(path, "/") && path != agt+"/" {
		path = strings.TrimSuffix(path, "/")
	}
	return path
}

var compiled sync.Map

func compile(pattern string) *regexp.Regexp {
	if re, ok := compiled.Load(pattern); ok {
		return re.(*regexp.Regexp)
	}
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		switch {
		case c == '*' && i+1 < len(pattern) && pattern[i+1] == '*':
			b.WriteString(".+")
			i++
		case c == '*':
			b.WriteString("[^/]+")
		case c == '{':
			end := strings.IndexByte(pattern[i:], '}') + i
			parts := strings.Split(pattern[i+1:end], ",")
			for j, p := range parts {
				parts[j] = regexp.QuoteMeta(p)
			}
			b.WriteString("(?:" + strings.Join(parts, "|") + ")")
			i = end
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	re := regexp.MustCompile(b.String())
	compiled.Store(pattern, re)
	return re
}
