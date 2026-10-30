package renderer

import (
	"net/http"

	inertia "github.com/petaki/inertia-go"
)

// InertiaRenderer renders responses using the Inertia.js protocol.
// It wraps petaki/inertia-go to implement the Renderer interface.
type InertiaRenderer struct {
	engine *inertia.Inertia
}

// NewInertiaRenderer creates an InertiaRenderer.
// rootTemplate is the path to the root HTML template (e.g., "resources/views/app.html").
// The template can install Escalated's route() helper with
// <script>{{ .escalatedRouteHelper }}</script> (see RouteHelperScript).
func NewInertiaRenderer(rootTemplate string) *InertiaRenderer {
	if rootTemplate == "" {
		rootTemplate = "resources/views/app.html"
	}
	engine := inertia.New("", rootTemplate, "")
	// The root template installs route() with {{ .escalatedRouteHelper }}.
	engine.ShareViewData(RouteHelperViewData, routeHelperJS)
	return &InertiaRenderer{engine: engine}
}

// Render writes an Inertia page response.
func (ir *InertiaRenderer) Render(w http.ResponseWriter, r *http.Request, component string, props map[string]any) error {
	return ir.engine.Render(w, r, component, withSharedProps(r.Context(), props))
}
