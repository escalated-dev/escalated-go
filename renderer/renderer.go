// Package renderer provides the Renderer interface and implementations for
// rendering responses as Inertia pages or plain JSON.
package renderer

import "net/http"

// Renderer is the interface for rendering responses to the client.
// Implementations decide whether to return an Inertia page or raw JSON.
type Renderer interface {
	// Render writes a response. For Inertia, component is the Vue/React page name;
	// for JSON, it is ignored and props are serialized directly.
	Render(w http.ResponseWriter, r *http.Request, component string, props map[string]any) error
}
