package handlers

import (
	"net/http"
	"strings"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/renderer"
)

// PageSupport lets an admin handler that answers JSON also serve its shared
// page. The router sets it when the UI is enabled; the zero value keeps the
// handler JSON-only.
//
// A GET renders the page for a page visit (X-Inertia) or a browser asking for
// HTML, and answers JSON otherwise, so API clients see no change. A write
// answers a page visit the Inertia way (a 303, or the page again with errors)
// and everyone else as before.
type PageSupport struct {
	Renderer    renderer.Renderer
	RoutePrefix string
	UserID      func(r *http.Request) models.UserID
	Users       UserLookup
}

// wantsPage reports whether a GET should render the page.
func (p PageSupport) wantsPage(r *http.Request) bool {
	if p.Renderer == nil {
		return false
	}
	return isInertia(r) || strings.Contains(r.Header.Get("Accept"), "text/html")
}

// path is a path under the route prefix.
func (p PageSupport) path(suffix string) string {
	prefix := p.RoutePrefix
	if prefix == "" {
		prefix = "/escalated"
	}
	return prefix + suffix
}

// render renders a page, merging errs as its errors prop.
func (p PageSupport) render(w http.ResponseWriter, r *http.Request, component string, props map[string]any, errs map[string]string) {
	if errs != nil {
		props["errors"] = pageErrors(errs)
	}
	_ = p.Renderer.Render(w, r, component, props)
}

// succeed ends a successful write: a 303 to location for a page visit, the
// JSON body otherwise (no body for 204).
func (p PageSupport) succeed(w http.ResponseWriter, r *http.Request, location string, code int, body any) {
	if p.Renderer != nil && isInertia(r) {
		seeOther(w, r, location)
		return
	}
	if code == http.StatusNoContent {
		w.WriteHeader(code)
		return
	}
	writeJSON(w, code, body)
}

// fail ends a write that failed validation: the page again with errs for a
// page visit (when rerender is given), else the plain-text error API clients
// have always had.
func (p PageSupport) fail(w http.ResponseWriter, r *http.Request, code int, msg string, errs map[string]string, rerender func(errs map[string]string)) {
	if p.Renderer != nil && isInertia(r) && rerender != nil {
		rerender(errs)
		return
	}
	http.Error(w, msg, code)
}

// creator is a record's creator as the admin lists show it: {id, name}.
func (p PageSupport) creator(r *http.Request, id *models.UserID) any {
	if id == nil || id.Empty() {
		return nil
	}
	name := p.Users.name(r.Context(), *id)
	if name == "" {
		name = "Support"
	}
	return map[string]any{"id": *id, "name": name}
}

// currentUser is the signed-in user: the PageSupport resolver when set, else
// the context value the host's auth middleware may have stored.
func (p PageSupport) currentUser(r *http.Request) models.UserID {
	if p.UserID != nil {
		if uid := p.UserID(r); !uid.Empty() {
			return uid
		}
	}
	return currentAgentID(r)
}
