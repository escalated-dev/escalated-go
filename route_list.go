package escalated

import (
	"net/http"
	"strings"

	"github.com/escalated-dev/escalated-go/renderer"
)

// NamedRoute is one entry of the route list the shared pages call through
// Ziggy's route(name, params): the URI (no leading slash, {param}
// placeholders) and the HTTP methods it answers.
type NamedRoute struct {
	URI     string   `json:"uri"`
	Methods []string `json:"methods"`
}

var (
	routeGet     = []string{http.MethodGet, http.MethodHead}
	routePost    = []string{http.MethodPost}
	routePut     = []string{http.MethodPut, http.MethodPatch}
	routePatch   = []string{http.MethodPatch}
	routeDelete  = []string{http.MethodDelete}
	routeGetPost = []string{http.MethodGet, http.MethodHead, http.MethodPost}
)

// builtInRoutes are the names escalated-laravel gives the routes this package
// mounts, with the paths (relative to RoutePrefix) router.MountChi and
// router.MountStdlib serve them at. Parameter names follow Laravel's, so
// route(name, {param: value}) fills the same placeholders.
var builtInRoutes = map[string]NamedRoute{
	// Agent
	"escalated.agent.dashboard":          {"agent", routeGet},
	"escalated.agent.tickets.index":      {"agent/tickets", routeGet},
	"escalated.agent.tickets.bulk":       {"agent/tickets/bulk", routePost},
	"escalated.agent.tickets.show":       {"agent/tickets/{ticket}", routeGet},
	"escalated.agent.tickets.update":     {"agent/tickets/{ticket}", []string{http.MethodPut}},
	"escalated.agent.tickets.reply":      {"agent/tickets/{ticket}/reply", routePost},
	"escalated.agent.tickets.note":       {"agent/tickets/{ticket}/note", routePost},
	"escalated.agent.tickets.assign":     {"agent/tickets/{ticket}/assign", routePost},
	"escalated.agent.tickets.status":     {"agent/tickets/{ticket}/status", routePost},
	"escalated.agent.tickets.priority":   {"agent/tickets/{ticket}/priority", routePost},
	"escalated.agent.tickets.tags":       {"agent/tickets/{ticket}/tags", routePost},
	"escalated.agent.tickets.department": {"agent/tickets/{ticket}/department", routePost},
	"escalated.agent.tickets.macro":      {"agent/tickets/{ticket}/macro", routePost},
	"escalated.agent.tickets.follow":     {"agent/tickets/{ticket}/follow", routePost},
	"escalated.agent.tickets.pin":        {"agent/tickets/{ticket}/replies/{reply}/pin", routePost},
	// The live chat on the ticket screen: {session} is the chat session id.
	"escalated.admin.chat.end":      {"agent/chat/{session}/end", routePost},
	"escalated.admin.chat.transfer": {"agent/chat/{session}/transfer", routePost},

	// Admin tickets
	"escalated.admin.tickets.index":                    {"admin/tickets", routeGet},
	"escalated.admin.tickets.bulk":                     {"admin/tickets/bulk", routePost},
	"escalated.admin.tickets.merge-search":             {"admin/tickets/merge-search", routeGet},
	"escalated.admin.tickets.show":                     {"admin/tickets/{ticket}", routeGet},
	"escalated.admin.tickets.update":                   {"admin/tickets/{ticket}", []string{http.MethodPut}},
	"escalated.admin.tickets.reply":                    {"admin/tickets/{ticket}/reply", routePost},
	"escalated.admin.tickets.note":                     {"admin/tickets/{ticket}/note", routePost},
	"escalated.admin.tickets.assign":                   {"admin/tickets/{ticket}/assign", routePost},
	"escalated.admin.tickets.status":                   {"admin/tickets/{ticket}/status", routePost},
	"escalated.admin.tickets.priority":                 {"admin/tickets/{ticket}/priority", routePost},
	"escalated.admin.tickets.tags":                     {"admin/tickets/{ticket}/tags", routePost},
	"escalated.admin.tickets.department":               {"admin/tickets/{ticket}/department", routePost},
	"escalated.admin.tickets.macro":                    {"admin/tickets/{ticket}/macro", routePost},
	"escalated.admin.tickets.follow":                   {"admin/tickets/{ticket}/follow", routePost},
	"escalated.admin.tickets.pin":                      {"admin/tickets/{ticket}/replies/{reply}/pin", routePost},
	"escalated.admin.tickets.split":                    {"admin/tickets/{ticket}/split", routePost},
	"escalated.admin.tickets.merge":                    {"admin/tickets/{ticket}/merge", routePost},
	"escalated.admin.tickets.links.index":              {"admin/tickets/{ticket}/links", routeGet},
	"escalated.admin.tickets.links.store":              {"admin/tickets/{ticket}/links", routePost},
	"escalated.admin.tickets.links.destroy":            {"admin/tickets/{ticket}/links/{link}", routeDelete},
	"escalated.admin.tickets.side-conversations.index": {"admin/tickets/{ticket}/side-conversations", routeGet},
	"escalated.admin.tickets.side-conversations.store": {"admin/tickets/{ticket}/side-conversations", routePost},
	"escalated.admin.tickets.side-conversations.reply": {"admin/tickets/{ticket}/side-conversations/{sideConversation}/reply", routePost},
	"escalated.admin.tickets.side-conversations.close": {"admin/tickets/{ticket}/side-conversations/{sideConversation}/close", routePost},

	// Admin resources
	"escalated.admin.departments.index":   {"admin/departments", routeGet},
	"escalated.admin.departments.create":  {"admin/departments/create", routeGet},
	"escalated.admin.departments.store":   {"admin/departments", routePost},
	"escalated.admin.departments.edit":    {"admin/departments/{department}/edit", routeGet},
	"escalated.admin.departments.update":  {"admin/departments/{department}", routePut},
	"escalated.admin.departments.destroy": {"admin/departments/{department}", routeDelete},

	"escalated.admin.sla-policies.index":   {"admin/sla-policies", routeGet},
	"escalated.admin.sla-policies.create":  {"admin/sla-policies/create", routeGet},
	"escalated.admin.sla-policies.store":   {"admin/sla-policies", routePost},
	"escalated.admin.sla-policies.edit":    {"admin/sla-policies/{sla_policy}/edit", routeGet},
	"escalated.admin.sla-policies.update":  {"admin/sla-policies/{sla_policy}", routePut},
	"escalated.admin.sla-policies.destroy": {"admin/sla-policies/{sla_policy}", routeDelete},

	"escalated.admin.business-hours.index":   {"admin/business-hours", routeGet},
	"escalated.admin.business-hours.create":  {"admin/business-hours/create", routeGet},
	"escalated.admin.business-hours.store":   {"admin/business-hours", routePost},
	"escalated.admin.business-hours.edit":    {"admin/business-hours/{business_hour}/edit", routeGet},
	"escalated.admin.business-hours.update":  {"admin/business-hours/{business_hour}", routePut},
	"escalated.admin.business-hours.destroy": {"admin/business-hours/{business_hour}", routeDelete},

	"escalated.admin.custom-fields.index":   {"admin/custom-fields", routeGet},
	"escalated.admin.custom-fields.create":  {"admin/custom-fields/create", routeGet},
	"escalated.admin.custom-fields.store":   {"admin/custom-fields", routePost},
	"escalated.admin.custom-fields.reorder": {"admin/custom-fields/reorder", routePost},
	"escalated.admin.custom-fields.edit":    {"admin/custom-fields/{custom_field}/edit", routeGet},
	"escalated.admin.custom-fields.update":  {"admin/custom-fields/{custom_field}", routePut},
	"escalated.admin.custom-fields.destroy": {"admin/custom-fields/{custom_field}", routeDelete},

	"escalated.admin.custom-objects.index":           {"admin/custom-objects", routeGet},
	"escalated.admin.custom-objects.create":          {"admin/custom-objects/create", routeGet},
	"escalated.admin.custom-objects.store":           {"admin/custom-objects", routePost},
	"escalated.admin.custom-objects.edit":            {"admin/custom-objects/{custom_object}/edit", routeGet},
	"escalated.admin.custom-objects.update":          {"admin/custom-objects/{custom_object}", routePut},
	"escalated.admin.custom-objects.destroy":         {"admin/custom-objects/{custom_object}", routeDelete},
	"escalated.admin.custom-objects.records":         {"admin/custom-objects/{customObject}/records", routeGet},
	"escalated.admin.custom-objects.records.store":   {"admin/custom-objects/{customObject}/records", routePost},
	"escalated.admin.custom-objects.records.update":  {"admin/custom-objects/{customObject}/records/{record}", routePut},
	"escalated.admin.custom-objects.records.destroy": {"admin/custom-objects/{customObject}/records/{record}", routeDelete},

	"escalated.admin.statuses.index":   {"admin/statuses", routeGet},
	"escalated.admin.statuses.create":  {"admin/statuses/create", routeGet},
	"escalated.admin.statuses.store":   {"admin/statuses", routePost},
	"escalated.admin.statuses.edit":    {"admin/statuses/{status}/edit", routeGet},
	"escalated.admin.statuses.update":  {"admin/statuses/{status}", routePut},
	"escalated.admin.statuses.destroy": {"admin/statuses/{status}", routeDelete},

	"escalated.admin.escalation-rules.index":   {"admin/escalation-rules", routeGet},
	"escalated.admin.escalation-rules.create":  {"admin/escalation-rules/create", routeGet},
	"escalated.admin.escalation-rules.store":   {"admin/escalation-rules", routePost},
	"escalated.admin.escalation-rules.edit":    {"admin/escalation-rules/{escalation_rule}/edit", routeGet},
	"escalated.admin.escalation-rules.update":  {"admin/escalation-rules/{escalation_rule}", routePut},
	"escalated.admin.escalation-rules.destroy": {"admin/escalation-rules/{escalation_rule}", routeDelete},

	"escalated.admin.automations.index":   {"admin/automations", routeGet},
	"escalated.admin.automations.create":  {"admin/automations/create", routeGet},
	"escalated.admin.automations.store":   {"admin/automations", routePost},
	"escalated.admin.automations.edit":    {"admin/automations/{automation}/edit", routeGet},
	"escalated.admin.automations.update":  {"admin/automations/{automation}", routePut},
	"escalated.admin.automations.destroy": {"admin/automations/{automation}", routeDelete},

	"escalated.admin.webhooks.index":      {"admin/webhooks", routeGet},
	"escalated.admin.webhooks.create":     {"admin/webhooks/create", routeGet},
	"escalated.admin.webhooks.store":      {"admin/webhooks", routePost},
	"escalated.admin.webhooks.edit":       {"admin/webhooks/{webhook}/edit", routeGet},
	"escalated.admin.webhooks.update":     {"admin/webhooks/{webhook}", routePut},
	"escalated.admin.webhooks.destroy":    {"admin/webhooks/{webhook}", routeDelete},
	"escalated.admin.webhooks.deliveries": {"admin/webhooks/{webhook}/deliveries", routeGet},
	"escalated.admin.webhooks.retry":      {"admin/webhooks/deliveries/{delivery}/retry", routePost},

	"escalated.admin.skills.index":   {"admin/skills", routeGet},
	"escalated.admin.skills.create":  {"admin/skills/new", routeGet},
	"escalated.admin.skills.store":   {"admin/skills", routePost},
	"escalated.admin.skills.edit":    {"admin/skills/{skill}/edit", routeGet},
	"escalated.admin.skills.update":  {"admin/skills/{skill}", routePut},
	"escalated.admin.skills.destroy": {"admin/skills/{skill}", routeDelete},

	// Roles: 404 while roles & permissions is off.
	"escalated.admin.roles.index":   {"admin/roles", routeGet},
	"escalated.admin.roles.create":  {"admin/roles/create", routeGet},
	"escalated.admin.roles.store":   {"admin/roles", routePost},
	"escalated.admin.roles.edit":    {"admin/roles/{role}/edit", routeGet},
	"escalated.admin.roles.update":  {"admin/roles/{role}", routePut},
	"escalated.admin.roles.destroy": {"admin/roles/{role}", routeDelete},

	"escalated.admin.kb-articles.index":     {"admin/kb-articles", routeGet},
	"escalated.admin.kb-articles.create":    {"admin/kb-articles/create", routeGet},
	"escalated.admin.kb-articles.store":     {"admin/kb-articles", routePost},
	"escalated.admin.kb-articles.edit":      {"admin/kb-articles/{kb_article}/edit", routeGet},
	"escalated.admin.kb-articles.update":    {"admin/kb-articles/{kb_article}", routePut},
	"escalated.admin.kb-articles.destroy":   {"admin/kb-articles/{kb_article}", routeDelete},
	"escalated.admin.kb-categories.index":   {"admin/kb-categories", routeGet},
	"escalated.admin.kb-categories.store":   {"admin/kb-categories", routePost},
	"escalated.admin.kb-categories.update":  {"admin/kb-categories/{kb_category}", routePut},
	"escalated.admin.kb-categories.destroy": {"admin/kb-categories/{kb_category}", routeDelete},

	"escalated.admin.tags.index":   {"admin/tags", routeGet},
	"escalated.admin.tags.store":   {"admin/tags", routePost},
	"escalated.admin.tags.update":  {"admin/tags/{tag}", routePut},
	"escalated.admin.tags.destroy": {"admin/tags/{tag}", routeDelete},

	"escalated.admin.macros.index":   {"admin/macros", routeGet},
	"escalated.admin.macros.store":   {"admin/macros", routePost},
	"escalated.admin.macros.update":  {"admin/macros/{macro}", routePut},
	"escalated.admin.macros.destroy": {"admin/macros/{macro}", routeDelete},

	"escalated.admin.canned-responses.index":   {"admin/canned-responses", routeGet},
	"escalated.admin.canned-responses.store":   {"admin/canned-responses", routePost},
	"escalated.admin.canned-responses.update":  {"admin/canned-responses/{cannedResponse}", routePut},
	"escalated.admin.canned-responses.destroy": {"admin/canned-responses/{cannedResponse}", routeDelete},

	"escalated.admin.capacity.index":  {"admin/capacity", routeGet},
	"escalated.admin.capacity.update": {"admin/capacity/{capacity}", routePut},

	"escalated.admin.workflows.index":         {"admin/workflows", routeGet},
	"escalated.admin.workflows.create":        {"admin/workflows/create", routeGet},
	"escalated.admin.workflows.store":         {"admin/workflows", routePost},
	"escalated.admin.workflows.logs":          {"admin/workflows/logs", routeGet},
	"escalated.admin.workflows.reorder":       {"admin/workflows/reorder", routePost},
	"escalated.admin.workflows.edit":          {"admin/workflows/{workflow}", routeGet},
	"escalated.admin.workflows.update":        {"admin/workflows/{workflow}", routePut},
	"escalated.admin.workflows.destroy":       {"admin/workflows/{workflow}", routeDelete},
	"escalated.admin.workflows.toggle":        {"admin/workflows/{workflow}/toggle", routePost},
	"escalated.admin.workflows.workflow-logs": {"admin/workflows/{workflow}/logs", routeGet},

	"escalated.admin.users.index": {"admin/users", routeGet},
	"escalated.admin.users.role":  {"admin/users/{user}/role", routePatch},
	"escalated.admin.users.roles": {"admin/users/{user}/roles", []string{http.MethodPut}},
	"escalated.admin.audit-log":   {"admin/audit-log", routeGet},

	"escalated.admin.api-tokens.index":   {"admin/api-tokens", routeGet},
	"escalated.admin.api-tokens.store":   {"admin/api-tokens", routePost},
	"escalated.admin.api-tokens.update":  {"admin/api-tokens/{token}", routePut},
	"escalated.admin.api-tokens.destroy": {"admin/api-tokens/{token}", routeDelete},

	// Import wizard
	"escalated.admin.import.index":           {"admin/import", routeGet},
	"escalated.admin.import.connect":         {"admin/import/{platform}/connect", routeGet},
	"escalated.admin.import.test-connection": {"admin/import/{platform}/test-connection", routePost},
	"escalated.admin.import.mapping":         {"admin/import/{platform}/mapping", routeGet},
	"escalated.admin.import.review":          {"admin/import/{platform}/review", routePost},
	"escalated.admin.import.start":           {"admin/import/{platform}/start", routePost},
	"escalated.admin.import.progress":        {"admin/import/progress/{jobId}", routeGet},
	"escalated.admin.import.status":          {"admin/import/status/{jobId}", routeGet},
	"escalated.admin.import.pause":           {"admin/import/pause/{jobId}", routePost},
	"escalated.admin.import.resume":          {"admin/import/resume/{jobId}", routePost},
	"escalated.admin.import.cancel":          {"admin/import/cancel/{jobId}", routePost},

	// Plugins
	"escalated.admin.plugins.index":      {"admin/plugins", routeGet},
	"escalated.admin.plugins.activate":   {"admin/plugins/{slug}/activate", routePost},
	"escalated.admin.plugins.deactivate": {"admin/plugins/{slug}/deactivate", routePost},
	"escalated.admin.plugins.destroy":    {"admin/plugins/{slug}", routeDelete},

	// Reports
	"escalated.admin.reports":                  {"admin/reports", routeGet},
	"escalated.admin.reports.dashboard":        {"admin/reports/dashboard", routeGet},
	"escalated.admin.reports.agents":           {"admin/reports/agents", routeGet},
	"escalated.admin.reports.sla":              {"admin/reports/sla", routeGet},
	"escalated.admin.reports.csat":             {"admin/reports/csat", routeGet},
	"escalated.admin.reports.sla-trends":       {"admin/reports/sla-trends", routeGet},
	"escalated.admin.reports.frt":              {"admin/reports/frt", routeGet},
	"escalated.admin.reports.resolution":       {"admin/reports/resolution", routeGet},
	"escalated.admin.reports.response-times":   {"admin/reports/response-times", routeGet},
	"escalated.admin.reports.resolution-times": {"admin/reports/resolution-times", routeGet},
	"escalated.admin.reports.agent-ranking":    {"admin/reports/agent-ranking", routeGet},
	"escalated.admin.reports.agent-detail":     {"admin/reports/agent/{id}/detail", routeGet},
	"escalated.admin.reports.cohorts":          {"admin/reports/cohorts", routeGet},
	"escalated.admin.reports.comparison":       {"admin/reports/comparison", routeGet},

	// Settings
	"escalated.admin.settings":                       {"admin/settings", routeGet},
	"escalated.admin.settings.update":                {"admin/settings", routePost},
	"escalated.admin.settings.csat":                  {"admin/settings/csat", routeGet},
	"escalated.admin.settings.csat.update":           {"admin/settings/csat", routePost},
	"escalated.admin.settings.data-retention":        {"admin/settings/data-retention", routeGet},
	"escalated.admin.settings.data-retention.update": {"admin/settings/data-retention", routePost},
	"escalated.admin.settings.database":              {"admin/settings/database", routeGet},
	"escalated.admin.settings.database.update":       {"admin/settings/database", routePost},
	"escalated.admin.settings.database.test":         {"admin/settings/database/test", routePost},
	"escalated.admin.settings.email":                 {"admin/settings/email", routeGet},
	"escalated.admin.settings.email.update":          {"admin/settings/email", routePost},
	"escalated.admin.settings.public-tickets":        {"admin/settings/public-tickets", routeGet},
	"escalated.admin.settings.public-tickets.update": {"admin/settings/public-tickets", routePost},
	// SSO settings and sign-in: 404 while SSO is off.
	"escalated.admin.settings.sso":        {"admin/settings/sso", routeGet},
	"escalated.admin.settings.sso.update": {"admin/settings/sso", routePost},
	"escalated.sso.login":                 {"sso/login", routeGet},
	"escalated.sso.saml.metadata":         {"sso/saml/metadata", routeGet},
	"escalated.sso.saml.acs":              {"sso/saml/acs", routePost},
	"escalated.sso.oauth.callback":        {"sso/oauth/callback", routeGet},
	"escalated.sso.jwt.callback":          {"sso/jwt/callback", routeGetPost},
	// Two-factor: 404 while two-factor is off.
	"escalated.admin.two-factor.index":   {"admin/settings/two-factor", routeGet},
	"escalated.admin.two-factor.setup":   {"admin/settings/two-factor/setup", routePost},
	"escalated.admin.two-factor.confirm": {"admin/settings/two-factor/confirm", routePost},
	"escalated.admin.two-factor.disable": {"admin/settings/two-factor/disable", routePost},
	"escalated.two-factor.challenge":     {"two-factor/challenge", routeGet},
	"escalated.two-factor.verify":        {"two-factor/challenge", routePost},

	// Customer
	"escalated.customer.tickets.index":  {"tickets", routeGet},
	"escalated.customer.tickets.create": {"tickets/create", routeGet},
	"escalated.customer.tickets.store":  {"tickets", routePost},
	"escalated.customer.tickets.show":   {"tickets/{ticket}", routeGet},
	"escalated.customer.tickets.reply":  {"tickets/{ticket}/reply", routePost},
	"escalated.customer.tickets.close":  {"tickets/{ticket}/close", routePost},
	"escalated.customer.tickets.reopen": {"tickets/{ticket}/reopen", routePost},
	"escalated.customer.tickets.rate":   {"tickets/{ticket}/rate", routePost},
	"escalated.customer.kb.index":       {"kb", routeGet},
	"escalated.customer.kb.show":        {"kb/{slug}", routeGet},
	"escalated.customer.kb.feedback":    {"kb/{slug}/feedback", routePost},

	// Guest
	"escalated.guest.tickets.create": {"guest/create", routeGet},
	"escalated.guest.tickets.store":  {"guest", routePost},
	"escalated.guest.tickets.show":   {"guest/{token}", routeGet},
	"escalated.guest.tickets.reply":  {"guest/{token}/reply", routePost},
	"escalated.guest.tickets.rate":   {"guest/{token}/rate", routePost},
	"escalated.guest.verification":   {"api/guest/verification", routePost},
	"escalated.guest.lookup":         {"api/guest/lookup", routePost},

	"escalated.attachments.download": {"attachments/{attachment}/download", routeGet},
}

