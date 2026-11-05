package renderer

import (
	"context"
	"net/http"
	"sync"

	inertia "github.com/petaki/inertia-go"
)

// InertiaRenderer renders responses using the Inertia.js protocol.
// It wraps petaki/inertia-go to implement the Renderer interface.
type InertiaRenderer struct {
	engine *inertia.Inertia

	mu    sync.Mutex
	roots map[string]*inertia.Inertia
}

// NewInertiaRenderer creates an InertiaRenderer.
// rootTemplate is the path to the root HTML template (e.g., "resources/views/app.html").
// The template can install Escalated's route() helper with
// <script>{{ .escalatedRouteHelper }}</script> (see RouteHelperScript).
func NewInertiaRenderer(rootTemplate string) *InertiaRenderer {
	if rootTemplate == "" {
		rootTemplate = "resources/views/app.html"
	}
	return &InertiaRenderer{engine: newEngine(rootTemplate)}
}

func newEngine(rootTemplate string) *inertia.Inertia {
	engine := inertia.New("", rootTemplate, "")
	// The root template installs route() with {{ .escalatedRouteHelper }}.
	engine.ShareViewData(RouteHelperViewData, routeHelperJS)
	return engine
}

// Render writes an Inertia page response.
func (ir *InertiaRenderer) Render(w http.ResponseWriter, r *http.Request, component string, props map[string]any) error {
	engine := ir.engine
	if root, _ := r.Context().Value(rootViewKey{}).(string); root != "" {
		engine = ir.root(root)
	}
	if r.Header.Get("X-Inertia") == "" {
		if data, _ := r.Context().Value(viewDataKey{}).([]viewData); len(data) > 0 {
			ctx := r.Context()
			for _, d := range data {
				ctx = engine.WithViewData(ctx, d.key, d.value())
			}
			r = r.WithContext(ctx)
		}
	}
	return engine.Render(w, r, component, withSharedProps(r.Context(), props))
}

// root is the engine for another root template, parsed once.
func (ir *InertiaRenderer) root(path string) *inertia.Inertia {
	ir.mu.Lock()
	defer ir.mu.Unlock()
	if ir.roots == nil {
		ir.roots = map[string]*inertia.Inertia{}
	}
	engine, ok := ir.roots[path]
	if !ok {
		engine = newEngine(path)
		ir.roots[path] = engine
	}
	return engine
}

type rootViewKey struct{}

// WithRootView makes InertiaRenderer render full page loads for the request
// with the root template at path instead of its own: the embedded admin's
// root view (Config.UI.Embed.RootView).
func WithRootView(ctx context.Context, path string) context.Context {
	return context.WithValue(ctx, rootViewKey{}, path)
}

type viewDataKey struct{}

type viewData struct {
	key   string
	value func() any
}

// WithViewData adds root template data for a full page load of the request,
// computed only when such a page is rendered.
func WithViewData(ctx context.Context, key string, value func() any) context.Context {
	existing, _ := ctx.Value(viewDataKey{}).([]viewData)
	next := append(append([]viewData{}, existing...), viewData{key: key, value: value})
	return context.WithValue(ctx, viewDataKey{}, next)
}
