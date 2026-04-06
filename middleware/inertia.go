package middleware

import (
	"net/http"
	"strings"
)

// Inertia returns middleware that handles Inertia.js protocol headers.
// It sets the Vary header and handles version conflicts with 409 responses.
func Inertia(assetVersion string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Always set Vary header for Inertia requests
			w.Header().Set("Vary", "X-Inertia")

			if r.Header.Get("X-Inertia") == "" {
				next.ServeHTTP(w, r)
				return
			}

			// Check asset version for GET requests
			if r.Method == http.MethodGet && assetVersion != "" {
				clientVersion := r.Header.Get("X-Inertia-Version")
				if clientVersion != "" && clientVersion != assetVersion {
					w.Header().Set("X-Inertia-Location", r.URL.String())
					w.WriteHeader(http.StatusConflict)
					return
				}
			}

			// For non-GET Inertia requests that would normally redirect,
			// convert 302 to 303 so the browser issues a GET
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				w = &inertiaResponseWriter{ResponseWriter: w, method: r.Method}
			}

			next.ServeHTTP(w, r)
		})
	}
}

type inertiaResponseWriter struct {
	http.ResponseWriter
	method string
}

func (w *inertiaResponseWriter) WriteHeader(code int) {
	if code == http.StatusFound && !strings.EqualFold(w.method, http.MethodGet) {
		code = http.StatusSeeOther
	}
	w.ResponseWriter.WriteHeader(code)
}