// BuiltInRoutes is the route list under prefix (Config.RoutePrefix): every
// route name the shared pages call that this package serves, with its URI
// in Ziggy's shape. Names the pages call that are missing have no route
// here; route() answers "#" for them.
func BuiltInRoutes(prefix string) map[string]NamedRoute {
	base := strings.Trim(prefix, "/")
	out := make(map[string]NamedRoute, len(builtInRoutes))
	for name, route := range builtInRoutes {
		uri := route.URI
		if base != "" {
			uri = base + "/" + uri
		}
		out[name] = NamedRoute{URI: uri, Methods: append([]string(nil), route.Methods...)}
	}
	return out
}

// RouteList is BuiltInRoutes under Config.RoutePrefix with
// Config.RouteOverrides applied: an override replaces or adds a name, and
// one with an empty URI removes it.
func (e *Escalated) RouteList() map[string]NamedRoute {
	routes := BuiltInRoutes(e.Config.RoutePrefix)
	for name, route := range e.Config.RouteOverrides {
		if route.URI == "" {
			delete(routes, name)
			continue
		}
		methods := route.Methods
		if len(methods) == 0 {
			methods = routeGet
		}
		routes[name] = NamedRoute{URI: strings.TrimLeft(route.URI, "/"), Methods: append([]string(nil), methods...)}
	}
	return routes
}

// RouteListMiddleware shares the `ziggy` prop on every Escalated page: the
// route list (RouteList) in the shape ziggy-js reads, {url, port, defaults,
// routes, location}. A ziggy prop the host shares first (renderer.WithShared
// in middleware that runs before this one) or a page sets wins, name by
// name. router.MountChi and router.MountStdlib install it.
func (e *Escalated) RouteListMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := renderer.WithShared(r.Context(), "ziggy", func() any {
			routes := map[string]any{}
			for name, route := range e.RouteList() {
				methods := make([]any, len(route.Methods))
				for i, m := range route.Methods {
					methods[i] = m
				}
				routes[name] = map[string]any{"uri": route.URI, "methods": methods}
			}
			origin := requestOrigin(r)
			return map[string]any{
				"url":      origin,
				"port":     nil,
				"defaults": map[string]any{},
				"routes":   routes,
				"location": origin + r.URL.RequestURI(),
			}
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requestOrigin is the scheme and host the request came in on.
func requestOrigin(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}
