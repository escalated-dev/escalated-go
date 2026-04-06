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
func NewInertiaRenderer(rootTemplate string) *InertiaRenderer {
	if rootTemplate == "" {
		rootTemplate = "resources/views/app.html"
	}
	engine := inertia.New("", rootTemplate, "")
	return &InertiaRenderer{engine: engine}
}

// Render writes an Inertia page response.
func (ir *InertiaRenderer) Render(w http.ResponseWriter, r *http.Request, component string, props map[string]any) error {
	return ir.engine.Render(w, r, component, props)
}
