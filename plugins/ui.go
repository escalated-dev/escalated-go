package plugins

import (
	"net/http"

	"github.com/escalated-dev/escalated-go/renderer"
)

// UIExtensions is escalated.plugins for one request: active plugins' menu
// items, page components and dashboard widgets, without the items whose
// capability the viewer lacks and without admin menu items for anyone but an
// admin. Empty ({}) when no plugin is active. plugin is the slug: the frontend
// loads a plugin's components from a directory named after it.
func (h *Host) UIExtensions(r *http.Request) map[string]any {
	if h == nil || !h.hasPlugins() {
		return map[string]any{}
	}
	active, err := h.activeEntries(r.Context())
	if err != nil {
		h.logf("UI extensions: %v", err)
		return map[string]any{}
	}
	if len(active) == 0 {
		return map[string]any{}
	}
	admin := h.adminCheck(r)
	menuItems := []map[string]any{}
	pageComponents := map[string][]map[string]any{}
	dashboardWidgets := []map[string]any{}
	prefix := h.routePrefix
	if prefix == "" {
		prefix = "/escalated"
	}
	for _, e := range active {
		for _, p := range e.manifest.Pages {
			route := pageRoute(p)
			if p.Menu == nil || route == "" || p.Component == "" {
				continue
			}
			section := p.Menu.Section
			if section == "" {
				section = "admin"
			}
			if section == "admin" && !admin {
				continue
			}
			if !h.allows(r, p.Capability) {
				continue
			}
			var icon any
			if p.Menu.Icon != "" {
				icon = p.Menu.Icon
			}
			menuItems = append(menuItems, map[string]any{
				"label":    p.Menu.Label,
				"route":    prefix + "/admin/plugins/" + e.slug + "/" + route,
				"icon":     icon,
				"position": orDefault(p.Menu.Position, 100),
				"section":  section,
				"plugin":   e.slug,
			})
		}
		for _, c := range e.manifest.Components {
			if c.Page == "" || c.Slot == "" || c.Component == "" || !h.allows(r, c.Capability) {
				continue
			}
			props := c.Props
			if props == nil {
				props = map[string]any{}
			}
			key := c.Page + "." + c.Slot
			pageComponents[key] = append(pageComponents[key], map[string]any{
				"plugin":    e.slug,
				"component": c.Component,
				"data":      props,
				"position":  orDefault(c.Order, 100),
			})
		}
		for _, w := range e.manifest.Widgets {
			if w.Component == "" || !h.allows(r, w.Capability) {
				continue
			}
			size := w.Size
			if size == "" {
				size = "half"
			}
			dashboardWidgets = append(dashboardWidgets, map[string]any{
				"id":        e.slug + "." + w.Component,
				"title":     w.Label,
				"component": w.Component,
				"plugin":    e.slug,
				"position":  orDefault(w.Order, 100),
				"width":     size,
			})
		}
	}
	return map[string]any{"menuItems": menuItems, "pageComponents": pageComponents, "dashboardWidgets": dashboardWidgets}
}

func orDefault(v, def int) int {
	if v == 0 {
		return def
	}
	return v
}

// ShareMiddleware shares escalated.plugins with every page rendered for the
// request. It is computed only when a page is rendered.
func (h *Host) ShareMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		shared := renderer.WithShared(r.Context(), "escalated", func() any {
			return map[string]any{"plugins": h.UIExtensions(r)}
		})
		next.ServeHTTP(w, r.WithContext(shared))
	})
}
