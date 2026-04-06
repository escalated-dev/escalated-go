package renderer

import (
	"encoding/json"
	"net/http"
)

// JSONRenderer renders responses as plain JSON.
// Use this when UIEnabled is false (headless / API-only mode).
type JSONRenderer struct{}

// NewJSONRenderer creates a new JSONRenderer.
func NewJSONRenderer() *JSONRenderer {
	return &JSONRenderer{}
}

// Render writes props as a JSON response.
func (j *JSONRenderer) Render(w http.ResponseWriter, _ *http.Request, _ string, props map[string]any) error {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	return json.NewEncoder(w).Encode(props)
}
